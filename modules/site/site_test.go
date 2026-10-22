package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
)

func TestRichText(t *testing.T) {
	doc := map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": float64(2)}, "content": []any{map[string]any{"type": "text", "text": "A <b>"}}},
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "go ", "marks": []any{map[string]any{"type": "bold"}, map[string]any{"type": "italic"}}},
			map[string]any{"type": "text", "text": "here", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "item:abc"}}}},
			map[string]any{"type": "text", "text": "gone", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "item:missing"}}}},
			map[string]any{"type": "text", "text": "xss", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "javascript:alert(1)"}}}},
			map[string]any{"type": "text", "text": "ext", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/?a=1&b=2"}}}},
		}},
		map[string]any{"type": "orderedList", "attrs": map[string]any{"start": float64(3)}, "content": []any{
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "x"}}}}},
		}},
		map[string]any{"type": "codeBlock", "attrs": map[string]any{"language": "go\"><x"}, "content": []any{map[string]any{"type": "text", "text": "<tag>"}}},
	}}
	got := renderRichText(doc, func(id string) string {
		if id == "abc" {
			return "/en/about"
		}
		return ""
	})
	want := `<h2>A &lt;b&gt;</h2><p><strong><em>go </em></strong><a href="/en/about">here</a>gone` +
		`xss<a href="https://example.com/?a=1&amp;b=2">ext</a></p><ol start="3"><li><p>x</p></li></ol><pre><code>&lt;tag&gt;</code></pre>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if renderRichText(nil, nil) != "" || renderRichText("text", nil) != "" {
		t.Error("a non-document renders something")
	}
}

func TestRichTextTablesImagesMarks(t *testing.T) {
	var doc any
	src := `{"type":"doc","content":[
	{"type":"table","content":[
	 {"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":2,"rowspan":1},"content":[{"type":"paragraph","content":[{"type":"text","text":"h"}]}]}]},
	 {"type":"tableRow","content":[{"type":"tableCell","attrs":{"rowspan":3,"colwidth":[100]},"content":[{"type":"paragraph","content":[
	  {"type":"text","text":"u","marks":[{"type":"underline"},{"type":"strike"}]},
	  {"type":"text","text":"2","marks":[{"type":"superscript"}]},
	  {"type":"text","text":"3","marks":[{"type":"subscript"}]}]}]}]}]},
	{"type":"image","attrs":{"src":"https://a.b/x.png?a=1&b=2","alt":"a\"b","title":"t","width":10,"height":20}},
	{"type":"image","attrs":{"src":"/assets/x/y.png"}},
	{"type":"image","attrs":{"src":"javascript:alert(1)"}},
	{"type":"image","attrs":{"src":"//evil.com/x.png"}},
	{"type":"image","attrs":{"src":"data:image/png;base64,AA"}}]}`
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	want := `<table><tbody><tr><th colspan="2"><p>h</p></th></tr><tr><td rowspan="3"><p><u><s>u</s></u><sup>2</sup><sub>3</sub></p></td></tr></tbody></table>` +
		`<img src="https://a.b/x.png?a=1&amp;b=2" alt="a&quot;b" title="t" width="10" height="20" loading="lazy">` +
		`<img src="/assets/x/y.png" alt="" loading="lazy">`
	if got := renderRichText(doc, nil); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestRoutes(t *testing.T) {
	rt, err := parseRoute(Route{Path: "/blog/{slug}", Type: "post"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := rt.match("/blog/hello%20world"); !ok || v != "hello world" {
		t.Errorf("match = %q %v", v, ok)
	}
	for _, p := range []string{"/blog", "/blog/a/b", "/news/a", "/blog/"} {
		if _, ok := rt.match(p); ok {
			t.Errorf("%s matches", p)
		}
	}
	if p, ok := rt.build("1", map[string]any{"slug": "a b/c"}); !ok || p != "/blog/a%20b%2Fc" {
		t.Errorf("build = %q %v", p, ok)
	}
	if _, ok := rt.build("1", map[string]any{"slug": ".."}); ok {
		t.Error("built a path from ..")
	}
	list, err := parseRoute(Route{Path: "/blog", Type: "post", Template: "posts"})
	if err != nil || list.param != "" || list.templateName() != "collections/posts.html" {
		t.Errorf("listing = %+v %v", list, err)
	}
	for _, bad := range []Route{
		{Path: "blog/{slug}", Type: "post"}, {Path: "/", Type: "post"}, {Path: "/a/{x}/{y}", Type: "post"}, {Path: "/a/x{y}", Type: "post"},
		{Path: "/blog", Type: "post"}, {Path: "/a/{x}", Type: "po st"}, {Path: "/a/{x}", Type: "post", Template: "../x"},
	} {
		if _, err := parseRoute(bad); err == nil {
			t.Errorf("%+v is accepted", bad)
		}
	}
	idr, _ := parseRoute(Route{Path: "/p/{id}", Type: "post"})
	if p, ok := idr.build("42", nil); !ok || p != "/p/42" {
		t.Errorf("id route = %q", p)
	}
}

func TestPageCache(t *testing.T) {
	c := newPageCache(1 << 20)
	gen := c.generation()
	c.put(&cached{key: "a", body: []byte("A"), tags: []string{"i:1", "r:main"}}, gen)
	c.put(&cached{key: "b", body: []byte("B"), tags: []string{"i:2", "r:main"}}, gen)
	if c.get("a") == nil || c.get("b") == nil {
		t.Fatal("entries missing")
	}
	c.purge("i:1")
	if c.get("a") != nil || c.get("b") == nil {
		t.Error("purge by item tag")
	}
	// a page rendered before a purge must not be stored after it
	gen = c.generation()
	c.purge("zzz")
	c.put(&cached{key: "c", body: []byte("C")}, gen)
	if c.get("c") != nil {
		t.Error("stale page stored")
	}
	c.purge("r:main")
	if c.get("b") != nil {
		t.Error("purge by route tag")
	}
	c.put(&cached{key: "d", body: []byte("D"), tags: []string{"x"}}, c.generation())
	c.purge(cachetag.All)
	if c.get("d") != nil {
		t.Error("purge all")
	}
	// the byte bound evicts the least recently used
	small := newPageCache(8000)
	for i := range 40 {
		small.put(&cached{key: string(rune('a' + i%26)), body: make([]byte, 400)}, small.generation())
	}
	if small.used > small.max {
		t.Errorf("used %d over %d", small.used, small.max)
	}
}

func TestInjectPreview(t *testing.T) {
	page := []byte("<!doctype html><html><head><title>x</title></head><body class=\"a\"><main>hi</main></body></html>")
	got := string(injectPreview(page, "http://localhost:8080", "abc"))
	for _, want := range []string{
		`<meta name="robots" content="noindex"><meta name="hydrogen-preview" content="abc">` + "\n</head>",
		`<body class="a"><div data-h-root><main>hi</main></div><script src="/__hydrogen/bridge.js" data-auto data-mode="reload" data-admin-origin="http://localhost:8080"></script>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q in %s", want, got)
		}
	}
	marked := []byte("<html><head></head><body><div data-h-root>x</div></body></html>")
	if got := string(injectPreview(marked, "o", "abc")); strings.Count(got, "data-h-root") != 1 || !strings.Contains(got, "</div>\n<script") && !strings.Contains(got, "</div><script") {
		t.Errorf("a marked root is wrapped again: %s", got)
	}
}

