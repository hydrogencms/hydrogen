package pages

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
)

func web(name, group string, langs []string, m matcher, opts map[string]any) *channel.Channel {
	o := map[string]any{optMatch: m}
	for k, v := range opts {
		o[k] = v
	}
	return &channel.Channel{Name: name, Group: group, Languages: langs, Options: o}
}

func testIndex() *webIndex {
	return newWebIndex([]*channel.Channel{
		web("corp_en", "corp", []string{"en"}, matcher{Host: "www.acme.com", Prefix: "/en"}, map[string]any{optSite: "corporate"}),
		web("corp_de", "corp", []string{"de"}, matcher{Host: "www.acme.com", Prefix: "/de"}, map[string]any{optSite: "corporate"}),
		web("corp_dech", "corp", []string{"de-CH", "de"}, matcher{Host: "www.acme.com", Prefix: "/de-ch"}, map[string]any{optSite: "corporate"}),
		web("corp_host", "corp", []string{"en"}, matcher{Host: "www.acme.com"}, map[string]any{optSite: "corporate"}),
		web("careers", "", []string{"en"}, matcher{Host: "jobs.acme.com"}, map[string]any{optSite: "careers"}),
		web("mobile", "", []string{"en"}, matcher{Prefix: "/m", Header: &headerMatch{Name: "X-App", Value: "1"}}, nil),
		web("anyprefix", "", []string{"en"}, matcher{Prefix: "/m"}, nil),
		{Name: "ios", Languages: []string{"en"}},
	}, []string{"corporate", "careers"})
}

func TestMatchSpecificity(t *testing.T) {
	x := testIndex()
	app := http.Header{"X-App": {"1"}}
	for _, c := range []struct {
		url    string
		h      http.Header
		name   string
		path   string
		wantOK bool
	}{
		{"https://www.acme.com/en/about", nil, "corp_en", "/about", true},
		{"https://WWW.ACME.COM:8443/en/about", nil, "corp_en", "/about", true}, // host case and port ignored
		{"https://www.acme.com/en", nil, "corp_en", "/", true},
		{"https://www.acme.com/en/", nil, "corp_en", "/", true},
		{"https://www.acme.com/english", nil, "corp_host", "/english", true}, // not a segment boundary
		{"https://www.acme.com/de-ch/x", nil, "corp_dech", "/x", true},
		{"https://www.acme.com/de/x", nil, "corp_de", "/x", true},
		{"https://www.acme.com/", nil, "corp_host", "/", true},
		{"https://www.acme.com", nil, "corp_host", "/", true},
		{"https://jobs.acme.com/roles?x=1", nil, "careers", "/roles", true},
		{"https://other.com/m/page", nil, "anyprefix", "/page", true},
		{"https://other.com/m/page", app, "mobile", "/page", true}, // 2 parts beat 1
		{"/m/page", app, "mobile", "/page", true},                  // path-only URL
		{"https://other.com/x", nil, "", "", false},
		{"/en/about", nil, "", "", false}, // channels with a host never match a hostless URL
	} {
		host, p, _, err := splitURL(c.url)
		if err != nil {
			t.Fatal(err)
		}
		w, path, ok := x.match(host, p, c.h)
		if ok != c.wantOK {
			t.Errorf("%s: ok %v", c.url, ok)
			continue
		}
		if ok && (w.ch.Name != c.name || path != c.path) {
			t.Errorf("%s: got %s %q, want %s %q", c.url, w.ch.Name, path, c.name, c.path)
		}
	}
}

func TestMatchHeaderRequired(t *testing.T) {
	x := testIndex()
	w, _, ok := x.match("other.com", "/m/page", nil)
	if !ok || w.ch.Name != "anyprefix" {
		t.Errorf("without header: %v", w)
	}
	w, _, ok = x.match("other.com", "/m/page", http.Header{"X-App": {"2"}})
	if !ok || w.ch.Name != "anyprefix" {
		t.Errorf("wrong header value: %v", w)
	}
}

