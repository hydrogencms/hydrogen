package graphql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
)

type fixture struct {
	acme, alice, bob, a1, a2, a3, img, settings ids.UUID
}

func (e *env) fixture() fixture {
	var f fixture
	f.acme = e.create("company", "acme", "en", `{"title":"Acme"}`, true)
	f.alice = e.create("person", "alice", "en", fmt.Sprintf(`{"fullName":"Alice","company":%q}`, f.acme), true)
	f.bob = e.create("person", "bob", "en", `{"fullName":"Bob"}`, true)
	f.img = e.image("cat.png", 1, `{"alt":"A cat"}`)
	f.a2 = e.create("article", "second", "en", fmt.Sprintf(`{"title":"Second","rating":5,"kind":"video","author":%q,"tags":["y"]}`, f.bob), true)
	f.a1 = e.create("article", "hello", "en", fmt.Sprintf(`{"title":"Hello","summary":"Hi there","rating":3,"featured":true,"price":9.5,
		"stage":"done","url":"https://example.com/x","contact":"a@example.com","eventDay":"2026-02-03",
		"body":{"type":"doc","content":[{"type":"paragraph"}]},
		"author":%q,"related":[%q],"subject":%q,"tags":["x","y"],"hero":%q,"gallery":[%q],"logo":%q,
		"address":{"street":"1 Main","city":"Metro","geo":{"lat":1.5}},"seo":{"metaTitle":"SEO"},
		"places":[{"label":"A","rank":1},{"label":"B","rank":2}],"extra":{"k":[1,2,{"z":true}]}}`,
		f.alice, f.a2, f.alice, f.img, f.img, f.img), true)
	f.a3 = e.create("article", "third", "en", `{"title":"Third","rating":1,"tags":["z"]}`, true)
	e.update(f.a1, "de", `{"title":"Hallo"}`)
	e.publish(f.a1)
	f.settings = e.create("settings", "", "en", `{"siteName":"Hydrogen"}`, true)
	return f
}

