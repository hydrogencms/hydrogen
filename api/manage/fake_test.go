package manage

import (
	"context"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const testSchema = `
groups:
  seo:
    fields:
      metaTitle: { type: string }
types:
  person:
    fields:
      fullName: { type: string, required: true }
  article:
    label: Article
    description: News.
    fields:
      title:   { type: string, translatable: true, required: true, max: 120 }
      kind:    { type: enum, values: [news, video], default: news }
      rating:  { type: number, integer: true, min: 0, max: 5, index: true }
      author:  { type: ref, to: [person], index: true }
      tags:    { type: list, of: { type: string }, index: true }
      address: { type: group, fields: { city: { type: string } } }
      seo:     { type: group, use: seo }
  settings:
    singleton: true
    fields:
      siteName: { type: string, required: true }
`

func testSch(tb testing.TB) *schema.Schema {
	tb.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	fsys := fstest.MapFS{"schema/s.yaml": &fstest.MapFile{Data: []byte(testSchema)}}
	if err := b.LoadFS(fsys, "schema"); err != nil {
		tb.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

type call struct {
	name string
	id   ids.UUID
	typ  string
	arg  any
}

// fake records calls and returns canned results.
type fake struct {
	sc *schema.Schema

	mu    sync.Mutex
	calls []call

	view     *content.View
	list     *content.ListResult
	facets   map[string][]content.FacetValue
	versions []store.VersionMeta
	ver      *content.VersionView
	refs     []content.Referrer
	err      error
	panics   bool
}

func (f *fake) rec(c call) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if f.panics {
		panic("boom")
	}
	return f.err
}

func (f *fake) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return call{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fake) Schema() *schema.Schema { return f.sc }

func (f *fake) Create(_ context.Context, in content.CreateInput) (*content.View, error) {
	return f.view, f.rec(call{name: "Create", arg: in})
}

func (f *fake) Get(_ context.Context, id ids.UUID, o content.GetOptions) (*content.View, error) {
	return f.view, f.rec(call{name: "Get", id: id, arg: o})
}

func (f *fake) Update(_ context.Context, id ids.UUID, in content.UpdateInput) (*content.View, error) {
	return f.view, f.rec(call{name: "Update", id: id, arg: in})
}

func (f *fake) Publish(_ context.Context, id ids.UUID, o content.PublishOptions) (*content.View, error) {
	return f.view, f.rec(call{name: "Publish", id: id, arg: o})
}

func (f *fake) Discard(_ context.Context, id ids.UUID, parts []string) error {
	return f.rec(call{name: "Discard", id: id, arg: parts})
}

func (f *fake) Delete(_ context.Context, id ids.UUID) error {
	return f.rec(call{name: "Delete", id: id})
}

func (f *fake) Unpublish(_ context.Context, id ids.UUID) (*content.View, error) {
	return f.view, f.rec(call{name: "Unpublish", id: id})
}

func (f *fake) Restore(_ context.Context, id ids.UUID) (*content.View, error) {
	return f.view, f.rec(call{name: "Restore", id: id})
}

func (f *fake) Purge(_ context.Context, id ids.UUID) error {
	return f.rec(call{name: "Purge", id: id})
}

func (f *fake) ListTrash(_ context.Context, o content.TrashOptions) (*content.TrashResult, error) {
	return &content.TrashResult{}, f.rec(call{name: "ListTrash", arg: o})
}

func (f *fake) List(_ context.Context, o content.ListOptions) (*content.ListResult, error) {
	return f.list, f.rec(call{name: "List", arg: o})
}

func (f *fake) Facets(_ context.Context, o content.FacetOptions) (map[string][]content.FacetValue, error) {
	return f.facets, f.rec(call{name: "Facets", arg: o})
}

func (f *fake) Versions(_ context.Context, id ids.UUID) ([]store.VersionMeta, error) {
	return f.versions, f.rec(call{name: "Versions", id: id})
}

func (f *fake) VersionContent(_ context.Context, id ids.UUID, n int) (*content.VersionView, error) {
	return f.ver, f.rec(call{name: "VersionContent", id: id, arg: n})
}

func (f *fake) RestoreVersion(_ context.Context, id ids.UUID, n int, locales []string) (*content.View, error) {
	return f.view, f.rec(call{name: "RestoreVersion", id: id, arg: []any{n, locales}})
}

func (f *fake) ReferencesVisible(_ context.Context, id ids.UUID) ([]content.Referrer, int, error) {
	return f.refs, 0, f.rec(call{name: "References", id: id})
}

func (f *fake) Singleton(_ context.Context, typ string, o content.GetOptions) (*content.View, error) {
	return f.view, f.rec(call{name: "Singleton", typ: typ, arg: o})
}

func (f *fake) UpdateSingleton(_ context.Context, typ string, in content.UpdateInput) (*content.View, error) {
	return f.view, f.rec(call{name: "UpdateSingleton", typ: typ, arg: in})
}

func (f *fake) PublishSingleton(_ context.Context, typ string, o content.PublishOptions) (*content.View, error) {
	return f.view, f.rec(call{name: "PublishSingleton", typ: typ, arg: o})
}

var testID = ids.UUID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func testView() *content.View {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	return &content.View{ID: testID, Type: "article", Locale: "en", MainLocale: "en", Status: content.StatusDraft,
		Version: 1, Name: "Hello", Fields: map[string]any{"title": "Hello"}, Locales: []string{"en"},
		DraftParts: []string{"en"}, CreatedAt: at, UpdatedAt: at}
}

func newFake(tb testing.TB) *fake {
	tb.Helper()
	return &fake{sc: testSch(tb), view: testView(), list: &content.ListResult{}}
}
