package pages

import (
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// treeFixture builds a small tree without a database:
//
//	root (en: home)
//	├─ a (en: about, de: ueber)         sort "a1"
//	│  └─ b (en: team)                  sort "a1"
//	├─ news (mount, canonical)          sort "a2"
//	└─ archive (mount)                  sort "a0"
func treeFixture() (*graph, map[string]ids.UUID) {
	g := newGraph(nil)
	id := map[string]ids.UUID{}
	add := func(name string, parent string, sortKey, kind string, spec *MountSpec) {
		u := ids.UUID{byte(len(id) + 1)}
		id[name] = u
		n := &gnode{nodeRow: nodeRow{ID: u, Site: "main", Kind: kind, SortKey: sortKey, MainLocale: "en"}, mount: spec}
		if parent != "" {
			p := id[parent]
			n.Parent = &p
		}
		item := ids.UUID{0xf0, byte(len(id))}
		if kind == KindPage {
			n.Item = &item
		}
		g.nodes[u] = n
	}
	add("root", "", "a0", KindPage, nil)
	add("a", "root", "a1", KindPage, nil)
	add("b", "a", "a1", KindPage, nil)
	add("news", "root", "a2", KindMount, &MountSpec{Segment: "news", Canonical: true})
	add("archive", "root", "a0", KindMount, &MountSpec{Segment: "archive"})
	slug := func(node, locale, s string) { g.slugs[slugKey{id[node], locale}] = slugRow{Slug: s, Vanity: ""} }
	slug("root", "en", "ignored")
	slug("a", "en", "about")
	slug("a", "de", "ueber")
	slug("b", "en", "team")
	return g, id
}

func TestGraphPaths(t *testing.T) {
	g, id := treeFixture()
	for _, c := range []struct {
		node, locale, want string
		ok                 bool
	}{
		{"root", "en", "/", true},
		{"root", "de", "/", true},
		{"a", "en", "/about", true},
		{"a", "de", "/ueber", true},
		{"b", "en", "/about/team", true},
		{"b", "de", "/ueber/team", true}, // b has no German slug: its main-locale slug stands in
		{"news", "en", "/news", true},
		{"archive", "de", "/archive", true},
	} {
		got, ok := g.pathOf(id[c.node], c.locale)
		if got != c.want || ok != c.ok {
			t.Errorf("%s/%s = %q, %v; want %q, %v", c.node, c.locale, got, ok, c.want, c.ok)
		}
	}
	// A page without any slug has no path, nor do its descendants.
	delete(g.slugs, slugKey{id["a"], "en"})
	clear(g.memo)
	if p, ok := g.pathOf(id["b"], "en"); ok {
		t.Errorf("b under a slugless parent = %q", p)
	}
	if p, ok := g.pathOf(id["b"], "de"); !ok || p != "/ueber/team" {
		t.Errorf("b/de = %q, %v", p, ok)
	}
}

func TestGraphPageWants(t *testing.T) {
	g, id := treeFixture()
	g.slugs[slugKey{id["a"], "en"}] = slugRow{Slug: "about", Vanity: "/promo"}
	g.slugs[slugKey{id["b"], "en"}] = slugRow{Slug: "team", Vanity: "/about/team"} // equal to its own path: no alias
	var nodes []*gnode
	for _, n := range g.nodes {
		nodes = append(nodes, n)
	}
	var got []string
	for _, w := range g.pageWants(nodes, nil) {
		got = append(got, w.Locale+" "+w.State+" "+w.Path)
	}
	slices.Sort(got)
	want := []string{"de current /ueber", "en alias /promo", "en current /", "en current /about", "en current /about/team"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("wants = %q, want %q", got, want)
	}
	got = nil
	for _, w := range g.pageWants(nodes, []string{"de"}) {
		got = append(got, w.Locale+" "+w.State+" "+w.Path)
	}
	if !slices.Equal(got, []string{"de current /ueber"}) {
		t.Errorf("de wants = %q", got)
	}
}

func TestGraphMountWants(t *testing.T) {
	g, id := treeFixture()
	item := ids.UUID{0xaa}
	ms := []membership{
		{Node: id["archive"], Item: item, Locale: "en", Rel: "2026/x"},
		{Node: id["news"], Item: item, Locale: "en", Rel: "x"},
		{Node: id["news"], Item: item, Locale: "de", Rel: "y"},
	}
	var got []string
	for _, w := range g.mountWants(ms, nil) {
		got = append(got, w.Locale+" "+w.State+" "+w.Path)
	}
	// news is canonical although archive sorts first in the tree.
	want := []string{"de current /news/y", "en alias /archive/2026/x", "en current /news/x"}
	if !slices.Equal(got, want) {
		t.Errorf("wants = %q, want %q", got, want)
	}
	// Without a canonical flag the first mount in tree order wins.
	g.nodes[id["news"]].mount.Canonical = false
	got = nil
	for _, w := range g.mountWants(ms, []string{"en"}) {
		got = append(got, w.Locale+" "+w.State+" "+w.Path)
	}
	want = []string{"en current /archive/2026/x", "en alias /news/x"}
	if !slices.Equal(got, want) {
		t.Errorf("wants = %q, want %q", got, want)
	}
}

func TestGraphCanonicalPath(t *testing.T) {
	g, id := treeFixture()
	g.slugs[slugKey{id["a"], "en"}] = slugRow{Slug: "about", Canonical: "/company"}
	g.slugs[slugKey{id["root"], "en"}] = slugRow{Slug: "ignored", Canonical: "/nope"}
	g.slugs[slugKey{id["b"], "de"}] = slugRow{Slug: "mannschaft", Canonical: "no-slash"} // invalid: ignored
	for _, c := range []struct{ node, locale, want string }{
		{"a", "en", "/company"},
		{"b", "en", "/company/team"},
		{"root", "en", "/"},
		{"a", "de", "/ueber"},
		{"b", "de", "/ueber/mannschaft"},
	} {
		if got, _ := g.pathOf(id[c.node], c.locale); got != c.want {
			t.Errorf("%s/%s = %q, want %q", c.node, c.locale, got, c.want)
		}
	}
	var nodes []*gnode
	for _, n := range g.nodes {
		nodes = append(nodes, n)
	}
	var got []string
	for _, w := range g.pageWants(nodes, []string{"en"}) {
		got = append(got, w.State+" "+w.Path)
	}
	slices.Sort(got)
	want := []string{"current /", "current /company", "current /company/team", "redirect /about"}
	if !slices.Equal(got, want) {
		t.Errorf("wants = %q, want %q", got, want)
	}
}