func TestSpecificityTieBreaks(t *testing.T) {
	x := newWebIndex([]*channel.Channel{
		web("b", "", []string{"en"}, matcher{Prefix: "/a"}, nil),
		web("a", "", []string{"en"}, matcher{Prefix: "/a/b"}, nil),
		web("c", "", []string{"en"}, matcher{Header: &headerMatch{Name: "X-A", Value: "1"}}, nil),
		web("d", "", []string{"en"}, matcher{Header: &headerMatch{Name: "X-B", Value: "1"}}, nil),
	}, []string{"main"})
	if w, rest, _ := x.match("", "/a/b/c", nil); w.ch.Name != "a" || rest != "/c" {
		t.Errorf("longest prefix: %s %s", w.ch.Name, rest)
	}
	if w, _, _ := x.match("", "/a/c", nil); w.ch.Name != "b" {
		t.Errorf("shorter prefix: %s", w.ch.Name)
	}
	h := http.Header{"X-A": {"1"}, "X-B": {"1"}}
	if w, _, _ := x.match("", "/z", h); w.ch.Name != "c" {
		t.Errorf("name order: %s", w.ch.Name)
	}
}

// The ranking compares (host, prefix, header) lexicographically before it looks at the prefix length.
func TestMatcherPrecedence(t *testing.T) {
	hdr := &headerMatch{Name: "X-A", Value: "1"}
	x := newWebIndex([]*channel.Channel{
		web("host", "", []string{"en"}, matcher{Host: "a.test"}, nil),
		web("prefix", "", []string{"en"}, matcher{Prefix: "/long/prefix"}, nil),
		web("prefix_header", "", []string{"en"}, matcher{Prefix: "/long", Header: hdr}, nil),
		web("host_prefix", "", []string{"en"}, matcher{Host: "a.test", Prefix: "/p"}, nil),
		web("header", "", []string{"en"}, matcher{Header: hdr}, nil),
		web("host_header", "", []string{"en"}, matcher{Host: "a.test", Header: hdr}, nil),
	}, []string{"main"})
	h := http.Header{"X-A": {"1"}}
	for _, c := range []struct {
		host, path string
		h          http.Header
		want       string
	}{
		{"a.test", "/long/prefix/x", nil, "host"},        // host-only beats prefix-only
		{"a.test", "/long/x", h, "host_header"},          // host+header beats host-only
		{"a.test", "/p/x", nil, "host_prefix"},           // host+prefix beats host-only
		{"a.test", "/p/x", h, "host_prefix"},             // (T,T,F) beats (T,F,T)
		{"b.test", "/long/prefix/x", h, "prefix_header"}, // prefix+header beats prefix-only, whatever the length
		{"b.test", "/z", h, "header"},
		{"b.test", "/long/prefix/x", nil, "prefix"},
	} {
		if w, _, ok := x.match(c.host, c.path, c.h); !ok || w.ch.Name != c.want {
			t.Errorf("%s%s %v: %v, want %s", c.host, c.path, c.h, w, c.want)
		}
	}
	// A host-only matcher beats a prefix+header one (no host), even when both fit.
	y := newWebIndex([]*channel.Channel{
		web("host", "", []string{"en"}, matcher{Host: "a.test"}, nil),
		web("prefix_header", "", []string{"en"}, matcher{Prefix: "/m", Header: hdr}, nil),
	}, []string{"main"})
	if w, _, _ := y.match("a.test", "/m/x", h); w.ch.Name != "host" {
		t.Errorf("host-only vs prefix+header: %s", w.ch.Name)
	}
}

