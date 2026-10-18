package tests

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
)

const seSchema = `
types:
  article:
    name: "{title}"
    fields:
      title:  { type: string, translatable: true }
      body:   { type: text, translatable: true }
      topic:  { type: string, index: true }
      tags:   { type: list, of: { type: string }, index: true }
      rank:   { type: number, integer: true, index: true }
      pinned: { type: boolean, index: true }
  note:
    name: "{title}"
    fields:
      title: { type: string }
      topic: { type: string, index: true }
  settings:
    singleton: true
    fields:
      site: { type: string }
`

type seEnv struct {
	*env
	eng search.Engine
}

func seRun(t *testing.T, fn func(t *testing.T, e *seEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: seSchema})
		fn(t, &seEnv{e, search.Database(e.db, search.WithSchema(buildSchema(t, seSchema)))})
	})
}

// add creates and publishes an article.
func (e *seEnv) add(fields string) ids.UUID {
	e.t.Helper()
	v := e.create("article", "en", fields)
	e.publish(v.ID)
	return v.ID
}

func (e *seEnv) search(r search.Request) search.Result {
	e.t.Helper()
	if r.Types == nil {
		r.Types = []string{"article"}
	}
	if r.Locale == "" {
		r.Locale = "en"
	}
	if r.Limit == 0 {
		r.Limit = 20
	}
	res, err := e.eng.Search(ctx, r)
	if err != nil {
		e.t.Fatalf("search %+v: %v", r, err)
	}
	return res
}

func (e *seEnv) found(r search.Request) []ids.UUID {
	e.t.Helper()
	res := e.search(r)
	out := make([]ids.UUID, len(res.Hits))
	for i, h := range res.Hits {
		out[i] = h.Item
	}
	return out
}

func seSame(t *testing.T, what string, got []ids.UUID, want ...ids.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	seen := map[ids.UUID]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

func TestSearchEngineAvailability(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		live := e.add(`{"title":"Solar panels","body":"cheap power"}`)
		off := e.add(`{"title":"Solar offline"}`)
		trashed := e.add(`{"title":"Solar trashed"}`)
		draft := e.create("article", "en", `{"title":"Solar draft"}`)
		other := e.create("note", "en", `{"title":"Solar note"}`)
		e.publish(other.ID)
		if _, err := e.svc.Unpublish(ctx, off); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Trash(ctx, trashed); err != nil {
			t.Fatal(err)
		}
		_ = draft

		seSame(t, "article hits", e.found(search.Request{Text: "solar"}), live)
		seSame(t, "both types", e.found(search.Request{Text: "solar", Types: []string{"article", "note"}}), live, other.ID)
		seSame(t, "note only", e.found(search.Request{Text: "solar", Types: []string{"note"}}), other.ID)
		seSame(t, "prefix", e.found(search.Request{Text: "sol pan"}), live)
		seSame(t, "body text", e.found(search.Request{Text: "cheap"}), live)
		seSame(t, "no match", e.found(search.Request{Text: "wind"}))
		res := e.search(search.Request{Text: "solar"})
		if res.Total != 1 || res.Estimated || res.Hits[0].Type != "article" || res.Hits[0].Locale != "en" || res.Hits[0].Score == 0 {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestSearchEngineLocales(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		both := e.create("article", "en", `{"title":"Solar panels"}`)
		e.update(both.ID, "de", `{"title":"Sonnenkollektoren"}`)
		e.publish(both.ID)
		enOnly := e.add(`{"title":"Solar roof"}`)
		always := e.create("article", "en", `{"title":"Solar always"}`)
		yes := true
		e.tick()
		if _, err := e.svc.Update(ctx, always.ID, content.UpdateInput{AlwaysAvailable: &yes}); err != nil {
			t.Fatal(err)
		}
		e.publish(always.ID)

		seSame(t, "en", e.found(search.Request{Text: "solar"}), both.ID, enOnly, always.ID)
		de := e.search(search.Request{Text: "sonnen", Locale: "de"})
		if len(de.Hits) != 1 || de.Hits[0].Item != both.ID || de.Hits[0].Locale != "de" {
			t.Errorf("de hits = %+v", de.Hits)
		}
		seSame(t, "fr", e.found(search.Request{Text: "solar", Locale: "fr"}), always.ID)

		// Text in de finds the always-available item through its main-locale document.
		fb := e.search(search.Request{Text: "always", Locale: "de"})
		if len(fb.Hits) != 1 || fb.Hits[0].Item != always.ID || fb.Hits[0].Locale != "en" {
			t.Errorf("fallback hits = %+v", fb.Hits)
		}
		seSame(t, "no fallback without always", e.found(search.Request{Text: "roof", Locale: "de"}))

		// Browsing in de: the item published in de, and the always-available one in its main locale.
		browse := e.search(search.Request{Locale: "de"})
		seSame(t, "browse de", e.found(search.Request{Locale: "de"}), both.ID, always.ID)
		for _, h := range browse.Hits {
			want := map[ids.UUID]string{both.ID: "de", always.ID: "en"}[h.Item]
			if h.Locale != want {
				t.Errorf("hit %s locale = %s, want %s", h.Item, h.Locale, want)
			}
		}
	})
}

func TestSearchEngineFilters(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		a := e.add(`{"title":"Solar a","topic":"energy","tags":["home","roof"],"rank":1,"pinned":true}`)
		b := e.add(`{"title":"Solar b","topic":"energy","tags":["roof"],"rank":5,"pinned":false}`)
		c := e.add(`{"title":"Solar c","topic":"travel","rank":3}`)
		req := func(f ...search.Filter) search.Request { return search.Request{Text: "solar", Where: f} }

		seSame(t, "eq", e.found(req(search.Filter{Field: "topic", Op: store.OpEq, Value: "energy"})), a, b)
		seSame(t, "ne", e.found(req(search.Filter{Field: "topic", Op: store.OpNe, Value: "energy"})), c)
		seSame(t, "in", e.found(req(search.Filter{Field: "topic", Op: store.OpIn, Value: []string{"travel", "x"}})), c)
		seSame(t, "prefix", e.found(req(search.Filter{Field: "topic", Op: store.OpPrefix, Value: "ene"})), a, b)
		seSame(t, "contains", e.found(req(search.Filter{Field: "tags", Op: store.OpContains, Value: "home"})), a)
		seSame(t, "gte", e.found(req(search.Filter{Field: "rank", Op: store.OpGte, Value: 3.0})), b, c)
		seSame(t, "lt int", e.found(req(search.Filter{Field: "rank", Op: store.OpLt, Value: 3})), a)
		seSame(t, "bool", e.found(req(search.Filter{Field: "pinned", Op: store.OpEq, Value: true})), a)
		seSame(t, "exists", e.found(req(search.Filter{Field: "tags", Op: store.OpExists, Value: true})), a, b)
		seSame(t, "and", e.found(req(search.Filter{Field: "topic", Op: store.OpEq, Value: "energy"},
			search.Filter{Field: "rank", Op: store.OpGt, Value: 1.0})), b)

		for name, f := range map[string]search.Filter{
			"unknown field": {Field: "nope", Op: store.OpEq, Value: "x"},
			"unindexed":     {Field: "body", Op: store.OpEq, Value: "x"},
			"bad op":        {Field: "topic", Op: "like", Value: "x"},
			"bad value":     {Field: "rank", Op: store.OpEq, Value: "x"},
			"contains":      {Field: "topic", Op: store.OpContains, Value: "x"},
			"prefix":        {Field: "rank", Op: store.OpPrefix, Value: "x"},
			"exists":        {Field: "rank", Op: store.OpExists, Value: "yes"},
		} {
			_, err := e.eng.Search(ctx, search.Request{Types: []string{"article"}, Locale: "en", Limit: 5, Where: []search.Filter{f}})
			if !errors.Is(err, search.ErrInvalid) {
				t.Errorf("%s: err = %v", name, err)
			}
		}
		if _, err := e.eng.Search(ctx, search.Request{Types: []string{"article"}, Locale: "en", Limit: 0}); !errors.Is(err, search.ErrInvalid) {
			t.Errorf("limit 0: %v", err)
		}
	})
}

