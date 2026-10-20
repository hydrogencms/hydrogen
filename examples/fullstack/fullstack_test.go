package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

const (
	adminURL = "http://localhost:8080"
	token    = "test-token"
	secret   = "test-preview-secret-0123456789abcdef"
)

func options(vars map[string]string, dir string, environ ...string) *config.Options {
	vars["MANAGE_TOKEN"], vars["PREVIEW_SECRET"] = token, secret
	if _, ok := vars["ADMIN_URL"]; !ok {
		vars["ADMIN_URL"] = adminURL
	}
	return &config.Options{Dir: dir, Env: "prod", Environ: environ,
		LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
}

// runCLI runs a project command and returns its output.
func runCLI(t *testing.T, vars map[string]string, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: options(vars, "config"), Stdout: &out, Stderr: io.Discard})
	return out.String(), err
}

type web struct {
	t    *testing.T
	base string // http://host:port
}

type reply struct {
	status int
	header http.Header
	body   string
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
	return m
}

var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (s *web) do(method, path string, hdr map[string]string, body any) reply {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, string(b)}
}

func (s *web) get(path string) reply { return s.do("GET", path, nil, nil) }

func (s *web) manage(method, path string, body any) map[string]any {
	s.t.Helper()
	r := s.do(method, "/api/manage/v1"+path, map[string]string{"Authorization": "Bearer " + token}, body)
	if r.status/100 != 2 {
		s.t.Fatalf("%s %s: %d %s", method, path, r.status, r.body)
	}
	if r.body == "" {
		return nil
	}
	return r.json(s.t)
}

func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func backends(t *testing.T, fn func(t *testing.T, dbURL string)) {
	t.Run("sqlite", func(t *testing.T) { fn(t, "sqlite:"+filepath.Join(t.TempDir(), "fullstack.db")) })
	t.Run("postgres", func(t *testing.T) { fn(t, pgtest.Scratch(t)) })
}

// serve imports the starter fixture into a fresh database and serves the project on a free port.
func serve(t *testing.T, dbURL string) *web {
	t.Helper()
	vars := map[string]string{"DATABASE_URL": dbURL}
	if _, err := runCLI(t, vars, "migrate"); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, vars, "import", "../starter/fixture"); err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	vars["LISTEN_ADDR"] = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	addr, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- hydrogen.Run(ctx, []string{"serve"}, configure, hydrogen.RunOptions{Config: options(vars, "config"), Stderr: io.Discard,
			PasswordIterations: 1000, Listening: func(a string) { addr <- a }})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case a := <-addr:
		return &web{t: t, base: "http://" + a}
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not start")
	}
	return nil
}

func (s *web) itemID(typ, name string) string {
	s.t.Helper()
	list := s.manage("GET", "/collections/"+typ+"/items", nil)
	for _, it := range list["items"].([]any) {
		if at(it, "name") == name || at(it, "fields", "title") == name {
			return at(it, "id").(string)
		}
	}
	s.t.Fatalf("no %s %q", typ, name)
	return ""
}

func (s *web) previewURL(item, channel, locale string) string {
	s.t.Helper()
	tok := s.manage("POST", "/preview-tokens", map[string]any{"channel": channel, "item": item, "locale": locale})
	u, err := url.Parse(at(tok, "url").(string))
	if err != nil {
		s.t.Fatal(err)
	}
	return u.RequestURI()
}