func TestSplitURL(t *testing.T) {
	for _, c := range []struct {
		raw, host, path string
		bad             bool
	}{
		{"https://WWW.Acme.com./en/x", "www.acme.com", "/en/x", false}, // trailing dot
		{"https://a.test/a%2Fb", "a.test", "/a/b", true},
		{"https://a.test/a%2fb", "a.test", "/a/b", true},
		{"https://a.test/a%5Cb", "a.test", "/a\\b", false},
		{"https://a.test/%ff", "a.test", "/\xff", true},
		{"/x%E4", "", "/x\xe4", true},
		{"https://a.test", "a.test", "/", false},
	} {
		host, path, bad, err := splitURL(c.raw)
		if err != nil || host != c.host || path != c.path || bad != c.bad {
			t.Errorf("%s: %q %q %v %v", c.raw, host, path, bad, err)
		}
	}
}

func TestResolvablePath(t *testing.T) {
	for p, want := range map[string]bool{
		"/a/b": true, "/": true, "/legacy.html": true,
		"/a%2Fb": false, "/a%2fb": false, "/\xff": false, "/a\x00b": false, "/a\\b": false,
	} {
		if _, ok := resolvablePath(p); ok != want {
			t.Errorf("%q: %v", p, ok)
		}
	}
}

func TestTrimSlashAndReserved(t *testing.T) {
	for _, c := range []struct {
		in, out string
		slash   bool
	}{
		{"/", "/", false}, {"/a", "/a", false}, {"/a/", "/a", true}, {"/a//", "/a", true}, {"//", "/", false},
	} {
		if out, slash := trimSlash(c.in); out != c.out || slash != c.slash {
			t.Errorf("%q: %q %v", c.in, out, slash)
		}
	}
	r := parseReserved("/cart")
	if p, _ := trimSlash("/cart/"); !r.matches(p) {
		t.Error("a trailing slash is trimmed before matching")
	}
	if p, _ := trimSlash("/Cart"); r.matches(p) {
		t.Error("reserved paths are case-sensitive")
	}
}

func TestDecodeHostASCII(t *testing.T) {
	dir := t.TempDir()
	yaml := "m1: { host: bücher.example }\nm2: { host: Www.test }\nm3: { host: xn--bcher-kva.example }\n"
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: dir, Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Skipf("config does not accept free-form keys: %v", err)
	}
	for key, want := range map[string]string{"m1": "app.yaml:1: host \"bücher.example\" must be ASCII", "m2": "must be lower-case", "m3": ""} {
		n, err := cfg.Node(key)
		if err != nil || n == nil {
			t.Fatalf("%s: %v %v", key, n, err)
		}
		_, err = decodeMatch(n)
		switch {
		case want == "" && err != nil, want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: %v, want %q", key, err, want)
		}
	}
}

func TestReserved(t *testing.T) {
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"/cart", "/cart", true},
		{"/cart", "/cart/x", false},
		{"/cart", "/carts", false},
		{"/account/*", "/account", true},
		{"/account/*", "/account/orders/1", true},
		{"/account/*", "/accounts", false},
	} {
		if got := parseReserved(c.pattern).matches(c.path); got != c.want {
			t.Errorf("%s vs %s: %v", c.pattern, c.path, got)
		}
	}
}

