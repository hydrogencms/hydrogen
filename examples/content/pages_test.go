package main

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

type pageNode struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Item     string     `json:"item"`
	Parent   string     `json:"parent"`
	Hidden   bool       `json:"hidden"`
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Target   string     `json:"target"`
	Children int        `json:"children"`
	Nodes    []pageNode `json:"nodes"`
}

type resolution struct {
	Kind     string `json:"kind"`
	Item     string `json:"item"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	Location string `json:"location"`
}

type siteFixture struct {
	s                            *server
	root, about, team, mount, ln pageNode
	articleID                    string
}

// create makes an item of a collection in en with the fields, and publishes it.
func (f *siteFixture) create(typ string, fields map[string]any) view {
	f.s.t.Helper()
	var v view
	f.s.must(f.s.do("POST", "/collections/"+typ+"/items", map[string]any{"locale": "en", "fields": fields}, &v), 201, "create "+typ)
	f.s.must(f.s.do("POST", "/items/"+v.ID+"/publish", map[string]any{}, &v), 200, "publish "+typ)
	return v
}

func (f *siteFixture) place(item view, parent string) pageNode {
	f.s.t.Helper()
	body := map[string]any{"kind": "page", "item": item.ID}
	if parent != "" {
		body["parent"] = parent
	}
	var n pageNode
	f.s.must(f.s.do("POST", "/pages/sites/main/nodes", body, &n), 201, "place "+item.Name)
	return n
}

func (f *siteFixture) resolve(locale, path string) resolution {
	f.s.t.Helper()
	var r resolution
	f.s.must(f.s.do("GET", "/pages/sites/main/resolve?locale="+locale+"&path="+url.QueryEscape(path), nil, &r), 200, "resolve "+path)
	return r
}

func (f *siteFixture) wantPage(locale, path, item string) {
	f.s.t.Helper()
	if r := f.resolve(locale, path); r.Status != 200 || r.Item != item {
		f.s.t.Errorf("%s %s: %+v, want item %s", locale, path, r, item)
	}
}

func (f *siteFixture) wantRedirect(locale, path, to string) {
	f.s.t.Helper()
	if r := f.resolve(locale, path); r.Kind != "redirect" || r.Status != 301 || r.Location != to {
		f.s.t.Errorf("%s %s: %+v, want 301 to %s", locale, path, r, to)
	}
}

func (f *siteFixture) wantGone(locale, path string) {
	f.s.t.Helper()
	if r := f.resolve(locale, path); r.Status != 404 || r.Kind != "not_found" {
		f.s.t.Errorf("%s %s: %+v, want not found", locale, path, r)
	}
}

func flatten(nodes []pageNode, into map[string]pageNode) {
	for _, n := range nodes {
		into[n.ID] = n
		flatten(n.Nodes, into)
	}
}

func (f *siteFixture) tree() map[string]pageNode {
	f.s.t.Helper()
	var out struct{ Items []pageNode }
	f.s.must(f.s.do("GET", "/pages/sites/main/tree?locale=en&depth=5", nil, &out), 200, "tree")
	all := map[string]pageNode{}
	flatten(out.Items, all)
	return all
}

func buildSite(t *testing.T, s *server) *siteFixture {
	f := &siteFixture{s: s}
	var sites struct{ Items []string }
	s.must(s.do("GET", "/pages/sites", nil, &sites), 200, "sites")
	if len(sites.Items) != 1 || sites.Items[0] != "main" {
		t.Fatalf("sites: %v", sites.Items)
	}

	home := f.create("page", map[string]any{"title": "Home"})
	f.root = f.place(home, "")
	about := f.create("page", map[string]any{"title": "About"})
	f.about = f.place(about, f.root.ID)
	team := f.create("page", map[string]any{"title": "Team"})
	f.team = f.place(team, f.about.ID)
	var tv view
	s.must(s.do("PATCH", "/items/"+team.ID, map[string]any{"locale": "de", "fields": map[string]any{"title": "Mannschaft"}}, &tv), 200, "team de")
	s.must(s.do("POST", "/items/"+team.ID+"/publish", map[string]any{"locales": []string{"de"}}, &tv), 200, "publish team de")

	s.must(s.do("POST", "/pages/sites/main/nodes", map[string]any{"kind": "mount", "parent": f.root.ID, "segment": "news",
		"collection": "article", "route": "{publishDate:yyyy}/{slug}", "label": map[string]string{"en": "News"}}, &f.mount), 201, "mount")
	art := f.create("article", map[string]any{"title": "Hello Hydrogen", "publishDate": "2026-10-14T10:00:00Z"})
	f.articleID = art.ID
	s.must(s.do("POST", "/pages/sites/main/nodes", map[string]any{"kind": "link", "parent": f.root.ID, "after": f.about.ID,
		"label": map[string]string{"en": "About us"}, "node": f.about.ID}, &f.ln), 201, "link")

	// Every page node is a root-first chain of the ones above.
	if f.root.Kind != "page" || f.about.Parent != f.root.ID || f.team.Parent != f.about.ID || f.mount.Kind != "mount" || f.ln.Kind != "link" {
		t.Fatalf("nodes: %+v %+v %+v %+v %+v", f.root, f.about, f.team, f.mount, f.ln)
	}
	return f
}

func pagesFlow(t *testing.T, s *server) {
	f := buildSite(t, s)
	home, about, team := f.root.Item, f.about.Item, f.team.Item

	f.wantPage("en", "/", home)
	f.wantPage("en", "/about", about)
	f.wantPage("en", "/about/team", team)
	f.wantPage("de", "/about/mannschaft", team) // About has no German slug: its main-locale slug is used
	f.wantGone("de", "/about/team")
	f.wantGone("en", "/about/mannschaft")
	f.wantGone("en", "/nope")
	if r := f.resolve("en", "/news/2026/hello-hydrogen"); r.Kind != "item" || r.Item != f.articleID || r.Status != 200 {
		t.Errorf("mounted article: %+v", r)
	}

	// Tree: names, paths, hidden, link targets.
	var kids struct{ Items []pageNode }
	s.must(s.do("GET", "/pages/sites/main/tree?locale=en&parent="+f.about.ID, nil, &kids), 200, "tree of about")
	if len(kids.Items) != 1 || kids.Items[0].ID != f.team.ID || kids.Items[0].Name != "Team" || kids.Items[0].Path != "/about/team" {
		t.Errorf("tree of about: %+v", kids.Items)
	}
	s.must(s.do("GET", "/pages/sites/main/tree?depth=11", nil, nil), 400, "depth too large")
	var tv view
	var patched pageNode
	s.must(s.do("PATCH", "/pages/nodes/"+f.team.ID, map[string]any{"hidden": true}, &patched), 200, "hide team")
	tree := f.tree()
	if n := tree[f.team.ID]; n.Name != "Team" || n.Path != "/about/team" || !n.Hidden {
		t.Errorf("team in tree: %+v", n)
	}
	if n := tree[f.ln.ID]; n.Kind != "link" || n.Name != "About us" || n.Target != "/about" {
		t.Errorf("link in tree: %+v", n)
	}
	if n := tree[f.mount.ID]; n.Kind != "mount" || n.Name != "News" || n.Path != "/news" {
		t.Errorf("mount in tree: %+v", n)
	}
	s.must(s.do("PATCH", "/pages/nodes/"+f.team.ID, map[string]any{"hidden": false}, &patched), 200, "unhide team")

	// Reordering: the link moves to the front among the root's children.
	s.must(s.do("PATCH", "/pages/nodes/"+f.ln.ID, map[string]any{"after": nil}, &patched), 200, "reorder link")
	var top struct{ Items []pageNode }
	s.must(s.do("GET", "/pages/sites/main/tree?locale=en&parent="+f.root.ID, nil, &top), 200, "tree of root")
	if len(top.Items) != 3 || top.Items[0].ID != f.ln.ID {
		t.Errorf("order after reorder: %+v", top.Items)
	}

	// A sibling with the same slug is rejected when published.
	dup := s.createDraft("page", map[string]any{"title": "About"})
	f.place(dup, f.root.ID)
	var bad apiError
	s.must(s.do("POST", "/items/"+dup.ID+"/publish", map[string]any{}, &bad), 422, "publish duplicate slug")
	if len(bad.Error.Fields) == 0 || bad.Error.Fields[0].Path != "slug" || bad.Error.Fields[0].Code != "slug_taken" {
		t.Errorf("slug collision: %+v", bad.Error)
	}
	f.wantPage("en", "/about", about)

	// Renaming About's slug: old paths redirect, new ones resolve, the article is untouched.
	s.must(s.do("PATCH", "/items/"+about, map[string]any{"locale": "en", "fields": map[string]any{"slug": "about-us"}}, &tv), 200, "rename slug")
	s.must(s.do("POST", "/items/"+about+"/publish", map[string]any{}, &tv), 200, "publish rename")
	f.wantPage("en", "/about-us", about)
	f.wantPage("en", "/about-us/team", team)
	f.wantRedirect("en", "/about", "/about-us")
	f.wantRedirect("en", "/about/team", "/about-us/team")
	if r := f.resolve("en", "/news/2026/hello-hydrogen"); r.Item != f.articleID {
		t.Errorf("article after rename: %+v", r)
	}
	if n := f.tree()[f.ln.ID]; n.Target != "/about-us" {
		t.Errorf("link target after rename: %+v", n)
	}

	// Moving Team under Home leaves a redirect from its old paths.
	s.must(s.do("PATCH", "/pages/nodes/"+f.team.ID, map[string]any{"parent": f.root.ID}, &patched), 200, "move team")
	f.wantPage("en", "/team", team)
	f.wantRedirect("en", "/about-us/team", "/team")
	f.wantRedirect("en", "/about/team", "/team")
	s.mustClientError(s.do("PATCH", "/pages/nodes/"+f.team.ID, map[string]any{"parent": f.team.ID}, nil), "move under itself")

	// Manual redirects work and cannot shadow a page.
	s.must(s.do("POST", "/collections/redirect/items", map[string]any{"locale": "en",
		"fields": map[string]any{"from": "/old-team", "to": "/team"}}, &tv), 201, "create redirect")
	s.must(s.do("POST", "/items/"+tv.ID+"/publish", map[string]any{}, &tv), 200, "publish redirect")
	f.wantRedirect("en", "/old-team", "/team")
	var shadow view
	s.must(s.do("POST", "/collections/redirect/items", map[string]any{"locale": "en",
		"fields": map[string]any{"from": "/team", "to": "/about-us"}}, &shadow), 201, "create shadowing redirect")
	s.must(s.do("POST", "/items/"+shadow.ID+"/publish", map[string]any{}, &bad), 422, "publish shadowing redirect")
	if len(bad.Error.Fields) == 0 || bad.Error.Fields[0].Path != "from" {
		t.Errorf("shadow: %+v", bad.Error)
	}
	f.wantPage("en", "/team", team)

	// Paths of an item.
	var paths struct {
		Items []struct{ Locale, Path, State string }
	}
	s.must(s.do("GET", "/pages/items/"+team+"/paths", nil, &paths), 200, "paths")
	states := map[string]string{}
	for _, p := range paths.Items {
		states[p.Locale+" "+p.Path] = p.State
	}
	if states["en /team"] != "current" || states["en /about-us/team"] != "redirect" || states["de /about-us/mannschaft"] == "" && states["de /mannschaft"] == "" {
		t.Errorf("paths: %v", states)
	}

	// Deleting the mount removes its routes.
	s.must(s.do("DELETE", "/pages/nodes/"+f.mount.ID, nil, nil), 204, "delete mount")
	f.wantGone("en", "/news/2026/hello-hydrogen")
	s.must(s.do("GET", "/pages/items/"+f.articleID+"/paths", nil, &paths), 200, "article paths")
	if len(paths.Items) != 0 {
		t.Errorf("article routes after mount delete: %+v", paths.Items)
	}
	s.must(s.do("GET", "/pages/nodes/"+f.mount.ID, nil, nil), 404, "mount gone")
	s.mustClientError(s.do("DELETE", "/pages/nodes/"+f.about.ID, nil, nil), "page nodes are not deleted through the nodes API")
}

func (s *server) mustClientError(status int, what string) {
	s.t.Helper()
	if status < 400 || status >= 500 {
		s.t.Fatalf("%s: status %d, want a 4xx", what, status)
	}
}

// createDraft makes a draft without publishing it.
func (s *server) createDraft(typ string, fields map[string]any) view {
	s.t.Helper()
	var v view
	s.must(s.do("POST", "/collections/"+typ+"/items", map[string]any{"locale": "en", "fields": fields}, &v), 201, "create "+typ)
	return v
}

func TestPagesSQLite(t *testing.T) {
	pagesFlow(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configure))
}

func TestPagesPostgres(t *testing.T) {
	pagesFlow(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configure))
}
