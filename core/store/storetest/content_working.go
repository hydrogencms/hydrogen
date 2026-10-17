package storetest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func replaceIndex(t *testing.T, db store.DB, id ids.UUID, p store.Projection, rows ...store.IndexRow) {
	t.Helper()
	withContent(t, db, func(c store.ContentStore) error { return c.ReplaceIndex(context.Background(), id, p, rows) })
}

func replaceSearch(t *testing.T, db store.DB, id ids.UUID, p store.Projection, docs ...store.SearchDoc) {
	t.Helper()
	withContent(t, db, func(c store.ContentStore) error { return c.ReplaceSearch(context.Background(), id, p, docs) })
}

func replaceModule(t *testing.T, db store.DB, id ids.UUID, module string, rows ...store.IndexRow) {
	t.Helper()
	withContent(t, db, func(c store.ContentStore) error { return c.ReplaceModuleIndex(context.Background(), id, module, rows) })
}

func testWorkingIndex(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	a, b, d := cat.ids['A'], cat.ids['B'], cat.ids['D']
	q := func(working bool, w []store.Cond, sort ...store.Sort) string {
		return cat.sorted(t, db, store.Query{Type: "article", Locale: "en", Working: working, Where: w, Sort: sort})
	}
	rating := func(v float64) []store.Cond { return where("rating", store.ColNum, store.OpEq, v) }

	// Working rows start empty: filters find nothing until they are written, but items always list.
	if got := q(true, nil); got != "ABCDE" {
		t.Errorf("working listing without filters sees every item: %q", got)
	}
	if got := q(true, rating(5)); got != "" {
		t.Errorf("working rows before any write: %q", got)
	}
	replaceIndex(t, db, a, store.ProjectionWorking, num("", "rating", 9), txt("", "title", "avocado"))
	replaceIndex(t, db, d, store.ProjectionWorking, num("", "rating", 1), txt("", "title", "delta"))
	if got := q(true, rating(9)); got != "A" {
		t.Errorf("working rating 9: %q", got)
	}
	if got := q(false, rating(9)); got != "" {
		t.Errorf("published sees a working row: %q", got)
	}
	if got := q(false, rating(5)); got != "A" {
		t.Errorf("published row lost: %q", got)
	}
	if got := q(true, rating(5)); got != "" {
		t.Errorf("working sees a published row: %q", got)
	}
	if got := q(true, rating(1)); got != "D" {
		t.Errorf("never-published item by working row: %q", got)
	}

	// Writing one projection leaves the other alone.
	replaceIndex(t, db, a, store.ProjectionPublished, num("", "rating", 6))
	if got := q(false, rating(6)); got != "A" || q(true, rating(6)) != "" || q(true, rating(9)) != "A" {
		t.Errorf("published write touched the working rows")
	}
	replaceIndex(t, db, a, store.ProjectionWorking, num("", "rating", 7))
	if got := q(true, rating(9)); got != "" {
		t.Errorf("working rows not replaced: %q", got)
	}
	if got := q(true, where("title", store.ColText, store.OpExists, true)); got != "D" {
		t.Errorf("old working title row survived: %q", got)
	}
	if got := q(false, rating(6)); got != "A" {
		t.Errorf("published row lost after a working write: %q", got)
	}

	// Sorting walks the working index; items without a working value come last in id order.
	replaceIndex(t, db, b, store.ProjectionWorking, num("", "rating", 2))
	asc := []store.Sort{{Field: "rating", Column: store.ColNum}}
	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Working: true, Sort: asc})
	if got := cat.letters(res); got != "DBACE" || res.Total != 5 {
		t.Errorf("working sort asc: %q total %d", got, res.Total)
	}
	res = cat.run(t, db, store.Query{Type: "article", Locale: "en", Working: true, Sort: []store.Sort{{Field: "rating", Column: store.ColNum, Desc: true}}})
	if got := cat.letters(res); got != "ABDEC" {
		t.Errorf("working sort desc: %q", got)
	}
	res = cat.run(t, db, store.Query{Type: "article", Locale: "en", Working: true, Sort: asc, Limit: 2, Offset: 3})
	if got := cat.letters(res); got != "CE" || res.Total != 5 {
		t.Errorf("working sort page: %q total %d", got, res.Total)
	}
	res = cat.run(t, db, store.Query{Type: "article", Locale: "en", Sort: asc})
	if got := cat.letters(res); got != "BCAED" {
		t.Errorf("published sort: %q", got)
	}
	if got := q(true, where("rating", store.ColNum, store.OpLt, 8.0), asc[0]); got != "DBA" {
		t.Errorf("working range + sort: %q", got)
	}

	replaceIndex(t, db, a, store.ProjectionWorking)
	if got := q(true, where("rating", store.ColNum, store.OpExists, true)); got != "BD" {
		t.Errorf("clearing the working rows: %q", got)
	}
	if got := q(false, rating(6)); got != "A" {
		t.Errorf("clearing the working rows touched the published ones: %q", got)
	}
}

