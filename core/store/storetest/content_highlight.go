package storetest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func testQueryTypes(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	for _, l := range []byte("APB") {
		replaceSearch(t, db, cat.ids[l], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "shared marker"})
	}
	for _, tc := range []struct {
		name string
		q    store.Query
		want string
	}{
		{"person and article", store.Query{Types: []string{"person", "article"}}, "ABCDEP"},
		{"one of two", store.Query{Types: []string{"person"}}, "P"},
		{"unknown", store.Query{Types: []string{"nope"}}, ""},
		{"with Type", store.Query{Type: "article", Types: []string{"person"}}, ""},
		{"with matching Type", store.Query{Type: "person", Types: []string{"person", "nope"}}, "P"},
		{"with search", store.Query{Types: []string{"person", "article"}, Search: "marker"}, "ABP"},
		{"search narrowed", store.Query{Types: []string{"person"}, Search: "marker"}, "P"},
		{"with status", store.Query{Types: []string{"person", "article"}, Status: store.StatusDraft}, "DE"},
	} {
		tc.q.Locale = "en"
		res := cat.run(t, db, tc.q)
		if got := sortString(cat.letters(res)); got != tc.want || res.Total != len(tc.want) {
			t.Errorf("%s: %q total %d, want %q", tc.name, got, res.Total, tc.want)
		}
	}
}

func testHighlight(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	long := strings.Repeat("filler words go here ", 30) + "the zebrafish swims " + strings.Repeat("more filler text follows ", 30)
	replaceSearch(t, db, cat.ids['A'], store.ProjectionPublished,
		store.SearchDoc{Locale: "en", Text: long}, store.SearchDoc{Locale: "de", Text: "ein Zebrafisch schwimmt"})
	replaceSearch(t, db, cat.ids['D'], store.ProjectionWorking, store.SearchDoc{Locale: "en", Text: "unreleased zebrafish draft"})
	replaceSearch(t, db, cat.ids['B'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "Zebrafish and zebrafishes"})

	snippets := func(q store.Query) map[byte]string {
		q.Locale = "en"
		res := cat.run(t, db, q)
		out := map[byte]string{}
		for _, it := range res.Items {
			out[cat.label[it.ID]] = it.Snippet
		}
		return out
	}

	got := snippets(store.Query{Type: "article", Search: "zebrafish", Highlight: true})
	if len(got) != 2 {
		t.Fatalf("highlight results: %v", got)
	}
	if s := got['A']; !strings.Contains(s, "\x02zebrafish\x03") {
		t.Errorf("A snippet lacks a marked match: %q", s)
	} else if n := len(strings.Fields(s)); n > 40 || len(s) >= len(long)/2 {
		t.Errorf("A snippet is not a fragment (%d words, %d bytes): %q", n, len(s), s)
	}
	if s := got['B']; !strings.Contains(s, "\x02Zebrafish\x03") || !strings.Contains(s, "\x02zebrafishes\x03") {
		t.Errorf("B snippet must keep the text's case and mark prefix matches: %q", s)
	}
	for _, s := range got {
		if strings.Contains(s, "<") || strings.Count(s, "\x02") != strings.Count(s, "\x03") {
			t.Errorf("snippet must be raw text with paired markers: %q", s)
		}
	}

	for _, tc := range []struct {
		name string
		q    store.Query
	}{
		{"without Highlight", store.Query{Type: "article", Search: "zebrafish"}},
		{"without Search", store.Query{Type: "article", Highlight: true}},
		{"punctuation only", store.Query{Type: "article", Search: "!!", Highlight: true}},
	} {
		for l, s := range snippets(tc.q) {
			if s != "" {
				t.Errorf("%s: %c has snippet %q", tc.name, l, s)
			}
		}
	}

	if s := snippets(store.Query{Type: "article", Search: "zebrafish", Highlight: true, Sort: []store.Sort{{Field: "name"}}, Limit: 1})['A']; !strings.Contains(s, "\x02") {
		t.Errorf("snippet with sort and limit: %q", s)
	}

	w := snippets(store.Query{Type: "article", Search: "zebrafish", Highlight: true, Working: true})
	if len(w) != 1 || w['D'] != "unreleased \x02zebrafish\x03 draft" {
		t.Errorf("working projection snippets: %q", w)
	}

	de := cat.run(t, db, store.Query{Type: "article", Locale: "de", Search: "zebrafisch", Highlight: true})
	if len(de.Items) != 1 || de.Items[0].Snippet != "ein \x02Zebrafisch\x03 schwimmt" {
		t.Errorf("de snippet: %+v", de.Items)
	}
	if res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "zebrafisch", Highlight: true}); len(res.Items) != 0 {
		t.Errorf("de text matched in en: %+v", res.Items)
	}
}