func TestSearchEngineSortAndPaging(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		var all []ids.UUID
		for i := 1; i <= 5; i++ {
			all = append(all, e.add(fmt.Sprintf(`{"title":"Solar %d","rank":%d}`, i, i)))
		}
		desc := e.search(search.Request{Text: "solar", Sort: []search.Sort{{Field: "rank", Desc: true}}, Limit: 2})
		if desc.Total != 5 || len(desc.Hits) != 2 || desc.Hits[0].Item != all[4] || desc.Hits[1].Item != all[3] || desc.Hits[0].Score != 0 {
			t.Errorf("desc = %+v", desc)
		}
		page := e.search(search.Request{Text: "solar", Sort: []search.Sort{{Field: "rank"}}, Limit: 2, Offset: 2})
		if page.Total != 5 || len(page.Hits) != 2 || page.Hits[0].Item != all[2] || page.Hits[1].Item != all[3] {
			t.Errorf("page = %+v", page)
		}
		for _, bad := range []string{"tags", "body", "nope"} {
			_, err := e.eng.Search(ctx, search.Request{Types: []string{"article"}, Locale: "en", Limit: 5, Sort: []search.Sort{{Field: bad}}})
			if !errors.Is(err, search.ErrInvalid) {
				t.Errorf("sort %s: %v", bad, err)
			}
		}
	})
}

