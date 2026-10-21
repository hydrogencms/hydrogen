package site

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/core/channel"
)

func theme(name, base string, files map[string]string) Theme {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return Theme{Name: name, Base: base, FS: m}
}

func TestThemeChain(t *testing.T) {
	x := newExtensions()
	for _, th := range []Theme{theme("base", "", nil), theme("child", "base", nil), theme("grand", "child", nil)} {
		if err := x.AddTheme(th); err != nil {
			t.Fatal(err)
		}
	}
	chain, err := x.chain("grand")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, th := range chain {
		names = append(names, th.Name)
	}
	if strings.Join(names, ">") != "base>child>grand" {
		t.Errorf("chain = %v", names)
	}
	if chain, err := x.chain("starter"); err != nil || len(chain) != 1 {
		t.Errorf("starter chain = %v, %v", chain, err)
	}

	bad := newExtensions()
	for _, th := range []Theme{theme("a", "b", nil), theme("b", "c", nil), theme("c", "a", nil), theme("lost", "ghost", nil)} {
		if err := bad.AddTheme(th); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"a":       "theme cycle: a -> b -> c -> a",
		"lost":    `theme lost: base "ghost" is not registered`,
		"missing": `unknown theme "missing"`,
	} {
		if _, err := bad.chain(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("chain(%s) = %v, want %q", name, err, want)
		}
	}
	// every registered theme is validated, not only the selected one
	if err := bad.checkThemes("starter"); err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("checkThemes = %v", err)
	}
	if err := x.checkThemes("grand"); err != nil {
		t.Error(err)
	}

	for _, th := range []Theme{{Name: "Bad"}, {Name: "x"}, theme("self", "self", nil), theme("x", "Bad Base", nil)} {
		if err := newExtensions().AddTheme(th); err == nil {
			t.Errorf("%+v accepted", th)
		}
	}
	if err := x.AddTheme(theme("base", "", nil)); err == nil {
		t.Error("a theme registered twice")
	}
	if err := x.AddTheme(theme("starter", "", nil)); err == nil {
		t.Error("the starter theme was replaced")
	}
}

func TestThemeLayering(t *testing.T) {
	x := newExtensions()
	_ = x.AddTheme(theme("base", "", map[string]string{
		"templates/layout.html":        `base-layout{{block "content" .}}{{end}}`,
		"templates/pages/default.html": `{{define "content"}}base-page{{end}}`,
		"templates/partials/a.html":    `A-base`,
		"translations/en.yaml":         "a: base-a\nb: base-b\n",
		"assets/x.css":                 "body{}",
		"assets/y.css":                 "p{}",
	}))
	_ = x.AddTheme(theme("child", "base", map[string]string{
		"templates/layout.html": `child-layout{{block "content" .}}{{end}}`,
		"translations/en.yaml":  "b: child-b\n",
		"assets/y.css":          "p{color:red}",
	}))
	dir := t.TempDir()
	for name, data := range map[string]string{
		"templates/pages/default.html": `{{define "content"}}project-page{{end}}`,
		"translations/en.yaml":         "c: project-c\n",
		"translations/de.yaml":         "a: projekt-a\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	chain, err := x.chain("child")
	if err != nil {
		t.Fatal(err)
	}
	e, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		"layout.html":        "theme child/templates",
		"partials/a.html":    "theme base/templates",
		"pages/default.html": dir,
	} {
		if got := e.sources[file]; !strings.Contains(got, want) {
			t.Errorf("%s comes from %q, want %q", file, got, want)
		}
	}
	// translations merge key by key across layers
	for loc, kv := range map[string]map[string]string{
		"en": {"a": "base-a", "b": "child-b", "c": "project-c"},
		"de": {"a": "projekt-a"},
	} {
		for k, v := range kv {
			if got := e.trans[loc][k]; got != v {
				t.Errorf("%s.%s = %q, want %q", loc, k, got, v)
			}
		}
	}
	// assets: the nearest layer wins; the starter's are not part of this chain
	if _, ok := e.assets.url("x.css"); !ok {
		t.Error("base asset missing")
	}
	if _, ok := e.assets.url("site.css"); ok {
		t.Error("the starter's asset leaked into another chain")
	}
	f := e.assets.byURL[strings.TrimPrefix(mustURL(t, e, "y.css"), assetsURL)]
	if f.where == "" || !strings.Contains(f.where, "theme child/assets") {
		t.Errorf("y.css from %q", f.where)
	}
	// the starter-based chain still has the starter's files, with the project over them
	sc, _ := x.chain("starter")
	se, err := loadEngine(&sources{chain: sc, dir: dir}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !se.has("blocks/hero.html") || !strings.Contains(se.sources["pages/default.html"], dir) {
		t.Errorf("starter chain: %v", se.sources["pages/default.html"])
	}
}

func mustURL(t *testing.T, e *engine, name string) string {
	t.Helper()
	u, ok := e.assets.url(name)
	if !ok {
		t.Fatalf("no asset %s", name)
	}
	return u
}

type fakeRenderer struct{ out string }

func (f fakeRenderer) Render(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, f.out)
	return err
}