func testSearchTiers(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	pub := store.ProjectionPublished
	rank := func(text string) string {
		res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: text, Highlight: true})
		for i := 1; i < len(res.Items); i++ {
			if res.Items[i-1].Searched < res.Items[i].Searched {
				t.Errorf("%q: not ordered by Searched: %+v", text, res.Items)
			}
		}
		return cat.letters(res)
	}

	replaceSearch(t, db, cat.ids['A'], pub, store.SearchDoc{Locale: "en", Low: "zebra", Text: "filler"})
	replaceSearch(t, db, cat.ids['B'], pub, store.SearchDoc{Locale: "en", Text: "zebra"})
	replaceSearch(t, db, cat.ids['E'], pub, store.SearchDoc{Locale: "en", High: "zebra", Text: "filler"})
	if got := rank("zebra"); got != "EBA" {
		t.Errorf("high, normal, low: %q", got)
	}

	replaceSearch(t, db, cat.ids['A'], pub, store.SearchDoc{Locale: "en", High: "solar panels", Text: "an article about energy"})
	replaceSearch(t, db, cat.ids['B'], pub, store.SearchDoc{Locale: "en", Text: "energy from solar panels and solar heat, solar roofs and solar farms"})
	if got := rank("solar"); got != "AB" {
		t.Errorf("a title match outranks repeated body matches: %q", got)
	}
	if got := rank("sol pan"); got != "AB" {
		t.Errorf("prefixes: %q", got)
	}
	if got := sortString(rank("energy")); got != "AB" {
		t.Errorf("every tier is searched: %q", got)
	}

	replaceSearch(t, db, cat.ids['A'], pub, store.SearchDoc{Locale: "en", Text: "solar panels"})
	replaceSearch(t, db, cat.ids['B'], pub, store.SearchDoc{Locale: "en", High: "solar panels"})
	if got := rank("solar"); got != "BA" {
		t.Errorf("a changed weight changes the order: %q", got)
	}

	replaceSearch(t, db, cat.ids['A'], pub, store.SearchDoc{Locale: "en", High: "zebra title", Text: "a grazing zebra"}, store.SearchDoc{Locale: "en", Low: "zebra note"})
	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "zebra", Highlight: true})
	var snippet string
	for _, it := range res.Items {
		if cat.label[it.ID] == 'A' {
			snippet = it.Snippet
		}
	}
	if !strings.Contains(snippet, "\x02zebra\x03") {
		t.Errorf("snippet over tiers: %q", snippet)
	}

	replaceSearch(t, db, cat.ids['A'], pub)
	replaceSearch(t, db, cat.ids['B'], pub)
	if got := rank("solar"); got != "" {
		t.Errorf("rows removed: %q", got)
	}
}

func testSearchFallback(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	withContent(t, db, func(c store.ContentStore) error {
		b, err := c.GetItem(ctx, cat.ids['B'], false)
		if err != nil {
			return err
		}
		b.AlwaysAvailable = false
		return c.UpdateItem(ctx, b)
	})
	pub := store.ProjectionPublished
	replaceSearch(t, db, cat.ids['A'], pub, store.SearchDoc{Locale: "en", Text: "a lonely zebra grazes"})
	replaceSearch(t, db, cat.ids['B'], pub, store.SearchDoc{Locale: "en", Text: "another zebra"})
	replaceSearch(t, db, cat.ids['E'], pub, store.SearchDoc{Locale: "en", Text: "english zebra"}, store.SearchDoc{Locale: "de", Text: "nur deutsch"})

	run := func(locale, text string, fallback bool) store.QueryResult {
		return cat.run(t, db, store.Query{Type: "article", Locale: locale, Search: text, SearchFallback: fallback, Highlight: true})
	}
	if got := cat.letters(run("de", "zebra", false)); got != "" {
		t.Errorf("without fallback: %q", got)
	}
	res := run("de", "zebra", true)
	if got := cat.letters(res); got != "A" || res.Total != 1 || res.Items[0].Searched <= 0 || res.Items[0].Snippet != "a lonely \x02zebra\x03 grazes" {
		t.Errorf("fallback in de: %q %+v", got, res.Items)
	}
	if got := cat.letters(run("de", "deutsch", true)); got != "E" {
		t.Errorf("own document: %q", got)
	}
	if got := sortString(cat.letters(run("en", "zebra", true))); got != "ABE" {
		t.Errorf("fallback in the main locale: %q", got)
	}

	facet := func(q store.Query) string {
		var out []string
		viewContent(t, db, func(c store.ContentStore) error {
			got, err := c.Facet(ctx, q, "tags", 100)
			for _, f := range got {
				out = append(out, fmt.Sprintf("%s=%d", f.Value, f.Count))
			}
			return err
		})
		return strings.Join(out, " ")
	}
	if got := facet(store.Query{Type: "article", Locale: "de", Search: "zebra", SearchFallback: true}); got != "db=1 go=1" {
		t.Errorf("facet with fallback: %q", got)
	}
	if got := facet(store.Query{Type: "article", Locale: "de", Search: "zebra"}); got != "" {
		t.Errorf("facet without fallback: %q", got)
	}
}

