package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
)

type fakeAPI struct {
	calls []string
	node  *Node
	err   error
	place Place
	mount Mount
	link  Link
	upd   NodeUpdate
	depth int
	tree  []TreeNode
	res   *Resolution
	loc   *Located
	hdr   http.Header
}

func (f *fakeAPI) rec(s string) { f.calls = append(f.calls, s) }
func (f *fakeAPI) Sites() []string {
	f.rec("sites")
	return nil
}
func (f *fakeAPI) Place(_ context.Context, site string, in Place) (*Node, error) {
	f.rec("place " + site)
	f.place = in
	return f.node, f.err
}
func (f *fakeAPI) AddMount(_ context.Context, site string, in Mount) (*Node, error) {
	f.rec("mount " + site)
	f.mount = in
	return f.node, f.err
}
func (f *fakeAPI) AddLink(_ context.Context, site string, in Link) (*Node, error) {
	f.rec("link " + site)
	f.link = in
	return f.node, f.err
}
func (f *fakeAPI) Node(_ context.Context, id ids.UUID) (*Node, error) {
	f.rec("node " + id.String())
	return f.node, f.err
}
func (f *fakeAPI) Update(_ context.Context, id ids.UUID, in NodeUpdate) (*Node, error) {
	f.rec("update " + id.String())
	f.upd = in
	return f.node, f.err
}
func (f *fakeAPI) DeleteNode(_ context.Context, id ids.UUID) error {
	f.rec("delete " + id.String())
	return f.err
}
func (f *fakeAPI) Tree(_ context.Context, site, locale string, parent *ids.UUID, depth int) ([]TreeNode, error) {
	f.rec(fmt.Sprintf("tree %s %s %v", site, locale, parent != nil))
	f.depth = depth
	return f.tree, f.err
}
func (f *fakeAPI) Resolve(_ context.Context, site, locale, path string) (*Resolution, error) {
	f.rec(fmt.Sprintf("resolve %s %s %s", site, locale, path))
	return f.res, f.err
}
func (f *fakeAPI) Paths(_ context.Context, item ids.UUID) ([]Route, error) {
	f.rec("paths " + item.String())
	return nil, f.err
}

func (f *fakeAPI) Locate(_ context.Context, rawURL string, h http.Header) (*Located, error) {
	f.rec("locate " + rawURL)
	f.hdr = h
	return f.loc, f.err
}

var (
	idA = ids.UUID{1}
	idB = ids.UUID{2}
	idC = ids.UUID{3}
)

func newHandler(f *fakeAPI) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range apiRoutes(func() pagesAPI { return f }) {
		mux.HandleFunc(rt.Method+" "+rt.Path, rt.Handler)
	}
	return http.StripPrefix("/api/manage/v1", mux)
}

func send(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/manage/v1"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errCodeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("not an error body: %s", w.Body)
	}
	return e.Error.Code
}

