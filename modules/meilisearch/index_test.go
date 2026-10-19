package meilisearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
)

var bg = context.Background()

func uid(n int) ids.UUID {
	u, err := ids.Parse(fmt.Sprintf("01900000-0000-7000-8000-%012d", n))
	if err != nil {
		panic(err)
	}
	return u
}

func day(m time.Month, d int) time.Time { return time.Date(2024, m, d, 12, 0, 0, 0, time.UTC) }

func testSettings(fp string) search.Settings {
	return search.Settings{
		Types: []string{"article", "page"},
		Fields: []search.FieldSetting{
			{Name: "author", Kind: search.KindRef, Sortable: true},
			{Name: "featured", Kind: search.KindBool, Sortable: true},
			{Name: "kind", Kind: search.KindText, Sortable: true},
			{Name: "rank", Kind: search.KindNum, Sortable: true},
			{Name: "tags", Kind: search.KindText, List: true},
			{Name: "when", Kind: search.KindTime, Sortable: true},
		},
		Fingerprint: fp,
	}
}

var longText = strings.Repeat("filler words go here ", 12) + "the solar array is huge " + strings.Repeat("and more filler follows ", 12)

func fixture() []search.Doc {
	return []search.Doc{
		{Item: uid(1), Type: "article", Locale: "en", MainLocale: "en", Locales: []string{"de", "en"}, Name: "Solar panels",
			Text: "cheap solar power for every home", PublishedAt: day(1, 10),
			Fields: map[string]any{"rank": 1.0, "tags": []string{"energy", "home"}, "kind": "news", "when": day(1, 1)}},
		{Item: uid(1), Type: "article", Locale: "de", MainLocale: "en", Locales: []string{"de", "en"}, Name: "Solarzellen",
			Text: "guenstiger Solarstrom", PublishedAt: day(1, 10), Fields: map[string]any{"rank": 1.0}},
		{Item: uid(2), Type: "article", Locale: "en", MainLocale: "en", AlwaysAvailable: true, Locales: []string{"en"},
			Name: "Wind turbines", Text: "wind power and solar storage <b>tricky</b> markup", PublishedAt: day(2, 10),
			Fields: map[string]any{"rank": 2.0, "tags": []string{"energy"}, "kind": `qu"ote\back`}},
		{Item: uid(3), Type: "article", Locale: "en", MainLocale: "en", Locales: []string{"en"}, Name: "Roof tiles",
			Text: longText, PublishedAt: day(3, 10),
			Fields: map[string]any{"rank": 3.0, "tags": []string{"home"}, "featured": true, "when": day(6, 1), "author": uid(99)}},
		{Item: uid(4), Type: "page", Locale: "en", MainLocale: "en", Locales: []string{"en"}, Name: "Solar contact page",
			Text: "write to us", PublishedAt: day(4, 10)},
		{Item: uid(5), Type: "article", Locale: "en", MainLocale: "en", Locales: []string{"en"}, Name: "Gardening",
			Text: "tomatoes and beans", PublishedAt: day(5, 10), Fields: map[string]any{"tags": []string{"garden"}}},
	}
}

func fill(docs []search.Doc) func(ctx context.Context) ([]search.Doc, error) {
	first := true
	return func(ctx context.Context) ([]search.Doc, error) {
		if !first {
			return nil, nil
		}
		first = false
		return docs, nil
	}
}

func search1(t *testing.T, ix *Index, r search.Request) search.Result {
	t.Helper()
	if r.Limit == 0 {
		r.Limit = 50
	}
	res, err := ix.Search(bg, r)
	if err != nil {
		t.Fatalf("search %+v: %v", r, err)
	}
	return res
}

// items lists the hits as "n/locale".
func items(res search.Result) []string {
	var out []string
	for _, h := range res.Hits {
		out = append(out, fmt.Sprintf("%s/%s", h.Item.String()[len(h.Item.String())-2:], h.Locale))
	}
	return out
}

func sorted(s []string) []string { slices.Sort(s); return s }

func want(t *testing.T, label string, got []string, exp ...string) {
	t.Helper()
	if !slices.Equal(sorted(slices.Clone(got)), sorted(slices.Clone(exp))) {
		t.Errorf("%s: got %v, want %v", label, got, exp)
	}
}