func testFacetSearch(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	replaceSearch(t, db, cat.ids['A'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "fox quick"})
	replaceSearch(t, db, cat.ids['B'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "fox"})
	replaceSearch(t, db, cat.ids['E'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "quick"})
	facet := func(q store.Query) string {
		t.Helper()
		var out []string
		viewContent(t, db, func(c store.ContentStore) error {
			got, err := c.Facet(ctx, q, "tags", 100)
			for _, f := range got {
				out = append(out, fmt.Sprintf("%s=%d", f.Value, f.Count))
			}
			return err
		})
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		name string
		q    store.Query
		want string
	}{
		{"fox", store.Query{Type: "article", Search: "fox"}, "go=2 db=1"},
		{"quick", store.Query{Type: "article", Search: "quick"}, "db=1 go=1 rust=1"},
		{"both", store.Query{Search: "fox quick"}, "db=1 go=1"},
		{"prefix", store.Query{Types: []string{"article"}, Search: "qui"}, "db=1 go=1 rust=1"},
		{"no match", store.Query{Search: "missing"}, ""},
		{"punctuation", store.Query{Search: "!!"}, ""},
		{"other type", store.Query{Types: []string{"person"}, Search: "fox"}, ""},
	} {
		tc.q.Locale = "en"
		if got := facet(tc.q); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func testPublishedAtSort(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	putSnaps(t, db, cat.ids['A'], snapAt(cat.ids['A'], "en", t0.Add(time.Hour)), snapAt(cat.ids['A'], "de", t0.Add(3*time.Hour)))
	putSnaps(t, db, cat.ids['B'], snapAt(cat.ids['B'], "en", t0.Add(2*time.Hour)))
	for _, desc := range []bool{false, true} {
		res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Sort: []store.Sort{{Field: "publishedAt", Desc: desc}}})
		got := cat.letters(res)
		want := "BACDE"
		if desc {
			want = "ABCDE"
		}
		if len(got) != 5 || got[:2] != want[:2] {
			t.Errorf("publishedAt desc=%v: %q, want %q first and the unpublished-snapshot items last", desc, got, want[:2])
		}
	}
}

func snapAt(item ids.UUID, locale string, at time.Time) store.Snapshot {
	s := snap(item, "article", "", locale, 1, `{}`)
	s.PublishedAt = at
	return s
}

func testSearchIndexEmpty(t *testing.T, db store.DB) {
	ctx := context.Background()
	empty := func() bool {
		var got bool
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			got, err = c.SearchIndexEmpty(ctx)
			return err
		})
		return got
	}
	if empty() {
		t.Error("no items: not empty")
	}
	cat := seedCatalog(t, db)
	if !empty() {
		t.Error("published items without search rows: empty")
	}
	replaceSearch(t, db, cat.ids['A'], store.ProjectionWorking, store.SearchDoc{Locale: "en", Text: "draft"})
	if !empty() {
		t.Error("working rows alone: empty (no published rows)")
	}
	replaceSearch(t, db, cat.ids['A'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "text"})
	if empty() {
		t.Error("with published and working search rows: not empty")
	}
	replaceSearch(t, db, cat.ids['A'], store.ProjectionWorking)
	if !empty() {
		t.Error("published rows alone: empty (no working rows)")
	}
}

func testSearchControlChars(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	replaceSearch(t, db, cat.ids['A'], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "forged \x02mark\x03 and\ttab zebra"})
	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "zebra", Highlight: true})
	if len(res.Items) != 1 || res.Items[0].Snippet != "forged mark and\ttab \x02zebra\x03" {
		t.Errorf("control characters must not reach the snippet: %+v", res.Items)
	}
}

func testQueryNoTotal(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", NoTotal: true, Limit: 2, Offset: 1})
	if len(res.Items) != 2 || res.Total != 3 {
		t.Errorf("NoTotal page: %d items, total %d", len(res.Items), res.Total)
	}
	if res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Limit: 2, Offset: 1}); res.Total != 5 {
		t.Errorf("total without NoTotal: %d", res.Total)
	}
}
