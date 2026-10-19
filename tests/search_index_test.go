package tests

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/overlays"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const siSchema = `
types:
  article:
    name: "{title}"
    fields:
      title: { type: string, translatable: true }
      topic: { type: string, index: true }
  settings:
    singleton: true
    fields:
      site: { type: string }
`

// fakeIndex is an in-memory search.Index.
type fakeIndex struct {
	mu          sync.Mutex
	fingerprint string
	docs        map[string]search.Doc
	upserts     int
	deletes     int
	syncs       []search.Settings
	rebuilds    int
	begun       int // Rebuild calls, finished or not
	rebuilt     []search.Settings
	// onNext runs before each page of a rebuild is requested (page counts from 1).
	onNext func(page int)
}

func newFakeIndex() *fakeIndex { return &fakeIndex{docs: map[string]search.Doc{}} }

func (f *fakeIndex) Name() string { return "fake" }

func (f *fakeIndex) Search(context.Context, search.Request) (search.Result, error) {
	return search.Result{}, nil
}

func (f *fakeIndex) Sync(_ context.Context, s search.Settings) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs = append(f.syncs, s)
	return f.fingerprint != s.Fingerprint, nil
}

func (f *fakeIndex) Upsert(_ context.Context, docs []search.Doc) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts++
	for _, d := range docs {
		f.docs[d.Item.String()+"/"+d.Locale] = d
	}
	return nil
}

func (f *fakeIndex) Delete(_ context.Context, items []ids.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	for k, d := range f.docs {
		if slices.Contains(items, d.Item) {
			delete(f.docs, k)
		}
	}
	return nil
}

func (f *fakeIndex) Rebuild(ctx context.Context, s search.Settings, next func(context.Context) ([]search.Doc, error)) error {
	f.mu.Lock()
	f.begun++
	f.mu.Unlock()
	fresh := map[string]search.Doc{}
	for page := 1; ; page++ {
		if f.onNext != nil {
			f.onNext(page)
		}
		docs, err := next(ctx)
		if err != nil {
			return err
		}
		if docs == nil {
			break
		}
		for _, d := range docs {
			fresh[d.Item.String()+"/"+d.Locale] = d
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = fresh
	f.fingerprint = s.Fingerprint // last, as search.Index requires
	f.rebuilds++
	f.rebuilt = append(f.rebuilt, s)
	return nil
}

func (f *fakeIndex) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.docs))
	for k := range f.docs {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (f *fakeIndex) count() (rebuilds int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rebuilds
}

type fakeSearchModule struct{ idx search.Engine }

func (fakeSearchModule) Name() string { return "fakesearch" }

func (m fakeSearchModule) Register(r *kernel.Registry) error { return r.SearchEngine(m.idx) }

// siApp starts an app on db with fx as its search engine. The clock moves a second per call of its Now, so
// every write gets a new timestamp.
func siApp(t *testing.T, db store.DB, clk *tickClock, fx *fakeIndex) *kernel.App {
	t.Helper()
	cfg := mediaConfig(t, siSchema, `http: { addr: "127.0.0.1:0" }`, nil)
	a, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), fakeSearchModule{fx})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type tickClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

func newTickClock() *tickClock { return &tickClock{t: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)} }

func TestSearchIndexConsumer(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		svc := a.Services().Content
		if err := a.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.EnsureSchemaRev(ctx); err != nil {
			t.Fatal(err)
		}
		drain := func() {
			t.Helper()
			if _, err := a.Services().Outbox.Drain(ctx); err != nil {
				t.Fatal(err)
			}
		}
		keys := func(want ...string) {
			t.Helper()
			if got := fx.keys(); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("index = %v, want %v", got, want)
			}
		}

		v, err := svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: doc(t, `{"title":"Solar","topic":"energy"}`)})
		if err != nil {
			t.Fatal(err)
		}
		id := v.ID.String()
		drain()
		keys()

		publish := func() {
			t.Helper()
			if _, err := svc.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
				t.Fatal(err)
			}
			drain()
		}
		publish()
		keys(id + "/en")
		if d := fx.docs[id+"/en"]; d.Name != "Solar" || d.Fields["topic"] != "energy" || d.Type != "article" {
			t.Errorf("doc = %+v", d)
		}

		if _, err := svc.Update(ctx, v.ID, content.UpdateInput{Locale: "de", Fields: doc(t, `{"title":"Sonne"}`)}); err != nil {
			t.Fatal(err)
		}
		publish()
		keys(id+"/de", id+"/en")
		if d := fx.docs[id+"/de"]; d.Name != "Sonne" || !slices.Equal(d.Locales, []string{"de", "en"}) {
			t.Errorf("de doc = %+v", d)
		}

		if _, err := svc.Unpublish(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		keys()

		publish()
		keys(id+"/de", id+"/en")
		if err := svc.Trash(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		keys()

		if _, err := svc.Restore(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		keys() // restored items are not published

		publish()
		keys(id+"/de", id+"/en")
		if err := svc.Trash(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Purge(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		keys()

		// Not-searchable types are never indexed.
		s, err := svc.Create(ctx, content.CreateInput{Type: "settings", Locale: "en", Fields: doc(t, `{"site":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Publish(ctx, s.ID, content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		before := fx.upserts
		drain()
		keys()
		if fx.upserts != before {
			t.Error("a singleton was upserted")
		}
	})
}