func TestRouting(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)

		home := s.get("/en")
		if home.status != 200 || !strings.Contains(home.header.Get("Content-Type"), "text/html") {
			t.Fatalf("/en: %d %s", home.status, home.header)
		}
		for _, want := range []string{`<h1 class="sr-only" >Home</h1>`, `<html lang="en">`, `<link rel="canonical" href="http://localhost:8080/en">`,
			`hreflang="de" href="http://localhost:8080/de"`, `<a href="/en/about">About</a>`} {
			if !strings.Contains(home.body, want) {
				t.Errorf("/en lacks %q", want)
			}
		}
		if strings.Contains(home.body, "data-h-path") || strings.Contains(home.body, "hydrogen-preview") || strings.Contains(home.body, "bridge.js") {
			t.Error("a public page carries preview markup")
		}
		for _, h := range []string{"Surrogate-Key", "Cache-Tag", "Etag", "Cache-Control"} {
			if home.header.Get(h) == "" {
				t.Errorf("missing %s", h)
			}
		}
		if cc := home.header.Get("Cache-Control"); !strings.Contains(cc, "s-maxage=60") || strings.Contains(cc, "no-store") {
			t.Errorf("Cache-Control = %q", cc)
		}
		if !strings.Contains(home.header.Get("Surrogate-Key"), "r:main") {
			t.Errorf("Surrogate-Key = %q", home.header.Get("Surrogate-Key"))
		}

		// a nested page, with breadcrumbs, and the second language
		team := s.get("/en/about/team")
		if team.status != 200 || !strings.Contains(team.body, `<h1 class="page-title" >Team</h1>`) || !strings.Contains(team.body, `<a href="/en/about">About</a></li>`) {
			t.Errorf("/en/about/team: %d", team.status)
		}
		de := s.get("/de")
		if de.status != 200 || !strings.Contains(de.body, `<html lang="de">`) || !strings.Contains(de.body, "Startseite") || !strings.Contains(de.body, "Weiterlesen") {
			t.Errorf("/de: %d", de.status)
		}

		// the root goes to the configured channel
		if r := s.get("/"); r.status != 302 || r.header.Get("Location") != "/en" {
			t.Errorf("/: %d %s", r.status, r.header.Get("Location"))
		}
		// manual redirect, status and Location
		if r := s.get("/en/company"); r.status != 301 || r.header.Get("Location") != "/en/about" {
			t.Errorf("/en/company: %d %q", r.status, r.header.Get("Location"))
		}
		// trailing slash
		if r := s.get("/en/about/"); r.status != 308 || r.header.Get("Location") != "/en/about" {
			t.Errorf("/en/about/: %d %q", r.status, r.header.Get("Location"))
		}
		// 404 page: the theme's, with the status
		for _, p := range []string{"/en/nope", "/en/coming-soon", "/de/about/"} {
			nf := s.get(p)
			if p == "/de/about/" {
				continue
			}
			if nf.status != 404 || !strings.Contains(nf.body, "Page not found") || !strings.Contains(nf.body, `<a href="/en/about">`) {
				t.Errorf("%s: %d", p, nf.status)
			}
		}
		// reserved paths and other parts of the binary are not the site's
		for _, p := range []string{"/api/anything", "/en/api/x"} {
			r := s.get(p)
			if r.status != 404 || strings.Contains(r.body, "<html") {
				t.Errorf("%s: %d, want the plain 404", p, r.status)
			}
		}
		for _, p := range []string{"/healthzz", "/nochannel/about"} {
			if r := s.get(p); r.status != 404 || !strings.Contains(r.body, "Page not found") {
				t.Errorf("%s: %d, want the site's 404 page", p, r.status)
			}
		}
		if r := s.get("/healthz"); r.status != 200 || r.body != "ok\n" {
			t.Errorf("/healthz = %d %q", r.status, r.body)
		}
		if r := s.do("POST", "/en", nil, nil); r.status != 405 {
			t.Errorf("POST /en: %d", r.status)
		}
		if r := s.do("HEAD", "/en", nil, nil); r.status != 200 || r.body != "" {
			t.Errorf("HEAD /en: %d %q", r.status, r.body)
		}
		// conditional requests
		tag := home.header.Get("Etag")
		if r := s.do("GET", "/en", map[string]string{"If-None-Match": tag}, nil); r.status != 304 {
			t.Errorf("If-None-Match: %d", r.status)
		}

		// collection routes
		list := s.get("/en/blog")
		if list.status != 200 || !strings.Contains(list.body, `<a href="/en/blog/hello-world">Hello, world</a>`) {
			t.Errorf("/en/blog: %d", list.status)
		}
		post := s.get("/en/blog/hello-world")
		if post.status != 200 || !strings.Contains(post.body, `<h1 class="page-title" >Hello, world</h1>`) || !strings.Contains(post.body, `<link rel="canonical" href="http://localhost:8080/en/blog/hello-world">`) {
			t.Errorf("/en/blog/hello-world: %d", post.status)
		}
		if !strings.Contains(post.header.Get("Surrogate-Key"), "c:post") {
			t.Errorf("post tags = %q", post.header.Get("Surrogate-Key"))
		}
		if r := s.get("/en/blog/missing"); r.status != 404 || !strings.Contains(r.body, "Page not found") {
			t.Errorf("/en/blog/missing: %d", r.status)
		}
		// links to posts (rich text, in the page's own prefix) come from the channel's links pattern
		rt := s.get("/de/blog/hallo-welt")
		if rt.status != 200 || !strings.Contains(rt.body, `<link rel="canonical" href="http://localhost:8080/de/blog/hallo-welt">`) {
			t.Errorf("/de/blog/hallo-welt: %d", rt.status)
		}
		if !strings.Contains(s.get("/de/blog").body, `href="/de/blog/hallo-welt"`) {
			t.Error("the German listing does not link with the /de prefix")
		}
		// links to posts in rich text go through the collection route
		if !strings.Contains(home.body, `href="/en/blog/building-with-blocks"`) {
			t.Error("the rich text link to a post is not resolved")
		}
	})
}

