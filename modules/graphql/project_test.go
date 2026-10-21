package graphql

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// traced runs fn and returns the delivery requests it made.
func (e *env) traced(fn func()) []string {
	var mu sync.Mutex
	var reqs []string
	e.mod.trace = func(r string) {
		mu.Lock()
		reqs = append(reqs, r)
		mu.Unlock()
	}
	defer func() { e.mod.trace = nil }()
	fn()
	return reqs
}

func fieldsOf(t *testing.T, req string) string {
	t.Helper()
	_, q, _ := strings.Cut(req, "?")
	v, err := url.ParseQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	return v.Get("fields")
}

func TestProjectionList(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()
	var r result
	reqs := e.traced(func() {
		r = e.post("web", `fragment P on Person { fullName }
		{ articleList(sort: "-rating") { items { __typename t: title ... on Article { rating } author { ...P company { title } } } } }`, nil)
	})
	r.noErrors(t)
	if len(reqs) != 1 || fieldsOf(t, reqs[0]) != "author,rating,title" {
		t.Fatalf("requests: %v", reqs)
	}
	items := path(r.data(), "articleList", "items").([]any)
	eq(t, "title", path(items, 0, "t"), "Second")
	eq(t, "author", path(items, 1, "author", "fullName"), "Alice")
	eq(t, "company", path(items, 1, "author", "company", "title"), "Acme")
	eq(t, "typename", path(items, 1, "__typename"), "Article")
	_ = f
}

func TestProjectionEnvelopeOnly(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	var r result
	reqs := e.traced(func() {
		r = e.post("web", `{ articleList { items { id name key url } } a: article(key: "hello") { id } s: settings { id } }`, nil)
	})
	r.noErrors(t)
	// a small field keeps the response small: an empty fields would mean no projection
	for _, q := range reqs {
		if got := fieldsOf(t, q); got != "kind" && got != "" {
			t.Errorf("%s: fields %q", q, got)
		}
	}
	if len(reqs) != 3 {
		t.Errorf("requests: %v", reqs)
	}
	eq(t, "name", path(r.data(), "articleList", "items", 0, "name"), "Hello")
	eq(t, "id", path(r.data(), "a", "id") != nil, true)
}

func TestProjectionBroadAndUnanalysable(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()
	reqs := e.traced(func() {
		r := e.post("web", `{ article(key: "hello") { title subject { ... on ArticleOrCompanyOrPerson { __typename } ... on Person { fullName } } } }`, nil)
		r.noErrors(t)
		eq(t, "subject", path(r.data(), "article", "subject", "fullName"), "Alice")
	})
	if len(reqs) != 1 || fieldsOf(t, reqs[0]) != "subject,title" {
		t.Errorf("requests: %v", reqs)
	}
	// a fragment on a union cannot be mapped to fields: the whole item is fetched
	reqs = e.traced(func() {
		r := e.post("web", fmt.Sprintf(`{ item(id: %q) { ... on ArticleOrCompanyOrPerson { __typename } ... on Article { title } } }`, f.a1), nil)
		r.noErrors(t)
		eq(t, "title", path(r.data(), "item", "title"), "Hello")
	})
	if len(reqs) != 1 || strings.Contains(reqs[0], "fields=") {
		t.Errorf("union fragment must not project: %v", reqs)
	}
	// an interface fragment and the Item keys project to the fields of the concrete fragments
	reqs = e.traced(func() {
		r := e.post("web", fmt.Sprintf(`{ item(id: %q) { __typename id ... on Item { name } ... on Article { title } ... on Person { fullName } } }`, f.a1), nil)
		r.noErrors(t)
		eq(t, "title", path(r.data(), "item", "title"), "Hello")
	})
	if len(reqs) != 1 || fieldsOf(t, reqs[0]) != "fullName,title" {
		t.Errorf("requests: %v", reqs)
	}
}

func TestProjectionRegistryNotReused(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()
	var r result
	// Hello is listed with title only; its related article Second links back through a second list selecting
	// other fields, so a projected item must not answer a lookup that needs more.
	reqs := e.traced(func() {
		r = e.post("web", fmt.Sprintf(`{
			l: articleList(limit: 5) { items { title related { title author { fullName } } } }
			g: article(id: %q) { summary tags related { rating } }
		}`, f.a1.String()), nil)
	})
	r.noErrors(t)
	items := path(r.data(), "l", "items").([]any)
	var hello map[string]any
	for _, it := range items {
		if path(it, "title") == "Hello" {
			hello = it.(map[string]any)
		}
	}
	eq(t, "related", path(hello, "related", 0, "author", "fullName"), "Bob")
	eq(t, "summary", path(r.data(), "g", "summary"), "Hi there")
	eq(t, "rating", path(r.data(), "g", "related", 0, "rating"), 5)
	if len(reqs) > 4 {
		t.Errorf("too many calls: %v", reqs)
	}
}

func TestProjectionDoesNotDropBigFields(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	var r result
	reqs := e.traced(func() {
		r = e.post("web", `{ a: article(key: "hello") { title } b: article(key: "hello") { body address { city } } }`, nil)
	})
	r.noErrors(t)
	eq(t, "title", path(r.data(), "a", "title"), "Hello")
	eq(t, "body", path(r.data(), "b", "body", "type"), "doc")
	eq(t, "city", path(r.data(), "b", "address", "city"), "Metro")
	if len(reqs) != 2 {
		t.Fatalf("requests: %v", reqs)
	}
	// The two fields may resolve in either order.
	got := []string{fieldsOf(t, reqs[0]), fieldsOf(t, reqs[1])}
	slices.Sort(got)
	if got[0] != "address,body" || got[1] != "title" {
		t.Errorf("requests: %v", reqs)
	}
}