func TestTranslations(t *testing.T) {
	tr := translations{"en": {"hi": "Hello {name}", "bye": "Bye"}, "de": {"hi": "Hallo {name}"}}
	if got := tr.text("de-CH", "en", "hi", "name", "Ada"); got != "Hallo Ada" {
		t.Errorf("de-CH → de: %q", got)
	}
	if got := tr.text("de", "en", "bye"); got != "Bye" {
		t.Errorf("fallback to the default locale: %q", got)
	}
	if got := tr.text("de", "en", "unknown"); got != "unknown" {
		t.Errorf("missing key: %q", got)
	}
}

func TestSafeHref(t *testing.T) {
	if safeHref("https://x.test/a") == "" || safeHref("/a") == "" || safeHref("mailto:a@b.c") == "" {
		t.Error("a safe link is refused")
	}
	for _, bad := range []string{"javascript:x", "JaVaScRiPt:x", " java\nscript:x", "//evil.test", `/\evil.test`, "data:text/html,x", ""} {
		if safeHref(bad) != "" {
			t.Errorf("%q is accepted", bad)
		}
	}
}

func TestNormalizeOrigin(t *testing.T) {
	for in, want := range map[string]string{"https://Cms.Example.com:443": "https://cms.example.com", "http://LOCALHOST:80": "http://localhost",
		"http://localhost:8080": "http://localhost:8080", "https://a.test:8443": "https://a.test:8443"} {
		if got, err := normalizeOrigin(in); err != nil || got != want {
			t.Errorf("%s → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"cms.example.com", "https://a.test/x", "ftp://a.test", "*", ""} {
		if _, err := normalizeOrigin(bad); err == nil {
			t.Errorf("%q is accepted", bad)
		}
	}
}

func TestInjectPreviewRobustness(t *testing.T) {
	// the text "data-h-root" is not a root; no </head>; upper-case tags
	got := string(injectPreview([]byte(`<HTML><BODY><p>mentions data-h-root in text</p></BODY></HTML>`), "o", "abc"))
	for _, want := range []string{`<BODY><meta name="robots" content="noindex"><meta name="hydrogen-preview" content="abc"><div data-h-root><p>`, `</div><script src=`} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q in %s", want, got)
		}
	}
	got = string(injectPreview([]byte(`<html><head><title>x</title><body><div data-h-root class="a">x</div></body></html>`), "o", "abc"))
	if strings.Count(got, "data-h-root") != 1 || !strings.Contains(got, `<head><meta name="robots" content="noindex"><meta name="hydrogen-preview" content="abc"><title>`) {
		t.Errorf("got %s", got)
	}
	if got := string(injectPreview([]byte(`<header>x</header>`), "o", "abc")); !strings.HasPrefix(got, `<meta name="robots" content="noindex"><meta name="hydrogen-preview"`) {
		t.Errorf("a fragment: %s", got)
	}
}