func (r result) noErrors(t *testing.T) {
	t.Helper()
	if r.Code != 200 || len(r.errors()) > 0 {
		t.Fatalf("status %d: %s", r.Code, r.Body)
	}
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestGetByIDAndKey(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()

	r := e.post("web", `query($id: ID, $key: String) {
		byKey: article(key: "hello") { id type name key locale title kind stage urlField url rating price featured summary contact eventDay publishDate body tags }
		byId: article(id: $id) { title }
		missing: article(key: "nope") { title }
		wrongType: article(id: $person) { title }
	}`, map[string]any{"id": f.a1.String(), "person": f.alice.String()})
	if r.Code != 400 {
		// $person is undefined: validation error
		t.Fatalf("undefined variable should be a validation error: %d %s", r.Code, r.Body)
	}

	r = e.post("web", `query($id: ID, $person: ID) {
		byKey: article(key: "hello") { id type name key locale title kind stage urlField url rating price featured summary contact eventDay publishDate body tags }
		byId: article(id: $id) { title }
		missing: article(key: "nope") { title }
		wrongType: article(id: $person) { title }
	}`, map[string]any{"id": f.a1.String(), "person": f.alice.String()})
	r.noErrors(t)
	d := r.data()
	a := path(d, "byKey").(map[string]any)
	eq(t, "id", a["id"], f.a1.String())
	eq(t, "type", a["type"], "article")
	eq(t, "name", a["name"], "Hello")
	eq(t, "key", a["key"], "hello")
	eq(t, "locale", a["locale"], "en")
	eq(t, "kind", a["kind"], "news")
	eq(t, "stage", a["stage"], "done")
	eq(t, "urlField", a["urlField"], "https://example.com/x")
	eq(t, "rating", a["rating"], 3)
	eq(t, "price", a["price"], 9.5)
	eq(t, "featured", a["featured"], true)
	eq(t, "contact", a["contact"], "a@example.com")
	eq(t, "eventDay", a["eventDay"], "2026-02-03")
	eq(t, "tags", a["tags"], "[x y]")
	if _, ok := a["publishDate"]; !ok || a["publishDate"] != nil {
		t.Errorf("publishDate = %v", a["publishDate"])
	}
	eq(t, "body", path(a, "body", "type"), "doc")
	eq(t, "byId", path(d, "byId", "title"), "Hello")
	if d["missing"] != nil || d["wrongType"] != nil {
		t.Errorf("missing/wrongType = %v / %v", d["missing"], d["wrongType"])
	}

	// exactly one of id and key
	r = e.post("web", `{ article { title } }`, nil)
	if r.Code != 200 || len(r.errors()) != 1 || path(r.errors(), 0, "extensions", "status") != float64(400) {
		t.Fatalf("no selector: %d %s", r.Code, r.Body)
	}
	eq(t, "data", r.data()["article"], "<nil>")
}

func TestListWhereSortLimit(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()

	r := e.post("web", `{ articleList(sort: "-rating") { hasMore total items { title rating } } }`, nil)
	r.noErrors(t)
	items := path(r.data(), "articleList", "items").([]any)
	if len(items) != 3 || path(items, 0, "title") != "Second" || path(items, 2, "title") != "Third" {
		t.Fatalf("sorted: %s", r.Body)
	}
	if path(r.data(), "articleList", "total") != nil {
		// total is counted only on request and is selected here, so it is present
		eq(t, "total", path(r.data(), "articleList", "total"), 3)
	} else {
		t.Errorf("total missing: %s", r.Body)
	}
	if e.calls < 1 {
		t.Errorf("calls = %d", e.calls)
	}

	r = e.post("web", `query($w: JSON) { articleList(where: $w, sort: "rating", limit: 1) { hasMore items { title } } }`,
		map[string]any{"w": map[string]any{"rating": map[string]any{"gte": 3}}})
	r.noErrors(t)
	eq(t, "title", path(r.data(), "articleList", "items", 0, "title"), "Hello")
	eq(t, "hasMore", path(r.data(), "articleList", "hasMore"), true)
	eq(t, "total omitted", path(r.data(), "articleList", "items").([]any)[0].(map[string]any)["title"], "Hello")

	// a literal object, shorthand equality and a value list for in
	r = e.post("web", `{ a: articleList(where: {kind: "video"}) { items { title } }
		b: articleList(where: {rating: {in: [1, 5]}}, sort: "rating") { items { title } }
		c: articleList(where: {tags: {eq: "x"}}) { items { title } } }`, nil)
	r.noErrors(t)
	eq(t, "a", path(r.data(), "a", "items", 0, "title"), "Second")
	eq(t, "b", fmt.Sprintf("%v %v", path(r.data(), "b", "items", 0, "title"), path(r.data(), "b", "items", 1, "title")), "Third Second")
	eq(t, "c", len(path(r.data(), "c", "items").([]any)), 1)

	// a bad filter is reported on the field, the rest of the query runs
	r = e.post("web", `{ bad: articleList(where: {nope: {eq: 1}}) { items { title } } ok: personList { items { fullName } } }`, nil)
	if r.Code != 200 || len(r.errors()) != 1 || path(r.errors(), 0, "extensions", "status") != float64(400) {
		t.Fatalf("bad filter: %d %s", r.Code, r.Body)
	}
	eq(t, "bad", r.data()["bad"], "<nil>")
	eq(t, "ok", len(path(r.data(), "ok", "items").([]any)), 2)

	// where must be an object
	r = e.post("web", `{ articleList(where: [1]) { items { title } } }`, nil)
	if len(r.errors()) != 1 {
		t.Fatalf("where array: %s", r.Body)
	}
}

func TestRefsUnionInterfaceFragments(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()

	q := `{
		article(key: "hello") {
			author { fullName company { title } }
			related { title author { fullName } }
			subject { __typename ... on Person { fullName } ... on Article { title } }
			logo { name }
		}
	}`
	r := e.post("web", q, nil)
	r.noErrors(t)
	a := path(r.data(), "article")
	eq(t, "author", path(a, "author", "fullName"), "Alice")
	eq(t, "company", path(a, "author", "company", "title"), "Acme")
	eq(t, "related", path(a, "related", 0, "title"), "Second")
	eq(t, "related author", path(a, "related", 0, "author", "fullName"), "Bob")
	eq(t, "subject", path(a, "subject", "__typename"), "Person")
	eq(t, "subject name", path(a, "subject", "fullName"), "Alice")
	eq(t, "logo", path(a, "logo", "name"), "cat.png")
	// one call for the article with include=3, then nothing: references come from "included"
	if e.calls != 1 {
		t.Errorf("calls = %d, want 1 (included references are reused)", e.calls)
	}

	// the Item interface with fragments, root item(id) and a list of mixed results
	r = e.post("web", `
		query($id: ID!) {
			item(id: $id) { ...common ... on Person { fullName } ... on Article { title } }
			alice: item(id: $alice) { __typename id name ...common }
		}
		fragment common on Item { id type locale }`, map[string]any{"id": f.a1.String()})
	if r.Code != 400 {
		t.Fatalf("undefined $alice should fail validation: %d", r.Code)
	}
	r = e.post("web", `
		query($id: ID!, $alice: ID!) {
			item(id: $id) { ...common ... on Person { fullName } ... on Article { title } }
			alice: item(id: $alice) { __typename id name ...common ... on Person { company { title } } }
		}
		fragment common on Item { id type locale }`, map[string]any{"id": f.a1.String(), "alice": f.alice.String()})
	r.noErrors(t)
	eq(t, "item type", path(r.data(), "item", "type"), "article")
	eq(t, "item title", path(r.data(), "item", "title"), "Hello")
	eq(t, "alice", path(r.data(), "alice", "__typename"), "Person")
	eq(t, "alice name", path(r.data(), "alice", "name"), "Alice")
	eq(t, "alice company", path(r.data(), "alice", "company", "title"), "Acme")

	// in the lim channel the logo target (image) is not available: the field is typed Item and null
	sdl := e.mod.sdlOf(t, "lim")
	if !strings.Contains(sdl, "logo: Item") {
		t.Errorf("lim logo is not Item:\n%s", sdl)
	}
	r = e.post("lim", `{ article(key: "hello") { title logo { id } author { fullName } } }`, nil)
	r.noErrors(t)
	if path(r.data(), "article", "logo") != nil {
		t.Errorf("logo in lim = %v", path(r.data(), "article", "logo"))
	}
	eq(t, "lim author", path(r.data(), "article", "author", "fullName"), "Alice")

	// a ref to an item that is not published is null and costs no call beyond the one with include
	e.fixtureUnpublishedRef(t)
}

func (e *env) fixtureUnpublishedRef(t *testing.T) {
	t.Helper()
	draft := e.create("person", "ghost", "en", `{"fullName":"Ghost"}`, false)
	e.create("article", "haunted", "en", fmt.Sprintf(`{"title":"Haunted","author":%q}`, draft), true)
	r := e.post("web", `{ article(key: "haunted") { title author { fullName } } }`, nil)
	r.noErrors(t)
	if path(r.data(), "article", "author") != nil {
		t.Errorf("author = %v", path(r.data(), "article", "author"))
	}
	if e.calls != 1 {
		t.Errorf("calls = %d, want 1", e.calls)
	}
}

func TestGroupsListsJSONAssets(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	r := e.post("web", `{ article(key: "hello") {
		address { street city geo { lat lng } }
		seo { metaTitle metaDescription }
		places { label rank }
		extra
		tags
		hero { id url name mime size width height alt fields focal variants }
		gallery { name }
		sections
		attachment { id }
	} }`, nil)
	r.noErrors(t)
	a := path(r.data(), "article")
	eq(t, "street", path(a, "address", "street"), "1 Main")
	eq(t, "lat", path(a, "address", "geo", "lat"), 1.5)
	if path(a, "address", "geo", "lng") != nil {
		t.Error("lng set")
	}
	eq(t, "seo", path(a, "seo", "metaTitle"), "SEO")
	eq(t, "places", fmt.Sprintf("%v %v", path(a, "places", 0, "label"), path(a, "places", 1, "rank")), "A 2")
	eq(t, "extra", fmt.Sprint(path(a, "extra", "k")), "[1 2 map[z:true]]")
	hero := path(a, "hero").(map[string]any)
	eq(t, "hero name", hero["name"], "cat.png")
	eq(t, "hero mime", hero["mime"], "image/png")
	eq(t, "hero width", hero["width"], 8)
	eq(t, "hero height", hero["height"], 6)
	eq(t, "hero alt", hero["alt"], "A cat")
	eq(t, "hero fields", path(hero, "fields", "alt"), "A cat")
	if s, _ := hero["url"].(string); !strings.HasPrefix(s, "/assets/") || !strings.HasSuffix(s, "/cat.png") {
		t.Errorf("hero url = %v", hero["url"])
	}
	if n, _ := hero["size"].(float64); n <= 0 {
		t.Errorf("hero size = %v", hero["size"])
	}
	eq(t, "gallery", path(a, "gallery", 0, "name"), "cat.png")
	if path(a, "attachment") != nil {
		t.Errorf("attachment = %v", path(a, "attachment"))
	}
	if e.calls != 1 {
		t.Errorf("calls = %d", e.calls)
	}

	// an image item has its file; the file is a File
	r = e.post("web", `{ imageList { items { alt file { id name mime size width height } tags folder } } }`, nil)
	r.noErrors(t)
	eq(t, "file name", path(r.data(), "imageList", "items", 0, "file", "name"), "cat.png")
	eq(t, "file width", path(r.data(), "imageList", "items", 0, "file", "width"), 8)
}

func TestSingleton(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	r := e.post("web", `{ settings { siteName type key } }`, nil)
	r.noErrors(t)
	eq(t, "siteName", path(r.data(), "settings", "siteName"), "Hydrogen")
	eq(t, "type", path(r.data(), "settings", "type"), "settings")
}

func TestLocale(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	r := e.post("web", `{ en: article(key: "hello") { title locale } de: article(key: "hello", locale: "de") { title locale }
		list: articleList(locale: "de", sort: "rating") { items { title locale } } }`, nil)
	r.noErrors(t)
	eq(t, "en", path(r.data(), "en", "title"), "Hello")
	eq(t, "de", path(r.data(), "de", "title"), "Hallo")
	eq(t, "de locale", path(r.data(), "de", "locale"), "de")
	// the other articles have no German version and fall back to English
	eq(t, "list", fmt.Sprintf("%v %v", path(r.data(), "list", "items", 0, "title"), path(r.data(), "list", "items", 0, "locale")), "Third en")

	// the request's ?locale= is the default
	r = e.post("web", `{ article(key: "hello") { title } }`, nil, "locale=de")
	r.noErrors(t)
	eq(t, "default locale", path(r.data(), "article", "title"), "Hallo")

	// a locale the channel does not have is an error on the field
	r = e.post("web", `{ article(key: "hello", locale: "fr") { title } }`, nil)
	if len(r.errors()) != 1 || path(r.errors(), 0, "extensions", "status") != float64(400) {
		t.Fatalf("fr: %d %s", r.Code, r.Body)
	}
}

func TestSearch(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	e.drain()
	r := e.post("web", `{ search(q: "Hello", types: ["article"]) { total hits items { __typename ... on Article { title author { fullName } } } } }`, nil)
	r.noErrors(t)
	if path(r.data(), "search", "items", 0, "title") != "Hello" {
		t.Fatalf("search: %s", r.Body)
	}
	eq(t, "author", path(r.data(), "search", "items", 0, "author", "fullName"), "Alice")
	if path(r.data(), "search", "hits") == nil {
		t.Error("no hits")
	}
	// an unknown type is the delivery API's 400, on the field
	r = e.post("web", `{ search(q: "x", types: ["nope"]) { total } }`, nil)
	if len(r.errors()) != 1 {
		t.Fatalf("bad types: %s", r.Body)
	}
}

func TestVariablesAndLiterals(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	r := e.post("web", `query($n: Int, $s: String, $w: JSON) { articleList(limit: $n, sort: $s, where: $w) { items { title } } }`,
		map[string]any{"n": 2, "s": "-rating", "w": map[string]any{"featured": true}})
	r.noErrors(t)
	eq(t, "items", fmt.Sprintf("%d %v", len(path(r.data(), "articleList", "items").([]any)), path(r.data(), "articleList", "items", 0, "title")), "1 Hello")
	r = e.post("web", `query($n: Int) { articleList(limit: $n, sort: "-rating") { items { title } } }`, map[string]any{"n": 2})
	r.noErrors(t)
	eq(t, "limited", len(path(r.data(), "articleList", "items").([]any)), 2)

	// variables inside a JSON literal are not supported: pass the whole value as a variable
	r = e.post("web", `query($r: Int) { articleList(where: {rating: {gte: $r}}) { items { title } } }`, map[string]any{"r": 3})
	if r.Code != 400 {
		t.Errorf("variable in literal: %d %s", r.Code, r.Body)
	}
}