func TestURLBuilding(t *testing.T) {
	site := newWebChannel(web("a", "", []string{"en"}, matcher{Host: "www.acme.com", Prefix: "/en"}, nil), "main")
	front := newWebChannel(&channel.Channel{Name: "f", Frontend: "https://front.acme.com/app/", Options: map[string]any{
		optMatch: matcher{Prefix: "/de"}}}, "main")
	bare := newWebChannel(web("b", "", []string{"en"}, matcher{Prefix: "/x"}, nil), "main")
	hostOnly := newWebChannel(web("h", "", []string{"en"}, matcher{Host: "a.test"}, nil), "main")
	for _, c := range []struct {
		w        *webChannel
		path     string
		abs, rel string
	}{
		{site, "/news/a", "https://www.acme.com/en/news/a", "/en/news/a"},
		{site, "/", "https://www.acme.com/en", "/en"},
		{front, "/news", "https://front.acme.com/app/de/news", "/de/news"},
		{bare, "/news", "/x/news", "/x/news"},
		{hostOnly, "/", "https://a.test/", "/"},
	} {
		if got := c.w.abs(c.path); got != c.abs {
			t.Errorf("abs %s: %s, want %s", c.path, got, c.abs)
		}
		if got := c.w.rel(c.path); got != c.rel {
			t.Errorf("rel %s: %s, want %s", c.path, got, c.rel)
		}
	}
	if got, ok := site.siteLink("/new"); !ok || got != "/en/new" {
		t.Errorf("siteLink: %s", got)
	}
	for _, t2 := range []string{"https://x.test/a", "http://x.test/a?b=1"} {
		if got, ok := site.siteLink(t2); !ok || got != t2 {
			t.Errorf("siteLink %s: %s %v", t2, got, ok)
		}
	}
	for _, t2 := range []string{"//x.test/a", "mailto:a@b", `/\x.test`, "/a\nb", "/a\x00", "/a b", "https://x.test/a\\b",
		"https:///a", "javascript:alert(1)", "", "a/b", "/\t/x.test"} {
		if got, ok := site.siteLink(t2); ok {
			t.Errorf("siteLink %q passed as %q", t2, got)
		}
	}
}

func TestOptionSiteDefault(t *testing.T) {
	x := testIndex()
	if w := x.of(&channel.Channel{Name: "ios"}); w.site != "corporate" {
		t.Errorf("default site: %s", w.site)
	}
	w, _, _ := x.match("jobs.acme.com", "/", nil)
	if w.site != "careers" {
		t.Errorf("site: %s", w.site)
	}
}

var routesOfPage = itemRoutes{
	"corporate": {"en": "/about", "de": "/ueber-uns"}, // no de-CH
	"careers":   {"en": "/about-careers"},
}

func alternateNames(alts []Alternate) string {
	var out []string
	for _, a := range alts {
		out = append(out, a.Channel+"/"+a.Locale+"="+a.URL)
	}
	return strings.Join(out, " ")
}

func TestAlternatesAndCanonical(t *testing.T) {
	x := testIndex()
	dech, _, _ := x.match("www.acme.com", "/de-ch/x", nil)
	peers := x.peers(dech)
	var names []string
	for _, p := range peers {
		names = append(names, p.ch.Name)
	}
	if strings.Join(names, ",") != "corp_de,corp_dech,corp_en,corp_host" {
		t.Errorf("peers: %v", names)
	}
	// dech shows the de fallback: it owns the "de" hreflang, corp_de (also de) is not listed again, and
	// corp_host (en, like corp_en) is dropped as a repeat.
	got := alternateNames(alternates(dech, peers, routesOfPage, "page", "de", "/ueber-uns"))
	want := "corp_dech/de=https://www.acme.com/de-ch/ueber-uns corp_en/en=https://www.acme.com/en/about"
	if got != want {
		t.Errorf("alternates:\n got %s\nwant %s", got, want)
	}
	// With a de-CH route, dech lists itself as de-CH and corp_de lists de; no hreflang repeats.
	r := itemRoutes{"corporate": {"en": "/about", "de": "/ueber-uns", "de-CH": "/ueber-uns-ch"}}
	got = alternateNames(alternates(dech, peers, r, "page", "de-CH", "/ueber-uns-ch"))
	want = "corp_de/de=https://www.acme.com/de/ueber-uns corp_dech/de-CH=https://www.acme.com/de-ch/ueber-uns-ch " +
		"corp_en/en=https://www.acme.com/en/about"
	if got != want {
		t.Errorf("alternates:\n got %s\nwant %s", got, want)
	}
	seen := map[string]bool{}
	for _, a := range alternates(dech, peers, r, "page", "de-CH", "/x") {
		if seen[a.Locale] {
			t.Errorf("hreflang %s repeats", a.Locale)
		}
		seen[a.Locale] = true
	}
	// dech without a de-CH route is not an alternate for de-CH, though corp_de's de route exists.
	deAlts := alternates(x.of(x.byName[0].ch), peers, routesOfPage, "page", "de", "/ueber-uns")
	if strings.Contains(alternateNames(deAlts), "corp_dech") {
		t.Errorf("corp_dech has no route in its first language: %v", deAlts)
	}
	// dech shows the de fallback: the canonical is corp_de (first language de), not the current channel.
	if c := canonicalURL(dech, peers, routesOfPage, "page", "de", "/ueber-uns"); c != "https://www.acme.com/de/ueber-uns" {
		t.Errorf("canonical: %s", c)
	}
	// corp_en shows en: its own URL wins over corp_host (also first language en).
	en, _, _ := x.match("www.acme.com", "/en/x", nil)
	if c := canonicalURL(en, x.peers(en), routesOfPage, "page", "en", "/about"); c != "https://www.acme.com/en/about" {
		t.Errorf("canonical en: %s", c)
	}
	// No peer has the locale as first language: the current URL.
	if c := canonicalURL(dech, peers[1:2], routesOfPage, "page", "de-CH", "/zzz"); c != "https://www.acme.com/de-ch/zzz" {
		t.Errorf("canonical fallback: %s", c)
	}
}

