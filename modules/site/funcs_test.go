package site

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
)

func run(t *testing.T, tpl string, data any) (string, error) {
	t.Helper()
	tt, err := template.New("t").Funcs(builtinFuncs()).Parse(tpl)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	err = tt.Execute(&sb, data)
	return sb.String(), err
}

func TestBuiltinFuncs(t *testing.T) {
	data := map[string]any{"n": 7.0, "f": 2.5, "s": "Héllo wörld", "l": []any{"a", 2.0, "c"}, "m": map[string]any{"k": "v"}, "e": ""}
	for tpl, want := range map[string]string{
		`{{add 1 2}} {{add .n 1}} {{add .f 1}} {{sub 5 .n}} {{mul 3 4.0}} {{mul .f 2}}`:                                   "3 8 3.5 -2 12 5",
		`{{div 6 3}} {{div 7 2}} {{div .n 2}} {{mod 7 3}} {{mod .n 4.0}}`:                                                 "2 3.5 3.5 1 3",
		`{{range seq 3}}{{.}}{{end}}|{{range seq 2 4}}{{.}}{{end}}|{{range seq 3 1}}{{.}}{{end}}|{{range seq 0}}x{{end}}`: "012|234|321|",
		`{{default "d" .e}} {{default "d" .s}} {{default 5 0}} {{default "d" .missing}} {{.e | default "p"}}`:             "d Héllo wörld 5 d p",
		`{{lower "ABC"}}{{upper "abc"}} {{title "hello big-world"}} [{{trim "  x "}}]`:                                    "abcABC Hello Big-World [x]",
		`{{contains "ll" .s}} {{contains "z" .s}} {{contains "a" .l}} {{contains 2 .l}} {{contains "z" .l}}`:              "true false true true false",
		`{{hasPrefix "Hé" .s}} {{hasSuffix "ld" .s}} {{split "," "a,b" | join "+"}} {{join ", " .l}}`:                     "true true a&#43;b a, 2, c",
		`{{replace "l" "L" .s}} {{truncate 5 .s}} {{truncate 6 "…" .s}} {{truncate 50 "…" .s}}`:                           "HéLLo wörLd Héllo Héllo… Héllo wörld",
		`{{first .l}} {{last .l}} [{{first .e}}] {{len (dict "a" 1 "b" 2)}} {{len (list 1 2 3)}}`:                         "a c [] 2 3",
		`{{json .m}} {{json .l}}`: `{&#34;k&#34;:&#34;v&#34;} [&#34;a&#34;,2,&#34;c&#34;]`,
		`{{date "02.01.2006" "2026-10-14"}} {{date "2006" "2026-10-14T10:00:00+02:00"}} [{{date "2006" "nope"}}] [{{date "2006" 5}}]`: "14.10.2026 2026 [] []",
	} {
		got, err := run(t, tpl, data)
		if err != nil || got != want {
			t.Errorf("%s\n got %q (%v)\nwant %q", tpl, got, err, want)
		}
	}
	if got := dateFn("Jan 2", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)); got != "Oct 14" {
		t.Errorf("date(time) = %q", got)
	}
	if v, _ := arith("add", 1, 2); reflect.TypeOf(v).Kind() != reflect.Int {
		t.Errorf("add of integers is %T", v)
	}
	if v, _ := arith("add", 1.0, 2); reflect.TypeOf(v).Kind() != reflect.Int {
		t.Errorf("add of integral float is %T", v)
	}
	if v, _ := arith("div", 7, 2); v != 3.5 {
		t.Errorf("7/2 = %v", v)
	}
	for _, tpl := range []string{`{{div 1 0}}`, `{{mod 1 0}}`, `{{mod 1.5 2}}`, `{{add "a" 1}}`, `{{dict "a"}}`, `{{dict 1 2}}`, `{{seq 1.5}}`, `{{seq 1 100000}}`, `{{seq}}`, `{{truncate 1}}`} {
		if got, err := run(t, tpl, nil); err == nil {
			t.Errorf("%s = %q, want an error", tpl, got)
		}
	}
	// the string is escaped by the context it lands in
	got, err := run(t, `<i data-x="{{json .m}}">{{json "<b>"}}`, data)
	if err != nil || got != `<i data-x="{&#34;k&#34;:&#34;v&#34;}">&#34;\u003cb\u003e&#34;` {
		t.Errorf("json contexts: %q %v", got, err)
	}
}

