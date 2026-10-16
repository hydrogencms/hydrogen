package tests

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/review"
	"github.com/hydrogencms/hydrogen/store"
)

const reviewSchema = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
  article:
    review: true
    name: "{title}"
    fields:
      title: { type: string, translatable: true, required: true }
      body:  { type: text }
      author: { type: ref, to: [person], on_delete: nullify }
  note:
    name: "{title}"
    fields:
      title: { type: string, required: true }
`

// reviewProbe records the review.* outbox events.
type reviewProbe struct {
	mu     sync.Mutex
	events []outbox.Event
}

func (p *reviewProbe) Name() string { return "reviewprobe" }

func (p *reviewProbe) Register(r *kernel.Registry) error {
	r.Consumer(outbox.Consumer{Name: "reviewprobe", Topics: []string{"review.*"}, Handle: func(_ context.Context, e outbox.Event) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.events = append(p.events, e)
		return nil
	}})
	return nil
}

func (p *reviewProbe) topics() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		out = append(out, e.Topic)
	}
	return out
}

func (p *reviewProbe) last(topic string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.events) - 1; i >= 0; i-- {
		if p.events[i].Topic == topic {
			var m map[string]any
			_ = json.Unmarshal(p.events[i].Payload, &m)
			return m
		}
	}
	return nil
}

type revNode struct {
	*dnode
	probe *reviewProbe
}

// runReview runs fn on every backend with the review schema; extra is added to the content section of app.yaml.
func runReview(t *testing.T, extra string, fn func(t *testing.T, n *revNode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		probe := &reviewProbe{}
		n := newReviewNode(t, b.open(t, clk), clk, extra, probe)
		fn(t, &revNode{dnode: n, probe: probe})
	})
}

func newReviewNode(t testing.TB, db store.DB, clk *clock.Manual, extra string, mods ...kernel.Module) *dnode {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": reviewSchema, "config/channels.yaml": deliveryChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + extra + " }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) {
			if k == "APP_TOKEN" {
				return appToken, true
			}
			return "", false
		}})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	mods = append([]kernel.Module{sqlite.Module(), postgres.Module()}, mods...)
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Services().Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.Services().Delivery.Start(lctx); err != nil {
		t.Fatal(err)
	}
	h, err := a.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &dnode{t: t, app: a, db: cdb, h: h, clk: clk}
}

func (n *revNode) svc() *review.Service { return n.app.Services().Review }

// who is a token principal with a name: its secret, its context for service calls and its actor.
type who struct {
	name   string
	secret string
	ctx    context.Context
	actor  string
}

func (n *revNode) person(name string, policies ...string) who {
	n.t.Helper()
	s := n.app.Services()
	var ps []access.RolePolicy
	for _, p := range policies {
		ps = append(ps, rolePolicy(p, nil))
	}
	role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: ps})
	if err != nil {
		n.t.Fatal(err)
	}
	tok, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
	if err != nil {
		n.t.Fatal(err)
	}
	p, err := s.Auth.PrincipalOf(ctx, "token:"+tok.ID.String())
	if err != nil || p == nil {
		// agent tokens carry another kind
		p, err = s.Auth.PrincipalOf(ctx, tok.Kind+":"+tok.ID.String())
	}
	if err != nil || p == nil {
		n.t.Fatalf("principal of %s: %v", name, err)
	}
	return who{name: name, secret: secret, ctx: access.WithPrincipal(ctx, p), actor: p.Actor().String()}
}

var (
	editorRights   = []string{"content.read", "content.create", "content.edit", "content.publish", "review.submit"}
	reviewerRights = []string{"content.read", "review.approve"}
)

func (n *revNode) edit(w who, id ids.UUID, fields string) {
	n.t.Helper()
	n.tick()
	if _, err := n.app.Services().Content.Update(w.ctx, id, content.UpdateInput{Fields: doc(n.t, fields)}); err != nil {
		n.t.Fatalf("%s edits: %v", w.name, err)
	}
}

func (n *revNode) call(w who, method, path string, body any, want int) resp {
	n.t.Helper()
	var r resp
	if w.secret == "" {
		r = n.manage(method, path, body)
	} else {
		r = n.as(w.secret, method, path, body)
	}
	if r.Code != want {
		n.t.Fatalf("%s %s as %s: %d, want %d: %s", method, path, w.name, r.Code, want, r.Body)
	}
	return r
}

var root = who{name: "admin"}

func (n *revNode) review(id ids.UUID) map[string]any {
	n.t.Helper()
	return n.call(root, "GET", "/items/"+id.String()+"/review", nil, 200).json()
}

func (n *revNode) state(id ids.UUID) string { return n.review(id)["state"].(string) }

func (n *revNode) act(w who, id ids.UUID, verb string, body any, want int) map[string]any {
	n.t.Helper()
	return n.call(w, "POST", "/items/"+id.String()+"/review/"+verb, body, want).json()
}

func errCode(r map[string]any) any { return field(r, "error", "code") }

func kinds(rv map[string]any) []string {
	var out []string
	for _, e := range rv["events"].([]any) {
		out = append(out, e.(map[string]any)["kind"].(string))
	}
	return out
}

func (n *revNode) auditData(id ids.UUID, action string) []map[string]any {
	n.t.Helper()
	var out []map[string]any
	err := n.app.Services().DB.View(ctx, func(tx store.Tx) error {
		evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: id.String(), Action: action, Limit: 50})
		for _, e := range evs {
			var m map[string]any
			_ = json.Unmarshal(e.Data, &m)
			out = append(out, m)
		}
		return err
	})
	if err != nil {
		n.t.Fatal(err)
	}
	return out
}

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }
