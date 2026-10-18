package tests

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const searchAPISchema = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
  article:
    name: "{title}"
    fields:
      title:  { type: string, translatable: true, required: true }
      body:   { type: text, translatable: true }
      topic:  { type: string, index: true }
      tags:   { type: list, of: { type: string }, index: true }
      rank:   { type: number, integer: true, index: true }
      author: { type: ref, to: [person], on_delete: nullify }
  memo:
    search: false
    name: "{title}"
    fields:
      title: { type: string, required: true }
  settings:
    singleton: true
    fields:
      siteName: { type: string }
`

const searchAPIChannels = `
languages: { en: {}, de: {} }
channels:
  web:    { languages: [en, de] }
  web_de: { languages: [de, en] }
  blog:   { languages: [en], collections: [article] }
  app:
    languages: [en]
    access: token
    tokens: { v1: "${env:APP_TOKEN}" }
`

func newSearchAPINode(t testing.TB, db store.DB, clk clock.Clock, mods ...kernel.Module) *dnode {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": searchAPISchema, "config/channels.yaml": searchAPIChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) {
			switch k {
			case "APP_TOKEN":
				return appToken, true
			case "PREVIEW_ONE":
				return "preview-secret-one-0123456789abcdef", true
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
	return &dnode{t: t, app: a, db: cdb, h: h, clk: clk.(*clock.Manual)}
}

func runSearchAPI(t *testing.T, fn func(t *testing.T, n *dnode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, newSearchAPINode(t, b.open(t, clk), clk))
	})
}

func (n *dnode) createAlways(typ, locale, fields string) ids.UUID {
	n.t.Helper()
	n.tick()
	v, err := n.app.Services().Content.Create(ctx, content.CreateInput{Type: typ, Locale: locale, AlwaysAvailable: true, Fields: doc(n.t, fields)})
	if err != nil {
		n.t.Fatal(err)
	}
	n.publish(v.ID)
	return v.ID
}

// hitIDs returns the ids of items, checking that hits[i] describes items[i].
func hitIDs(t *testing.T, m map[string]any) []string {
	t.Helper()
	items, _ := m["items"].([]any)
	hits, _ := m["hits"].([]any)
	if len(items) != len(hits) {
		t.Fatalf("%d items but %d hits", len(items), len(hits))
	}
	out := make([]string, len(items))
	for i := range items {
		id := items[i].(map[string]any)["id"].(string)
		h := hits[i].(map[string]any)
		if h["id"] != id {
			t.Errorf("hits[%d].id = %v, items[%d].id = %s", i, h["id"], i, id)
		}
		out[i] = id
	}
	return out
}

func idStrings(list ...ids.UUID) []string {
	out := make([]string, len(list))
	for i, id := range list {
		out[i] = id.String()
	}
	return out
}

func sameStrings(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }

func TestDeliverySearch(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		a1 := n.create("article", "", "en", `{"title":"Solar panels","body":"Cheap solar power for every roof","topic":"energy","tags":["power","roof"],"rank":3}`, true)
		a2 := n.create("article", "", "en", `{"title":"Wind farms","body":"Wind and a little solar","topic":"energy","tags":["power"],"rank":1}`, true)
		a3 := n.create("article", "", "en", `{"title":"Solar cooking","body":"Cook with the sun","topic":"food","tags":["roof"],"rank":2}`, true)
		n.create("article", "", "en", `{"title":"Solar draft","topic":"energy"}`, false)
		p := n.create("person", "", "en", `{"fullName":"Solar Sam"}`, true)
		n.create("memo", "", "en", `{"title":"Solar memo"}`, true)
		n.create("settings", "settings", "en", `{"siteName":"Solar site"}`, true)

		r := n.wantStatus("/search?channel=web&q=solar", 200)
		got := hitIDs(t, r.json())
		if len(got) != 4 {
			t.Fatalf("hits = %v, want the three published articles and the person", got)
		}
		for _, id := range got {
			if id != a1.String() && id != a2.String() && id != a3.String() && id != p.String() {
				t.Errorf("unexpected hit %s", id)
			}
		}
		m := r.json()
		deq(t, "total", m["total"], float64(4))
		deq(t, "estimated", m["estimated"], false)
		if m["facets"] != nil || m["included"] != nil {
			t.Errorf("no facets or includes were asked for: %v", m)
		}
		h := m["hits"].([]any)[0].(map[string]any)
		deq(t, "hit locale", h["locale"], "en")
		hl, _ := h["highlights"].(map[string]any)
		if !strings.Contains(hl["name"].(string)+hl["text"].(string), "<mark>") {
			t.Errorf("highlights = %v", hl)
		}
		if r.Header().Get("Surrogate-Key") == "" || !strings.Contains(r.Header().Get("Surrogate-Key"), "c:article") {
			t.Errorf("surrogate key = %q", r.Header().Get("Surrogate-Key"))
		}

		// highlight=false leaves them out; a type list narrows; memo (search: false) and singletons never match.
		m = n.wantStatus("/search?channel=web&q=solar&highlight=false&type=article", 200).json()
		if len(hitIDs(t, m)) != 3 {
			t.Errorf("articles = %v", m["hits"])
		}
		for _, h := range m["hits"].([]any) {
			if h.(map[string]any)["highlights"] != nil {
				t.Errorf("highlights with highlight=false: %v", h)
			}
		}
		for _, typ := range []string{"memo", "settings", "nope"} {
			n.wantStatus("/search?channel=web&q=solar&type="+typ, 400)
		}
		// A channel searches its own collections only.
		n.wantStatus("/search?channel=blog&q=solar&type=person", 400)
		if got := hitIDs(t, n.wantStatus("/search?channel=blog&q=solar", 200).json()); len(got) != 3 {
			t.Errorf("blog channel hits = %v", got)
		}
		// Token channels need their token.
		n.wantStatus("/search?q=solar", 401, "Authorization", "Bearer wrong")
		if got := hitIDs(t, n.wantStatus("/search?q=solar&type=article", 200, "Authorization", "Bearer "+appToken).json()); len(got) != 3 {
			t.Errorf("token channel hits = %v", got)
		}

		// Filters, sort, paging.
		m = n.wantStatus("/search?channel=web&q=solar&type=article&where[topic]=energy", 200).json()
		if got := hitIDs(t, m); !sameStrings(sortedStrings(got), sortedStrings(idStrings(a1, a2))) {
			t.Errorf("topic=energy: %v", got)
		}
		m = n.wantStatus("/search?channel=web&q=solar&type=article&where[rank][gte]=2&where[tags][contains]=roof&sort=-rank", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(a1, a3)) {
			t.Errorf("rank>=2 roof by -rank: %v", got)
		}
		deq(t, "score of a sorted hit", m["hits"].([]any)[0].(map[string]any)["score"], float64(0))
		m = n.wantStatus("/search?channel=web&q=solar&type=article&sort=rank", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(a2, a3, a1)) {
			t.Errorf("by rank: %v", got)
		}
		m = n.wantStatus("/search?channel=web&q=solar&type=article&sort=-publishedAt", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(a3, a2, a1)) {
			t.Errorf("by -publishedAt: %v", got)
		}
		m = n.wantStatus("/search?channel=web&q=solar&type=article&sort=rank&limit=1&offset=1", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(a3)) {
			t.Errorf("page 2: %v", got)
		}
		deq(t, "total of a page", m["total"], float64(3))

		// Facets count over all matches, not the page.
		m = n.wantStatus("/search?channel=web&q=solar&type=article&facets=topic,tags&limit=1", 200).json()
		topics := asJSON(field(m, "facets", "topic"))
		if topics != `[{"count":2,"value":"energy"},{"count":1,"value":"food"}]` {
			t.Errorf("topic facet = %s", topics)
		}
		tags := asJSON(field(m, "facets", "tags"))
		if !strings.Contains(tags, `{"count":2,"value":"roof"}`) || !strings.Contains(tags, `{"count":2,"value":"power"}`) {
			t.Errorf("tags facet = %s", tags)
		}
	})
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestDeliverySearchAvailability(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		both := n.create("article", "", "en", `{"title":"Solar both"}`, false)
		n.update(both, "de", `{"title":"Solar beide"}`)
		n.publish(both)
		enOnly := n.create("article", "", "en", `{"title":"Solar english only"}`, true)
		always := n.createAlways("article", "en", `{"title":"Solar always there"}`)

		en := hitIDs(t, n.wantStatus("/search?channel=web&q=solar&type=article", 200).json())
		if !sameStrings(sortedStrings(en), sortedStrings(idStrings(both, enOnly, always))) {
			t.Errorf("en hits = %v", en)
		}
		// In German: the translated item, and the always-available one in its main locale; not the English-only one.
		m := n.wantStatus("/search?channel=web&locale=de&q=solar&type=article", 200).json()
		de := hitIDs(t, m)
		if !sameStrings(sortedStrings(de), sortedStrings(idStrings(both, always))) {
			t.Fatalf("de hits = %v", de)
		}
		for i, h := range m["hits"].([]any) {
			hit := h.(map[string]any)
			item := m["items"].([]any)[i].(map[string]any)
			switch hit["id"] {
			case both.String():
				deq(t, "translated hit locale", hit["locale"], "de")
				deq(t, "translated item locale", item["locale"], "de")
			case always.String():
				deq(t, "fallback hit locale", hit["locale"], "en")
				deq(t, "fallback item", item["fallback"], true)
			}
		}
		// The channel's first language is the default locale.
		if got := hitIDs(t, n.wantStatus("/search?channel=web_de&q=solar&type=article", 200).json()); !sameStrings(sortedStrings(got), sortedStrings(de)) {
			t.Errorf("web_de hits = %v, want %v", got, de)
		}
		n.wantStatus("/search?channel=blog&locale=de&q=solar", 400)

		// Unpublishing removes the item from the next answer.
		n.drain()
		if _, err := n.app.Services().Content.Unpublish(ctx, enOnly); err != nil {
			t.Fatal(err)
		}
		n.drain()
		eventually(t, "the unpublished item to leave the results", func() bool {
			got := hitIDs(t, n.wantStatus("/search?channel=web&q=solar&type=article", 200).json())
			return len(got) == 2
		})
	})
}

func TestDeliverySearchInclude(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		art := n.create("article", "", "en", `{"title":"Solar by Ann","author":"`+ann.String()+`"}`, true)
		m := n.wantStatus("/search?channel=web&q=solar&type=article&include=1", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(art)) {
			t.Fatalf("hits = %v", got)
		}
		if field(m, "included", ann.String(), "fields", "fullName") != "Ann" {
			t.Errorf("included = %v", m["included"])
		}
		n.wantStatus("/search?channel=web&q=solar&include=4", 400)
	})
}

func TestDeliverySearchErrors(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		n.create("article", "", "en", `{"title":"Solar"}`, true)
		bad := []string{
			"", "&q=", "&q=%20", "&q=" + strings.Repeat("a", 201), "&q=a&limit=0", "&q=a&limit=51", "&q=a&limit=x", "&q=a&offset=1001",
			"&q=a&offset=-1", "&q=a&facets=title", "&q=a&facets=rank", "&q=a&facets=nope", "&q=a&facets=topic,tags,topic,topic,topic,topic",
			"&q=a&sort=title", "&q=a&sort=tags", "&q=a&sort=-", "&q=a&where[nope]=1", "&q=a&where[topic][zz]=1", "&q=a&where[rank]=x",
			"&q=a&highlight=maybe", "&q=a&total=true", "&q=a&bogus=1", "&q=a&q=b", "&q=a&type=article&type=person", "&q=a&locale=fr",
		}
		for _, p := range bad {
			n.wantStatus("/search?channel=web"+p, 400)
		}
		n.wantStatus("/search?channel=web&q=a&type=", 200) // an empty type means the default
		n.wantStatus("/search?channel=nope&q=a", 400)
		// Errors are not cached and not tagged.
		r := n.wantStatus("/search?channel=web&q=a&limit=0", 400)
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control = %q", r.Header().Get("Cache-Control"))
		}
	})
}

func TestDeliverySearchCache(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		n.create("article", "", "en", `{"title":"Solar one"}`, true)
		n.drain()
		path := "/search?channel=web&q=solar&type=article"
		first := n.wantStatus(path, 200)
		before := n.db.dbCalls()
		second := n.wantStatus(path, 200)
		if n.db.dbCalls() != before {
			t.Error("a repeated search must be served from the cache")
		}
		if first.Header().Get("ETag") == "" || first.Header().Get("ETag") != second.Header().Get("ETag") {
			t.Errorf("etags %q %q", first.Header().Get("ETag"), second.Header().Get("ETag"))
		}
		if r := n.get(path, "If-None-Match", first.Header().Get("ETag")); r.Code != 304 {
			t.Errorf("conditional status = %d", r.Code)
		}
		if len(hitIDs(t, first.json())) != 1 {
			t.Fatal("one hit expected")
		}
		// Publishing a new match of the searched type purges the cached search.
		n.create("article", "", "en", `{"title":"Solar two"}`, true)
		n.drain()
		eventually(t, "the search to see the new article", func() bool {
			return len(hitIDs(t, n.wantStatus(path, 200).json())) == 2
		})
		// Another type's publish keeps it.
		n.wantStatus(path, 200)
		before = n.db.dbCalls()
		n.create("person", "", "en", `{"fullName":"Someone"}`, true)
		n.drain()
		n.wantStatus(path, 200)
		if n.db.dbCalls() != before {
			t.Error("publishing a person must not purge an article search")
		}
	})
}

// A hit whose item the channel cannot show (unknown, unpublished, other collection) is dropped from items and
// hits; total stays the engine's.
type fakeEngine struct{ hits []search.Hit }

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) Search(_ context.Context, r search.Request) (search.Result, error) {
	return search.Result{Hits: f.hits, Total: 42, Estimated: true, Facets: map[string][]search.Facet{"topic": {{Value: "x", Count: 1}}}}, nil
}

type fakeEngineModule struct{ e search.Engine }

func (fakeEngineModule) Name() string { return "fakesearch" }

func (m fakeEngineModule) Register(r *kernel.Registry) error { return r.SearchEngine(m.e) }

func TestDeliverySearchDropsUnavailableHits(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fe := &fakeEngine{}
		n := newSearchAPINode(t, b.open(t, clk), clk, fakeEngineModule{fe})
		shown := n.create("article", "", "en", `{"title":"Shown"}`, true)
		draft := n.create("article", "", "en", `{"title":"Draft"}`, false)
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		fe.hits = []search.Hit{
			{Item: draft, Type: "article", Locale: "en", Score: 3},
			{Item: ids.UUID{9}, Type: "article", Locale: "en", Score: 2},
			{Item: shown, Type: "article", Locale: "en", Score: 1, Highlights: map[string]string{"name": "<mark>Shown</mark>"}},
			{Item: ann, Type: "person", Locale: "en", Score: 0.5},
		}
		m := n.wantStatus("/search?channel=blog&q=shown&facets=topic", 200).json()
		if got := hitIDs(t, m); !sameStrings(got, idStrings(shown)) {
			t.Errorf("hits = %v, want only the published article", got)
		}
		deq(t, "total", m["total"], float64(42))
		deq(t, "estimated", m["estimated"], true)
		if asJSON(m["facets"]) != `{"topic":[{"count":1,"value":"x"}]}` {
			t.Errorf("facets = %v", m["facets"])
		}
		hit := m["hits"].([]any)[0].(map[string]any)
		if hl, _ := hit["highlights"].(map[string]any); len(hl) != 1 || hl["name"] != "<mark>Shown</mark>" {
			t.Errorf("highlights = %v", hit["highlights"])
		}
	})
}

func TestDeliverySearchOpenAPI(t *testing.T) {
	runSearchAPI(t, func(t *testing.T, n *dnode) {
		doc := n.wantStatus("/openapi.json", 200).json()
		if field(doc, "paths", "/search", "get", "operationId") != "searchItems" {
			t.Errorf("/search is not documented")
		}
		if field(doc, "components", "schemas", "SearchResponse") == nil {
			t.Error("SearchResponse schema missing")
		}
	})
}
