package tests

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/site"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const siteSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  section:
    page: true
    children: [section]
    fields:
      title: { type: string, translatable: true, required: true }
  article:
    name: "{title}"
    fields:
      title: { type: string, translatable: true, required: true }
      slug:  { type: slug, from: title, translatable: true, index: true }
`

const siteChannels = `
languages:
  en: {}
groups:
  web:
    site: main
    render: go
    reserved: [/raw/*, /search, /account/*, /tags/*, /comp, /contact, /small, /open]
channels:
  en: { group: web, languages: [en], match: { prefix: /en } }
`

// siteRegistrar collects what the site helpers register, like hydrogen.Builder.Use.
type siteRegistrar struct{ mods []kernel.Module }

func (r *siteRegistrar) Use(m ...kernel.Module) { r.mods = append(r.mods, m...) }

type siteEnv struct {
	t     testing.TB
	app   *kernel.App
	c     *content.Service
	pages *pages.Service
	h     http.Handler
	ex    *site.Exporter
	clk   *clock.Manual
}

// fakeComponent is a Renderer that is not templ.
type fakeComponent struct{ v *site.View }

func (f fakeComponent) Render(ctx context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "<!doctype html><title>comp</title><p>"+f.v.T("home")+"|"+f.v.Path+"|"+fmt.Sprint(f.v.Data)+"</p>")
	return err
}

// vpComponent renders a template through View.Partial.
type vpComponent struct{ v *site.View }

func (c vpComponent) Render(_ context.Context, w io.Writer) error {
	out, err := c.v.Partial("partials/bit.html")
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "<p>vp:"+string(out)+"</p>")
	return err
}

// routesTheme adds the template the custom routes render, on top of the starter theme.
func routesTheme() site.Theme {
	return site.Theme{Name: "routes", Base: "starter", FS: fstest.MapFS{
		"templates/routes/info.html":  {Data: []byte(`{{define "content"}}<div class="info" data-status="{{.Status}}">{{.Title}}|{{.Data}}|{{.Params.id}}</div>{{end}}`)},
		"templates/routes/frag.html":  {Data: []byte(`<li class="frag">{{t "home"}}|{{.Data}}|{{.Params.id}}|{{partial "partials/bit.html"}}</li>`)},
		"templates/routes/feed.html":  {Data: []byte(`<rss><title>{{.Data}}</title></rss>`)},
		"templates/partials/bit.html": {Data: []byte(`<b>bit</b>`)},
		"translations/en.yaml":        {Data: []byte("home: Start\n")},
	}}
}

func newSiteEnv(t testing.TB, db store.DB, clk *clock.Manual, themeName string, setup func(r *siteRegistrar)) (*siteEnv, error) {
	t.Helper()
	return newSiteEnvWith(t, db, clk, themeName, siteSchema, siteChannels, setup)
}

func newSiteEnvWith(t testing.TB, db store.DB, clk *clock.Manual, themeName, schemaYAML, channelsYAML string, setup func(r *siteRegistrar)) (*siteEnv, error) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", schemaYAML)
	write("config/channels.yaml", channelsYAML)
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(dir, "schema")+" }\nmodules:\n  pages: { sites: [main] }\n  site: { theme: "+themeName+", dir: "+filepath.Join(dir, "nosite")+", routes: [{ path: /posts, type: article, template: posts, limit: 2 }] }\n")
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	reg := &siteRegistrar{}
	setup(reg)
	mods := append([]kernel.Module{sqlite.Module(), postgres.Module(), pages.Module(), site.Module()}, reg.mods...)
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		return nil, err
	}
	if err := app.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := app.Services()
	if _, err := s.Content.EnsureSchemaRev(context.Background()); err != nil {
		t.Fatal(err)
	}
	ps, _ := kernel.LookupService[*pages.Service](s)
	ex, ok := kernel.LookupService[*site.Exporter](s)
	if !ok {
		t.Fatal("the site module provides no Exporter")
	}
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &siteEnv{t: t, app: app, c: s.Content, pages: ps, h: h, ex: ex, clk: clk}, nil
}

func (e *siteEnv) publishItem(typ, title string) ids.UUID {
	e.t.Helper()
	e.clk.Advance(1e9)
	v, err := e.c.Create(ctx, content.CreateInput{Type: typ, Locale: "en", Fields: doc(e.t, `{"title":"`+title+`"}`)})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.Publish(ctx, v.ID, content.PublishOptions{Locales: []string{"en"}}); err != nil {
		e.t.Fatal(err)
	}
	return v.ID
}

func (e *siteEnv) place(typ, title string, parent *ids.UUID) (ids.UUID, ids.UUID) {
	e.t.Helper()
	item := e.publishItem(typ, title)
	n, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: parent})
	if err != nil {
		e.t.Fatal(err)
	}
	return item, n.ID
}

type siteReply struct {
	status int
	header http.Header
	body   string
}

func (e *siteEnv) do(method, path string, body io.Reader) siteReply {
	e.t.Helper()
	req := httptest.NewRequest(method, "http://site.test"+path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return siteReply{rec.Code, rec.Header(), rec.Body.String()}
}

func (e *siteEnv) get(path string) siteReply { return e.do("GET", path, nil) }

func siteRoutes(r *siteRegistrar) {
	site.UseTheme(r, routesTheme())
	site.Handle(r, "GET /search", func(q *site.Request) (*site.Response, error) {
		items, _, err := q.Items("article", url.Values{"limit": {"5"}})
		if err != nil {
			return nil, err
		}
		return &site.Response{Template: "routes/info.html", Title: "Search", Data: fmt.Sprintf("q=%s n=%d", q.Query.Get("q"), len(items)),
			Headers: http.Header{"X-Custom": {"yes"}}, Tags: []string{"extra"}}, nil
	}).Sample(func() *site.Response { return &site.Response{Template: "routes/info.html", Data: "sample"} })
	site.Handle(r, "POST /account/login", func(q *site.Request) (*site.Response, error) {
		if err := q.R.ParseForm(); err != nil {
			return nil, err
		}
		return &site.Response{Redirect: q.URL("/account/home?user=" + url.QueryEscape(q.R.PostForm.Get("user"))),
			Headers: http.Header{"Set-Cookie": {"sid=1; Path=/; HttpOnly"}}}, nil
	})
	site.Handle(r, "POST /contact", func(q *site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/info.html", Data: "sent"}, nil
	})
	site.Handle(r, "POST /small", func(q *site.Request) (*site.Response, error) {
		if err := q.R.ParseForm(); err != nil {
			return nil, err
		}
		return &site.Response{Template: "routes/info.html", Data: "got"}, nil
	}).MaxBody(10)
	site.Handle(r, "POST /open", func(q *site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/info.html", Data: "open"}, nil
	}).AllowCrossSite()
	site.Handle(r, "GET /account/{id}", func(q *site.Request) (*site.Response, error) {
		switch q.Params["id"] {
		case "missing":
			return nil, site.ErrNotFound
		case "teapot":
			return &site.Response{Template: "routes/info.html", Status: http.StatusTeapot, Title: "Teapot"}, nil
		case "boom":
			return nil, errors.New("boom")
		case "badtemplate":
			return &site.Response{Template: "routes/nope.html"}, nil
		case "go":
			return &site.Response{Redirect: "/elsewhere", Status: http.StatusMovedPermanently}, nil
		}
		return &site.Response{Template: "routes/info.html", Title: "Account", Data: "ok"}, nil
	}).Sample(func() *site.Response { return &site.Response{Template: "routes/info.html"} })
	site.Handle(r, "GET /tags/{id}", func(q *site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/info.html", Title: "Tag", Data: "tag"}, nil
	}).Static(func(ctx context.Context) ([]map[string]string, error) {
		if site.ClientFrom(ctx) == nil {
			return nil, errors.New("no client in the static context")
		}
		return []map[string]string{{"id": "a"}, {"id": "b c"}}, nil
	})
	site.Handle(r, "GET /comp", func(q *site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/comp.html", Data: "payload"}, nil
	})
	site.Handle(r, "GET /raw/{kind}", func(q *site.Request) (*site.Response, error) {
		items, _, err := q.Items("article", url.Values{"limit": {"5"}})
		if err != nil {
			return nil, err
		}
		cache := q.Query.Get("cache") != ""
		resp := &site.Response{Cacheable: cache, Tags: []string{"extra"}}
		if q.Query.Get("age") != "" {
			resp.MaxAge = 5 * time.Minute
		}
		switch q.Params["kind"] {
		case "json":
			resp.JSON = map[string]any{"n": len(items), "html": "<b>&</b>"}
		case "xml":
			resp.XML = struct {
				XMLName xml.Name `xml:"urlset"`
				N       int      `xml:"n"`
			}{N: len(items)}
		case "body":
			resp.Body, resp.ContentType = []byte("User-agent: *\n"), "text/plain; charset=utf-8"
		case "nobody":
			resp.Body = []byte("x")
		case "frag":
			resp.Partial, resp.Data = "routes/frag.html", "d"
		case "feed":
			resp.Partial, resp.Data, resp.ContentType = "routes/feed.html", "T", "application/rss+xml; charset=utf-8"
		case "plain":
			resp.Partial = "partials/bit.html"
		case "vp":
			resp.Partial = "routes/vp.html"
		case "both":
			resp.JSON, resp.Template = 1, "routes/info.html"
		case "twoempty":
			resp.Redirect, resp.Body = "/x", []byte("x")
		case "none":
			return &site.Response{}, nil
		}
		return resp, nil
	}).Sample(func() *site.Response { return &site.Response{Partial: "routes/frag.html"} })
	site.Component(r, "routes/vp.html", func(v *site.View) site.Renderer { return vpComponent{v} })
	site.Component(r, "routes/comp.html", func(v *site.View) site.Renderer { return fakeComponent{v} })
}

func runSite(t *testing.T, theme string, fn func(t *testing.T, e *siteEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnv(t, b.open(t, clk), clk, theme, siteRoutes)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, e)
	})
}

func TestSiteCustomRoutes(t *testing.T) {
	runSite(t, "routes", func(t *testing.T, e *siteEnv) {
		home, _ := e.place("home", "Home", nil)
		_ = home
		e.publishItem("article", "Hello")
		e.publishItem("article", "World")

		r := e.get("/en/search?q=go")
		if r.status != 200 {
			t.Fatalf("search: %d %s", r.status, r.body)
		}
		for _, want := range []string{`<html lang="en">`, `<div class="info" data-status="200">Search|q=go n=2|</div>`, `<link rel="canonical" href="/en/search">`, `class="site-header"`} {
			if !strings.Contains(r.body, want) {
				t.Errorf("search lacks %q\n%s", want, r.body)
			}
		}
		if r.header.Get("X-Custom") != "yes" || r.header.Get("Cache-Control") != "no-store" || r.header.Get("Etag") != "" {
			t.Errorf("headers = %v", r.header)
		}
		tags := strings.Fields(r.header.Get("Surrogate-Key"))
		if !slices.Contains(tags, "c:article") || !slices.Contains(tags, "extra") {
			t.Errorf("tags = %v", tags)
		}
		// the theme's translation layers over the starter's
		if !strings.Contains(r.body, ">Start</a>") && !strings.Contains(r.body, "Start") {
			t.Errorf("theme translation missing")
		}

		// redirect after POST: 303, Location, headers
		form := strings.NewReader("user=ann+lee")
		p := e.do("POST", "/en/account/login", form)
		if p.status != 303 || p.header.Get("Location") != "/en/account/home?user=ann+lee" || !strings.Contains(p.header.Get("Set-Cookie"), "sid=1") {
			t.Errorf("login: %d %v", p.status, p.header)
		}
		// the same path with another method
		if g := e.get("/en/contact"); g.status != 405 {
			t.Errorf("GET contact: %d", g.status)
		} else if !strings.Contains(g.header.Get("Allow"), "POST") {
			t.Errorf("Allow = %q", g.header.Get("Allow"))
		}
		if d := e.do("DELETE", "/en/search", nil); d.status != 405 {
			t.Errorf("DELETE search: %d", d.status)
		}
		if g := e.do("POST", "/en/contact", strings.NewReader("a=1")); g.status != 200 || !strings.Contains(g.body, "|sent|") || g.header.Get("Cache-Control") != "no-store" {
			t.Errorf("POST contact: %d %s", g.status, g.body)
		}
		post := func(path, body string, hdr map[string]string) siteReply {
			req := httptest.NewRequest("POST", "http://site.test"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			return siteReply{rec.Code, rec.Header(), rec.Body.String()}
		}
		if g := post("/en/contact", "a=1", map[string]string{"Sec-Fetch-Site": "cross-site"}); g.status != 403 {
			t.Errorf("cross-site POST: %d", g.status)
		}
		if g := post("/en/contact", "a=1", map[string]string{"Origin": "http://evil.test"}); g.status != 403 {
			t.Errorf("foreign Origin POST: %d", g.status)
		}
		if g := post("/en/contact", "a=1", map[string]string{"Origin": "http://site.test", "Sec-Fetch-Site": "same-origin"}); g.status != 200 {
			t.Errorf("same-origin POST: %d", g.status)
		}
		if g := post("/en/open", "a=1", map[string]string{"Origin": "http://evil.test"}); g.status != 200 {
			t.Errorf("AllowCrossSite POST: %d", g.status)
		}
		if g := post("/en/small", "a=1", nil); g.status != 200 {
			t.Errorf("small body: %d", g.status)
		}
		if g := post("/en/small", "a=12345678901234567890", nil); g.status != 413 {
			t.Errorf("large body: %d", g.status)
		}
		// statuses
		if g := e.get("/en/account/missing"); g.status != 404 || !strings.Contains(g.body, "Page not found") {
			t.Errorf("missing: %d", g.status)
		}
		if g := e.get("/en/account/teapot"); g.status != 418 || !strings.Contains(g.body, `data-status="418"`) || strings.Contains(g.header.Get("Cache-Control"), "s-maxage") {
			t.Errorf("teapot: %d %v", g.status, g.header)
		}
		if g := e.get("/en/account/boom"); g.status != 500 || strings.Contains(g.body, "boom") {
			t.Errorf("boom: %d %q", g.status, g.body)
		}
		if g := e.get("/en/account/badtemplate"); g.status != 500 {
			t.Errorf("badtemplate: %d", g.status)
		}
		if g := e.get("/en/account/go"); g.status != 301 || g.header.Get("Location") != "/elsewhere" {
			t.Errorf("go: %d %v", g.status, g.header)
		}
		if g := e.get("/en/account/7"); g.status != 200 || !strings.Contains(g.body, "Account|ok|7") {
			t.Errorf("params: %d %s", g.status, g.body)
		}
		// a path no route claims is still the page tree's: reserved, so a plain 404
		if g := e.get("/en/account"); g.status != 404 {
			t.Errorf("/en/account: %d", g.status)
		}

		// a component answers a template name before files
		c := e.get("/en/comp")
		if c.status != 200 || !strings.Contains(c.body, "<p>Start|/en/comp|payload</p>") {
			t.Errorf("component: %d %s", c.status, c.body)
		}
	})
}

func TestSiteRawResponses(t *testing.T) {
	runSite(t, "routes", func(t *testing.T, e *siteEnv) {
		e.place("home", "Home", nil)
		e.publishItem("article", "Hello")
		e.publishItem("article", "World")
		cached := "public, max-age=0, s-maxage=60, stale-while-revalidate=60"
		for _, tc := range []struct {
			name, path string
			status     int
			ct, body   string
			noLayout   bool
		}{
			{"json", "/en/raw/json", 200, "application/json", `{"html":"<b>&</b>","n":2}` + "\n", true},
			{"xml", "/en/raw/xml", 200, "application/xml; charset=utf-8", `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset><n>2</n></urlset>`, true},
			{"body", "/en/raw/body", 200, "text/plain; charset=utf-8", "User-agent: *\n", true},
			{"body without type", "/en/raw/nobody", 500, "text/html; charset=utf-8", "", false},
			{"partial", "/en/raw/frag", 200, "text/html; charset=utf-8", `<li class="frag">Start|d||<b>bit</b></li>`, true},
			{"partial xml", "/en/raw/feed", 200, "application/rss+xml; charset=utf-8", `<rss><title>T</title></rss>`, true},
			{"plain partial", "/en/raw/plain", 200, "text/html; charset=utf-8", `<b>bit</b>`, true},
			{"view partial", "/en/raw/vp", 200, "text/html; charset=utf-8", `<p>vp:<b>bit</b></p>`, true},
			{"two answers", "/en/raw/both", 500, "text/html; charset=utf-8", "", false},
			{"redirect and body", "/en/raw/twoempty", 500, "text/html; charset=utf-8", "", false},
			{"nothing", "/en/raw/none", 500, "text/html; charset=utf-8", "", false},
		} {
			r := e.get(tc.path)
			if r.status != tc.status || r.header.Get("Content-Type") != tc.ct || (tc.body != "" && r.body != tc.body) || r.header.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s: %d %q %q", tc.name, r.status, r.header.Get("Content-Type"), r.body)
			}
			if tc.noLayout && strings.Contains(r.body, "<html") {
				t.Errorf("%s: layout in the answer: %s", tc.name, r.body)
			}
			if tc.status == 200 {
				if tags := strings.Fields(r.header.Get("Surrogate-Key")); !slices.Contains(tags, "c:article") || !slices.Contains(tags, "extra") {
					t.Errorf("%s: tags = %v", tc.name, tags)
				}
				if r.header.Get("Cache-Control") != "no-store" || r.header.Get("Etag") != "" {
					t.Errorf("%s: uncached answer has %v", tc.name, r.header)
				}
			}
		}
		if r := e.get("/en/raw/both"); strings.Contains(r.body, "JSON") {
			t.Errorf("the 500 leaks the cause: %s", r.body)
		}

		// cacheable: shared caches, ETag and 304; MaxAge overrides the channel's lifetime
		r := e.get("/en/raw/json?cache=1")
		etag := r.header.Get("Etag")
		if r.header.Get("Cache-Control") != cached || etag == "" || !strings.Contains(r.header.Get("Cache-Tag"), "extra") {
			t.Errorf("cacheable: %v", r.header)
		}
		req := httptest.NewRequest("GET", "http://site.test/en/raw/json?cache=1", nil)
		req.Header.Set("If-None-Match", etag)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != 304 || rec.Body.Len() != 0 {
			t.Errorf("If-None-Match: %d %q", rec.Code, rec.Body.String())
		}
		if r := e.get("/en/raw/json?cache=1&age=1"); r.header.Get("Cache-Control") != "public, max-age=0, s-maxage=300, stale-while-revalidate=300" {
			t.Errorf("MaxAge: %v", r.header)
		}
		if r := e.get("/en/raw/json?age=1"); r.header.Get("Cache-Control") != "no-store" {
			t.Errorf("MaxAge without Cacheable: %v", r.header)
		}
		if r := e.do("HEAD", "/en/raw/json?cache=1", nil); r.status != 200 || r.header.Get("Etag") != etag || r.body != "" {
			t.Errorf("HEAD: %d %v", r.status, r.header)
		}

		// export: a non-HTML answer is a file at its path
		out, err := e.ex.Render(ctx, "en", "/en/raw/xml")
		if err != nil || !strings.HasPrefix(out.Header.Get("Content-Type"), "application/xml") || !strings.Contains(string(out.Body), "<urlset>") {
			t.Errorf("export render: %v %+v", err, out)
		}
	})
}

func TestSiteListingPages(t *testing.T) {
	runSite(t, "starter", func(t *testing.T, e *siteEnv) {
		for i := range 5 {
			e.publishItem("article", fmt.Sprintf("Post %d", i))
		}
		first := e.get("/en/posts")
		if first.status != 200 || !strings.Contains(first.body, `<a rel="next" href="/en/posts/page/2">`) || strings.Contains(first.body, `rel="prev"`) {
			t.Errorf("page 1: %d\n%s", first.status, first.body)
		}
		mid := e.get("/en/posts/page/2")
		if mid.status != 200 || !strings.Contains(mid.body, `<a rel="prev" href="/en/posts">`) || !strings.Contains(mid.body, `<a rel="next" href="/en/posts/page/3">`) ||
			!strings.Contains(mid.body, `<link rel="canonical" href="/en/posts/page/2">`) {
			t.Errorf("page 2: %d", mid.status)
		}
		last := e.get("/en/posts/page/3")
		if last.status != 200 || !strings.Contains(last.body, `<a rel="prev" href="/en/posts/page/2">`) || strings.Contains(last.body, `rel="next"`) {
			t.Errorf("page 3: %d", last.status)
		}
		for _, p := range []string{"/en/posts/page/4", "/en/posts/page/1", "/en/posts/page/0", "/en/posts/page/x", "/en/posts/page/02"} {
			if r := e.get(p); r.status != 404 {
				t.Errorf("%s: %d", p, r.status)
			}
		}
		targets, err := e.ex.Targets(ctx, "en")
		if err != nil {
			t.Fatal(err)
		}
		var listing []string
		for _, tg := range targets {
			if tg.Kind == "listing" {
				listing = append(listing, tg.URL)
			}
		}
		if strings.Join(listing, " ") != "/en/posts /en/posts/page/2 /en/posts/page/3" {
			t.Errorf("listing targets = %v", listing)
		}
		out, err := e.ex.Render(ctx, "en", "/en/posts/page/2")
		if err != nil || out.Status != 200 || !slices.Contains(out.Tags, "c:article") {
			t.Errorf("render page 2: %v %+v", err, out)
		}
	})
}

func TestSiteCustomRouteChecks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		_, err := newSiteEnv(t, b.open(t, clk), clk, "starter", func(r *siteRegistrar) {
			site.Handle(r, "GET /not-reserved", func(*site.Request) (*site.Response, error) { return nil, nil })
			site.Handle(r, "GET /{a}/edit", func(*site.Request) (*site.Response, error) { return nil, nil })
			site.Handle(r, "GET /tags/{x}/{rest...}", func(*site.Request) (*site.Response, error) { return nil, nil })
		})
		if err == nil || !strings.Contains(err.Error(), "/not-reserved is not on a reserved path") {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(err.Error(), "/{a}/edit is not on a reserved path") {
			t.Errorf("a wildcard first segment passed: %v", err)
		}
		if strings.Contains(err.Error(), "tags") {
			t.Errorf("a reserved wildcard path is rejected: %v", err)
		}
		for name, setup := range map[string]func(r *siteRegistrar){
			"bad pattern": func(r *siteRegistrar) {
				site.Handle(r, "GET search", func(*site.Request) (*site.Response, error) { return nil, nil })
			},
			"bad method": func(r *siteRegistrar) {
				site.Handle(r, "FETCH /search", func(*site.Request) (*site.Response, error) { return nil, nil })
			},
			"duplicate": func(r *siteRegistrar) {
				site.Handle(r, "GET /search", func(*site.Request) (*site.Response, error) { return nil, nil })
				site.Handle(r, "GET /search", func(*site.Request) (*site.Response, error) { return nil, nil })
			},
			"unknown channel": func(r *siteRegistrar) {
				site.Handle(r, "GET /search", func(*site.Request) (*site.Response, error) { return nil, nil }).Channels("de")
			},
		} {
			if _, err := newSiteEnv(t, b.open(t, clk), clk, "starter", setup); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}
	})
}

func TestSiteThemeErrors(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		mk := func(name, base string) site.Theme {
			return site.Theme{Name: name, Base: base, FS: fstest.MapFS{}}
		}
		for _, c := range []struct {
			name, theme, want string
			themes            []site.Theme
		}{
			{"unknown", "nope", `unknown theme "nope"`, nil},
			{"missing base", "child", `base "ghost" is not registered`, []site.Theme{mk("child", "ghost")}},
			{"cycle", "a", "theme cycle: a -> b -> a", []site.Theme{mk("a", "b"), mk("b", "a")}},
			{"own base", "a", "its own base", []site.Theme{mk("a", "a")}},
			{"starter twice", "starter", "registered twice", []site.Theme{mk("starter", "")}},
			{"no FS", "a", "has no FS", []site.Theme{{Name: "a"}}},
			{"bad name", "a", "must be lower-case", []site.Theme{mk("Bad Name", "")}},
		} {
			_, err := newSiteEnv(t, b.open(t, clk), clk, c.theme, func(r *siteRegistrar) {
				for _, th := range c.themes {
					site.UseTheme(r, th)
				}
			})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
			}
		}
	})
}

func TestSiteExport(t *testing.T) {
	runSite(t, "routes", func(t *testing.T, e *siteEnv) {
		rootItem, rootNode := e.place("home", "Home", nil)
		aboutItem, _ := e.place("section", "About", &rootNode)
		_ = rootItem
		art := e.publishItem("article", "Hello")
		_ = art

		chans := e.ex.Channels()
		if len(chans) != 1 || chans[0].Name != "en" || chans[0].Prefix != "/en" || chans[0].Host != "" {
			t.Fatalf("channels = %+v", chans)
		}
		targets, err := e.ex.Targets(ctx, "en")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, tg := range targets {
			got[tg.URL] = tg.Kind
		}
		want := map[string]string{"/en": "page", "/en/about": "page", "/en/tags/a": "route", "/en/tags/b%20c": "route",
			"/en/__hydrogen/404": "not_found"}
		for u, k := range want {
			if got[u] != k {
				t.Errorf("target %s = %q, want %q (all %v)", u, got[u], k, got)
			}
		}
		for u := range got {
			if strings.Contains(u, "/search") || strings.Contains(u, "/account") || strings.Contains(u, "/comp") {
				t.Errorf("a route without static params is a target: %s", u)
			}
		}
		if !slices.IsSortedFunc(targets, func(a, b site.Target) int { return strings.Compare(a.URL, b.URL) }) {
			t.Error("targets are not sorted")
		}
		for _, tg := range targets {
			if tg.Kind == "page" && tg.Item == "" {
				t.Errorf("page target without item: %+v", tg)
			}
		}

		// every target renders as a visitor sees it
		for _, tg := range targets {
			out, err := e.ex.Render(ctx, "en", tg.URL)
			if err != nil {
				t.Fatalf("%s: %v", tg.URL, err)
			}
			wantStatus := 200
			if tg.Kind == "not_found" {
				wantStatus = 404
			}
			if out.Status != wantStatus || !strings.Contains(string(out.Body), "<html") {
				t.Errorf("%s: %d", tg.URL, out.Status)
			}
		}
		out, err := e.ex.Render(ctx, "en", "/en/about")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(out.Tags, "i:"+aboutItem.String()) || !slices.Contains(out.Tags, "r:main") || !slices.IsSorted(out.Tags) {
			t.Errorf("tags = %v", out.Tags)
		}
		if !strings.Contains(out.Header.Get("Content-Type"), "text/html") || strings.Contains(string(out.Body), "hydrogen-preview") {
			t.Errorf("headers = %v", out.Header)
		}
		// the custom route renders with its tags too, and static params are escaped path segments
		tag, err := e.ex.Render(ctx, "en", "/en/tags/b%20c")
		if err != nil || tag.Status != 200 || !strings.Contains(string(tag.Body), "Tag|tag|b c") {
			t.Fatalf("tag: %v %+v", err, tag)
		}
		// misses are results, not errors
		if nf, err := e.ex.Render(ctx, "en", "/en/nothing-here"); err != nil || nf.Status != 404 {
			t.Errorf("miss: %v %+v", err, nf)
		}
		// errors: other channels' URLs, preview parameters, 5xx
		for _, bad := range []string{"/other/x", "/en/about?preview=abc", "http://x/en", "en/about"} {
			if _, err := e.ex.Render(ctx, "en", bad); err == nil {
				t.Errorf("%s rendered", bad)
			}
		}
		if _, err := e.ex.Render(ctx, "nope", "/en"); err == nil {
			t.Error("unknown channel rendered")
		}
		if r, err := e.ex.Render(ctx, "en", "/en/account/boom"); err == nil || r == nil || r.Status != 500 {
			t.Errorf("5xx: %v %+v", err, r)
		}
		// a change is visible at once: the page cache is bypassed
		e.clk.Advance(1e9)
		if _, err := e.c.Update(ctx, aboutItem, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"About us"}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.Publish(ctx, aboutItem, content.PublishOptions{Locales: []string{"en"}}); err != nil {
			t.Fatal(err)
		}
		e.ex.Purge("i:" + aboutItem.String())
		if out, err := e.ex.Render(ctx, "en", "/en/about"); err != nil || !strings.Contains(string(out.Body), "About us") {
			t.Errorf("after publish: %v", err)
		}
		assets, err := e.ex.Assets()
		if err != nil || len(assets) == 0 || !strings.HasPrefix(assets[0].URL, "/__hydrogen/assets/") || len(assets[0].Data) == 0 {
			t.Errorf("assets: %v %v", err, len(assets))
		}
	})
}