func TestProjectFuncs(t *testing.T) {
	x := newExtensions()
	ok := template.FuncMap{"shout": func(s string) string { return strings.ToUpper(s) + "!" }, "risky": func() (int, error) { return 1, nil }}
	if err := x.AddFuncs(ok); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]template.FuncMap{
		"shout":      {"shout": func() string { return "" }},
		"blocks":     {"blocks": func() string { return "" }},
		"partial":    {"partial": func() string { return "" }},
		"dict":       {"dict": func() string { return "" }},
		"len":        {"len": func() string { return "" }},
		"valid name": {"a-b": func() string { return "" }},
		"not a func": {"x": 5},
		"nil":        {"x": nil},
		"three":      {"x": func() (int, int, error) { return 0, 0, nil }},
		"bad second": {"x": func() (int, int) { return 0, 0 }},
	} {
		if err := x.AddFuncs(m); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if x.funcs["a-b"] != nil || len(x.funcs) != 2 {
		t.Errorf("a refused registration leaked: %v", x.funcs)
	}
	chain, _ := x.chain("starter")
	dir := t.TempDir()
	writeFile(t, dir, "templates/partials/p.html", `{{shout "hi"}} {{add 1 1}}`)
	e, err := loadEngine(&sources{chain: chain, dir: dir, funcs: x.funcs}, true, nil)
	if err != nil {
		t.Fatalf("templates using project and built-in functions do not parse: %v", err)
	}
	var sb strings.Builder
	if err := e.base.ExecuteTemplate(&sb, "partials/p.html", nil); err != nil || sb.String() != "HI! 2" {
		t.Errorf("render = %q %v", sb.String(), err)
	}
	if _, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil); err == nil {
		t.Error("a template using an unregistered function parsed")
	}
}

func TestPartial(t *testing.T) {
	x := newExtensions()
	chain, _ := x.chain("starter")
	dir := t.TempDir()
	writeFile(t, dir, "templates/blocks/box/a.html", `[A {{.Name}}]`)
	writeFile(t, dir, "templates/blocks/box.html", `{{partial (printf "blocks/box/%s.html" .Props.view) .Props}}|{{partial "blocks/box/none.html"}}`)
	writeFile(t, dir, "templates/partials/loop.html", `{{partial "partials/loop.html"}}`)
	e, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := &channel.Channel{Name: "t", Languages: []string{"en"}}
	newPage := func(strict bool) *page {
		v := &View{Locale: "en", Fields: map[string]any{}, Params: map[string]string{}, Channel: ChannelView{Home: "/"}}
		return &page{eng: e, ch: ch, locale: "en", def: "en", view: v, strict: strict, links: map[string]string{}}
	}
	p := newPage(false)
	b := &BlockView{Type: "box", Props: map[string]any{"view": "a", "Name": "x"}}
	b.Props["Name"] = "x"
	out, err := p.blocksFn(b)
	if err != nil || string(out) != "[A x]|" {
		t.Errorf("lenient = %q %v", out, err)
	}
	if _, err := newPage(true).blocksFn(b); err == nil {
		t.Error("a missing partial passed in strict mode")
	}
	// started from a component: no template set yet
	if out, err := newPage(true).partialFn("partials/loop.html"); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Errorf("recursion = %q %v", out, err)
	}
	if ok, err := newPage(true).hasTemplateFn("blocks/box/a.html"); !ok || err != nil {
		t.Errorf("hasTemplate a = %v %v", ok, err)
	}
	if ok, _ := newPage(true).hasTemplateFn("blocks/box/none.html"); ok {
		t.Error("hasTemplate found a missing template")
	}
	// a blocks value read from another item renders read only
	raw := []any{map[string]any{"type": "box", "props": map[string]any{"view": "a", "Name": "y"}}}
	pp := newPage(false)
	pp.preview = true
	if out, err := pp.blocksFn(raw); err != nil || string(out) != "[A y]|" {
		t.Errorf("raw blocks = %q %v", out, err)
	}
}