// scenario is the behaviour every server (the fake and a real Meilisearch) must show.
func scenario(t *testing.T, ix *Index) {
	s := testSettings("fp1")
	rebuild, err := ix.Sync(bg, s)
	if err != nil || !rebuild {
		t.Fatalf("Sync on a new index = %v, %v; want a rebuild", rebuild, err)
	}
	if rebuild, _ = ix.Sync(bg, s); !rebuild {
		t.Fatal("Sync before any Rebuild finished must still ask for one")
	}
	if err := ix.Rebuild(bg, s, fill(fixture())); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rebuild, err = ix.Sync(bg, s); err != nil || rebuild {
		t.Fatalf("Sync after Rebuild = %v, %v; want no rebuild", rebuild, err)
	}
	if rebuild, err = ix.Sync(bg, testSettings("fp2")); err != nil || !rebuild {
		t.Fatalf("Sync with a new fingerprint = %v, %v; want a rebuild", rebuild, err)
	}
	if _, err = ix.Sync(bg, s); err != nil {
		t.Fatal(err)
	}
	if err := ix.c.do(bg, http.MethodGet, idxPath(ix.nextUID()), nil, nil); !isCode(err, "index_not_found") {
		t.Errorf("the _next index must be gone after Rebuild, got %v", err)
	}

	req := func(text, locale string, types ...string) search.Request {
		return search.Request{Text: text, Locale: locale, Types: types, Limit: 50}
	}

	t.Run("types and availability", func(t *testing.T) {
		want(t, "en articles", items(search1(t, ix, req("solar", "en", "article"))), "01/en", "02/en", "03/en")
		want(t, "en articles and pages", items(search1(t, ix, req("solar", "en", "article", "page"))), "01/en", "02/en", "03/en", "04/en")
		// de: the item's own document, and the always-available item through its main-locale document.
		want(t, "de", items(search1(t, ix, req("solar", "de", "article"))), "01/de", "02/en")
		want(t, "unknown locale", items(search1(t, ix, req("solar", "fr", "article"))), "02/en")
		want(t, "no match", items(search1(t, ix, req("nonexistentterm", "en", "article"))))
		res := search1(t, ix, req("", "en", "article"))
		want(t, "empty text lists everything", items(res), "01/en", "02/en", "03/en", "05/en")
	})

	t.Run("filters", func(t *testing.T) {
		cases := []struct {
			name string
			f    search.Filter
			want []string
		}{
			{"eq with quote and backslash", search.Filter{Field: "kind", Op: store.OpEq, Value: `qu"ote\back`}, []string{"02/en"}},
			{"eq plain", search.Filter{Field: "kind", Op: store.OpEq, Value: "news"}, []string{"01/en"}},
			{"ne keeps documents without the field", search.Filter{Field: "kind", Op: store.OpNe, Value: "news"}, []string{"02/en", "03/en", "05/en"}},
			{"gte", search.Filter{Field: "rank", Op: store.OpGte, Value: 2.0}, []string{"02/en", "03/en"}},
			{"lt int", search.Filter{Field: "rank", Op: store.OpLt, Value: 3}, []string{"01/en", "02/en"}},
			{"in", search.Filter{Field: "rank", Op: store.OpIn, Value: []float64{1, 3}}, []string{"01/en", "03/en"}},
			{"in text", search.Filter{Field: "kind", Op: store.OpIn, Value: []string{"news", `qu"ote\back`}}, []string{"01/en", "02/en"}},
			{"contains", search.Filter{Field: "tags", Op: store.OpContains, Value: "energy"}, []string{"01/en", "02/en"}},
			{"bool", search.Filter{Field: "featured", Op: store.OpEq, Value: true}, []string{"03/en"}},
			{"exists true", search.Filter{Field: "featured", Op: store.OpExists, Value: true}, []string{"03/en"}},
			{"exists false", search.Filter{Field: "featured", Op: store.OpExists, Value: false}, []string{"01/en", "02/en", "05/en"}},
			{"time", search.Filter{Field: "when", Op: store.OpLt, Value: day(3, 1)}, []string{"01/en"}},
			{"publishedAt", search.Filter{Field: search.PublishedAt, Op: store.OpGt, Value: day(2, 1)}, []string{"02/en", "03/en", "05/en"}},
			{"ref", search.Filter{Field: "author", Op: store.OpEq, Value: uid(99)}, []string{"03/en"}},
		}
		for _, c := range cases {
			r := req("", "en", "article")
			r.Where = []search.Filter{c.f}
			want(t, c.name, items(search1(t, ix, r)), c.want...)
		}
		r := req("solar", "en", "article")
		r.Where = []search.Filter{{Field: "rank", Op: store.OpGte, Value: 2.0}, {Field: "tags", Op: store.OpContains, Value: "home"}}
		want(t, "filters are ANDed", items(search1(t, ix, r)), "03/en")
	})

	t.Run("invalid requests", func(t *testing.T) {
		bad := []search.Filter{
			{Field: "nosuch", Op: store.OpEq, Value: "x"},
			{Field: "kind", Op: store.OpPrefix, Value: "ne"},
			{Field: "kind", Op: store.OpLt, Value: "x"},
			{Field: "kind", Op: store.OpContains, Value: "x"},
			{Field: "rank", Op: store.OpEq, Value: "text"},
			{Field: "x y", Op: store.OpEq, Value: "x"},
		}
		for _, f := range bad {
			r := req("solar", "en", "article")
			r.Where = []search.Filter{f}
			if _, err := ix.Search(bg, r); !errors.Is(err, search.ErrInvalid) {
				t.Errorf("filter %+v: want ErrInvalid, got %v", f, err)
			}
		}
		r := req("solar", "en", "article")
		r.Sort = []search.Sort{{Field: "tags"}}
		if _, err := ix.Search(bg, r); !errors.Is(err, search.ErrInvalid) {
			t.Errorf("sort by a list: want ErrInvalid, got %v", err)
		}
		r.Sort = nil
		r.Limit = 0
		if _, err := ix.Search(bg, r); !errors.Is(err, search.ErrInvalid) {
			t.Errorf("limit 0: want ErrInvalid, got %v", err)
		}
	})

	t.Run("sort", func(t *testing.T) {
		r := req("solar", "en", "article")
		r.Sort = []search.Sort{{Field: "rank", Desc: true}}
		res := search1(t, ix, r)
		if got := items(res); !slices.Equal(got, []string{"03/en", "02/en", "01/en"}) {
			t.Errorf("rank desc = %v", got)
		}
		if res.Hits[0].Score != 0 {
			t.Errorf("a sorted hit has score %v, want 0", res.Hits[0].Score)
		}
		r.Sort = []search.Sort{{Field: search.PublishedAt}}
		if got := items(search1(t, ix, r)); !slices.Equal(got, []string{"01/en", "02/en", "03/en"}) {
			t.Errorf("publishedAt asc = %v", got)
		}
		r.Sort = nil
		if res := search1(t, ix, r); res.Hits[0].Score <= 0 {
			t.Errorf("relevance score = %v, want > 0", res.Hits[0].Score)
		}
	})

	t.Run("facets", func(t *testing.T) {
		r := req("", "en", "article")
		r.Facets = []string{"tags", "kind"}
		res := search1(t, ix, r)
		wantTags := []search.Facet{{Value: "energy", Count: 2}, {Value: "home", Count: 2}, {Value: "garden", Count: 1}}
		if !slices.Equal(res.Facets["tags"], wantTags) {
			t.Errorf("tags = %v, want %v", res.Facets["tags"], wantTags)
		}
		if len(res.Facets["kind"]) != 2 {
			t.Errorf("kind = %v", res.Facets["kind"])
		}
		r.Where = []search.Filter{{Field: "rank", Op: store.OpGte, Value: 2.0}}
		res = search1(t, ix, r)
		if want := []search.Facet{{Value: "energy", Count: 1}, {Value: "home", Count: 1}}; !slices.Equal(res.Facets["tags"], want) {
			t.Errorf("filtered tags = %v, want %v", res.Facets["tags"], want)
		}
	})

	t.Run("highlight", func(t *testing.T) {
		r := req("solar", "en", "article")
		r.Highlight = true
		res := search1(t, ix, r)
		by := map[string]search.Hit{}
		for _, h := range res.Hits {
			by[h.Item.String()[len(h.Item.String())-2:]] = h
		}
		if got := by["01"].Highlights["name"]; got != "<mark>Solar</mark> panels" {
			t.Errorf("name highlight = %q", got)
		}
		if got := by["01"].Highlights["text"]; !strings.Contains(got, "<mark>solar</mark>") {
			t.Errorf("text highlight = %q", got)
		}
		b := by["02"].Highlights["text"]
		if !strings.Contains(b, "<mark>solar</mark>") || !strings.Contains(b, "&lt;b&gt;tricky&lt;/b&gt;") || strings.Contains(b, "<b>") {
			t.Errorf("markup must be escaped around marks: %q", b)
		}
		c := by["03"].Highlights["text"]
		if n := len(strings.Fields(c)); n > 24 || !strings.Contains(c, "<mark>solar</mark>") {
			t.Errorf("cropped text has %d words: %q", n, c)
		}
		if strings.ContainsAny(c, "\x02\x03") {
			t.Errorf("markers leaked: %q", c)
		}
		r.Highlight = false
		if h := search1(t, ix, r).Hits[0]; h.Highlights != nil {
			t.Errorf("highlights without Highlight: %v", h.Highlights)
		}
	})

	t.Run("totals", func(t *testing.T) {
		r := req("solar", "en", "article")
		r.Limit = 2
		res := search1(t, ix, r)
		if len(res.Hits) != 2 || res.Total != 3 || res.Estimated {
			t.Errorf("page 1: %d hits, total %d, estimated %v", len(res.Hits), res.Total, res.Estimated)
		}
		r.Offset = 2
		res = search1(t, ix, r)
		if len(res.Hits) != 1 || res.Total != 3 || res.Estimated {
			t.Errorf("page 2: %d hits, total %d, estimated %v", len(res.Hits), res.Total, res.Estimated)
		}
		r.Offset = 1
		res = search1(t, ix, r)
		if len(res.Hits) != 2 || res.Total != 3 || !res.Estimated {
			t.Errorf("offset 1: %d hits, total %d, estimated %v", len(res.Hits), res.Total, res.Estimated)
		}
	})

	t.Run("upsert and delete", func(t *testing.T) {
		d := fixture()[0]
		d.Name = "Photovoltaic cells"
		d.Text = "photovoltaic only"
		if err := ix.Upsert(bg, []search.Doc{d, fixture()[1]}); err != nil {
			t.Fatal(err)
		}
		want(t, "replaced", items(search1(t, ix, req("photovoltaic", "en", "article"))), "01/en")
		want(t, "old text gone, de document kept", items(search1(t, ix, req("solar", "de", "article"))), "01/de", "02/en")
		// An item upserted without its de document loses it.
		if err := ix.Upsert(bg, []search.Doc{d}); err != nil {
			t.Fatal(err)
		}
		want(t, "unpublished locale removed", items(search1(t, ix, req("solar", "de", "article"))), "02/en")
		want(t, "other items untouched", items(search1(t, ix, req("wind", "en", "article"))), "02/en")
		if err := ix.Upsert(bg, []search.Doc{fixture()[1]}); err != nil {
			t.Fatal(err)
		}
		want(t, "de back, en gone", items(search1(t, ix, req("", "de", "article"))), "01/de", "02/en")
		if err := ix.Delete(bg, []ids.UUID{uid(1), uid(2)}); err != nil {
			t.Fatal(err)
		}
		want(t, "deleted en", items(search1(t, ix, req("", "en", "article"))), "03/en", "05/en")
		want(t, "deleted de", items(search1(t, ix, req("", "de", "article"))))
		if err := ix.Delete(bg, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rebuild replaces without an empty window", func(t *testing.T) {
		replacement := []search.Doc{fixture()[5]}
		first := true
		err := ix.Rebuild(bg, s, func(ctx context.Context) ([]search.Doc, error) {
			// While the new index fills, searches still see the old one.
			want(t, "during rebuild", items(search1(t, ix, req("", "en", "article"))), "03/en", "05/en")
			if !first {
				return nil, nil
			}
			first = false
			return replacement, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want(t, "after rebuild", items(search1(t, ix, req("", "en", "article"))), "05/en")
		if err := ix.c.do(bg, http.MethodGet, idxPath(ix.nextUID()), nil, nil); !isCode(err, "index_not_found") {
			t.Errorf("the _next index must be gone after Rebuild, got %v", err)
		}
	})

	t.Run("a failing document source aborts the rebuild", func(t *testing.T) {
		boom := errors.New("source failed")
		if err := ix.Rebuild(bg, s, func(context.Context) ([]search.Doc, error) { return nil, boom }); !errors.Is(err, boom) {
			t.Errorf("Rebuild = %v, want the source error", err)
		}
		want(t, "old index untouched", items(search1(t, ix, req("", "en", "article"))), "05/en")
	})
}

func newFakeIndex(t *testing.T, mutate func(*Config)) (*Index, *fakeMeili) {
	f, srv := newFake(t, "secret-key")
	cfg := defaults()
	cfg.URL, cfg.Key, cfg.Index = srv.URL, "secret-key", "site"
	cfg.Timeout = 2 * time.Second
	if mutate != nil {
		mutate(&cfg)
	}
	return New(cfg, nil), f
}

func TestFakeScenario(t *testing.T) {
	ix, _ := newFakeIndex(t, nil)
	scenario(t, ix)
}

func TestRealScenario(t *testing.T) {
	base := os.Getenv("HYDROGEN_TEST_MEILI_URL")
	if base == "" {
		t.Skip("HYDROGEN_TEST_MEILI_URL is not set")
	}
	cfg := defaults()
	cfg.URL, cfg.Key = base, os.Getenv("HYDROGEN_TEST_MEILI_KEY")
	cfg.Index = fmt.Sprintf("hydtest_%d", time.Now().UnixNano())
	cfg.Timeout = 30 * time.Second
	ix := New(cfg, nil)
	t.Cleanup(func() {
		for _, u := range []string{cfg.Index, ix.metaUID(), ix.nextUID()} {
			_ = ix.c.run(bg, http.MethodDelete, idxPath(u), nil)
		}
	})
	scenario(t, ix)
}

func TestSendsTheKey(t *testing.T) {
	ix, _ := newFakeIndex(t, func(c *Config) { c.Key = "wrong" })
	_, err := ix.Sync(bg, testSettings("x"))
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized || ae.Code != "invalid_api_key" {
		t.Fatalf("Sync with a wrong key = %v, want a 401 api error", err)
	}
}

func TestFailedTaskReportsTheServerMessage(t *testing.T) {
	ix, f := newFakeIndex(t, nil)
	if _, err := ix.Sync(bg, testSettings("x")); err != nil {
		t.Fatal(err)
	}
	f.failWrite = func(method, path string) string {
		if strings.HasSuffix(path, "/documents") {
			return "Document `x` has an invalid attribute"
		}
		return ""
	}
	err := ix.Upsert(bg, fixture()[:1])
	var te *taskError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "invalid attribute") {
		t.Fatalf("Upsert = %v, want the failed task's message", err)
	}
}

func TestTasksAreWaitedForAndBounded(t *testing.T) {
	ix, f := newFakeIndex(t, func(c *Config) { c.TaskTimeout = 150 * time.Millisecond })
	if _, err := ix.Sync(bg, testSettings("x")); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	before := len(f.requests)
	f.mu.Unlock()
	if err := ix.Upsert(bg, fixture()[:1]); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	polls := 0
	for _, r := range f.requests[before:] {
		if strings.HasPrefix(r, "GET /tasks/") {
			polls++
		}
	}
	f.stall = true
	f.mu.Unlock()
	if polls < 2 {
		t.Errorf("a task reported processing once must be polled again, polls = %d", polls)
	}
	start := time.Now()
	err := ix.Upsert(bg, fixture()[:1])
	if err == nil || !strings.Contains(err.Error(), "still processing") {
		t.Fatalf("Upsert with a stalled task = %v, want a timeout error", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("waiting took %v", time.Since(start))
	}
}

func TestBatchesLargeWrites(t *testing.T) {
	ix, f := newFakeIndex(t, nil)
	if _, err := ix.Sync(bg, testSettings("x")); err != nil {
		t.Fatal(err)
	}
	var docs []search.Doc
	for i := 0; i < writeBatch*2+1; i++ {
		docs = append(docs, search.Doc{Item: uid(1000 + i), Type: "article", Locale: "en", MainLocale: "en", Name: "n", Text: "t", PublishedAt: day(1, 1)})
	}
	if err := ix.Upsert(bg, docs); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := len(f.indexes["site"].docs); n != len(docs) {
		t.Errorf("stored %d documents, want %d", n, len(docs))
	}
	posts := 0
	for _, r := range f.requests {
		if r == "POST /indexes/site/documents" {
			posts++
		}
	}
	if posts != 3 {
		t.Errorf("document requests = %d, want 3", posts)
	}
}

func TestDocumentShape(t *testing.T) {
	d := fixture()[2]
	d.Name = "a\x02b\x03"
	m, err := document(d)
	if err != nil {
		t.Fatal(err)
	}
	if m["id"] != uid(2).String()+"_en" || m["item"] != uid(2).String() || m["type"] != "article" || m["locale"] != "en" ||
		m["is_main"] != true || m["always"] != true || m["name"] != "ab" || m["published_at"] != day(2, 10).Unix() ||
		!slices.Equal(m["locales"].([]string), []string{"en"}) {
		t.Errorf("document = %v", m)
	}
	if !slices.Equal(m["f_tags"].([]any), []any{"energy"}) || m["f_rank"] != 2.0 {
		t.Errorf("fields = %v", m)
	}
	de := fixture()[1]
	m, _ = document(de)
	if m["is_main"] != false {
		t.Errorf("a non-main locale must not be is_main: %v", m)
	}
	d.Fields = map[string]any{"x": struct{}{}}
	if _, err := document(d); err == nil {
		t.Error("an unsupported field value must fail")
	}
}

func TestSettingsBody(t *testing.T) {
	b := settingsBody(testSettings("x"))
	filterable := b["filterableAttributes"].([]string)
	sortable := b["sortableAttributes"].([]string)
	for _, a := range []string{"item", "type", "locale", "is_main", "always", "locales", "published_at", "f_tags", "f_rank"} {
		if !slices.Contains(filterable, a) {
			t.Errorf("%s must be filterable", a)
		}
	}
	if !slices.Contains(sortable, "published_at") || !slices.Contains(sortable, "f_rank") || slices.Contains(sortable, "f_tags") {
		t.Errorf("sortable = %v", sortable)
	}
	if !slices.Equal(b["searchableAttributes"].([]string), []string{"name", "text"}) {
		t.Errorf("searchable = %v", b["searchableAttributes"])
	}
}

func TestFilterTranslation(t *testing.T) {
	ix := New(Config{URL: "http://x", Index: "i", Timeout: time.Second}, nil)
	ix.setFields(testSettings("x"))
	const avail = `(locale = "en" OR (always = true AND is_main = true AND NOT locales = "en"))`
	table := []struct {
		name string
		r    search.Request
		want string
	}{
		{"types and locale", search.Request{Types: []string{"article", "page"}, Locale: "en"},
			`type IN ["article", "page"] AND ` + avail},
		{"quote and backslash escaped", search.Request{Types: []string{`a"b\c`}, Locale: "en"},
			`type IN ["a\"b\\c"] AND ` + avail},
		{"injection attempt stays a string", search.Request{Types: []string{"article"}, Locale: "en",
			Where: []search.Filter{{Field: "kind", Op: store.OpEq, Value: `x" OR type != "z`}}},
			`type IN ["article"] AND ` + avail + ` AND f_kind = "x\" OR type != \"z"`},
		{"locale escaped", search.Request{Types: []string{"article"}, Locale: `e"n`},
			`type IN ["article"] AND (locale = "e\"n" OR (always = true AND is_main = true AND NOT locales = "e\"n"))`},
		{"every operator", search.Request{Types: []string{"article"}, Locale: "en", Where: []search.Filter{
			{Field: "rank", Op: store.OpEq, Value: 1},
			{Field: "rank", Op: store.OpNe, Value: 1.5},
			{Field: "rank", Op: store.OpLt, Value: 2.0},
			{Field: "rank", Op: store.OpLte, Value: 3.0},
			{Field: "rank", Op: store.OpGt, Value: 4.0},
			{Field: "rank", Op: store.OpGte, Value: 5.0},
			{Field: "rank", Op: store.OpIn, Value: []float64{1, 2.5}},
			{Field: "tags", Op: store.OpContains, Value: "a b"},
			{Field: "featured", Op: store.OpEq, Value: false},
			{Field: "featured", Op: store.OpExists, Value: true},
			{Field: "kind", Op: store.OpExists, Value: false},
			{Field: "when", Op: store.OpGte, Value: day(1, 1)},
			{Field: "kind", Op: store.OpIn, Value: []string{"a", `b"`}},
			{Field: "author", Op: store.OpEq, Value: uid(7)},
			{Field: search.PublishedAt, Op: store.OpLt, Value: day(1, 1)},
		}},
			`type IN ["article"] AND ` + avail + ` AND f_rank = 1 AND f_rank != 1.5 AND f_rank < 2 AND f_rank <= 3 AND f_rank > 4` +
				` AND f_rank >= 5 AND f_rank IN [1, 2.5] AND f_tags = "a b" AND f_featured = false AND f_featured EXISTS` +
				` AND NOT f_kind EXISTS AND f_when >= 1704110400 AND f_kind IN ["a", "b\""] AND f_author = "` + uid(7).String() + `"` +
				` AND published_at < 1704110400`},
	}
	for _, c := range table {
		got, err := ix.filter(c.r)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestFilterTranslationWithoutSettingsGuessesKinds(t *testing.T) {
	ix := New(Config{URL: "http://x", Index: "i", Timeout: time.Second}, nil)
	got, err := ix.filterExpr(search.Filter{Field: "rank", Op: store.OpGte, Value: 2})
	if err != nil || got != "f_rank >= 2" {
		t.Errorf("= %q, %v", got, err)
	}
}

func TestSortTranslation(t *testing.T) {
	ix := New(Config{URL: "http://x", Index: "i", Timeout: time.Second}, nil)
	ix.setFields(testSettings("x"))
	got, err := ix.sortExprs([]search.Sort{{Field: "rank", Desc: true}, {Field: search.PublishedAt}})
	if err != nil || !slices.Equal(got, []string{"f_rank:desc", "published_at:asc"}) {
		t.Errorf("= %v, %v", got, err)
	}
	if _, err := ix.sortExprs([]search.Sort{{Field: "tags"}}); !errors.Is(err, search.ErrInvalid) {
		t.Errorf("list sort: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	ok := Config{URL: "http://127.0.0.1:7700", Key: "k", Index: "hydrogen", Timeout: time.Second, TaskTimeout: time.Second}
	table := []struct {
		name   string
		mod    func(*Config)
		dev    bool
		errHas string
	}{
		{"valid", func(*Config) {}, false, ""},
		{"https", func(c *Config) { c.URL = "https://search.example.com" }, false, ""},
		{"no url", func(c *Config) { c.URL = "" }, true, "url must be"},
		{"bad scheme", func(c *Config) { c.URL = "ftp://x" }, true, "url must be"},
		{"no host", func(c *Config) { c.URL = "http://" }, true, "url must be"},
		{"no key in dev", func(c *Config) { c.Key = "" }, true, ""},
		{"no key outside dev", func(c *Config) { c.Key = "" }, false, "key is required"},
		{"bad index", func(c *Config) { c.Index = "a/b" }, true, "index"},
		{"zero timeout", func(c *Config) { c.Timeout = 0 }, true, "timeout"},
		{"zero task timeout", func(c *Config) { c.TaskTimeout = 0 }, true, "task_timeout"},
	}
	for _, c := range table {
		cfg := ok
		c.mod(&cfg)
		err := cfg.validate(c.dev)
		switch {
		case c.errHas == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.errHas != "" && (err == nil || !strings.Contains(err.Error(), c.errHas)):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.errHas)
		}
	}
}

func TestSearchBeforeSyncMapsServerFilterErrors(t *testing.T) {
	ix, _ := newFakeIndex(t, nil)
	if _, err := ix.Sync(bg, testSettings("x")); err != nil {
		t.Fatal(err)
	}
	fresh := New(ix.cfg, nil) // no settings loaded: the server judges the field
	_, err := fresh.Search(bg, search.Request{Types: []string{"article"}, Locale: "en", Limit: 5,
		Where: []search.Filter{{Field: "nosuch", Op: store.OpEq, Value: "x"}}})
	if !errors.Is(err, search.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestIntegerKinds(t *testing.T) {
	for _, v := range []any{int(1), int8(1), int16(1), int32(1), int64(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1)} {
		if got, err := fieldValue(v); err != nil || got == nil {
			t.Errorf("%T: %v, %v", v, got, err)
		}
	}
}

func TestConcurrentWritesAreSerialised(t *testing.T) {
	ix, _ := newFakeIndex(t, nil)
	if _, err := ix.Sync(bg, testSettings("x")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- ix.Upsert(bg, fixture()[:2]) }()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	want(t, "both locales", items(search1(t, ix, search.Request{Types: []string{"article"}, Locale: "de"})), "01/de")
}

// TestRealRestrictedKey runs the module with a key that has only the documented actions, scoped to the
// index and its helper indexes.
func TestRealRestrictedKey(t *testing.T) {
	base := os.Getenv("HYDROGEN_TEST_MEILI_URL")
	if base == "" {
		t.Skip("HYDROGEN_TEST_MEILI_URL is not set")
	}
	name := fmt.Sprintf("hydkey_%d", time.Now().UnixNano())
	admin := New(Config{URL: base, Key: os.Getenv("HYDROGEN_TEST_MEILI_KEY"), Index: name, Timeout: 10 * time.Second, TaskTimeout: time.Minute}, nil)
	var key struct {
		Key string `json:"key"`
		UID string `json:"uid"`
	}
	err := admin.c.do(bg, http.MethodPost, "/keys", map[string]any{
		"name": "hydrogen test", "expiresAt": nil, "indexes": []string{name + "*"},
		"actions": []string{"search", "documents.add", "documents.get", "documents.delete", "indexes.create", "indexes.delete",
			"indexes.update", "indexes.swap", "settings.update", "tasks.get"},
	}, &key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, u := range []string{name, admin.metaUID(), admin.nextUID()} {
			_ = admin.c.run(bg, http.MethodDelete, idxPath(u), nil)
		}
		_ = admin.c.do(bg, http.MethodDelete, "/keys/"+key.UID, nil, nil)
	})
	cfg := admin.cfg
	cfg.Key = key.Key
	ix := New(cfg, nil)
	s := testSettings("fp")
	if rebuild, err := ix.Sync(bg, s); err != nil || !rebuild {
		t.Fatalf("Sync = %v, %v", rebuild, err)
	}
	if err := ix.Rebuild(bg, s, fill(fixture())); err != nil {
		t.Fatal(err)
	}
	if rebuild, err := ix.Sync(bg, s); err != nil || rebuild {
		t.Fatalf("Sync after Rebuild = %v, %v", rebuild, err)
	}
	if err := ix.Upsert(bg, fixture()[:1]); err != nil {
		t.Fatal(err)
	}
	if err := ix.Delete(bg, []ids.UUID{uid(5)}); err != nil {
		t.Fatal(err)
	}
	want(t, "restricted key", items(search1(t, ix, search.Request{Text: "solar", Types: []string{"article"}, Locale: "en"})), "01/en", "02/en", "03/en")
}

func TestRebuildConfirmsASwapItCannotSee(t *testing.T) {
	ix, f := newFakeIndex(t, nil)
	f.hideSwap = true
	s := testSettings("fp")
	if _, err := ix.Sync(bg, s); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(bg, s, fill(fixture())); err != nil {
		t.Fatal(err)
	}
	want(t, "swapped", items(search1(t, ix, search.Request{Types: []string{"article"}, Locale: "en"})), "01/en", "02/en", "03/en", "05/en")
}

func TestDeleteDuringRebuildSurvivesTheSwap(t *testing.T) {
	ix, _ := newFakeIndex(t, nil)
	s := testSettings("fp")
	if _, err := ix.Sync(bg, s); err != nil {
		t.Fatal(err)
	}
	calls := 0
	deleted := make(chan error, 1)
	err := ix.Rebuild(bg, s, func(ctx context.Context) ([]search.Doc, error) {
		calls++
		switch calls {
		case 1:
			return fixture()[:4], nil
		case 2:
			// The purge arrives while the rebuild runs; it waits for the batch lock.
			go func() { deleted <- ix.Delete(bg, []ids.UUID{uid(3)}) }()
			time.Sleep(50 * time.Millisecond)
			return fixture()[4:], nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	want(t, "purged item absent", items(search1(t, ix, search.Request{Types: []string{"article"}, Locale: "en"})), "01/en", "02/en", "05/en")
}

// roundTrip lets a test act on requests as the client sends them.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeleteStraddlingTheSwapSurvivesIt(t *testing.T) {
	_, srv := newFake(t, "")
	liveDeleted, swapped := make(chan struct{}), make(chan struct{})
	var once sync.Once
	wait := func(c chan struct{}) {
		select {
		case <-c:
		case <-time.After(time.Second):
		}
	}
	// The purge's delete on the live index lands just before the swap and the rest of the purge just after it.
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/swap-indexes" {
			wait(liveDeleted)
			resp, err := http.DefaultTransport.RoundTrip(r)
			close(swapped)
			return resp, err
		}
		resp, err := http.DefaultTransport.RoundTrip(r)
		if r.URL.Path == "/indexes/site/documents/delete" {
			once.Do(func() { close(liveDeleted) })
			wait(swapped)
		}
		return resp, err
	})}
	cfg := defaults()
	cfg.URL, cfg.Index, cfg.Timeout = srv.URL, "site", 5*time.Second
	ix := New(cfg, hc)
	s := testSettings("fp")
	if _, err := ix.Sync(bg, s); err != nil {
		t.Fatal(err)
	}
	calls := 0
	deleted := make(chan error, 1)
	err := ix.Rebuild(bg, s, func(ctx context.Context) ([]search.Doc, error) {
		calls++
		if calls == 1 {
			return fixture(), nil
		}
		// The purge arrives after the last batch; it runs once the batch lock is released.
		go func() { deleted <- ix.Delete(bg, []ids.UUID{uid(3)}) }()
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	want(t, "purged item absent", items(search1(t, ix, search.Request{Types: []string{"article"}, Locale: "en"})), "01/en", "02/en", "05/en")
}
