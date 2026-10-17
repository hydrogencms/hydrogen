package tests

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const pagesSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  section:
    page: true
    children: [section, landing]
    fields:
      title: { type: string, translatable: true, required: true }
  landing:
    page: true
    children: []
    fields:
      title: { type: string, translatable: true, required: true }
  article:
    name: "{title}"
    fields:
      title:       { type: string, translatable: true, required: true }
      slug:        { type: slug, from: title, translatable: true }
      publishDate: { type: datetime }
      category:    { type: string }
      kind:        { type: string }
`

// pagesEnv is a real kernel (core services, pages module) on one database.
type pagesEnv struct {
	t     testing.TB
	db    store.DB
	clk   *clock.Manual
	app   *kernel.App
	c     *content.Service
	pages *pages.Service
}

// pagesOpts tunes a pages environment.
type pagesOpts struct {
	schema string
	sites  []string
	extra  []kernel.Module
	// channels is the content of config/channels.yaml; empty: none.
	channels string
}

func newPagesEnv(t testing.TB, db store.DB, clk *clock.Manual, o pagesOpts) *pagesEnv {
	t.Helper()
	schemaYAML, sites := o.schema, o.sites
	if schemaYAML == "" {
		schemaYAML = pagesSchema
	}
	dir := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", schemaYAML)
	siteList := "[main]"
	if len(sites) > 0 {
		siteList = "["
		for i, s := range sites {
			if i > 0 {
				siteList += ", "
			}
			siteList += s
		}
		siteList += "]"
	}
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(dir, "schema")+" }\n"+
		"modules:\n  pages: { sites: "+siteList+" }\n")
	if o.channels != "" {
		write("config/channels.yaml", o.channels)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	mods := append([]kernel.Module{sqlite.Module(), postgres.Module(), pages.Module()}, o.extra...)
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := app.Services()
	if _, err := s.Content.EnsureSchemaRev(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc, ok := kernel.LookupService[*pages.Service](s)
	if !ok {
		t.Fatal("pages service not provided")
	}
	return &pagesEnv{t: t, db: db, clk: clk, app: app, c: s.Content, pages: svc}
}

func (e *pagesEnv) drain() {
	e.t.Helper()
	if _, err := e.app.Services().Outbox.Drain(ctx); err != nil {
		e.t.Fatal(err)
	}
}

// create creates a draft item and publishes the given locales (or just the creation locale).
func (e *pagesEnv) create(typ, locale, js string) ids.UUID {
	e.t.Helper()
	e.clk.Advance(1e9)
	v, err := e.c.Create(ctx, content.CreateInput{Type: typ, Locale: locale, Fields: doc(e.t, js)})
	if err != nil {
		e.t.Fatalf("create %s: %v", typ, err)
	}
	return v.ID
}

func (e *pagesEnv) publish(id ids.UUID, locales ...string) error {
	e.t.Helper()
	e.clk.Advance(1e9)
	_, err := e.c.Publish(ctx, id, content.PublishOptions{Locales: locales})
	return err
}

func (e *pagesEnv) mustPublish(id ids.UUID, locales ...string) {
	e.t.Helper()
	if err := e.publish(id, locales...); err != nil {
		e.t.Fatalf("publish: %v", err)
	}
}

func (e *pagesEnv) update(id ids.UUID, locale, js string) {
	e.t.Helper()
	e.clk.Advance(1e9)
	if _, err := e.c.Update(ctx, id, content.UpdateInput{Locale: locale, Fields: doc(e.t, js)}); err != nil {
		e.t.Fatalf("update: %v", err)
	}
}

// page creates and publishes a page item in en with a title (the slug derives from it).
func (e *pagesEnv) page(typ, title string) ids.UUID {
	e.t.Helper()
	id := e.create(typ, "en", `{"title":"`+title+`"}`)
	e.mustPublish(id)
	return id
}

// placed creates a published page and places it under parent (nil: as the site root) after the sibling.
func (e *pagesEnv) placed(typ, title string, parent *ids.UUID, after *ids.UUID) (ids.UUID, ids.UUID) {
	e.t.Helper()
	item := e.page(typ, title)
	n, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: parent, After: after})
	if err != nil {
		e.t.Fatalf("place %s: %v", title, err)
	}
	return item, n.ID
}

func (e *pagesEnv) resolve(locale, path string) *pages.Resolution {
	e.t.Helper()
	r, err := e.pages.Resolve(ctx, "main", locale, path)
	if err != nil {
		e.t.Fatalf("resolve %s: %v", path, err)
	}
	return r
}

// runPages runs fn on a fresh pages environment per backend.
func runPages(t *testing.T, o pagesOpts, fn func(t *testing.T, e *pagesEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, newPagesEnv(t, b.open(t, clk), clk, o))
	})
}

// probe records outbox events of the given topics.
type probe struct {
	mu     sync.Mutex
	topics []string
	events []outbox.Event
}

func (p *probe) Name() string { return "probe" }

func (p *probe) Register(r *kernel.Registry) error {
	r.Consumer(outbox.Consumer{Name: "probe", Topics: p.topics, Handle: func(_ context.Context, e outbox.Event) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.events = append(p.events, e)
		return nil
	}})
	return nil
}

// take returns and clears the events seen so far.
func (p *probe) take() []outbox.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.events
	p.events = nil
	return out
}

// migrationModule registers content migrations.
type migrationModule []content.Migration

func (migrationModule) Name() string { return "migrations" }

func (m migrationModule) Register(r *kernel.Registry) error {
	for _, mig := range m {
		r.ContentMigration(mig)
	}
	return nil
}

func (e *pagesEnv) audit(subject ids.UUID) []string {
	e.t.Helper()
	var out []string
	err := e.db.View(ctx, func(tx store.Tx) error {
		evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: subject.String(), Limit: 100})
		for i := len(evs) - 1; i >= 0; i-- {
			out = append(out, evs[i].Action)
		}
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}