func TestReadHelpers(t *testing.T) {
	var calls atomic.Int32
	var last url.Values
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		last = r.URL.Query()
		switch r.URL.Path {
		case "/collections/post/items":
			w.Header().Set("Surrogate-Key", "c:post i:1")
			_, _ = w.Write([]byte(`{"items":[{"id":"1","type":"post","name":"One","url":"/en/one","fields":{"title":"One"}}],"hasMore":false}`))
		case "/items/1":
			w.Header().Set("Surrogate-Key", "i:1")
			_, _ = w.Write([]byte(`{"item":{"id":"1","type":"post","name":"One","fields":{}}}`))
		case "/tree":
			w.Header().Set("Surrogate-Key", "r:main")
			_, _ = w.Write([]byte(`{"nodes":[{"name":"A"}]}`))
		case "/boom":
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	})
	f := &fetcher{h: h, hdr: http.Header{}, channel: "web", tags: map[string]bool{}}
	p := &page{f: f, locale: "en", itemPath: func(it *Item) string { return "/p/" + it.ID }}

	for i := 0; i < 3; i++ {
		items, err := p.itemsFn("post", map[string]any{"limit": 4, "sort": "-publishedAt", "where[category][eq]": "x", "tag": []any{"a", "b"}, "n": 1.5, "off": nil})
		if err != nil || len(items) != 1 || items[0].Path != "/p/1" {
			t.Fatalf("items = %v %v", items, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("identical queries hit delivery %d times", calls.Load())
	}
	if last.Get("limit") != "4" || last.Get("sort") != "-publishedAt" || last.Get("where[category][eq]") != "x" ||
		!reflect.DeepEqual(last["tag"], []string{"a", "b"}) || last.Get("n") != "1.5" || last.Get("channel") != "web" || last.Get("locale") != "en" || last.Has("off") {
		t.Errorf("query = %v", last)
	}
	if it, err := p.itemFn("1"); err != nil || it == nil || it.Path != "/p/1" {
		t.Errorf("item = %v %v", it, err)
	}
	if it, err := p.itemFn("2"); err != nil || it != nil {
		t.Errorf("missing item = %v %v", it, err)
	}
	if m, err := p.queryFn("/tree", map[string]any{"depth": 3}); err != nil || m["nodes"] == nil || last.Get("depth") != "3" {
		t.Errorf("query = %v %v", m, err)
	}
	if m, err := p.queryFn("/nothing"); err != nil || m != nil {
		t.Errorf("404 query = %v %v", m, err)
	}
	if _, err := p.queryFn("/boom"); err == nil {
		t.Error("a 5xx answer was not an error")
	}
	if _, err := p.queryFn("tree"); err == nil {
		t.Error("a relative path was accepted")
	}
	if _, err := p.itemsFn("post", map[string]any{"x": map[string]any{}}); err == nil {
		t.Error("a map parameter was accepted")
	}
	if got := strings.Join(f.tagList(), " "); got != "c:post i:1 r:main" {
		t.Errorf("tags = %q", got)
	}
	// site:check pages have no fetcher: empty answers
	q := &page{}
	if items, err := q.itemsFn("post"); items != nil || err != nil {
		t.Error("items without a fetcher")
	}
	if it, err := q.itemFn("1"); it != nil || err != nil {
		t.Error("item without a fetcher")
	}
	if m, err := q.queryFn("/tree"); m != nil || err != nil {
		t.Error("query without a fetcher")
	}
}

func TestReadHelpersFields(t *testing.T) {
	var last url.Values
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r.URL.Query()
		switch r.URL.Path {
		case "/collections/post/items":
			_, _ = w.Write([]byte(`{"items":[{"id":"1","type":"post","name":"One","fields":{"title":"One","slug":"one"}}],"hasMore":false}`))
		case "/items/1":
			_, _ = w.Write([]byte(`{"item":{"id":"1","type":"post","name":"One","fields":{"title":"One","slug":"one"}}}`))
		case "/tree":
			_, _ = w.Write([]byte(`{"nodes":[{"name":"A","fields":{"title":"A"}}]}`))
		default:
			w.WriteHeader(404)
		}
	})
	rt, err := parseRoute(Route{Path: "/blog/{slug}", Type: "post"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &server{routes: []*route{rt}}
	f := &fetcher{h: h, hdr: http.Header{}, channel: "web", tags: map[string]bool{}}
	web := &channel.Channel{Name: "web", Languages: []string{"en"}}
	p := &page{f: f, locale: "en", linkFields: func(typ string) []string { return srv.linkFields(web, typ) },
		itemPath: func(it *Item) string { p, _ := rt.build(it.ID, it.Fields); return p }}

	items, err := p.itemsFn("post", map[string]any{"fields": "title,seo.title"})
	if err != nil || len(items) != 1 || items[0].Path != "/blog/one" || last.Get("fields") != "title,seo.title,slug" {
		t.Fatalf("items = %v %v fields=%q", items, err, last.Get("fields"))
	}
	if _, err := p.itemsFn("post", map[string]any{"fields": "slug,title"}); err != nil || last.Get("fields") != "slug,title" {
		t.Errorf("a field already selected is not repeated: %q %v", last.Get("fields"), err)
	}
	if _, err := p.itemsFn("post"); err != nil || last.Has("fields") {
		t.Errorf("no projection without fields: %v", last)
	}
	it, err := p.itemFn("1", map[string]any{"fields": "title"})
	if err != nil || it == nil || it.Path != "/blog/one" || last.Get("fields") != "title,slug" || last.Get("locale") != "en" {
		t.Fatalf("item = %v %v %v", it, err, last)
	}
	if _, err := p.itemFn("1"); err != nil || last.Has("fields") {
		t.Errorf("item without a projection: %v %v", last, err)
	}
	if _, err := p.itemFn("1", map[string]any{"limit": 1}); err == nil {
		t.Error("an unsupported item parameter was accepted")
	}
	if m, err := p.queryFn("/tree", map[string]any{"fields": "title"}); err != nil || last.Get("fields") != "title" ||
		m["nodes"].([]any)[0].(map[string]any)["fields"] == nil {
		t.Errorf("tree = %v %v", m, err)
	}
}

func writeFile(t testing.TB, dir, name, data string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