func testWorkingSearch(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	a, b, d := cat.ids['A'], cat.ids['B'], cat.ids['D']
	s := func(working bool, text string) string {
		return cat.sorted(t, db, store.Query{Type: "article", Locale: "en", Working: working, Search: text})
	}
	replaceSearch(t, db, a, store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "published words"})
	replaceSearch(t, db, a, store.ProjectionWorking, store.SearchDoc{Locale: "en", Text: "edited words"}, store.SearchDoc{Locale: "de", Text: "geaendert"})
	replaceSearch(t, db, d, store.ProjectionWorking, store.SearchDoc{Locale: "en", Text: "unreleased words"})
	replaceSearch(t, db, b, store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "published only"})

	for _, tc := range []struct {
		working          bool
		text, want, what string
	}{
		{false, "published", "AB", "published rows"},
		{false, "edited", "", "published ignores working text"},
		{true, "edited", "A", "working text"},
		{true, "published", "", "working ignores published text"},
		{true, "words", "AD", "never-published item is searchable"},
		{false, "words", "A", "published words"},
		{true, "unreleased", "D", "never-published item by its own text"},
	} {
		if got := s(tc.working, tc.text); got != tc.want {
			t.Errorf("%s: search %q working=%v = %q, want %q", tc.what, tc.text, tc.working, got, tc.want)
		}
	}
	if got := cat.sorted(t, db, store.Query{Type: "article", Locale: "de", Working: true, Search: "geaendert"}); got != "A" {
		t.Errorf("working search in de: %q", got)
	}

	// Replacing one projection keeps the other; both survive repeated writes.
	replaceSearch(t, db, a, store.ProjectionWorking, store.SearchDoc{Locale: "en", Text: "second edit"})
	if s(true, "edited") != "" || s(true, "second") != "A" || s(false, "published") != "AB" {
		t.Errorf("replace working search: %q %q %q", s(true, "edited"), s(true, "second"), s(false, "published"))
	}
	replaceSearch(t, db, a, store.ProjectionWorking)
	if got := s(true, "second"); got != "" {
		t.Errorf("cleared working search: %q", got)
	}
	if got := s(false, "published"); got != "AB" {
		t.Errorf("clearing working search touched published: %q", got)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(context.Background(), d) })
	if got := s(true, "unreleased"); got != "" {
		t.Errorf("working search rows survived the item: %q", got)
	}
}

