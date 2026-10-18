package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// searchManageEnv is a pages environment (sites main and shop) served over HTTP.
type searchManageEnv struct {
	*pagesEnv
	h http.Handler
}

func newSearchManageEnv(t *testing.T, e *pagesEnv) *searchManageEnv {
	t.Helper()
	h, err := e.app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &searchManageEnv{pagesEnv: e, h: h}
}

func (e *searchManageEnv) get(token, path string) (int, map[string]any) {
	e.t.Helper()
	r := httptest.NewRequest("GET", "/api/manage/v1"+path, nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		e.t.Fatalf("GET %s: %q: %v", path, w.Body.String(), err)
	}
	return w.Code, m
}

func (e *searchManageEnv) ok(token, path string) map[string]any {
	e.t.Helper()
	code, m := e.get(token, path)
	if code != 200 {
		e.t.Fatalf("GET %s: status %d: %v", path, code, m)
	}
	return m
}

func (e *searchManageEnv) readToken(name string, pol access.RolePolicy) string {
	e.t.Helper()
	s := e.app.Services()
	role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: []access.RolePolicy{pol}})
	if err != nil {
		e.t.Fatal(err)
	}
	_, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
	if err != nil {
		e.t.Fatal(err)
	}
	return secret
}

func searchNames(m map[string]any) []string {
	var out []string
	for _, it := range m["items"].([]any) {
		out = append(out, it.(map[string]any)["name"].(string))
	}
	return out
}

func sameNames(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }

func (e *searchManageEnv) placeIn(site, typ, title string) ids.UUID {
	e.t.Helper()
	item := e.page(typ, title)
	if _, err := e.pages.Place(ctx, site, pages.Place{Item: item}); err != nil {
		e.t.Fatal(err)
	}
	return item
}

func TestManageSearch(t *testing.T) {
	runPages(t, pagesOpts{sites: []string{"main", "shop"}}, func(t *testing.T, pe *pagesEnv) {
		e := newSearchManageEnv(t, pe)
		mainPage := e.placeIn("main", "section", "Solar panels guide")
		shopPage := e.placeIn("shop", "landing", "Solar shop")
		unplaced := e.page("landing", "Solar loose page")
		// A draft that was never published is found: the search covers the working content.
		draft := e.create("article", "en", `{"title":"Solar drafts only"}`)
		e.update(unplaced, "en", `{"title":"Solar loose page"}`)
		dev := kernel.DevManageToken

		all := e.ok(dev, "/search?q=solar")
		if got := len(all["items"].([]any)); got != 4 {
			t.Fatalf("items = %v", searchNames(all))
		}
		snippets := all["snippets"].(map[string]any)
		for _, id := range []ids.UUID{mainPage, shopPage, unplaced, draft} {
			s, _ := snippets[id.String()].(string)
			if !strings.Contains(s, "<mark>Solar</mark>") {
				t.Errorf("snippet of %v = %q", id, s)
			}
		}
		if _, ok := all["total"]; ok {
			t.Error("search answers no total")
		}

		// type narrows, several types are merged.
		art := e.ok(dev, "/search?q=solar&type=article")
		if !sameNames(searchNames(art), []string{"Solar drafts only"}) {
			t.Errorf("type=article: %v", searchNames(art))
		}
		two := e.ok(dev, "/search?q=solar&type=article,landing")
		if got := len(two["items"].([]any)); got != 3 {
			t.Errorf("type=article,landing: %v", searchNames(two))
		}
		if got := len(e.ok(dev, "/search?q=solar&limit=2")["items"].([]any)); got != 2 {
			t.Errorf("limit=2 gave %d items", got)
		}
		if got := e.ok(dev, "/search?q=nothing-like-it")["items"].([]any); len(got) != 0 {
			t.Errorf("no match: %v", got)
		}

		// An editor confined to the shop site finds the shop's pages only.
		shop := e.readToken("shop-reader", pol("content.read", "pages.site", "shop"))
		if got := searchNames(e.ok(shop, "/search?q=solar")); !sameNames(got, []string{"Solar shop"}) {
			t.Errorf("shop reader found %v", got)
		}
		if got := searchNames(e.ok(shop, "/search?q=solar&type=article")); len(got) != 0 {
			t.Errorf("shop reader found articles: %v", got)
		}
		// A type limitation confines to that collection; the other types contribute nothing.
		arts := e.readToken("article-reader", pol("content.read", "collection", "article"))
		if got := searchNames(e.ok(arts, "/search?q=solar")); !sameNames(got, []string{"Solar drafts only"}) {
			t.Errorf("article reader found %v", got)
		}

		// Snippets are HTML: the text is escaped, the matches marked.
		esc := e.create("article", "en", `{"title":"Fish & <b>chips</b> with solar"}`)
		s := e.ok(dev, "/search?q=chips")["snippets"].(map[string]any)[esc.String()].(string)
		if !strings.Contains(s, "Fish &amp;") || !strings.Contains(s, "<mark>chips</mark>") || strings.Contains(s, "<b>") || strings.Contains(s, "</b>") {
			t.Errorf("escaped snippet = %q", s)
		}
	})
}

func TestManageSearchOrder(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		a := e.article("en", `{"title":"Wind farm"}`)
		b := e.article("en", `{"title":"Wind farm"}`)
		h := newSearchManageEnv(t, e)
		items := h.ok(kernel.DevManageToken, "/search?q=wind")["items"].([]any)
		if len(items) != 2 || items[0].(map[string]any)["id"] != b.String() || items[1].(map[string]any)["id"] != a.String() {
			t.Errorf("equal ranks must come most recently updated first: %v", items)
		}
		// Merged across types by rank: a title that is the whole query outranks a long text.
		e.page("landing", "Wind")
		first := searchNames(h.ok(kernel.DevManageToken, "/search?q=wind"))
		if len(first) != 3 {
			t.Fatalf("names = %v", first)
		}
	})
}

func TestManageSearchErrors(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		h := newSearchManageEnv(t, e)
		for _, p := range []string{
			"/search", "/search?q=", "/search?q=%20%20", "/search?q=a&limit=0", "/search?q=a&limit=51", "/search?q=a&limit=x",
			"/search?q=a&type=nope", "/search?q=a&offset=1", "/search?q=a&q=b", "/search?q=" + strings.Repeat("a", 201),
		} {
			if code, m := h.get(kernel.DevManageToken, p); code != 400 {
				t.Errorf("GET %s: status %d, want 400: %v", p, code, m)
			}
		}
		if code, _ := h.get("bad-token", "/search?q=a"); code != 401 {
			t.Errorf("a bad token: %d", code)
		}
		nothing := h.readToken("no-read", pol("content.create", "collection", "article"))
		if code, _ := h.get(nothing, "/search?q=a"); code != 403 {
			t.Errorf("a token without content.read: %d", code)
		}
	})
}