func TestSearchEngineFacets(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		e.add(`{"title":"Solar a","topic":"energy","tags":["home","roof"]}`)
		e.add(`{"title":"Solar b","topic":"energy","tags":["roof"]}`)
		e.add(`{"title":"Solar c","topic":"travel"}`)
		e.add(`{"title":"Wind d","topic":"energy","tags":["field"]}`)

		res := e.search(search.Request{Text: "solar", Facets: []string{"topic", "tags"}})
		want := map[string][]search.Facet{
			"topic": {{Value: "energy", Count: 2}, {Value: "travel", Count: 1}},
			"tags":  {{Value: "roof", Count: 2}, {Value: "home", Count: 1}},
		}
		for f, w := range want {
			if fmt.Sprint(res.Facets[f]) != fmt.Sprint(w) {
				t.Errorf("facet %s = %v, want %v", f, res.Facets[f], w)
			}
		}
		// Without text every published item counts; with a filter only the filtered ones.
		all := e.search(search.Request{Facets: []string{"topic"}})
		if fmt.Sprint(all.Facets["topic"]) != fmt.Sprint([]search.Facet{{Value: "energy", Count: 3}, {Value: "travel", Count: 1}}) {
			t.Errorf("all = %v", all.Facets)
		}
		filtered := e.search(search.Request{Text: "solar", Facets: []string{"topic"},
			Where: []search.Filter{{Field: "tags", Op: store.OpContains, Value: "roof"}}})
		if fmt.Sprint(filtered.Facets["topic"]) != fmt.Sprint([]search.Facet{{Value: "energy", Count: 2}}) {
			t.Errorf("filtered = %v", filtered.Facets)
		}
		_, err := e.eng.Search(ctx, search.Request{Types: []string{"article"}, Locale: "en", Limit: 5, Facets: []string{"rank"}})
		if !errors.Is(err, search.ErrInvalid) {
			t.Errorf("facet on a number: %v", err)
		}
	})
}

func TestSearchEngineHighlights(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		e.add(`{"title":"Solar <b>panels</b>","body":"Cheap SOLAR power for <your> home & roof"}`)
		res := e.search(search.Request{Text: "solar", Highlight: true})
		if len(res.Hits) != 1 {
			t.Fatalf("hits = %+v", res.Hits)
		}
		h := res.Hits[0].Highlights
		if h["name"] != "<mark>Solar</mark> &lt;b&gt;panels&lt;/b&gt;" {
			t.Errorf("name = %q", h["name"])
		}
		text := h["text"]
		if !strings.Contains(strings.ToLower(text), "<mark>solar</mark>") || strings.Contains(text, "<your>") || strings.Contains(text, "<b>") ||
			strings.ContainsAny(text, "\x02\x03") || !strings.Contains(text, "&amp; roof") {
			t.Errorf("text = %q", text)
		}
		if off := e.search(search.Request{Text: "solar"}); off.Hits[0].Highlights != nil {
			t.Errorf("highlights without the flag: %v", off.Hits[0].Highlights)
		}
	})
}

func TestSearchEngineSettingsTypes(t *testing.T) {
	sc := buildSchema(t, seSchema)
	s := search.NewSettings(sc)
	if fmt.Sprint(s.Types) != "[article note]" {
		t.Errorf("types = %v", s.Types)
	}
}

func TestSearchEngineOverlayLocales(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		sc := buildSchema(t, overlaySchema)
		eng := search.Database(e.db, search.WithSchema(sc))
		id := e.create("article", "de", `{"title":"Hallo","summary":"Sonnenenergie"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi"}`)
		e.publish(id)
		e.update(id, "de", `{"summary":"Windkraft"}`)
		e.publish(id) // rebuilds the descendants' snapshots

		var snaps []store.Snapshot
		for _, s := range e.snapshotsOf(id) {
			snaps = append(snaps, s)
		}
		docs, err := search.BuildDocs(sc, snaps)
		if err != nil {
			t.Fatal(err)
		}
		byLocale := map[string]search.Doc{}
		for _, d := range docs {
			byLocale[d.Locale] = d
		}
		if len(docs) != 3 || byLocale["de-ZH"].Name != "Grüezi" || byLocale["de-ZH"].Text != "Grüezi Windkraft" || byLocale["de"].Text != "Hallo Windkraft" {
			t.Errorf("docs = %+v", docs)
		}
		for _, loc := range []string{"de", "de-CH", "de-ZH"} {
			res, err := eng.Search(ctx, search.Request{Text: "windkraft", Types: []string{"article"}, Locale: loc, Limit: 5})
			if err != nil || len(res.Hits) != 1 || res.Hits[0].Locale != loc {
				t.Errorf("%s: %+v %v", loc, res, err)
			}
			if res, _ := eng.Search(ctx, search.Request{Text: "sonnenenergie", Types: []string{"article"}, Locale: loc, Limit: 5}); len(res.Hits) != 0 {
				t.Errorf("%s: the old text is still found", loc)
			}
		}
	})
}

func TestSearchEngineRejectsUnsearchableTypes(t *testing.T) {
	seRun(t, func(t *testing.T, e *seEnv) {
		for _, typ := range []string{"settings", "nope"} {
			_, err := e.eng.Search(ctx, search.Request{Text: "x", Types: []string{typ}, Locale: "en", Limit: 5})
			if !errors.Is(err, search.ErrInvalid) {
				t.Errorf("%s: %v", typ, err)
			}
		}
	})
}

func TestDeliverySearchTooManyTerms(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		n.wantStatus("/search?channel=web&q=a+b+c+d+e+f+g+h+i+j+k+l+m+n+o+p", 200)
		n.wantStatus("/search?channel=web&q=a+b+c+d+e+f+g+h+i+j+k+l+m+n+o+p+q", 400)
	})
}