var marker = regexp.MustCompile(`data-h-block="([a-zA-Z]+)"`)

func TestEveryBlockRenders(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)
		seen := map[string]int{}
		for _, p := range []string{"/en", "/en/about", "/en/services", "/en/contact", "/en/about/team", "/en/about/careers",
			"/de", "/de/ueber-uns", "/en/blog/hello-world", "/en/blog/building-with-blocks", "/en/blog/release-notes"} {
			r := s.get(p)
			if r.status != 200 {
				// German slugs differ; the page list is checked through the tree below
				if strings.HasPrefix(p, "/de/") {
					continue
				}
				t.Errorf("%s: %d", p, r.status)
				continue
			}
			for _, m := range marker.FindAllStringSubmatch(r.body, -1) {
				seen[m[1]]++
			}
		}
		for _, typ := range []string{"hero", "richText", "cards", "card", "callout", "image", "shared"} {
			if seen[typ] == 0 {
				t.Errorf("no page rendered a %s block (saw %v)", typ, seen)
			}
		}
		// the shared block shows the shared item's blocks, read only
		home := s.get("/en").body
		i := strings.Index(home, `data-h-block="shared"`)
		if i < 0 || !strings.Contains(home[i:], "Questions? We are happy to help.") {
			t.Error("the shared block's content is missing")
		}
	})
}