func TestComponentLookup(t *testing.T) {
	x := newExtensions()
	var seen *View
	for name, fn := range map[string]ComponentFunc{
		"pages/custom.html": func(v *View) Renderer {
			seen = v
			return fakeRenderer{"<main>component page " + v.T("home") + "</main>"}
		},
		"pages/default.html": func(v *View) Renderer { return fakeRenderer{"component default"} },
		"blocks/hero.html":   func(v *View) Renderer { return fakeRenderer{"<section hero " + v.Block.ID + ">"} },
		"widgets/card.html":  func(v *View) Renderer { return fakeRenderer{"<aside>card</aside>"} },
	} {
		if err := x.AddComponent(name, fn); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"", "/abs", "../x"} {
		if x.AddComponent(bad, func(*View) Renderer { return nil }) == nil {
			t.Errorf("component name %q accepted", bad)
		}
	}
	if x.AddComponent("pages/custom.html", func(*View) Renderer { return nil }) == nil || x.AddComponent("z", nil) == nil {
		t.Error("duplicate or nil component accepted")
	}
	chain, _ := x.chain("starter")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates/pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates/pages/viaTemplate.html"), []byte(`{{define "content"}}[{{component "widgets/card.html"}}]{{end}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := loadEngine(&sources{chain: chain, dir: dir, comps: x.comps}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// components first, then files: pages/default.html exists in the starter theme and as a component
	if n, ok := e.pageTemplate("custom"); !ok || n != "pages/custom.html" {
		t.Errorf("pageTemplate(custom) = %q %v", n, ok)
	}
	if n, ok := e.pageTemplate("unknown"); !ok || n != "pages/default.html" {
		t.Errorf("fallback = %q", n)
	}
	if !e.has("widgets/card.html") || e.has("widgets/none.html") {
		t.Error("has() ignores components")
	}
	ch := &channel.Channel{Name: "t", Languages: []string{"en"}}
	newPage := func() (*page, *View) {
		v := &View{Locale: "en", Fields: map[string]any{}, Params: map[string]string{}, Channel: ChannelView{Home: "/"}}
		return &page{eng: e, ch: ch, locale: "en", def: "en", view: v, strict: true, links: map[string]string{}}, v
	}
	p, v := newPage()
	out, err := e.render("pages/custom.html", p, v)
	if err != nil || string(out) != "<main>component page Home</main>" || seen != v {
		t.Errorf("component page = %q %v", out, err)
	}
	// a component stands in for the file of the same name
	p, v = newPage()
	if out, err := e.render("pages/default.html", p, v); err != nil || string(out) != "component default" {
		t.Errorf("default = %q %v", out, err)
	}
	// {{component}} inside a template
	p, v = newPage()
	out, err = e.render("pages/viaTemplate.html", p, v)
	if err != nil || !strings.Contains(string(out), "[<aside>card</aside>]") {
		t.Errorf("via template: %v\n%s", err, out)
	}
	// a block component finds its block in View.Block
	p, _ = newPage()
	bs := p.parseBlocks([]any{map[string]any{"id": "b1", "type": "hero"}}, "fields/body", false)
	html, err := p.blocksFn(bs)
	if err != nil || string(html) != "<section hero b1>" {
		t.Errorf("block component = %q %v", html, err)
	}
	// a missing component named from a template is an error in strict mode
	if _, err := p.componentFn("nope"); err == nil {
		t.Error("missing component accepted in strict mode")
	}
}