func testWorkingNames(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	names := func(locale string, working bool) string {
		res := cat.run(t, db, store.Query{Type: "article", Locale: locale, Working: working, Sort: []store.Sort{{Field: "name"}}})
		var parts []string
		for _, it := range res.Items {
			parts = append(parts, it.Name)
		}
		return strings.Join(parts, "|")
	}
	// E: published de "Aaron", en "Echo"; drafts: shared, de "Draft Aaron". D: never published, draft "delta".
	if got := names("de", false); got != "Aaron|Alpha|Bravo|Charlie|delta" {
		t.Errorf("published de names: %s", got)
	}
	if got := names("de", true); got != "Alpha|Bravo|Charlie|Draft Aaron|delta" {
		t.Errorf("working de names: %s", got)
	}
	if got := names("en", false); got != "Alpha|Bravo|Charlie|Echo|delta" {
		t.Errorf("published en names: %s", got)
	}
	if got := names("en", true); got != "Alpha|Bravo|Charlie|Echo|delta" {
		t.Errorf("working en names (no en draft): %s", got)
	}
	for _, it := range cat.run(t, db, store.Query{Type: "article", Locale: "de", Working: true}).Items {
		if cat.label[it.ID] == 'E' && it.Name != "Draft Aaron" {
			t.Errorf("E working de name: %q", it.Name)
		}
	}
	for _, it := range cat.run(t, db, store.Query{Type: "article", Locale: "de", Working: false}).Items {
		if cat.label[it.ID] == 'E' && it.Name != "Aaron" {
			t.Errorf("E published de name: %q", it.Name)
		}
	}
}

func testModuleIndex(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	a, b := cat.ids['A'], cat.ids['B']
	q := func(working bool, w []store.Cond) string {
		return cat.sorted(t, db, store.Query{Type: "article", Locale: "en", Working: working, Where: w})
	}
	site := func(v string) []store.Cond { return where("pages.site", store.ColText, store.OpEq, v) }

	replaceModule(t, db, a, "pages", txt("", "pages.site", "s1"), num("", "pages.depth", 2))
	replaceModule(t, db, b, "pages", txt("", "pages.site", "s2"), num("", "pages.depth", 1))
	for _, working := range []bool{false, true} {
		if got := q(working, site("s1")); got != "A" {
			t.Errorf("module row, working=%v: %q", working, got)
		}
		if got := q(working, where("pages.depth", store.ColNum, store.OpLt, 3.0)); got != "AB" {
			t.Errorf("module range, working=%v: %q", working, got)
		}
		res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Working: working,
			Where: where("pages.site", store.ColText, store.OpExists, true), Sort: []store.Sort{{Field: "pages.depth", Column: store.ColNum}}})
		if got := cat.letters(res); got != "BA" {
			t.Errorf("sort by module field, working=%v: %q", working, got)
		}
	}

	// Content writes of either projection leave module rows alone.
	replaceIndex(t, db, a, store.ProjectionPublished, num("", "rating", 5))
	replaceIndex(t, db, a, store.ProjectionWorking, num("", "rating", 6))
	replaceIndex(t, db, a, store.ProjectionWorking)
	for _, working := range []bool{false, true} {
		if got := q(working, site("s1")); got != "A" {
			t.Errorf("module row lost to a content write, working=%v: %q", working, got)
		}
	}

	// A module write replaces its own rows only, not content rows or another module's.
	replaceModule(t, db, a, "other", txt("", "other.tag", "t"))
	replaceModule(t, db, a, "pages", txt("", "pages.site", "s3"))
	for _, working := range []bool{false, true} {
		if got := q(working, site("s1")); got != "" {
			t.Errorf("module rows not replaced, working=%v: %q", working, got)
		}
		if got := q(working, site("s3")); got != "A" {
			t.Errorf("new module row, working=%v: %q", working, got)
		}
		if got := q(working, where("other.tag", store.ColText, store.OpEq, "t")); got != "A" {
			t.Errorf("other module's rows replaced, working=%v: %q", working, got)
		}
		if got := q(working, where("pages.depth", store.ColNum, store.OpExists, true)); got != "B" {
			t.Errorf("stale module row, working=%v: %q", working, got)
		}
	}
	if got := q(false, where("rating", store.ColNum, store.OpEq, 5.0)); got != "A" {
		t.Errorf("content row lost to a module write: %q", got)
	}
	replaceModule(t, db, a, "pages")
	if got := q(true, site("s3")); got != "" {
		t.Errorf("cleared module rows: %q", got)
	}

	for name, rows := range map[string][]store.IndexRow{
		"foreign prefix": {txt("", "other.x", "v")},
		"no prefix":      {txt("", "site", "v")},
		"two values":     {{Field: "pages.x", Text: str("a"), Num: new(float64)}},
	} {
		if err := expectErr(db, func(c store.ContentStore) error { return c.ReplaceModuleIndex(ctx, a, "pages", rows) }); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := expectErr(db, func(c store.ContentStore) error { return c.ReplaceModuleIndex(ctx, a, "", nil) }); err == nil {
		t.Error("empty module name accepted")
	}

	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, b) })
	if got := q(true, where("pages.depth", store.ColNum, store.OpExists, true)); got != "" {
		t.Errorf("module rows survived the item: %q", got)
	}
}