func TestAPISitesAndEmptyLists(t *testing.T) {
	h := newHandler(&fakeAPI{})
	if w := send(h, "GET", "/pages/sites", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Errorf("sites: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/pages/items/"+idA.String()+"/paths", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Errorf("paths: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/pages/sites/main/tree", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Errorf("tree: %d %s", w.Code, w.Body)
	}
}

func TestAPITreeParams(t *testing.T) {
	f := &fakeAPI{}
	h := newHandler(f)
	if w := send(h, "GET", "/pages/sites/main/tree", ""); w.Code != 200 || f.depth != 1 || f.calls[0] != "tree main  false" {
		t.Errorf("defaults: %d %v %d", w.Code, f.calls, f.depth)
	}
	if w := send(h, "GET", "/pages/sites/main/tree?locale=de&depth=10&parent="+idA.String(), ""); w.Code != 200 || f.depth != 10 || f.calls[1] != "tree main de true" {
		t.Errorf("params: %d %v", w.Code, f.calls)
	}
	for _, q := range []string{"depth=0", "depth=11", "depth=x", "parent=nope"} {
		if w := send(h, "GET", "/pages/sites/main/tree?"+q, ""); w.Code != 400 || errCodeOf(t, w) != "bad_request" {
			t.Errorf("%s: %d %s", q, w.Code, w.Body)
		}
	}
	if len(f.calls) != 2 {
		t.Errorf("service called on bad input: %v", f.calls)
	}
}

func TestAPICreate(t *testing.T) {
	f := &fakeAPI{node: &Node{ID: idC, Site: "main", Kind: KindPage}}
	h := newHandler(f)
	w := send(h, "POST", "/pages/sites/main/nodes", fmt.Sprintf(`{"kind":"page","item":%q,"parent":%q,"after":%q}`, idA, idB, idC))
	if w.Code != 201 || w.Header().Get("Location") != "/api/manage/v1/pages/nodes/"+idC.String() {
		t.Fatalf("page: %d %v %s", w.Code, w.Header(), w.Body)
	}
	if f.place.Item != idA || *f.place.Parent != idB || *f.place.After != idC {
		t.Errorf("place: %+v", f.place)
	}
	// The root page: no parent.
	send(h, "POST", "/pages/sites/main/nodes", fmt.Sprintf(`{"kind":"page","item":%q}`, idA))
	if f.place.Parent != nil {
		t.Errorf("root place: %+v", f.place)
	}

	w = send(h, "POST", "/pages/sites/main/nodes", fmt.Sprintf(`{"kind":"mount","parent":%q,"segment":"news","collection":"article",
		"filter":{"kind":["news"]},"route":"{publishDate:yyyy}/{slug}","canonical":true,"label":{"en":"News"}}`, idB))
	if w.Code != 201 || f.mount.Parent != idB || f.mount.Spec.Segment != "news" || !f.mount.Spec.Canonical ||
		f.mount.Spec.Label["en"] != "News" || f.mount.Spec.Filter["kind"] == nil {
		t.Fatalf("mount: %d %s %+v", w.Code, w.Body, f.mount)
	}

	w = send(h, "POST", "/pages/sites/main/nodes", fmt.Sprintf(`{"kind":"link","parent":%q,"label":{"en":"About"},"node":%q}`, idB, idA))
	if w.Code != 201 || f.link.Spec.Node == nil || *f.link.Spec.Node != idA || f.link.Spec.Label["en"] != "About" {
		t.Fatalf("link: %d %s %+v", w.Code, w.Body, f.link)
	}
}

func TestAPICreateValidation(t *testing.T) {
	f := &fakeAPI{node: &Node{}}
	h := newHandler(f)
	bodies := map[string]string{
		"no kind":          `{}`,
		"bad kind":         `{"kind":"x"}`,
		"page no item":     `{"kind":"page"}`,
		"page bad item":    `{"kind":"page","item":"nope"}`,
		"page extra":       `{"kind":"page","item":"` + idA.String() + `","segment":"a"}`,
		"unknown key":      `{"kind":"page","item":"` + idA.String() + `","nope":1}`,
		"mount no parent":  `{"kind":"mount","segment":"a","collection":"b","route":"{slug}"}`,
		"mount no route":   `{"kind":"mount","parent":"` + idA.String() + `","segment":"a","collection":"b"}`,
		"link no label":    `{"kind":"link","parent":"` + idA.String() + `","url":"/x"}`,
		"link no target":   `{"kind":"link","parent":"` + idA.String() + `","label":{"en":"x"}}`,
		"link two targets": `{"kind":"link","parent":"` + idA.String() + `","label":{"en":"x"},"url":"/x","node":"` + idB.String() + `"}`,
		"label type":       `{"kind":"link","parent":"` + idA.String() + `","label":"x","url":"/x"}`,
	}
	for name, body := range bodies {
		if w := send(h, "POST", "/pages/sites/main/nodes", body); w.Code != 400 || errCodeOf(t, w) != "bad_request" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("service called on bad input: %v", f.calls)
	}
}

func TestAPIPatch(t *testing.T) {
	parent := idB
	f := &fakeAPI{node: &Node{ID: idA, Parent: &parent}}
	h := newHandler(f)
	p := "/pages/nodes/" + idA.String()

	if w := send(h, "PATCH", p, fmt.Sprintf(`{"parent":%q,"after":%q,"hidden":true}`, idC, idB)); w.Code != 200 ||
		f.upd.Move == nil || f.upd.Move.Parent != idC || *f.upd.Move.After != idB || f.upd.Hidden == nil || !*f.upd.Hidden {
		t.Fatalf("move: %d %s %+v", w.Code, w.Body, f.upd)
	}
	if send(h, "PATCH", p, fmt.Sprintf(`{"parent":%q}`, idC)); f.upd.Move.After != nil {
		t.Errorf("move without after = first: %+v", f.upd.Move)
	}
	// Reorder among the current siblings: the handler fetches the node first.
	f.calls = nil
	if w := send(h, "PATCH", p, fmt.Sprintf(`{"after":%q}`, idC)); w.Code != 200 ||
		f.upd.Move == nil || f.upd.Move.Parent != idB || *f.upd.Move.After != idC || len(f.calls) != 2 || !strings.HasPrefix(f.calls[0], "node ") {
		t.Fatalf("reorder: %d %s %+v %v", w.Code, w.Body, f.upd.Move, f.calls)
	}
	if w := send(h, "PATCH", p, `{"after":null}`); w.Code != 200 || f.upd.Move == nil || f.upd.Move.After != nil || f.upd.Move.Parent != idB {
		t.Fatalf("reorder first: %d %s %+v", w.Code, w.Body, f.upd.Move)
	}
	if send(h, "PATCH", p, `{"hidden":false}`); f.upd.Move != nil || f.upd.Hidden == nil || *f.upd.Hidden {
		t.Errorf("hidden only: %+v", f.upd)
	}
	if send(h, "PATCH", p, `{"mount":{"segment":"s","collection":"c","route":"{slug}"}}`); f.upd.Mount == nil || f.upd.Mount.Segment != "s" {
		t.Errorf("mount: %+v", f.upd)
	}
	if send(h, "PATCH", p, `{"link":{"label":{"en":"x"},"url":"/x"}}`); f.upd.Link == nil || f.upd.Link.URL != "/x" {
		t.Errorf("link: %+v", f.upd)
	}

	f.calls = nil
	for _, body := range []string{`{}`, `{"after":"nope"}`, `{"after":3}`, `{"parent":"nope"}`, `{"nope":1}`,
		`{"mount":{"segment":"s","collection":"c","route":"r"},"link":{"label":{"en":"x"},"url":"/x"}}`} {
		if w := send(h, "PATCH", p, body); w.Code != 400 {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := send(h, "PATCH", "/pages/nodes/nope", `{"hidden":true}`); w.Code != 400 {
		t.Errorf("bad id: %d", w.Code)
	}
	if len(f.calls) != 0 {
		t.Errorf("service called on bad input: %v", f.calls)
	}

	f.node = &Node{ID: idA} // the root cannot be reordered
	if w := send(h, "PATCH", p, fmt.Sprintf(`{"after":%q}`, idC)); w.Code != 400 {
		t.Errorf("root reorder: %d %s", w.Code, w.Body)
	}
}

func TestAPIErrorsMapped(t *testing.T) {
	f := &fakeAPI{}
	h := newHandler(f)
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("node: %w", content.ErrNotFound), 404, "not_found"},
		{fmt.Errorf("bad: %w", content.ErrBadRequest), 400, "bad_request"},
		{fmt.Errorf("busy: %w", content.ErrConflict), 409, "conflict"},
		{fmt.Errorf("boom"), 500, "internal"},
	}
	for _, tc := range tests {
		f.err = tc.err
		for _, req := range [][3]string{
			{"GET", "/pages/nodes/" + idA.String(), ""},
			{"DELETE", "/pages/nodes/" + idA.String(), ""},
			{"GET", "/pages/sites/main/resolve?path=/x", ""},
			{"GET", "/pages/items/" + idA.String() + "/paths", ""},
			{"GET", "/pages/sites/main/tree", ""},
		} {
			if w := send(h, req[0], req[1], req[2]); w.Code != tc.status || errCodeOf(t, w) != tc.code {
				t.Errorf("%v %s: %d %s", tc.err, req[1], w.Code, w.Body)
			}
		}
	}
}

func TestAPIDeleteAndResolve(t *testing.T) {
	f := &fakeAPI{res: &Resolution{Kind: ResolvedNotFound, Status: 404}}
	h := newHandler(f)
	if w := send(h, "DELETE", "/pages/nodes/"+idA.String(), ""); w.Code != 204 || w.Body.Len() != 0 {
		t.Errorf("delete: %d %s", w.Code, w.Body)
	}
	// A lookup: a not-found resolution is still a 200.
	w := send(h, "GET", "/pages/sites/main/resolve?locale=de&path=/nope", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":404`) || f.calls[len(f.calls)-1] != "resolve main de /nope" {
		t.Errorf("resolve: %d %s %v", w.Code, w.Body, f.calls)
	}
	if w := send(h, "GET", "/pages/sites/main/resolve", ""); w.Code != 400 {
		t.Errorf("resolve without path: %d", w.Code)
	}
	if w := send(h, "GET", "/pages/nodes/nope", ""); w.Code != 400 {
		t.Errorf("bad id: %d", w.Code)
	}
}

func TestAPINotReady(t *testing.T) {
	var s *Service
	mux := http.NewServeMux()
	for _, rt := range routes(func() *Service { return s }) {
		mux.HandleFunc(rt.Method+" "+rt.Path, rt.Handler)
	}
	if w := send(http.StripPrefix("/api/manage/v1", mux), "GET", "/pages/sites", ""); w.Code != 503 {
		t.Errorf("status %d", w.Code)
	}
}

func TestAPIOpenAPI(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range routes(func() *Service { return nil }) {
		if rt.OpenAPI == nil || rt.Summary == "" {
			t.Errorf("%s %s: missing OpenAPI or summary", rt.Method, rt.Path)
			continue
		}
		id, _ := rt.OpenAPI["operationId"].(string)
		if id == "" || seen[id] {
			t.Errorf("%s %s: operationId %q", rt.Method, rt.Path, id)
		}
		seen[id] = true
		b, err := json.Marshal(rt.OpenAPI)
		if err != nil {
			t.Errorf("%s %s: %v", rt.Method, rt.Path, err)
		}
		resp := rt.OpenAPI["responses"].(obj)
		if resp["401"] == nil {
			t.Errorf("%s %s: no 401", rt.Method, rt.Path)
		}
		if rt.Method == "POST" && !strings.Contains(string(b), `"oneOf"`) {
			t.Errorf("create lacks oneOf: %s", b)
		}
		for _, name := range []string{"site", "id"} {
			if strings.Contains(rt.Path, "{"+name+"}") && !strings.Contains(string(b), `"name":"`+name+`"`) {
				t.Errorf("%s %s: parameter %s undocumented", rt.Method, rt.Path, name)
			}
		}
	}
	if len(seen) != 9 {
		t.Errorf("%d operations", len(seen))
	}
}

func TestAPILocate(t *testing.T) {
	f := &fakeAPI{loc: &Located{Channel: "web", Kind: ResolvedPage}}
	h := newHandler(f)
	w := send(h, "GET", "/pages/locate?url=https%3A%2F%2Fa.test%2Fen%2Fx&header.x-market=sg&header.Accept-Language=de", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"channel":"web"`) || f.calls[len(f.calls)-1] != "locate https://a.test/en/x" {
		t.Errorf("locate: %d %s %v", w.Code, w.Body, f.calls)
	}
	if f.hdr.Get("X-Market") != "sg" || f.hdr.Get("Accept-Language") != "de" {
		t.Errorf("headers: %v", f.hdr)
	}
	if w := send(h, "GET", "/pages/locate", ""); w.Code != 400 {
		t.Errorf("locate without url: %d", w.Code)
	}
}
