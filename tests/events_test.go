package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/webhook"
)

const eventsSchema = `
types:
  article:
    fields:
      title: { type: string, required: true }
  settings:
    singleton: true
    fields:
      title: { type: string }
`

type fnMod struct {
	name string
	reg  func(r *kernel.Registry) error
}

func (m fnMod) Name() string                      { return m.name }
func (m fnMod) Register(r *kernel.Registry) error { return m.reg(r) }

func newEventsApp(t *testing.T, db store.DB, clk clock.Clock, appYAML string, jobHold time.Duration, mods ...kernel.Module) *kernel.App {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"schema/s.yaml":   eventsSchema,
		"config/app.yaml": "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n" + appYAML,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	mods = append([]kernel.Module{sqlite.Module(), postgres.Module()}, mods...)
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard, JobHold: jobHold, PasswordIterations: 1000}, mods...)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Services().Content.EnsureSchemaRev(context.Background()); err != nil {
		t.Fatal(err)
	}
	return app
}

// purges collects hydrogen_purge payloads.
func purges(t *testing.T, db store.DB) func() []string {
	t.Helper()
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := db.Notifier().Listen(lctx, cachetag.PurgeChannel)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	go func() {
		for n := range ch {
			if n.Payload == "" {
				continue
			}
			mu.Lock()
			got = append(got, n.Payload)
			mu.Unlock()
		}
	}()
	return func() []string {
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
}

func createPublished(t *testing.T, app *kernel.App, clk *clock.Manual, typ string) string {
	t.Helper()
	clk.Advance(time.Second)
	v, err := app.Services().Content.Create(ctx, content.CreateInput{Type: typ, Locale: "en", Fields: doc(t, `{"title":"Hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Second)
	if _, err := app.Services().Content.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	return v.ID.String()
}

func TestPurgeBroadcast(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		nav := fnMod{"navmod", func(r *kernel.Registry) error {
			r.TagMapper(func(topic string, _ json.RawMessage) []string {
				switch topic {
				case "item.published":
					return []string{cachetag.Nav("main"), "i:dup"}
				case "x.big":
					out := make([]string, 0, 800)
					for i := range 800 {
						out = append(out, fmt.Sprintf("t:%s%05d", strings.Repeat("z", 20), i))
					}
					return out
				case "x.all":
					return []string{cachetag.All}
				}
				return nil
			})
			return nil
		}}
		app := newEventsApp(t, db, clk, "", 0, nav)
		got := purges(t, db)

		a := createPublished(t, app, clk, "article")
		s := createPublished(t, app, clk, "settings")
		if _, err := app.Services().Outbox.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		all := strings.Fields(strings.Join(got(), " "))
		for _, want := range []string{"i:" + a, "c:article", "i:" + s, "s:settings", "nav:main"} {
			if !slices.Contains(all, want) {
				t.Errorf("missing tag %s in %v", want, all)
			}
		}
		if slices.Contains(all, "c:settings") {
			t.Error("singleton tagged as collection")
		}

		publish := func(topic string) {
			err := db.Tx(ctx, func(tx store.Tx) error {
				_, err := app.Services().Outbox.Publish(ctx, tx, topic, map[string]string{})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.Services().Outbox.Drain(ctx); err != nil {
				t.Fatal(err)
			}
		}
		publish("x.big")
		chunks := got()
		n := 0
		for _, c := range chunks {
			if len(c) >= 7168 {
				t.Errorf("chunk of %d bytes", len(c))
			}
			n += len(strings.Fields(c))
		}
		if len(chunks) < 2 || n != 800 {
			t.Errorf("%d chunks, %d tags", len(chunks), n)
		}
		publish("x.all")
		if c := got(); len(c) != 1 || c[0] != "*" {
			t.Errorf("all = %v", c)
		}
		publish("unmapped.topic")
		if c := got(); len(c) != 0 {
			t.Errorf("unmapped purge = %v", c)
		}
	})
}

func TestWebhookDelivery(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		type hit struct {
			hdr  http.Header
			body []byte
		}
		var mu sync.Mutex
		var hits []hit
		fail := true
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			defer mu.Unlock()
			hits = append(hits, hit{r.Header.Clone(), body})
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()
		other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("webhook for other topics was called")
		}))
		defer other.Close()
		app := newEventsApp(t, db, clk, `
webhooks:
  revalidate:
    url: `+srv.URL+`/hook
    events: [item.published]
    secret: ${env:WH_SECRET:-whsecret}
  unrelated:
    url: `+other.URL+`
    events: [page.*]
    secret: ${env:WH_SECRET:-whsecret}
`, 0)
		id := createPublished(t, app, clk, "article")
		ob := app.Services().Outbox
		if _, err := ob.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(hits) != 1 {
			t.Fatalf("hits after first drain = %d", len(hits))
		}
		mu.Unlock()
		if _, err := ob.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(hits) != 1 {
			t.Fatalf("retried before backoff: %d hits", len(hits))
		}
		fail = false
		mu.Unlock()
		clk.Advance(5 * time.Second)
		if _, err := ob.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Hour)
		if _, err := ob.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(hits) != 2 {
			t.Fatalf("hits = %d, want 2 (one failure, one retry)", len(hits))
		}
		if hits[0].hdr.Get("Hydrogen-Delivery") != hits[1].hdr.Get("Hydrogen-Delivery") || hits[0].hdr.Get("Hydrogen-Delivery") == "" {
			t.Errorf("delivery ids differ: %q %q", hits[0].hdr.Get("Hydrogen-Delivery"), hits[1].hdr.Get("Hydrogen-Delivery"))
		}
		h := hits[1]
		if h.hdr.Get("Hydrogen-Event") != "item.published" {
			t.Errorf("event header = %q", h.hdr.Get("Hydrogen-Event"))
		}
		if err := webhook.Verify("whsecret", h.hdr.Get("Hydrogen-Signature"), h.body, clk.Now(), time.Hour+time.Minute); err != nil {
			t.Errorf("signature: %v", err)
		}
		var body struct {
			ID    string `json:"id"`
			Topic string `json:"topic"`
			Data  struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(h.body, &body); err != nil {
			t.Fatal(err)
		}
		if body.ID != h.hdr.Get("Hydrogen-Delivery") || body.Topic != "item.published" || body.Data.ID != id ||
			!slices.Equal(body.Tags, []string{"c:article", "i:" + id}) {
			t.Errorf("body = %s", h.body)
		}
	})
}

func TestEndpoints(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		mod := fnMod{"api", func(r *kernel.Registry) error {
			for _, e := range []struct {
				method, path string
				auth         kernel.Auth
				h            http.HandlerFunc
			}{
				{"GET", "/hello/{name}", kernel.Public, func(w http.ResponseWriter, r *http.Request) {
					s := kernel.ServicesFrom(r.Context())
					writeJSON(w, map[string]any{"hello": r.PathValue("name"), "env": s.Env})
				}},
				{"POST", "/secure", kernel.Authenticated, func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, map[string]any{"services": kernel.ServicesFrom(r.Context()) != nil})
				}},
				{"GET", "/boom", kernel.Public, func(http.ResponseWriter, *http.Request) { panic("boom") }},
			} {
				if err := r.Endpoint(e.method, e.path, e.auth, e.h); err != nil {
					return err
				}
			}
			return nil
		}}
		app := newEventsApp(t, b.open(t, clk), clk, "", 0, mod)
		h, err := app.Handler()
		if err != nil {
			t.Fatal(err)
		}
		do := func(method, path, token string) (int, string) {
			req := httptest.NewRequest(method, path, bytes.NewReader(nil))
			req.RemoteAddr = "127.0.0.1:50000" // the dev token only works from loopback
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec.Code, rec.Body.String()
		}
		if code, body := do("GET", "/api/x/hello/ann", ""); code != 200 || !strings.Contains(body, `"hello":"ann"`) || !strings.Contains(body, `"env":"dev"`) {
			t.Errorf("public: %d %s", code, body)
		}
		if code, _ := do("POST", "/api/x/secure", ""); code != 401 {
			t.Errorf("no token: %d", code)
		}
		if code, _ := do("POST", "/api/x/secure", "wrong"); code != 401 {
			t.Errorf("wrong token: %d", code)
		}
		if code, body := do("POST", "/api/x/secure", kernel.DevManageToken); code != 200 || !strings.Contains(body, `"services":true`) {
			t.Errorf("token: %d %s", code, body)
		}
		if code, _ := do("GET", "/api/x/secure", kernel.DevManageToken); code != 405 {
			t.Errorf("wrong method: %d", code)
		}
		if code, body := do("GET", "/api/x/boom", ""); code != 500 || !strings.Contains(body, `"internal"`) {
			t.Errorf("panic: %d %s", code, body)
		}
		if code, _ := do("GET", "/api/x/nothing", ""); code != 404 {
			t.Errorf("unknown: %d", code)
		}
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestEndpointConflictNamesModule(t *testing.T) {
	reg := func(name string) kernel.Module {
		return fnMod{name, func(r *kernel.Registry) error {
			return r.Endpoint("GET", "/same", kernel.Public, func(http.ResponseWriter, *http.Request) {})
		}}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("content: { schema_dir: none }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: dir, Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	err = kernel.Check(cfg, sqlite.Module(), reg("first"), reg("second"))
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestJobRunNow(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		var n atomic.Int32
		mod := fnMod{"jobsmod", func(r *kernel.Registry) error {
			return r.Job("count", "@every 1h", func(ctx context.Context, s *kernel.Services) error {
				if s == nil || s.Content == nil {
					t.Error("no services")
				}
				n.Add(1)
				return nil
			})
		}}
		app := newEventsApp(t, b.open(t, clk), clk, "", 0, mod)
		if err := app.RunJob(ctx, "count"); err != nil || n.Load() != 1 {
			t.Fatalf("RunJob: %v %d", err, n.Load())
		}
		if err := app.RunJob(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "count") {
			t.Fatalf("unknown: %v", err)
		}
		app.TickJobs(ctx)
		if n.Load() != 1 {
			t.Fatal("ran before due")
		}
		clk.Advance(time.Hour)
		app.TickJobs(ctx)
		if n.Load() != 2 {
			t.Fatalf("n = %d", n.Load())
		}
	})
}

func TestJobRunCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("content: { schema_dir: none }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := &config.Options{Dir: dir, Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }}
	db := openSQLite(t, newClock())
	ran := false
	configure := func(h *hydrogen.Builder) {
		h.Use(sqlite.Module())
		h.Job("hello", "0 3 * * *", func(context.Context, *kernel.Services) error { ran = true; return nil })
	}
	var out bytes.Buffer
	if err := hydrogen.Run(ctx, []string{"job:run", "hello"}, configure, hydrogen.RunOptions{Config: opts, DB: db, Stdout: &out, Stderr: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !ran || !strings.Contains(out.String(), "hello done") {
		t.Errorf("ran=%v out=%q", ran, out.String())
	}
	err := hydrogen.Run(ctx, []string{"job:run", "missing"}, configure, hydrogen.RunOptions{Config: opts, DB: db, Stdout: &out, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "hello") {
		t.Errorf("unknown job: %v", err)
	}
	bad := func(h *hydrogen.Builder) {
		h.Use(sqlite.Module())
		h.Job("x", "not a schedule", func(context.Context, *kernel.Services) error { return nil })
	}
	if err := hydrogen.Run(ctx, []string{"config:check"}, bad, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: io.Discard}); err == nil || !strings.Contains(err.Error(), "want 5 fields") {
		t.Errorf("bad schedule: %v", err)
	}
	dup := func(h *hydrogen.Builder) {
		h.Use(sqlite.Module())
		h.Endpoint("GET", "/a", kernel.Public, func(http.ResponseWriter, *http.Request) {})
		h.Endpoint("GET", "/a", kernel.Public, func(http.ResponseWriter, *http.Request) {})
	}
	if err := hydrogen.Run(ctx, []string{"config:check"}, dup, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: io.Discard}); err == nil || !strings.Contains(err.Error(), "project") {
		t.Errorf("dup endpoint: %v", err)
	}
}

// Two servers on one Postgres: each tick of a job runs once across both.
func TestJobsTwoNodes(t *testing.T) {
	url := pgtest.Scratch(t)
	clk := newClock()
	open := func() store.DB {
		db, err := postgres.Open(ctx, url, store.OpenOptions{Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	first := open()
	if err := first.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	mod := func(name string) kernel.Module {
		return fnMod{"jobs-" + name, func(r *kernel.Registry) error {
			return r.Job("tick", "* * * * *", func(ctx context.Context, _ *kernel.Services) error {
				runs.Add(1)
				time.Sleep(40 * time.Millisecond)
				return nil
			})
		}}
	}
	const hold = 300 * time.Millisecond
	apps := []*kernel.App{newEventsApp(t, first, clk, "", hold, mod("a")), newEventsApp(t, open(), clk, "", hold, mod("b"))}

	tick := func() {
		var wg sync.WaitGroup
		for _, a := range apps {
			wg.Go(func() { a.TickJobs(ctx) })
		}
		wg.Wait()
	}
	tick()
	if runs.Load() != 0 {
		t.Fatalf("ran before due: %d", runs.Load())
	}
	for i := 1; i <= 3; i++ {
		clk.Advance(time.Minute)
		tick()
		if got := runs.Load(); got != int32(i) {
			t.Fatalf("after tick %d: %d runs", i, got)
		}
		time.Sleep(hold + 200*time.Millisecond)
	}
}