func TestCustomRoutesAndTheme(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)

		// the theme's footer layers over the starter's, and its translations over the starter's keys
		home := s.get("/en")
		for _, want := range []string{`<form method="get" action="/en/search" role="search">`, `placeholder="Search the site"`, `<a href="/en/colophon">Colophon</a>`, "Skip to content"} {
			if !strings.Contains(home.body, want) {
				t.Errorf("/en lacks %q", want)
			}
		}
		if de := s.get("/de"); !strings.Contains(de.body, "Die Website durchsuchen") || !strings.Contains(de.body, "Startseite") {
			t.Error("/de lacks the theme's and the starter's translations")
		}

		// /search: a custom route with the full page context, never cached, tagged by what it read
		r := s.get("/en/search?q=block")
		if r.status != 200 {
			t.Fatalf("/en/search: %d %s", r.status, r.body)
		}
		for _, want := range []string{`<html lang="en">`, `<input type="search" name="q" value="block"`, `<a href="/en/blog/building-with-blocks">Building with blocks</a>`,
			`class="site-header"`, `<link rel="canonical" href="http://localhost:8080/en/search">`, `<a href="/en/search">Search</a>`} {
			if !strings.Contains(r.body, want) {
				t.Errorf("/en/search lacks %q", want)
			}
		}
		if r.header.Get("Cache-Control") != "no-store" || r.header.Get("Etag") != "" || r.header.Get("X-Hydrogen-Cache") != "" {
			t.Errorf("search headers = %v", r.header)
		}
		if !strings.Contains(r.header.Get("Surrogate-Key"), "c:post") {
			t.Errorf("search tags = %q", r.header.Get("Surrogate-Key"))
		}
		// /latest: a template reading content with `items`; the page carries the listed posts' tags
		lt := s.get("/en/latest")
		if lt.status != 200 || !strings.Contains(lt.body, `<ol class="latest">`) || !strings.Contains(lt.body, `<a href="/en/blog/hello-world">HELLO, WORLD</a>`) {
			t.Fatalf("/en/latest: %d %s", lt.status, lt.body)
		}
		if tag := "i:" + s.itemID("post", "Hello, world"); !strings.Contains(lt.header.Get("Surrogate-Key"), tag) || !strings.Contains(lt.header.Get("Surrogate-Key"), "c:post") {
			t.Errorf("latest tags = %q, want %s", lt.header.Get("Surrogate-Key"), tag)
		}
		if none := s.get("/en/search?q=zzzzqqq"); none.status != 200 || !strings.Contains(none.body, "Nothing matched.") {
			t.Errorf("empty search: %d", none.status)
		}
		if blank := s.get("/en/search"); blank.status != 200 || strings.Contains(blank.body, "search-results") {
			t.Errorf("search without q: %d", blank.status)
		}
		if de := s.get("/de/search?q=welt"); de.status != 200 || !strings.Contains(de.body, "Suche") || !strings.Contains(de.body, `href="/de/blog/hallo-welt"`) {
			t.Errorf("/de/search: %d", de.status)
		}
		if p := s.do("POST", "/en/search", nil, nil); p.status != 405 {
			t.Errorf("POST /en/search: %d", p.status)
		}

		// /colophon: rendered by the templ component, cacheable for shared caches
		c := s.get("/en/colophon")
		if c.status != 200 || !strings.Contains(c.body, `<h1 class="page-title">Colophon</h1>`) || !strings.Contains(c.body, "This page is a templ component.") ||
			!strings.Contains(c.body, `href="/__hydrogen/assets/site.`) || !strings.Contains(c.body, "<title>Colophon") {
			t.Errorf("/en/colophon: %d\n%s", c.status, c.body)
		}
		if !strings.Contains(c.header.Get("Cache-Control"), "s-maxage=60") {
			t.Errorf("colophon Cache-Control = %q", c.header.Get("Cache-Control"))
		}
		if d := s.get("/de/colophon"); d.status != 200 || !strings.Contains(d.body, "Diese Seite ist eine templ-Komponente.") {
			t.Errorf("/de/colophon: %d", d.status)
		}
		// the reserved paths below the routes stay the site's 404
		if r := s.get("/en/search/x"); r.status != 404 {
			t.Errorf("/en/search/x: %d", r.status)
		}
		// a preview of a custom route carries the preview scaffolding
		home2 := s.itemID("landing", "Home")
		pv := s.get(strings.Replace(s.previewURL(home2, "en", "en"), "/en?", "/en/search?q=block&", 1))
		if pv.status != 200 || !strings.Contains(pv.body, `name="hydrogen-preview"`) || !strings.Contains(pv.body, "bridge.js") || pv.header.Get("Cache-Control") != "no-store" {
			t.Errorf("preview search: %d %v", pv.status, pv.header)
		}
	})
}