func TestAlternatesHonourCollections(t *testing.T) {
	x := newWebIndex([]*channel.Channel{
		web("a_en", "g", []string{"en"}, matcher{Host: "a.test", Prefix: "/en"}, nil),
		func() *channel.Channel {
			c := web("a_de", "g", []string{"de"}, matcher{Host: "a.test", Prefix: "/de"}, nil)
			c.Collections = []string{"article"}
			return c
		}(),
	}, []string{"main"})
	en, _, _ := x.match("a.test", "/en/x", nil)
	r := itemRoutes{"main": {"en": "/x", "de": "/y"}}
	if got := alternateNames(alternates(en, x.peers(en), r, "page", "en", "/x")); got != "a_en/en=https://a.test/en/x" {
		t.Errorf("page: %s", got)
	}
	if got := alternateNames(alternates(en, x.peers(en), r, "article", "en", "/x")); !strings.Contains(got, "a_de/de") {
		t.Errorf("article: %s", got)
	}
	if c := canonicalURL(en, x.peers(en), r, "page", "de", "/y"); c != "https://a.test/en/y" {
		t.Errorf("canonical skips a channel that disallows the type: %s", c)
	}
}

func TestLink(t *testing.T) {
	x := testIndex()
	r := itemRoutes{"corporate": {"en": "/about"}, "careers": {"en": "/jobs", "de": "/stellen"}}
	corpEn, _, _ := x.match("www.acme.com", "/en/", nil)
	careers, _, _ := x.match("jobs.acme.com", "/", nil)
	for _, c := range []struct {
		name string
		from *channel.Channel
		r    itemRoutes
		want string
	}{
		{"same channel is relative", corpEn.ch, r, "/en/about"},
		{"cross site is absolute", corpEn.ch, itemRoutes{"careers": {"en": "/jobs"}}, "https://jobs.acme.com/jobs"},
		{"careers own", careers.ch, r, "/jobs"},
		{"same group first", corpEn.ch, itemRoutes{"corporate": {"en": "/about"}, "careers": {"en": "/jobs"}}, "/en/about"},
	} {
		got, ok := x.link(c.from, "page", c.r)
		if !ok || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.name, got, ok, c.want)
		}
	}
	// A group member without its own route links to a sibling in the group before another site.
	r2 := itemRoutes{"corporate": {"de": "/ueber"}, "careers": {"en": "/jobs"}}
	if got, _ := x.link(corpEn.ch, "page", r2); got != "https://www.acme.com/de/ueber" {
		t.Errorf("group sibling: %s", got)
	}
	// An app channel links to the best web channel, preferring its languages.
	app := &channel.Channel{Name: "ios", Languages: []string{"de", "en"}}
	if got, _ := x.link(app, "page", itemRoutes{"corporate": {"en": "/about", "de": "/ueber"}}); got != "https://www.acme.com/de/ueber" {
		t.Errorf("app: %s", got)
	}
	if _, ok := x.link(app, "page", itemRoutes{}); ok {
		t.Error("no routes should not link")
	}
}

