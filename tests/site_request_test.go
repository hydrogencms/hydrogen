package tests

import (
	"html/template"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/modules/site"
)

func requestTheme() site.Theme {
	return site.Theme{Name: "request", Base: "starter", FS: fstest.MapFS{
		"templates/pages/default.html":     {Data: []byte(`{{define "content"}}<p id="home">{{.Title}}|{{articleCount}}</p>{{end}}`)},
		"templates/collections/posts.html": {Data: []byte(`{{define "content"}}<p id="q">cat={{.Query "category"}}|all={{range .QueryValues "tag"}}[{{.}}]{{end}}|here={{here}}</p>{{blocks (list (dict "type" "note" "props" (dict)))}}{{end}}`)},
		"templates/blocks/note.html":       {Data: []byte(`<i id="note">{{.Page.Path}}|{{.Page.Query "category"}}</i>`)},
	}}
}

func requestSetup(r *siteRegistrar) {
	site.UseTheme(r, requestTheme())
	site.ViewFuncs(r, func(v *site.View) template.FuncMap {
		return template.FuncMap{
			"articleCount": func() (int, error) {
				items, err := v.List("article", map[string]any{"limit": 10})
				return len(items), err
			},
			"here": func() string { return v.Path + "~" + v.Query("category") },
		}
	})
}

func TestSiteRequestContext(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnv(t, b.open(t, clk), clk, "request", requestSetup)
		if err != nil {
			t.Fatal(err)
		}
		e.place("home", "Home", nil)
		e.publishItem("article", "One")
		e.publishItem("article", "Two")

		r := e.get("/en/posts?category=news&tag=a&tag=b&utm=1")
		if r.status != 200 {
			t.Fatalf("%d %s", r.status, r.body)
		}
		for _, want := range []string{`cat=news|all=[a][b]|here=/en/posts~news`, `<i id="note">/en/posts|news</i>`} {
			if !strings.Contains(r.body, want) {
				t.Errorf("missing %q\n%s", want, r.body)
			}
		}
		if r.header.Get("X-Hydrogen-Cache") != "miss" {
			t.Errorf("first render: %v", r.header)
		}
		// values are escaped like any template output
		x := e.get("/en/posts?category=" + url.QueryEscape(`<script>"`))
		if strings.Contains(x.body, "<script>") || !strings.Contains(x.body, "cat=&lt;script&gt;&#34;") {
			t.Errorf("query value not escaped\n%s", x.body)
		}

		// same parameters read: a hit, whatever else the URL carries
		if r := e.get("/en/posts?category=news&tag=a&tag=b&utm=2"); r.header.Get("X-Hydrogen-Cache") != "hit" {
			t.Errorf("other parameters split the cache: %v", r.header)
		}
		// another value: its own variant
		o := e.get("/en/posts?category=sport&tag=a&tag=b")
		if o.header.Get("X-Hydrogen-Cache") != "miss" || !strings.Contains(o.body, "cat=sport|") {
			t.Errorf("other value: %v\n%s", o.header, o.body)
		}
		if r := e.get("/en/posts?category=sport&tag=a&tag=b"); r.header.Get("X-Hydrogen-Cache") != "hit" || !strings.Contains(r.body, "cat=sport|") {
			t.Errorf("variant not cached: %v", r.header)
		}
		if r := e.get("/en/posts?category=news&tag=a&tag=b"); r.header.Get("X-Hydrogen-Cache") != "hit" || !strings.Contains(r.body, "cat=news|") {
			t.Errorf("first variant lost: %v", r.header)
		}
		// a long value is rendered and never stored
		long := strings.Repeat("x", 300)
		for range 2 {
			if r := e.get("/en/posts?category=" + long); r.header.Get("X-Hydrogen-Cache") == "hit" || !strings.Contains(r.body, "cat="+long) {
				t.Errorf("long value: %v", r.header)
			}
		}

		// a page that never reads the query has one entry
		h := e.get("/en?category=a")
		if h.status != 200 || !strings.Contains(h.body, `<p id="home">Home|2</p>`) || h.header.Get("X-Hydrogen-Cache") != "miss" {
			t.Fatalf("home: %d %v\n%s", h.status, h.header, h.body)
		}
		if r := e.get("/en?category=b&utm=2"); r.header.Get("X-Hydrogen-Cache") != "hit" {
			t.Errorf("a page that ignores the query was split: %v", r.header)
		}

		// publishing purges the pages and every variant
		e.publishItem("article", "Three")
		e.ex.Purge("c:article")
		if r := e.get("/en?category=a"); r.header.Get("X-Hydrogen-Cache") == "hit" || !strings.Contains(r.body, "Home|3") {
			t.Errorf("home after publish: %v", r.header)
		}
		for _, c := range []string{"news", "sport"} {
			if r := e.get("/en/posts?category=" + c + "&tag=a&tag=b"); r.header.Get("X-Hydrogen-Cache") == "hit" {
				t.Errorf("variant %s survived the purge", c)
			}
		}

		// an export has no query unless the target brings one
		out, err := e.ex.Render(ctx, "en", "/en/posts")
		if err != nil || !strings.Contains(string(out.Body), "cat=|all=|") {
			t.Errorf("export: %v\n%s", err, out.Body)
		}
	})
}