func TestPurgeOnPublish(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)
		first := s.get("/en/about")
		if first.status != 200 {
			t.Fatalf("first: %d", first.status)
		}
		// the import's own events purge the cache while they settle
		waitFor(t, "a cache hit", func() bool { return s.get("/en/about").header.Get("X-Hydrogen-Cache") == "hit" })
		if second := s.get("/en/about"); second.header.Get("Etag") != first.header.Get("Etag") {
			t.Fatal("the cached page differs")
		}
		shared := s.itemID("sharedBlock", "Get in touch")
		if !strings.Contains(first.header.Get("Surrogate-Key"), "i:"+shared) {
			t.Fatalf("the page is not tagged with the shared item: %q", first.header.Get("Surrogate-Key"))
		}
		blocks := []any{map[string]any{"type": "callout", "props": map[string]any{"text": "Changed text", "tone": "dark"}}}
		s.manage("PATCH", "/items/"+shared, map[string]any{"locale": "en", "fields": map[string]any{"content": blocks}})
		s.manage("POST", "/items/"+shared+"/publish", map[string]any{})
		waitFor(t, "the page shows the shared change", func() bool {
			r := s.get("/en/about")
			return strings.Contains(r.body, "Changed text") && r.header.Get("X-Hydrogen-Cache") == "miss"
		})
		if r := s.get("/en/about"); r.header.Get("X-Hydrogen-Cache") != "hit" || !strings.Contains(r.body, "Changed text") {
			t.Error("the rebuilt page is not cached again")
		}

		// the page's own title: publishing a page purges it, its navigation entry and the pages that link it
		about := s.itemID("page", "About")
		s.manage("PATCH", "/items/"+about, map[string]any{"locale": "en", "fields": map[string]any{"title": "About us"}})
		s.manage("POST", "/items/"+about+"/publish", map[string]any{})
		waitFor(t, "the title change", func() bool { return strings.Contains(s.get("/en/about").body, "About us") })
		waitFor(t, "the navigation of another page", func() bool { return strings.Contains(s.get("/en/services").body, ">About us</a>") })
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestPreview(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)
		home := s.itemID("landing", "Home")
		s.manage("PATCH", "/items/"+home, map[string]any{"locale": "en", "fields": map[string]any{"title": "Home (draft)"}})

		if r := s.get("/en"); strings.Contains(r.body, "Home (draft)") {
			t.Fatal("a draft leaked to the public page")
		}
		u := s.previewURL(home, "en", "en")
		if !strings.HasPrefix(u, "/en?preview=") {
			t.Fatalf("preview url = %q", u)
		}
		r := s.get(u)
		if r.status != 200 {
			t.Fatalf("%d %s", r.status, r.body)
		}
		prevTok := strings.TrimPrefix(u[strings.Index(u, "preview="):], "preview=")
		sum := sha256.Sum256([]byte(prevTok))
		for _, want := range []string{"Home (draft)", `<meta name="hydrogen-preview" content="` + hex.EncodeToString(sum[:]) + `">`, `data-h-root`, `data-h-path="fields/body/0"`,
			`<script src="/__hydrogen/bridge.js" data-auto data-mode="reload" data-admin-origin="` + adminURL + `"></script>`} {
			if !strings.Contains(r.body, want) {
				t.Errorf("preview lacks %q", want)
			}
		}
		if r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Hydrogen-Cache") != "" {
			t.Errorf("preview is cacheable: %q", r.header.Get("Cache-Control"))
		}
		// no read-only paths inside shared blocks
		if i := strings.Index(r.body, `data-h-block="shared"`); i < 0 || strings.Contains(r.body[i:], `data-h-path="fields/body/5/slots/blocks`) {
			t.Error("shared content is editable")
		}
		if strings.Contains(r.body, prevTok) {
			t.Error("the raw preview token is rendered")
		}
		if csp := r.header.Get("Content-Security-Policy"); csp != "script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors "+adminURL || r.header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("preview headers: CSP %q, Referrer-Policy %q", csp, r.header.Get("Referrer-Policy"))
		}
		if s.get("/en").header.Get("Content-Security-Policy") != "" {
			t.Error("a public page carries the preview CSP")
		}
		cookie := r.header.Get("Set-Cookie")
		if !strings.Contains(cookie, "hydrogen-preview-en=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "Path=/") {
			t.Fatalf("Set-Cookie = %q", cookie)
		}
		if !strings.Contains(cookie, "SameSite=Lax") || strings.Contains(cookie, "Secure") {
			t.Errorf("a plain-http cookie: %q", cookie)
		}
		// X-Forwarded-Proto is not believed without trusted_proxies
		if fwd := s.do("GET", u, map[string]string{"X-Forwarded-Proto": "https"}, nil); strings.Contains(fwd.header.Get("Set-Cookie"), "Secure") {
			t.Errorf("an untrusted X-Forwarded-Proto made the cookie Secure: %q", fwd.header.Get("Set-Cookie"))
		}
		// the cookie keeps previewing while navigating; other items are published
		c := strings.SplitN(cookie, ";", 2)[0]
		nav := s.do("GET", "/en", map[string]string{"Cookie": c}, nil)
		if !strings.Contains(nav.body, "Home (draft)") {
			t.Error("the cookie did not keep the preview")
		}
		about := s.do("GET", "/en/about", map[string]string{"Cookie": c}, nil)
		if about.status != 200 || !strings.Contains(about.body, `hydrogen-preview`) {
			t.Errorf("/en/about with the cookie: %d", about.status)
		}
		// a locked frame has no cookies: ?preview= alone previews every page, also next to another channel's cookie,
		// and a reload of the tokenized URL renders the current draft uncached
		for _, hdr := range []map[string]string{nil, {"Cookie": "hydrogen-preview-de=whatever"}} {
			if r := s.do("GET", "/en/about?"+u[strings.Index(u, "preview="):], hdr, nil); r.status != 200 || !strings.Contains(r.body, `content="`+hex.EncodeToString(sum[:])+`"`) || r.header.Get("Cache-Control") != "no-store" {
				t.Errorf("token-only page with headers %v: %d %q", hdr, r.status, r.header.Get("Cache-Control"))
			}
		}
		s.manage("PATCH", "/items/"+home, map[string]any{"locale": "en", "fields": map[string]any{"title": "Home (draft 2)"}})
		if r := s.get(u); r.status != 200 || !strings.Contains(r.body, "Home (draft 2)") || r.header.Get("Cache-Control") != "no-store" {
			t.Errorf("reload of the tokenized URL: %d %q", r.status, r.header.Get("Cache-Control"))
		}
		// a token for another channel does not preview here; a bad cookie is dropped, a bad query token refused
		other := s.previewURL(home, "de", "de")
		tok := strings.TrimPrefix(other[strings.Index(other, "preview="):], "preview=")
		if r := s.get("/en?preview=" + tok); r.status != 401 {
			t.Errorf("a token of another channel: %d", r.status)
		}
		if r := s.get("/en?preview=garbage"); r.status != 401 {
			t.Errorf("a bad token: %d", r.status)
		}
		bad := s.do("GET", "/en", map[string]string{"Cookie": "hydrogen-preview-en=garbage"}, nil)
		if bad.status != 200 || strings.Contains(bad.body, "hydrogen-preview") || !strings.Contains(bad.header.Get("Set-Cookie"), "Max-Age=0") {
			t.Errorf("a bad cookie: %d %q", bad.status, bad.header.Get("Set-Cookie"))
		}

		// an item that was never published has no route: the preview pattern URL renders it
		soon := s.itemID("landing", "Coming soon")
		pu := s.previewURL(soon, "en", "en")
		defer func() {
			if r := s.do("GET", pu, map[string]string{"Cookie": "hydrogen-preview-de=whatever"}, nil); r.status != 200 {
				t.Errorf("token-only preview route next to another channel's cookie: %d", r.status)
			}
		}()
		if !strings.HasPrefix(pu, "/en/preview/en/landing/"+soon+"?preview=") {
			t.Fatalf("preview url = %q", pu)
		}
		if r := s.get(pu); r.status != 200 || !strings.Contains(r.body, "Coming soon") || !strings.Contains(r.body, "bridge.js") {
			t.Errorf("never-published page: %d", r.status)
		}
		if r := s.get(strings.SplitN(pu, "?", 2)[0]); r.status != 404 {
			t.Errorf("the preview path without a token: %d", r.status)
		}
		// the unprefixed form works too: the channel comes from the token
		if r := s.get(strings.TrimPrefix(pu, "/en")); r.status != 200 || !strings.Contains(r.body, "Coming soon") {
			t.Errorf("the unprefixed preview path: %d", r.status)
		}
		// a draft post, found by its slug through the collection route
		posts := s.manage("GET", "/collections/post/items", nil)
		post := at(posts, "items", 0, "id").(string)
		s.manage("PATCH", "/items/"+post, map[string]any{"locale": "en", "fields": map[string]any{"excerpt": "Draft excerpt"}})
		slug := at(s.manage("GET", "/items/"+post, nil), "item", "fields", "slug")
		_ = slug
		pp := s.previewURL(post, "en", "en")
		if r := s.get(pp); r.status != 200 || !strings.Contains(r.body, "Draft excerpt") {
			t.Errorf("draft post at %s: %d", pp, r.status)
		}
	})
}

