package tests

import (
	"fmt"
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// seedPage inserts a published page item with two locales straight into the content store (the content
// service would make seeding thousands of pages dominate the benchmark) and places it.
func (e *pagesEnv) seedPage(typ, title, de string, parent *ids.UUID, rev int) (ids.UUID, ids.UUID) {
	e.t.Helper()
	id := e.app.Services().IDs.New()
	now := e.clk.Now()
	err := e.db.Tx(ctx, func(tx store.Tx) error {
		c := tx.Content()
		if err := c.InsertItem(ctx, store.Item{ID: id, Type: typ, MainLocale: "en", PublishedVersion: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		loc := func(code, t string) store.VersionLocale {
			slug := schemaSlug(t)
			return store.VersionLocale{Locale: code, Name: t, Data: fmt.Appendf(nil, `{"slug":%q,"title":%q}`, slug, t)}
		}
		return c.InsertVersion(ctx, store.Version{ItemID: id, Version: 1, SchemaRev: rev, Shared: []byte("{}"),
			Locales: []store.VersionLocale{loc("de", de), loc("en", title)}, CreatedAt: now})
	})
	if err != nil {
		e.t.Fatal(err)
	}
	n, err := e.pages.Place(ctx, "main", pages.Place{Item: id, Parent: parent})
	if err != nil {
		e.t.Fatal(err)
	}
	return id, n.ID
}

func schemaSlug(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range []byte(s) {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r == ' ':
			out = append(out, '-')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// bigTree builds root > top (1 + 40 sections + 1,960 landings, two locales), and two destination sections.
func bigTree(b *testing.B, e *pagesEnv) (top, destA, destB ids.UUID) {
	rev, err := e.c.EnsureSchemaRev(ctx)
	if err != nil {
		b.Fatal(err)
	}
	_, root := e.seedPage("home", "Home", "Start", nil, rev)
	_, destA = e.seedPage("section", "Dest A", "Ziel A", &root, rev)
	_, destB = e.seedPage("section", "Dest B", "Ziel B", &root, rev)
	_, top = e.seedPage("section", "Top", "Oben", &root, rev)
	for i := range 40 {
		_, sec := e.seedPage("section", fmt.Sprintf("Section %d", i), fmt.Sprintf("Bereich %d", i), &top, rev)
		for j := range 49 {
			e.seedPage("landing", fmt.Sprintf("Page %d", j), fmt.Sprintf("Seite %d", j), &sec, rev)
		}
	}
	return top, destA, destB
}

func BenchmarkPagesMoveSubtree2000(b *testing.B) {
	forEachBackend2(b, func(b *testing.B, e *pagesEnv) {
		top, destA, destB := bigTree(b, e)
		if r := e.resolveTB(b, "de", "/ziel-a"); r.Kind != pages.ResolvedPage {
			b.Fatalf("setup: %+v", r)
		}
		dests := []ids.UUID{destA, destB}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			e.moveTB(b, top, dests[i%2])
		}
		b.StopTimer()
		if r := e.resolveTB(b, "de", fmt.Sprintf("/%s/oben/bereich-3/seite-7", map[int]string{0: "ziel-a", 1: "ziel-b"}[(b.N-1)%2])); r.Kind != pages.ResolvedPage {
			b.Fatalf("after moves: %+v", r)
		}
	})
}

func BenchmarkPagesResolve(b *testing.B) {
	forEachBackend2(b, func(b *testing.B, e *pagesEnv) {
		bigTree(b, e)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r, err := e.pages.Resolve(ctx, "main", "de", "/oben/bereich-3/seite-7")
			if err != nil || r.Kind != pages.ResolvedPage || len(r.Breadcrumbs) != 4 {
				b.Fatalf("resolve: %+v, %v", r, err)
			}
		}
	})
}

func forEachBackend2(b *testing.B, fn func(b *testing.B, e *pagesEnv)) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			clk := newClock()
			fn(b, newPagesEnv(b, be.open(b, clk), clk, pagesOpts{}))
		})
	}
}

func (e *pagesEnv) resolveTB(b testing.TB, locale, path string) *pages.Resolution {
	b.Helper()
	r, err := e.pages.Resolve(ctx, "main", locale, path)
	if err != nil {
		b.Fatal(err)
	}
	return r
}

func (e *pagesEnv) moveTB(b testing.TB, node, parent ids.UUID) {
	b.Helper()
	if _, err := e.pages.Update(ctx, node, pages.NodeUpdate{Move: &pages.Move{Parent: parent}}); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkPagesTree2000(b *testing.B) {
	forEachBackend2(b, func(b *testing.B, e *pagesEnv) {
		bigTree(b, e)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nodes, err := e.pages.Tree(ctx, "main", "de", nil, 3)
			if err != nil || len(nodes) != 3 {
				b.Fatalf("tree: %d nodes, %v", len(nodes), err)
			}
		}
	})
}