func TestLinkCollectionsAndPathOnly(t *testing.T) {
	art := web("art", "g", []string{"en"}, matcher{Host: "a.test", Prefix: "/art"}, nil)
	art.Collections = []string{"article"}
	pathOnly := web("path_only", "g", []string{"en"}, matcher{Prefix: "/p"}, nil)
	pg := web("pg", "g", []string{"en"}, matcher{Host: "a.test", Prefix: "/pg"}, nil)
	pg.Collections = []string{"page"}
	x := newWebIndex([]*channel.Channel{art, pathOnly, pg}, []string{"main"})
	r := itemRoutes{"main": {"en": "/x"}}
	app := &channel.Channel{Name: "app", Languages: []string{"en"}}
	if got, ok := x.link(app, "page", r); !ok || got != "https://a.test/pg/x" {
		t.Errorf("page: %q %v", got, ok)
	}
	if got, ok := x.link(app, "article", r); !ok || got != "https://a.test/art/x" {
		t.Errorf("article: %q %v", got, ok)
	}
	if _, ok := x.link(app, "product", r); ok {
		t.Error("no channel allows a product")
	}
	// The origin's own relative link needs the type to be allowed; a path-only channel is not a candidate
	// for others, but it links to itself.
	if got, ok := x.link(pathOnly, "page", r); !ok || got != "/p/x" {
		t.Errorf("path-only self link: %q %v", got, ok)
	}
	if got, ok := x.link(art, "page", r); !ok || got != "https://a.test/pg/x" {
		t.Errorf("a disallowed type is not linked relatively: %q %v", got, ok)
	}
	only := newWebIndex([]*channel.Channel{pathOnly}, []string{"main"})
	if _, ok := only.link(app, "page", r); ok {
		t.Error("a path-only channel cannot be linked from elsewhere")
	}
}

func TestDuplicateMatchers(t *testing.T) {
	a := web("a", "", nil, matcher{Host: "x.test", Prefix: "/en"}, nil)
	b := web("b", "", nil, matcher{Host: "x.test", Prefix: "/en"}, nil)
	c := web("c", "", nil, matcher{Host: "x.test", Prefix: "/en", Header: &headerMatch{Name: "X", Value: "1"}}, nil)
	err := duplicateMatchers([]*channel.Channel{a, b, c, {Name: "app"}})
	if err == nil || !strings.Contains(err.Error(), "channels a and b both match host=x.test prefix=/en") || strings.Count(err.Error(), "both match") != 1 {
		t.Errorf("err: %v", err)
	}
	if err := duplicateMatchers([]*channel.Channel{a, c}); err != nil {
		t.Errorf("distinct: %v", err)
	}
}

func BenchmarkMatch(b *testing.B) {
	x := testIndex()
	h := http.Header{}
	b.ReportAllocs()
	for b.Loop() {
		host, path, _, _ := splitURL("https://www.acme.com/de-ch/about/team")
		if _, _, ok := x.match(host, path, h); !ok {
			b.Fatal("no match")
		}
	}
}

func TestSimulatedHeadersDeterministic(t *testing.T) {
	q := url.Values{"header.x-a": {"low"}, "header.X-A": {"up"}, "header.X-B": {"b"}, "other": {"x"}, "header.": {"x"}}
	for range 50 {
		h := simulatedHeaders(q)
		// "header.X-A" sorts before "header.x-a", so the lower-case key wins.
		if h.Get("X-A") != "low" || h.Get("X-B") != "b" || len(h) != 2 {
			t.Fatalf("headers: %v", h)
		}
	}
}