// seedScope creates ten published docs a..j with creators, content fields, search text and module rows:
//
//	creator  user:a: a d g j   user:b: b e h   user:c: c f i
//	rank     0..8 (j has none); group even/odd by rank; pages.site s1 for a..e, s2 for f..j; "gadget" text on evens
func seedScope(t *testing.T, db store.DB) *catalog {
	t.Helper()
	ctx := context.Background()
	g := newIDs()
	cat := &catalog{ids: map[byte]ids.UUID{}, label: map[ids.UUID]byte{}}
	withContent(t, db, func(c store.ContentStore) error {
		for i := range 10 {
			label := byte('a' + i)
			id := g.New()
			cat.ids[label], cat.label[id] = id, label
			it := newItem(id, "doc", "")
			it.PublishedVersion = 1
			it.CreatedBy = "user:" + string(rune('a'+i%3))
			it.CreatedAt = t0.Add(time.Duration(i) * time.Minute)
			it.UpdatedAt = it.CreatedAt
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
			group := "even"
			if i%2 == 1 {
				group = "odd"
			}
			rows := []store.IndexRow{txt("", "group", group)}
			if i < 9 {
				rows = append(rows, num("", "rank", float64(i)))
			}
			site := "s1"
			if i >= 5 {
				site = "s2"
			}
			for _, p := range []store.Projection{store.ProjectionPublished, store.ProjectionWorking} {
				if err := c.ReplaceIndex(ctx, id, p, rows); err != nil {
					return err
				}
				text := "plain"
				if i%2 == 0 {
					text = "gadget"
				}
				if err := c.ReplaceSearch(ctx, id, p, []store.SearchDoc{{Locale: "en", Text: text}}); err != nil {
					return err
				}
			}
			if err := c.ReplaceModuleIndex(ctx, id, "pages", []store.IndexRow{txt("", "pages.site", site)}); err != nil {
				return err
			}
		}
		return nil
	})
	return cat
}