func siPublished(t *testing.T, a *kernel.App, n int) []ids.UUID {
	t.Helper()
	svc := a.Services().Content
	var out []ids.UUID
	for i := range n {
		v, err := svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: doc(t, fmt.Sprintf(`{"title":"Article %d"}`, i))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		out = append(out, v.ID)
	}
	return out
}

func siSetup(t *testing.T, a *kernel.App) {
	t.Helper()
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Services().Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
}

func siWait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 3000 { // 30 s: the rebuild runs slower under -race on CI runners
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSearchIndexRebuildAtStart(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		// More published items than one rebuild page, an unpublished one and a singleton.
		published := siPublished(t, a, 103)
		svc := a.Services().Content
		if _, err := svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: doc(t, `{"title":"Draft"}`)}); err != nil {
			t.Fatal(err)
		}
		s, err := svc.Create(ctx, content.CreateInput{Type: "settings", Locale: "en", Fields: doc(t, `{"site":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Publish(ctx, s.ID, content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}

		// run serves a until done holds (or the wait times out), then stops it. Run waits for the start-up sync
		// and rebuild, so the app must still be running while the test waits for them.
		run := func(a *kernel.App, what string, done func() bool) {
			t.Helper()
			rctx, cancel := context.WithCancel(ctx)
			stopped := make(chan error, 1)
			listening := make(chan struct{})
			go func() { stopped <- a.Run(rctx, func(string) { close(listening) }) }()
			<-listening
			defer func() {
				cancel()
				if err := <-stopped; err != nil {
					t.Error(err)
				}
			}()
			siWait(t, what, done)
		}
		run(a, "the rebuild", func() bool { return fx.count() == 1 })
		if len(fx.keys()) != len(published) {
			t.Fatalf("indexed %d documents, want %d", len(fx.keys()), len(published))
		}
		want := search.NewSettings(a.Services().Schema)
		if fx.rebuilt[0].Fingerprint != want.Fingerprint || len(fx.rebuilt[0].Types) != 1 || fx.rebuilt[0].Types[0] != "article" {
			t.Errorf("rebuilt with %+v", fx.rebuilt[0])
		}

		// The same settings do not rebuild again; changed ones do.
		a2 := siApp(t, a.Services().DB, clk, fx)
		run(a2, "the index sync", func() bool {
			fx.mu.Lock()
			n := len(fx.syncs)
			fx.mu.Unlock()
			if n < 2 {
				return false
			}
			time.Sleep(100 * time.Millisecond) // time for a wrong rebuild to begin
			return true
		})
		fx.mu.Lock()
		begun := fx.begun
		fx.mu.Unlock()
		if begun != 1 {
			t.Errorf("rebuilds begun = %d after a restart with the same settings", begun)
		}
		fx.mu.Lock()
		fx.fingerprint = "old"
		fx.mu.Unlock()
		a3 := siApp(t, a.Services().DB, clk, fx)
		run(a3, "the second rebuild", func() bool { return fx.count() == 2 })
	})
}

func TestSearchReindexCommand(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		published := siPublished(t, a, 3)
		cmd, ok := a.Command(kernel.SearchReindexCommand)
		if !ok {
			t.Fatal("no search:reindex command")
		}
		if err := cmd.Run(ctx, a, []string{"extra"}); err == nil {
			t.Error("arguments accepted")
		}
		// The first run finds a new index (Sync asks for a rebuild); a forced one rebuilds even when it does not.
		fx.fingerprint = search.NewSettings(a.Services().Schema).Fingerprint
		if err := cmd.Run(ctx, a, nil); err != nil {
			t.Fatal(err)
		}
		if fx.count() != 1 || len(fx.keys()) != len(published) {
			t.Fatalf("rebuilds = %d, documents = %d", fx.count(), len(fx.keys()))
		}
		if err := cmd.Run(ctx, a, nil); err != nil || fx.count() != 2 {
			t.Fatalf("second run: rebuilds = %d, err = %v", fx.count(), err)
		}
	})
}

func TestSearchRebuildCatchesUpEdits(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		siPublished(t, a, 2)
		var late ids.UUID
		fx.onNext = func(page int) {
			if page == 2 { // after the first page was read
				late = siPublished(t, a, 1)[0]
			}
		}
		cmd, _ := a.Command(kernel.SearchReindexCommand)
		if err := cmd.Run(ctx, a, nil); err != nil {
			t.Fatal(err)
		}
		if late == (ids.UUID{}) {
			t.Fatal("no item was published during the rebuild")
		}
		fx.mu.Lock()
		_, ok := fx.docs[late.String()+"/en"]
		fx.mu.Unlock()
		if !ok || len(fx.keys()) != 3 {
			t.Fatalf("index = %v, want the late item too", fx.keys())
		}
	})
}

func TestSearchCatchUpSkipsABusyItem(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		siPublished(t, a, 2)
		var late []ids.UUID
		var held store.Lock
		fx.onNext = func(page int) {
			if page != 2 {
				return
			}
			late = siPublished(t, a, 3)
			// The newest item comes first in the catch-up; something else is indexing it the whole time.
			l, ok, err := a.Services().DB.Locker().TryLock(ctx, "search:item:"+late[2].String())
			if err != nil || !ok {
				t.Fatalf("lock: %v, %v", ok, err)
			}
			held = l
		}
		cmd, _ := a.Command(kernel.SearchReindexCommand)
		err := cmd.Run(ctx, a, nil)
		if held != nil {
			_ = held.Release(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(late) != 3 {
			t.Fatal("no items were published during the rebuild")
		}
		fx.mu.Lock()
		_, busy := fx.docs[late[2].String()+"/en"]
		_, a0 := fx.docs[late[0].String()+"/en"]
		_, a1 := fx.docs[late[1].String()+"/en"]
		fx.mu.Unlock()
		if busy || !a0 || !a1 || len(fx.keys()) != 4 {
			t.Fatalf("index = %v, want the late items except the busy one", fx.keys())
		}
	})
}

func TestSearchHealsMissingRows(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newTickClock()
		cfg := mediaConfig(t, siSchema, "", nil)
		a, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, PasswordIterations: 1000}, sqlite.Module())
		if err != nil {
			t.Fatal(err)
		}
		siSetup(t, a)
		s := a.Services()
		if ran, err := s.HealSearchRows(ctx); err != nil || ran {
			t.Fatalf("empty database: ran = %v, err = %v", ran, err)
		}
		id := siPublished(t, a, 1)[0]
		if ran, err := s.HealSearchRows(ctx); err != nil || ran {
			t.Fatalf("healthy: ran = %v, err = %v", ran, err)
		}
		err = s.DB.Tx(ctx, func(tx store.Tx) error {
			return tx.Content().ReplaceSearch(ctx, id, store.ProjectionPublished, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		find := func() int {
			res, err := s.Search.Search(ctx, search.Request{Text: "article", Types: []string{"article"}, Locale: "en", Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			return len(res.Hits)
		}
		if find() != 0 {
			t.Fatal("still found after the rows were removed")
		}
		if ran, err := s.HealSearchRows(ctx); err != nil || !ran {
			t.Fatalf("lost rows: ran = %v, err = %v", ran, err)
		}
		if find() != 1 {
			t.Error("not found after healing")
		}
	})
}

func TestSearchDefaultEngine(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newTickClock()
		cfg := mediaConfig(t, siSchema, "", nil)
		a, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, PasswordIterations: 1000}, sqlite.Module())
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Services().Search.Name(); got != "database" {
			t.Errorf("default engine = %q", got)
		}
		cmd, _ := a.Command(kernel.SearchReindexCommand)
		if err := cmd.Run(ctx, a, nil); err != nil {
			t.Errorf("reindex without an index: %v", err)
		}
	})
}

func TestSearchEngineRegistration(t *testing.T) {
	clk := newTickClock()
	cfg := mediaConfig(t, siSchema, "", nil)
	db := openSQLite(t, clk)
	_, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard}, sqlite.Module(),
		fakeSearchModule{newFakeIndex()}, secondSearchModule{})
	if err == nil {
		t.Fatal("two engines accepted")
	}
}

type secondSearchModule struct{}

func (secondSearchModule) Name() string { return "second" }

func (secondSearchModule) Register(r *kernel.Registry) error { return r.SearchEngine(newFakeIndex()) }

func TestSearchIndexOverlayLocales(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		dir := writeFiles(t, map[string]string{"schema/s.yaml": siSchema,
			"config/channels.yaml": "languages:\n  de: {}\n  de-CH: { inherits: de }\n",
			"config/app.yaml":      ""})
		if err := osWrite(dir+"/config/app.yaml", "content: { schema_dir: "+dir+"/schema, default_locale: de }\n"); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.Options{Dir: dir + "/config", Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
		if err != nil {
			t.Fatal(err)
		}
		a, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, PasswordIterations: 1000},
			sqlite.Module(), overlays.Module(), fakeSearchModule{fx})
		if err != nil {
			t.Fatal(err)
		}
		siSetup(t, a)
		v, err := a.Services().Content.Create(ctx, content.CreateInput{Type: "article", Locale: "de", Fields: doc(t, `{"title":"Hallo"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Services().Content.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Services().Outbox.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		id := v.ID.String()
		if got := fx.keys(); fmt.Sprint(got) != fmt.Sprint([]string{id + "/de", id + "/de-CH"}) {
			t.Fatalf("index = %v", got)
		}
		if d := fx.docs[id+"/de-CH"]; d.Name != "Hallo" || !slices.Equal(d.Locales, []string{"de", "de-CH"}) {
			t.Errorf("derived doc = %+v", d)
		}
	})
}

func TestSearchIndexFollowsMetadataAndSkipsDrafts(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		id := siPublished(t, a, 1)[0]
		drain := func() {
			t.Helper()
			if _, err := a.Services().Outbox.Drain(ctx); err != nil {
				t.Fatal(err)
			}
		}
		drain()
		key := id.String() + "/en"
		if fx.docs[key].AlwaysAvailable {
			t.Fatal("always available from the start")
		}
		before := fx.upserts
		if _, err := a.Services().Content.Update(ctx, id, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"Draft edit"}`)}); err != nil {
			t.Fatal(err)
		}
		drain()
		if fx.upserts != before || fx.docs[key].Name == "Draft edit" {
			t.Error("a draft write reached the index")
		}
		yes := true
		if _, err := a.Services().Content.Update(ctx, id, content.UpdateInput{AlwaysAvailable: &yes}); err != nil {
			t.Fatal(err)
		}
		drain()
		if !fx.docs[key].AlwaysAvailable {
			t.Error("always_available did not reach the index")
		}
	})
}

func TestSearchIndexPurgesDeliveryCache(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		fx := newFakeIndex()
		clk := newTickClock()
		a := siApp(t, b.open(t, clk), clk, fx)
		siSetup(t, a)
		lctx, cancel := context.WithCancel(ctx)
		defer cancel()
		ch, err := a.Services().DB.Notifier().Listen(lctx, cachetag.PurgeChannel)
		if err != nil {
			t.Fatal(err)
		}
		siPublished(t, a, 1)
		if _, err := a.Services().Outbox.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		// One purge from cache.purge and one after the index write.
		n := 0
		timeout := time.After(2 * time.Second)
		for n < 2 {
			select {
			case m := <-ch:
				if strings.Contains(m.Payload, cachetag.Collection("article")) {
					n++
				}
			case <-timeout:
				t.Fatalf("collection purges = %d, want 2", n)
			}
		}
	})
}
