package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

type backend struct {
	name string
	open func(t testing.TB, clk clock.Clock) store.DB
}

var backends = []backend{{"sqlite", openSQLite}, {"postgres", openPostgres}}

func openSQLite(t testing.TB, clk clock.Clock) store.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "sqlite:"+filepath.Join(t.TempDir(), "test.db"), store.OpenOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return db
}

func openPostgres(t testing.TB, clk clock.Clock) store.DB {
	t.Helper()
	url := pgtest.Scratch(t)
	db, err := postgres.Open(context.Background(), url, store.OpenOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return db
}

// forEachBackend runs fn as a subtest per database with a fresh database each.
func forEachBackend(t *testing.T, fn func(t *testing.T, b backend)) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) { fn(t, b) })
	}
}

// run runs fn against every backend with a ready environment on the standard schema.
func run(t *testing.T, fn func(t *testing.T, e *env)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
		fn(t, newEnv(t, b.open(t, clk), clk, envConfig{}))
	})
}

type envConfig struct {
	schema     string
	migrations []content.Migration
	setup      func(*hooks.Registry)
	skipRev    bool // do not record the schema revision
	overlays   content.Overlays
}

type env struct {
	t     testing.TB
	db    store.DB
	clk   *clock.Manual
	svc   *content.Service
	ob    *outbox.Outbox
	hooks *hooks.Registry

	mu     sync.Mutex
	events []outbox.Event
}

func buildSchema(t testing.TB, yaml string) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(yaml)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newEnv(t testing.TB, db store.DB, clk *clock.Manual, cfg envConfig) *env {
	t.Helper()
	if cfg.schema == "" {
		cfg.schema = standardSchema
	}
	e := &env{t: t, db: db, clk: clk, hooks: &hooks.Registry{}}
	if cfg.setup != nil {
		cfg.setup(e.hooks)
	}
	consumers := content.AsyncHookConsumers(e.hooks.Async(), func() *content.Service { return e.svc })
	consumers = append(consumers, outbox.Consumer{Name: "probe", Topics: []string{"item.*"},
		Handle: func(_ context.Context, ev outbox.Event) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.events = append(e.events, ev)
			return nil
		}})
	var err error
	e.ob, err = outbox.New(db, clk, nil, consumers, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.svc, err = content.New(content.Options{DB: db, Schema: buildSchema(t, cfg.schema), Hooks: e.hooks, Outbox: e.ob,
		Audit: audit.New(clk), Clock: clk, IDs: ids.NewGenerator(clk, nil), DefaultLocale: "en", Migrations: cfg.migrations, Overlays: cfg.overlays})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.skipRev {
		if _, err := e.svc.EnsureSchemaRev(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *env) drain() []outbox.Event {
	e.t.Helper()
	if _, err := e.ob.Drain(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.events
	e.events = nil
	return out
}

func (e *env) auditActions(id ids.UUID) []string {
	e.t.Helper()
	var out []string
	err := e.db.View(context.Background(), func(tx store.Tx) error {
		evs, err := tx.Audit().List(context.Background(), store.AuditQuery{Subject: id.String(), Limit: 100})
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

func (e *env) tick() { e.clk.Advance(time.Second) }

func doc(t testing.TB, js string) map[string]any {
	t.Helper()
	d, err := schema.DecodeDoc([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var ctx = context.Background()

const standardSchema = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
  article:
    name: "{title}"
    fields:
      title:       { type: string, translatable: true, required: true, max: 120 }
      slug:        { type: slug, from: title, translatable: true, index: true }
      summary:     { type: text, max: 300, translatable: true }
      body:        { type: richtext, translatable: true }
      kind:        { type: enum, values: [news, video], default: news, index: true }
      video:       { type: url, when: { field: kind, eq: video } }
      publishDate: { type: datetime, index: true }
      day:         { type: date, index: true }
      rating:      { type: number, integer: true, min: 0, max: 5, index: true }
      featured:    { type: boolean, default: false, index: true }
      author:      { type: ref, to: [person], on_delete: block, index: true }
      editor:      { type: ref, to: [person], on_delete: nullify }
      reviewer:    { type: ref, to: [person], on_delete: warn }
      related:     { type: list, of: { type: ref, to: [article, person], on_delete: nullify } }
      tags:        { type: list, of: { type: string, max: 30 }, max: 10, index: true }
      address:     { type: group, fields: { street: { type: string }, city: { type: string } } }
  settings:
    singleton: true
    fields:
      siteName:    { type: string, required: true, translatable: true }
      maintenance: { type: boolean }
`

func newClock() *clock.Manual { return clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)) }