func TestCustomRoutePatterns(t *testing.T) {
	ok := func(*Request) (*Response, error) { return nil, nil }
	for _, p := range []string{"/search", "GET /search", "POST /a/{id}", "/files/{rest...}", "DELETE /x/{$}", "/"} {
		if r := newCustomRoute(p, ok); r.err != nil {
			t.Errorf("%q: %v", p, r.err)
		}
	}
	for _, p := range []string{"search", "FETCH /x", "GET x", "GET host.com/x", "/a/{", "/a/{x}/{x}", "/a//b{x"} {
		if r := newCustomRoute(p, ok); r.err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	if r := newCustomRoute("/x", nil); r.err == nil {
		t.Error("nil function accepted")
	}
	r := newCustomRoute("GET /tags/{tag}/{rest...}", ok)
	if got := r.samplePath(); got != "/tags/x/x/y" {
		t.Errorf("samplePath = %q", got)
	}
	if got, err := r.fill(map[string]string{"tag": "a b", "rest": "c d/e"}); err != nil || got != "/tags/a%20b/c%20d/e" {
		t.Errorf("fill = %q %v", got, err)
	}
	if _, err := r.fill(map[string]string{"tag": "a"}); err == nil {
		t.Error("fill with a missing param")
	}
	if got, _ := newCustomRoute("/x/{$}", ok).fill(nil); got != "/x/" {
		t.Errorf("fill {$} = %q", got)
	}
	if !newCustomRoute("/x", ok).Export().exportable() || newCustomRoute("/x", ok).exportable() || newCustomRoute("POST /x", ok).Export().exportable() {
		t.Error("exportable")
	}
}

func TestReservedCheck(t *testing.T) {
	for pat, want := range map[string]bool{
		"/search": true, "/search/{x}": false, "/{a}/edit": false, "/account/{a}/edit": true, "/account/{rest...}": true,
		"/{a}/{b}": false, "/accounts/x": false, "/search/{$}": true,
	} {
		if got := reservedBy([]string{"/search", "/account/*"}, pat); got != want {
			t.Errorf("reservedBy(%s) = %v, want %v", pat, got, want)
		}
	}
	ch := &channel.Channel{Name: "en", Options: map[string]any{"render": "go", "reserved": []string{"/search", "/account/*"}}}
	other := &channel.Channel{Name: "de", Options: map[string]any{"reserved": []string{"/suche"}}}
	ok := func(*Request) (*Response, error) { return nil, nil }
	known := func(n string) bool { return n == "en" || n == "de" }
	run := func(chans []*channel.Channel, patterns ...string) error {
		var rs []*CustomRoute
		for _, p := range patterns {
			rs = append(rs, newCustomRoute(p, ok))
		}
		return checkCustomRoutes(rs, chans, known)
	}
	if err := run([]*channel.Channel{ch}, "GET /search", "POST /account/{id}", "/account/{a}/{rest...}"); err != nil {
		t.Errorf("reserved routes rejected: %v", err)
	}
	for _, p := range []string{"/other", "/searching", "/account", "/{x}", "/"} {
		err := run([]*channel.Channel{ch}, p)
		if p == "/account" {
			// /account/* also covers /account itself
			if err != nil {
				t.Errorf("%s: %v", p, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%s: %v", p, err)
		}
	}
	// a route for every channel must be reserved on each; Channels() narrows it
	if err := run([]*channel.Channel{ch, other}, "/search"); err == nil || !strings.Contains(err.Error(), "channel de") {
		t.Errorf("both channels: %v", err)
	}
	r := newCustomRoute("/search", ok).Channels("en")
	if err := checkCustomRoutes([]*CustomRoute{r}, []*channel.Channel{ch, other}, known); err != nil {
		t.Errorf("restricted route: %v", err)
	}
	r = newCustomRoute("/search", ok).Channels("fr")
	if err := checkCustomRoutes([]*CustomRoute{r}, []*channel.Channel{ch}, known); err == nil || !strings.Contains(err.Error(), `"fr"`) {
		t.Errorf("unknown channel: %v", err)
	}
}

func TestCrossSite(t *testing.T) {
	req := func(h map[string]string) *http.Request {
		r, _ := http.NewRequest("POST", "http://site.test/x", nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		return r
	}
	for name, c := range map[string]struct {
		h    map[string]string
		want bool
	}{
		"no headers":      {nil, false},
		"same origin":     {map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		"cross site":      {map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		"same site":       {map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		"origin matches":  {map[string]string{"Origin": "http://site.test"}, false},
		"origin differs":  {map[string]string{"Origin": "http://evil.test"}, true},
		"origin opaque":   {map[string]string{"Origin": "null"}, true},
		"frontend origin": {map[string]string{"Origin": "https://www.example.com"}, false},
	} {
		if got := crossSite(req(c.h), "https://www.example.com"); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestCustomMatch(t *testing.T) {
	ok := func(*Request) (*Response, error) { return nil, nil }
	ch := &channel.Channel{Name: "en"}
	routes := []*CustomRoute{newCustomRoute("GET /s/{id}", ok), newCustomRoute("POST /s/{id}/edit", ok), newCustomRoute("/any", ok)}
	cs, err := buildCustom(routes, []*channel.Channel{ch})
	if err != nil {
		t.Fatal(err)
	}
	c := context.Background()
	if h, _ := cs.match(c, ch, http.MethodGet, "/s/a%20b"); h == nil || h.params["id"] != "a b" {
		t.Errorf("GET /s/a%%20b = %+v", h)
	}
	if h, _ := cs.match(c, ch, http.MethodHead, "/s/1"); h == nil {
		t.Error("HEAD does not match a GET route")
	}
	if h, allow := cs.match(c, ch, http.MethodPost, "/s/1/edit"); h == nil || allow != "" {
		t.Error("POST edit")
	}
	if h, allow := cs.match(c, ch, http.MethodGet, "/s/1/edit"); h != nil || allow != "POST" {
		t.Errorf("GET edit: %v %q", h, allow)
	}
	if h, _ := cs.match(c, ch, http.MethodDelete, "/any"); h == nil {
		t.Error("a pattern without a method takes every method")
	}
	for _, p := range []string{"/nothing", "/s", "/s/1/2", "", "/s/%zz"} {
		if h, allow := cs.match(c, ch, http.MethodGet, p); h != nil || allow != "" {
			t.Errorf("%q matched", p)
		}
	}
	var none *customSet
	if h, _ := none.match(c, ch, http.MethodGet, "/s/1"); h != nil {
		t.Error("nil set matched")
	}
	if _, err := buildCustom([]*CustomRoute{newCustomRoute("/a/{x}", ok), newCustomRoute("/a/{y}", ok)}, []*channel.Channel{ch}); err == nil {
		t.Error("conflicting patterns accepted")
	}
}