func TestSchemeFollowsOnlyTrustedProxies(t *testing.T) {
	s := &server{proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	req := func(peer string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}
	if s.schemeOf(req("10.1.2.3:5555")) != "https" {
		t.Error("a trusted proxy's X-Forwarded-Proto is ignored")
	}
	if s.schemeOf(req("203.0.113.9:5555")) != "http" {
		t.Error("an untrusted peer chose the scheme")
	}
	if (&server{}).schemeOf(req("10.1.2.3:1")) != "http" {
		t.Error("X-Forwarded-Proto is believed without trusted proxies")
	}
	tls := httptest.NewRequest("GET", "https://x.test/", nil)
	if s.schemeOf(tls) != "https" {
		t.Error("TLS is not https")
	}
}

func TestCookieMaxAge(t *testing.T) {
	if got := cookieMaxAge(time.Time{}); got != 3600 {
		t.Errorf("no expiry: %d", got)
	}
	if got := cookieMaxAge(time.Now().Add(7 * 24 * time.Hour)); got < 7*24*3600-5 || got > 7*24*3600 {
		t.Errorf("a week: %d", got)
	}
	if got := cookieMaxAge(time.Now().Add(-time.Minute)); got != 1 {
		t.Errorf("expired: %d", got)
	}
}

func TestRespondRawAnswers(t *testing.T) {
	s := &server{cfg: &Config{PreviewCSP: true}}
	ch := &channel.Channel{Name: "web"}
	for _, tc := range []struct {
		name    string
		res     *result
		preview bool
		method  string
		cc      string
		csp     bool
		etag    bool
	}{
		{"cacheable json", &result{status: 200, body: []byte("{}"), custom: true, cacheable: true, contentType: "application/json", tags: []string{"b", "a"}}, false, "GET", "public, max-age=0, s-maxage=60, stale-while-revalidate=60", false, true},
		{"max age", &result{status: 200, body: []byte("x"), custom: true, cacheable: true, maxAge: 2 * time.Minute, contentType: "text/plain"}, false, "GET", "public, max-age=0, s-maxage=120, stale-while-revalidate=120", false, true},
		{"preview json", &result{status: 200, body: []byte("{}"), custom: true, cacheable: true, contentType: "application/json"}, true, "GET", "no-store", false, false},
		{"preview html", &result{status: 200, body: []byte("<p>"), custom: true, cacheable: true}, true, "GET", "no-store", true, false},
		{"post", &result{status: 200, body: []byte("{}"), custom: true, cacheable: true, contentType: "application/json"}, false, "POST", "no-store", false, false},
		{"not cacheable", &result{status: 200, body: []byte("{}"), custom: true, contentType: "application/json"}, false, "GET", "no-store", false, false},
		{"error status", &result{status: 404, body: []byte("{}"), custom: true, cacheable: true, contentType: "application/json"}, false, "GET", "no-store", false, false},
	} {
		w := httptest.NewRecorder()
		s.respond(w, httptest.NewRequest(tc.method, "http://site.test/x", nil), ch, tc.res, tc.preview, "", 0)
		h := w.Header()
		if h.Get("Cache-Control") != tc.cc || (h.Get("Content-Security-Policy") != "") != tc.csp || (h.Get("Etag") != "") != tc.etag ||
			(tc.preview && h.Get("Referrer-Policy") != "no-referrer") || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %v", tc.name, h)
		}
		if tc.res.contentType != "" && h.Get("Content-Type") != tc.res.contentType {
			t.Errorf("%s: content type %q", tc.name, h.Get("Content-Type"))
		}
	}
	w := httptest.NewRecorder()
	s.respond(w, httptest.NewRequest("GET", "http://site.test/x", nil), ch, &result{status: 200, body: []byte("a"), custom: true, tags: []string{"b", "a"}}, false, "", 0)
	if w.Header().Get("Surrogate-Key") != "a b" || w.Header().Get("Cache-Tag") != "a,b" {
		t.Errorf("tags: %v", w.Header())
	}
}