func testQueryScope(t *testing.T, db store.DB) {
	cat := seedScope(t, db)
	by := func(who ...string) []string {
		out := make([]string, len(who))
		for i, w := range who {
			out[i] = "user:" + w
		}
		return out
	}
	site := func(v string) store.Cond { return where("pages.site", store.ColText, store.OpEq, v)[0] }
	group := func(v string) store.Cond { return where("group", store.ColText, store.OpEq, v)[0] }
	byRank := []store.Sort{{Field: "rank", Column: store.ColNum}}
	scoped := func(terms ...store.ScopeTerm) store.Query {
		return store.Query{Type: "doc", Locale: "en", Scoped: true, Scope: terms, Sort: byRank}
	}
	check := func(name string, q store.Query, want string, total int) {
		t.Helper()
		res := cat.run(t, db, q)
		if got := cat.letters(res); got != want || res.Total != total {
			t.Errorf("%s: %q total %d, want %q total %d", name, got, res.Total, want, total)
		}
	}

	check("unrestricted", store.Query{Type: "doc", Locale: "en", Sort: byRank}, "abcdefghij", 10)
	check("no terms matches nothing", scoped(), "", 0)
	check("no terms, offset", func() store.Query { q := scoped(); q.Offset = 3; q.Limit = 5; return q }(), "", 0)
	check("terms without Scoped are ignored", store.Query{Type: "doc", Locale: "en", Scope: []store.ScopeTerm{{CreatedBy: by("a")}}, Sort: byRank},
		"abcdefghij", 10)
	check("creator", scoped(store.ScopeTerm{CreatedBy: by("a")}), "adgj", 4)
	check("creators", scoped(store.ScopeTerm{CreatedBy: by("a", "b")}), "abdeghj", 7)
	check("unknown creator", scoped(store.ScopeTerm{CreatedBy: by("zz")}), "", 0)
	check("where", scoped(store.ScopeTerm{Where: []store.Cond{site("s2")}}), "fghij", 5)
	check("creator and where", scoped(store.ScopeTerm{CreatedBy: by("a"), Where: []store.Cond{site("s2")}}), "gj", 2)
	check("two conds", scoped(store.ScopeTerm{Where: []store.Cond{site("s1"), group("odd")}}), "bd", 2)
	check("or of terms", scoped(
		store.ScopeTerm{CreatedBy: by("a"), Where: []store.Cond{site("s1")}},
		store.ScopeTerm{CreatedBy: by("c"), Where: []store.Cond{site("s2")}}), "adfi", 4)
	check("overlapping terms count once", scoped(
		store.ScopeTerm{CreatedBy: by("a")}, store.ScopeTerm{Where: []store.Cond{group("even")}}), "acdegij", 7)
	check("empty term is unrestricted", scoped(store.ScopeTerm{}), "abcdefghij", 10)
	check("empty term among others", scoped(store.ScopeTerm{CreatedBy: by("zz")}, store.ScopeTerm{}), "abcdefghij", 10)
	check("empty in-list matches nothing", scoped(store.ScopeTerm{Where: []store.Cond{{Field: "group", Column: store.ColText, Op: store.OpIn, Value: []string{}}}}), "", 0)
	check("empty in-list term beside another", scoped(
		store.ScopeTerm{Where: []store.Cond{{Field: "group", Column: store.ColText, Op: store.OpIn, Value: []string{}}}},
		store.ScopeTerm{CreatedBy: by("b")}), "beh", 3)

	// Scope combines with the query's own conditions, search, sort, paging and availability.
	q := scoped(store.ScopeTerm{CreatedBy: by("a", "b")})
	q.Where = []store.Cond{group("even")}
	check("scope and where", q, "aeg", 3)
	q = scoped(store.ScopeTerm{CreatedBy: by("a", "b")})
	q.Limit, q.Offset = 3, 2
	res := cat.run(t, db, q)
	full := cat.run(t, db, scoped(store.ScopeTerm{CreatedBy: by("a", "b")}))
	if res.Total != 7 || cat.letters(res) != cat.letters(full)[2:5] {
		t.Errorf("paging: %q total %d of %q", cat.letters(res), res.Total, cat.letters(full))
	}
	q.Offset = 20
	check("past the end keeps the total", q, "", 7)
	q = scoped(store.ScopeTerm{CreatedBy: by("a")})
	q.Where = []store.Cond{{Field: "rank", Column: store.ColNum, Op: store.OpGte, Value: 0.0}}
	check("scope and range", q, "adg", 3)
	q = scoped(store.ScopeTerm{CreatedBy: by("a")})
	q.Sort = []store.Sort{{Field: "rank", Column: store.ColNum, Desc: true}}
	check("walk descending, missing last", q, "gdaj", 4)
	q.Limit, q.Offset = 2, 3
	check("walk missing phase", q, "j", 4)
	q = scoped(store.ScopeTerm{CreatedBy: by("a")})
	q.Search = "gadget"
	check("scope and search", q, "ag", 2)
	q = scoped(store.ScopeTerm{CreatedBy: by("a")})
	q.Available = []string{"en"}
	check("scope and available", q, "adgj", 4)
	q.Available = []string{}
	check("scope and always-available", q, "adgj", 4)

	// Working scope reads working rows for its conditions.
	d := cat.ids['d']
	replaceIndex(t, db, d, store.ProjectionWorking, txt("", "group", "even"), num("", "rank", 3))
	q = scoped(store.ScopeTerm{CreatedBy: by("a"), Where: []store.Cond{group("odd")}})
	check("published scope condition", q, "dj", 2)
	q.Working = true
	check("working scope condition", q, "j", 1)
}