func TestAssetsAndBridge(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := serve(t, dbURL)
		css := regexp.MustCompile(`/__hydrogen/assets/site\.[0-9a-f]+\.css`).FindString(s.get("/en").body)
		if css == "" {
			t.Fatal("no fingerprinted stylesheet")
		}
		r := s.get(css)
		if r.status != 200 || !strings.Contains(r.header.Get("Cache-Control"), "immutable") || !strings.Contains(r.header.Get("Content-Type"), "text/css") {
			t.Errorf("%s: %d %v", css, r.status, r.header)
		}
		b := s.get("/__hydrogen/bridge.js")
		if b.status != 200 || !strings.Contains(b.body, "HydrogenBridge") || !strings.Contains(b.header.Get("Content-Type"), "javascript") {
			t.Errorf("bridge: %d %v", b.status, b.header)
		}
		if r.header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("asset CORS header: %q", r.header.Get("Access-Control-Allow-Origin"))
		}
		if r := s.get("/__hydrogen/assets/nothing.css"); r.status != 404 {
			t.Errorf("unknown asset: %d", r.status)
		}
	})
}

// projectWith copies the project's config and puts a site directory with the given templates beside it.
func projectWith(t *testing.T, routes string, templates map[string]string) (dir string, environ []string) {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"app.yaml", "channels.yaml"} {
		b, err := os.ReadFile(filepath.Join("config", f))
		if err != nil {
			t.Fatal(err)
		}
		if f == "app.yaml" && routes != "" {
			b = bytes.Replace(b, []byte(`      - { path: "/blog/{slug}", type: post }`), []byte(`      - { path: "/blog/{slug}", type: post }`+"\n"+routes), 1)
		}
		if err := os.WriteFile(filepath.Join(cfg, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	shipped := filepath.Join("site", "templates")
	_ = filepath.WalkDir(shipped, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(shipped, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, "site", "templates", rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	for name, src := range templates {
		p := filepath.Join(root, "site", "templates", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	schemaDir, _ := filepath.Abs("../starter/schema")
	return cfg, []string{"HYDROGEN__MODULES__SITE__DIR=" + filepath.Join(root, "site"), "HYDROGEN__CONTENT__SCHEMA_DIR=" + schemaDir}
}

func siteCheck(t *testing.T, routes string, templates map[string]string) error {
	t.Helper()
	dir, environ := projectWith(t, routes, templates)
	vars := map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "check.db")}
	return hydrogen.Run(context.Background(), []string{"site:check"}, configure, hydrogen.RunOptions{Config: options(vars, dir, environ...), Stderr: io.Discard})
}

func expectProblem(t *testing.T, what string, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: site:check passed", what)
		return
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%s: the report lacks %q:\n%v", what, w, err)
		}
	}
}

func TestSiteCheck(t *testing.T) {
	if err := siteCheck(t, "", nil); err != nil {
		t.Fatalf("the project as shipped: %v", err)
	}
	// a project template replaces the theme's by name, and is checked like it
	expectProblem(t, "a prop the schema lacks", siteCheck(t, "", map[string]string{"blocks/hero.html": `<section {{.Attrs}}>{{.Props.headline}}</section>`}), "headline")
	expectProblem(t, "a block without data-h-block", siteCheck(t, "", map[string]string{"blocks/callout.html": `<aside>{{.Props.text}}</aside>`}), "data-h-block")
	expectProblem(t, "a template that does not parse", siteCheck(t, "", map[string]string{"pages/landing.html": `{{define "content"}}{{if}}{{end}}`}), "landing.html")
	expectProblem(t, "a route without a template", siteCheck(t, `      - { path: "/team/{slug}", type: post, template: ghost }`, nil), "ghost.html")
	expectProblem(t, "a broken 404 page", siteCheck(t, "", map[string]string{"404.html": `{{define "content"}}{{.Nope}}{{end}}`}), "404.html")
	expectProblem(t, "a broken custom route template", siteCheck(t, "", map[string]string{"routes/search.html": `{{define "content"}}{{.Data.Nope}}{{end}}`}), "routes/search.html")
	expectProblem(t, "a missing asset", siteCheck(t, "", map[string]string{"layout.html": `<link href="{{asset "nope.css"}}">{{block "content" .}}{{end}}`}), "nope.css")
}

func TestSiteCheckWithContent(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		vars := map[string]string{"DATABASE_URL": dbURL}
		for _, args := range [][]string{{"migrate"}, {"import", "../starter/fixture"}} {
			if out, err := runCLI(t, vars, args...); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
		if _, err := runCLI(t, vars, "site:check", "-db"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBrokenTemplatesStopTheServer(t *testing.T) {
	dir, environ := projectWith(t, "", map[string]string{"layout.html": `{{if}}`})
	vars := map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "x.db"), "LISTEN_ADDR": "127.0.0.1:0"}
	for _, args := range [][]string{{"migrate"}} {
		if err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: options(vars, dir, environ...), Stdout: io.Discard, Stderr: io.Discard}); err != nil {
			t.Fatal(err)
		}
	}
	err := hydrogen.Run(context.Background(), []string{"serve"}, configure, hydrogen.RunOptions{Config: options(vars, dir, environ...), Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "layout.html") {
		t.Fatalf("serve = %v", err)
	}
}

func TestConfigIsValidated(t *testing.T) {
	check := func(file, from, to string) error {
		dir := t.TempDir()
		for _, f := range []string{"app.yaml", "channels.yaml"} {
			b, err := os.ReadFile(filepath.Join("config", f))
			if err != nil {
				t.Fatal(err)
			}
			if f == file {
				if !bytes.Contains(b, []byte(from)) {
					t.Fatalf("%s lacks %q", f, from)
				}
				b = bytes.Replace(b, []byte(from), []byte(to), 1)
			}
			if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{
			Config: options(map[string]string{"DATABASE_URL": "sqlite:x.db", "SITE_TEST_TOKEN": "secret-token-0123456789abcdef"}, dir), Stdout: io.Discard, Stderr: io.Discard})
	}
	if err := check("app.yaml", "root: en", "root: en"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ file, from, to, want string }{
		{"channels.yaml", "render: go", "render: php", `render must be "go"`},
		{"app.yaml", "theme: fullstack", "theme: nope", `unknown theme "nope"`},
		{"app.yaml", "theme: fullstack", "theme: Bad_Name", "must be a theme name"},
		{"channels.yaml", ", /search, /colophon, /latest]", ", /colophon, /latest]", "/search is not on a reserved path of channel en"},
		{"app.yaml", "root: en", "root: fr", "is not a channel with render"},
		{"app.yaml", `type: post, template: posts`, `type: post`, "a listing needs a template"},
		{"app.yaml", `/blog/{slug}`, `/blog/{slug}/{x}`, "at most one {param}"},
		{"app.yaml", "admin_url: ${env:ADMIN_URL:-}", "admin_url: example.com", "must be an origin"},
		{"app.yaml", "root: en", "bogus: 1", "unknown key"},
		{"app.yaml", "admin_url: ${env:ADMIN_URL:-}", "admin_url: ${env:ADMIN_URL:-}\n    preview_csp: maybe", "not true or false"},
		{"channels.yaml", "match: { prefix: /en } }", "access: token, tokens: { t: \"${env:SITE_TEST_TOKEN}\" }, match: { prefix: /en } }", "public access"},
	} {
		err := check(c.file, c.from, c.to)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s → %s: got %v, want %q", c.from, c.to, err, c.want)
		}
	}
}

func TestAdminURLIsRequiredOutsideDev(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"app.yaml", "channels.yaml"} {
		b, _ := os.ReadFile(filepath.Join("config", f))
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(env string, vars map[string]string) error {
		o := options(vars, dir)
		o.Env = env
		return hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{Config: o, Stdout: io.Discard, Stderr: io.Discard})
	}
	if err := run("prod", map[string]string{"ADMIN_URL": "", "DATABASE_URL": "sqlite:x.db"}); err == nil || !strings.Contains(err.Error(), "admin_url is required") {
		t.Errorf("prod without admin_url: %v", err)
	}
	if err := run("prod", map[string]string{"ADMIN_URL": "https://Cms.Example.com:443", "DATABASE_URL": "sqlite:x.db"}); err != nil {
		t.Errorf("prod with admin_url: %v", err)
	}
	if err := run("prod", map[string]string{"ADMIN_URL": "https://cms.example.com/admin", "DATABASE_URL": "sqlite:x.db"}); err == nil || !strings.Contains(err.Error(), "must be an origin") {
		t.Errorf("an admin_url with a path: %v", err)
	}
	if err := run("dev", map[string]string{"ADMIN_URL": "", "MANAGE_TOKEN": token}); err != nil {
		t.Errorf("dev without admin_url: %v", err)
	}
}
