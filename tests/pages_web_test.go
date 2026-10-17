package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const webChannels = `
channels:
  web_en:  { languages: [en],     match: { host: a.test, prefix: /en }, frontend: "https://front.a.test", reserved: [/cart] }
  web_de:  { languages: [de, en], match: { host: a.test, prefix: /de } }
  sg:      { languages: [en],     match: { host: a.test, header: { name: x-market, value: sg } } }
  sg_deep: { languages: [en],     match: { host: a.test, prefix: /en, header: { name: x-market, value: sg } } }
  app:     { languages: [en] }
`

func (e *pagesEnv) channel(name string) *channel.Channel {
	e.t.Helper()
	ch, ok := e.app.Services().Channels.Get(name)
	if !ok {
		e.t.Fatalf("no channel %s", name)
	}
	return ch
}

func (e *pagesEnv) locate(raw string, h http.Header) *pages.Located {
	e.t.Helper()
	l, err := e.pages.Locate(ctx, raw, h)
	if err != nil {
		e.t.Fatalf("locate %s: %v", raw, err)
	}
	return l
}

func TestWebLocate(t *testing.T) {
	runPages(t, pagesOpts{channels: webChannels}, func(t *testing.T, e *pagesEnv) {
		home, homeNode := e.placed("home", "Home", nil, nil)
		sec, _ := e.placed("section", "Press", &homeNode, nil)
		e.update(sec, "de", `{"title":"Presse"}`)
		e.mustPublish(sec, "de")
		e.drain()

		l := e.locate("https://a.test/en/press", nil)
		if l.Channel != "web_en" || l.Kind != "page" || l.Locale != "en" || *l.Resolution.Item != sec {
			t.Fatalf("en: %+v", l)
		}
		if l.Canonical != "https://front.a.test/en/press" {
			t.Errorf("canonical uses the frontend: %s", l.Canonical)
		}
		if l = e.locate("https://a.test/de/presse", nil); l.Channel != "web_de" || l.Locale != "de" {
			t.Errorf("de: %+v", l)
		}
		// de falls back to en for a page without a German route.
		if l = e.locate("https://a.test/de/", nil); l.Channel != "web_de" || l.Locale != "en" || *l.Resolution.Item != home {
			t.Errorf("de root falls back to en: %+v", l)
		}

		// Reserved paths never resolve, even if a page has the path.
		e.placed("section", "Cart", &homeNode, nil)
		e.drain()
		if l = e.locate("https://a.test/en/cart", nil); l.Kind != pages.LocatedReserved || l.Resolution != nil {
			t.Errorf("reserved: %+v", l)
		}
		if l = e.locate("https://a.test/de/cart", nil); l.Kind != "page" {
			t.Errorf("reserved is per channel: %+v", l)
		}

		// Header matchers: more matcher parts win.
		sg := http.Header{"X-Market": {"sg"}}
		if l = e.locate("https://a.test/en/press", sg); l.Channel != "sg_deep" {
			t.Errorf("host+prefix+header: %+v", l)
		}
		if l = e.locate("https://a.test/press", sg); l.Channel != "sg" {
			t.Errorf("host+header: %+v", l)
		}
		if l = e.locate("https://a.test/de/presse", sg); l.Channel != "web_de" {
			t.Errorf("two parts tie: the longer prefix wins: %+v", l)
		}
		if l = e.locate("https://nowhere.test/en/press", nil); l.Kind != pages.LocatedNoChannel || l.Channel != "" {
			t.Errorf("no channel: %+v", l)
		}
		if _, err := e.pages.Locate(ctx, "http://[::1", nil); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("bad url: %v", err)
		}
	})
}

func TestWebRedirects(t *testing.T) {
	runPages(t, pagesOpts{channels: webChannels}, func(t *testing.T, e *pagesEnv) {
		_, homeNode := e.placed("home", "Home", nil, nil)
		sec, _ := e.placed("section", "Press", &homeNode, nil)
		e.drain()
		e.update(sec, "en", `{"slug":"media"}`)
		e.mustPublish(sec)
		e.drain()
		l := e.locate("https://a.test/en/press", nil)
		if l.Kind != "redirect" || l.Resolution.Status != 301 || l.Resolution.Location != "/en/media" {
			t.Errorf("history redirect: %+v", l)
		}
		if l.Canonical != "" || len(l.Alternates) != 0 {
			t.Errorf("a redirect has no canonical or alternates: %+v", l)
		}

		r := e.create("redirect", "en", `{"from":"/legacy","to":"/media"}`)
		e.mustPublish(r)
		e.drain()
		if l = e.locate("https://a.test/de/legacy", nil); l.Kind != "redirect" || l.Resolution.Location != "/de/media" {
			t.Errorf("manual redirect keeps the prefix: %+v", l)
		}
		ext := e.create("redirect", "en", `{"from":"/elsewhere","to":"https://example.com/x"}`)
		e.mustPublish(ext)
		e.drain()
		if l = e.locate("https://a.test/en/elsewhere", nil); l.Resolution.Location != "https://example.com/x" {
			t.Errorf("absolute target untouched: %+v", l)
		}
	})
}

func TestWebURLs(t *testing.T) {
	runPages(t, pagesOpts{channels: webChannels}, func(t *testing.T, e *pagesEnv) {
		_, homeNode := e.placed("home", "Home", nil, nil)
		sec, _ := e.placed("section", "Press", &homeNode, nil)
		e.drain()

		u, ok, err := e.pages.URL(ctx, e.channel("web_en"), sec, "en")
		if err != nil || !ok || u != "https://front.a.test/en/press" {
			t.Errorf("URL with frontend: %q %v %v", u, ok, err)
		}
		if u, ok, _ = e.pages.URL(ctx, e.channel("web_de"), sec, "en"); !ok || u != "https://a.test/de/press" {
			t.Errorf("URL from host: %q %v", u, ok)
		}
		if _, ok, _ = e.pages.URL(ctx, e.channel("web_de"), sec, "de"); ok {
			t.Error("no German route")
		}
		if u, ok, _ = e.pages.URL(ctx, e.channel("app"), sec, "en"); !ok || u != "/press" {
			t.Errorf("channel without host or prefix is path-only: %q %v", u, ok)
		}
		if u, err = e.pages.LinkURL(ctx, e.channel("web_en"), sec); err != nil || u != "/en/press" {
			t.Errorf("relative link: %q %v", u, err)
		}
		if u, err = e.pages.LinkURL(ctx, e.channel("app"), sec); err != nil || u != "https://a.test/press" {
			t.Errorf("link from the app (best language, then name): %q %v", u, err)
		}
		if _, err = e.pages.LinkURL(ctx, e.channel("web_en"), ids.UUID{9}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown item: %v", err)
		}
		ch, path, ok := e.pages.Match("https://a.test/de-ch/x", nil)
		if ok {
			t.Errorf("prefix on a segment boundary: %v %s", ch, path)
		}
		if ch, path, ok = e.pages.Match("https://a.test/de/x/y", nil); !ok || ch.Name != "web_de" || path != "/x/y" {
			t.Errorf("match: %v %s %v", ch, path, ok)
		}
	})
}

func BenchmarkWebLocate(b *testing.B) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			clk := newClock()
			e := newPagesEnv(b, be.open(b, clk), clk, pagesOpts{channels: webChannels})
			bigTree(b, e)
			b.ResetTimer()
			for b.Loop() {
				l, err := e.pages.Locate(ctx, "https://a.test/de/oben/bereich-3/seite-7", nil)
				if err != nil || l.Kind != "page" || len(l.Alternates) != 2 || !strings.HasPrefix(l.Canonical, "https://") {
					b.Fatalf("locate: %+v %v", l, err)
				}
			}
		})
	}
}

// countingDB counts the statements that go through DB.Read, which is where Locate reads.
type countingDB struct {
	store.DB
	n atomic.Int64
}

func (c *countingDB) Read() store.SQL { return &countingSQL{SQL: c.DB.Read(), n: &c.n} }

type countingSQL struct {
	store.SQL
	n *atomic.Int64
}

func (c *countingSQL) Query(ctx context.Context, q string, args ...any) (store.Rows, error) {
	c.n.Add(1)
	return c.SQL.Query(ctx, q, args...)
}

func (c *countingSQL) QueryRow(ctx context.Context, q string, args ...any) store.Row {
	c.n.Add(1)
	return c.SQL.QueryRow(ctx, q, args...)
}

const collectionChannels = `
languages:
  en: {}
  de: {}
  de-CH: { inherits: de }
groups:
  corp: {}
channels:
  c_en:     { group: corp, languages: [en],         collections: [home, section], match: { host: c.test, prefix: /en }, reserved: [/cart] }
  c_de:     { group: corp, languages: [de],         match: { host: c.test, prefix: /de } }
  c_dech:   { group: corp, languages: [de-CH, de],  match: { host: c.test, prefix: /de-ch } }
  c_all:    { group: corp, languages: [en, de],     match: { host: c.test, prefix: /all } }
  a_pathy:  { languages: [en],                      match: { prefix: /pathy } }
  ios:      { languages: [en] }
`

func runCounting(t *testing.T, fn func(t *testing.T, e *pagesEnv, n func() int64)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := &countingDB{DB: b.open(t, clk)}
		e := newPagesEnv(t, db, clk, pagesOpts{channels: collectionChannels})
		fn(t, e, db.n.Load)
	})
}

func alternateList(l *pages.Located) string {
	var out []string
	for _, a := range l.Alternates {
		out = append(out, a.Channel+"/"+a.Locale)
	}
	return strings.Join(out, " ")
}

func TestWebCollectionsAndShapes(t *testing.T) {
	runCounting(t, func(t *testing.T, e *pagesEnv, n func() int64) {
		_, homeNode := e.placed("home", "Home", nil, nil)
		sec, secNode := e.placed("section", "Press", &homeNode, nil)
		e.update(sec, "de", `{"title":"Presse"}`)
		e.mustPublish(sec, "de")
		e.update(sec, "de-CH", `{"title":"Presse CH"}`)
		e.mustPublish(sec, "de-CH")
		team, _ := e.placed("landing", "Team", &secNode, nil)
		e.drain()

		count := func(raw string, h http.Header) (*pages.Located, int64) {
			before := n()
			l := e.locate(raw, h)
			return l, n() - before
		}

		// Collections: c_en does not serve landing pages, c_all does.
		if l, _ := count("https://c.test/en/press", nil); l.Kind != "page" || *l.Resolution.Item != sec {
			t.Fatalf("section in c_en: %+v", l)
		}
		l, q := count("https://c.test/en/press/team", nil)
		if l.Kind != pages.ResolvedNotFound || l.Channel != "c_en" {
			t.Errorf("landing in c_en: %+v", l)
		}
		if q != 1 {
			t.Errorf("a miss costs %d queries, want 1", q)
		}
		l, q = count("https://c.test/all/press/team", nil)
		if l.Kind != "page" || *l.Resolution.Item != team || l.Locale != "en" {
			t.Fatalf("landing in c_all: %+v", l)
		}
		if q != 2 {
			t.Errorf("a page hit costs %d queries, want 2", q)
		}
		if got := alternateList(l); got != "c_all/en" {
			t.Errorf("alternates of a landing page (c_en disallows it): %s", got)
		}
		if len(l.Resolution.Breadcrumbs) != 3 {
			t.Errorf("breadcrumbs: %+v", l.Resolution.Breadcrumbs)
		}

		// hreflang values never repeat: c_all and c_en are both English, c_dech shows de-CH.
		l, q = count("https://c.test/de-ch/presse-ch", nil)
		if l.Channel != "c_dech" || l.Locale != "de-CH" || q != 2 {
			t.Fatalf("de-CH: %+v (%d queries)", l, q)
		}
		if got := alternateList(l); got != "c_all/en c_de/de c_dech/de-CH" && got != "c_de/de c_dech/de-CH c_en/en" {
			t.Errorf("alternates: %s", got)
		}
		seen := map[string]bool{}
		for _, a := range l.Alternates {
			if seen[a.Locale] {
				t.Errorf("hreflang %s repeats: %v", a.Locale, l.Alternates)
			}
			seen[a.Locale] = true
		}
		// A page without a de-CH route: c_dech is no alternate (and shows the de fallback).
		l, _ = count("https://c.test/de-ch/presse", nil)
		if l.Locale != "de" || strings.Contains(alternateList(l), "c_dech/de-CH") {
			t.Errorf("de fallback: %s %v", l.Locale, l.Alternates)
		}
		if l.Canonical != "https://c.test/de/presse" {
			t.Errorf("canonical of the fallback: %s", l.Canonical)
		}

		// History redirects: the target must be in the channel's collections.
		e.update(team, "en", `{"slug":"crew"}`)
		e.mustPublish(team)
		e.drain()
		l, q = count("https://c.test/all/press/team", nil)
		if l.Kind != "redirect" || l.Resolution.Location != "/all/press/crew" || q != 1 {
			t.Errorf("history redirect: %+v (%d queries)", l.Resolution, q)
		}
		if l, _ = count("https://c.test/en/press/team", nil); l.Kind != pages.ResolvedNotFound {
			t.Errorf("history redirect to a disallowed type: %+v", l)
		}

		// Trailing slash: a 308 to the slashless path when that resolves, else not found.
		l, q = count("https://c.test/en/press/", nil)
		if l.Kind != "redirect" || l.Resolution.Status != 308 || l.Resolution.Location != "/en/press" || q != 1 {
			t.Errorf("trailing slash: %+v (%d queries)", l, q)
		}
		if l, _ = count("https://c.test/en/nope/", nil); l.Kind != pages.ResolvedNotFound {
			t.Errorf("trailing slash on nothing: %+v", l)
		}
		if l, _ = count("https://c.test/en/", nil); l.Kind != "page" || l.Resolution.Status != 200 {
			t.Errorf("the root is not redirected: %+v", l)
		}
		if l, _ = count("https://c.test/en/press//", nil); l.Kind != "redirect" || l.Resolution.Location != "/en/press" {
			t.Errorf("double trailing slash: %+v", l)
		}

		// Reserved paths ignore a trailing slash and are case-sensitive.
		if l, _ = count("https://c.test/en/cart/", nil); l.Kind != pages.LocatedReserved {
			t.Errorf("reserved with a slash: %+v", l)
		}
		if l, _ = count("https://c.test/en/Cart", nil); l.Kind != pages.ResolvedNotFound {
			t.Errorf("reserved is case-sensitive: %+v", l)
		}

		// Hostile paths are not found, never an error.
		for _, raw := range []string{"https://c.test/en/press%2Ftx", "https://c.test/en/press%2ftx", "https://c.test/en/%ff",
			"https://c.test/en/%00", "https://c.test/en/p%C3%28", "https://c.test/en/..%2F..%2Fetc"} {
			l, err := e.pages.Locate(ctx, raw, nil)
			if err != nil || l.Kind != pages.ResolvedNotFound || l.Channel != "c_en" {
				t.Errorf("%s: %+v %v", raw, l, err)
			}
		}
		if _, err := e.pages.Resolve(ctx, "main", "en", "/a%2Fb"); err != nil {
			t.Errorf("Resolve with an encoded slash: %v", err)
		}
		if r := e.resolve("en", "/\xff"); r.Kind != pages.ResolvedNotFound {
			t.Errorf("Resolve with invalid UTF-8: %+v", r)
		}

		// A trailing dot on the host is ignored.
		if l, _ = count("https://C.TEST./en/press", nil); l.Channel != "c_en" || l.Kind != "page" {
			t.Errorf("trailing dot host: %+v", l)
		}

		// A request that matches no channel or is reserved costs nothing.
		if _, q = count("https://nowhere.test/x", nil); q != 0 {
			t.Errorf("no channel: %d queries", q)
		}
		if _, q = count("https://c.test/en/cart", nil); q != 0 {
			t.Errorf("reserved: %d queries", q)
		}

		// URL: language and collection checks.
		if u, ok, err := e.pages.URL(ctx, e.channel("c_en"), team, "en"); err != nil || ok {
			t.Errorf("URL of a disallowed type: %q %v %v", u, ok, err)
		}
		if u, ok, _ := e.pages.URL(ctx, e.channel("c_all"), team, "en"); !ok || u != "https://c.test/all/press/crew" {
			t.Errorf("URL in c_all: %q %v", u, ok)
		}
		if _, ok, _ := e.pages.URL(ctx, e.channel("c_en"), sec, "de"); ok {
			t.Error("URL for a locale outside the channel's languages")
		}
		if u, ok, _ := e.pages.URL(ctx, e.channel("c_dech"), sec, "de-CH"); !ok || u != "https://c.test/de-ch/presse-ch" {
			t.Errorf("URL de-CH: %q %v", u, ok)
		}

		// LinkURL: c_en cannot link a landing page itself, so the group's best channel does; a path-only
		// channel is never a target for other channels.
		if u, err := e.pages.LinkURL(ctx, e.channel("c_en"), team); err != nil || u != "https://c.test/all/press/crew" {
			t.Errorf("link to a disallowed type: %q %v", u, err)
		}
		if u, err := e.pages.LinkURL(ctx, e.channel("ios"), sec); err != nil || u != "https://c.test/all/press" {
			t.Errorf("link from an app skips the path-only channel: %q %v", u, err)
		}
		if u, err := e.pages.LinkURL(ctx, e.channel("a_pathy"), sec); err != nil || u != "/pathy/press" {
			t.Errorf("a path-only channel links to itself: %q %v", u, err)
		}
	})
}

func TestWebManualRedirectTargets(t *testing.T) {
	runCounting(t, func(t *testing.T, e *pagesEnv, n func() int64) {
		_, homeNode := e.placed("home", "Home", nil, nil)
		sec, secNode := e.placed("section", "Press", &homeNode, nil)
		team, _ := e.placed("landing", "Team", &secNode, nil)
		e.drain()
		for _, c := range []struct{ from, to string }{
			{"/to-team", "item:" + team.String()}, {"/to-press", "item:" + sec.String()},
			{"/evil1", `/\evil.test`}, {"/evil2", "//evil.test/x"}, {"/ok", "https://example.com/x"},
		} {
			r := e.create("redirect", "en", fmt.Sprintf(`{"from":%q,"to":%q}`, c.from, c.to))
			if err := e.publish(r); err != nil {
				t.Logf("publish %s: %v (refused at the source)", c.to, err)
			}
		}
		e.drain()
		if l := e.locate("https://c.test/en/to-team", nil); l.Kind != pages.ResolvedNotFound {
			t.Errorf("manual redirect to a disallowed type: %+v", l)
		}
		if l := e.locate("https://c.test/all/to-team", nil); l.Kind != "redirect" || l.Resolution.Location != "/all/press/team" {
			t.Errorf("manual redirect to an item: %+v", l)
		}
		if l := e.locate("https://c.test/en/to-press", nil); l.Kind != "redirect" || l.Resolution.Location != "/en/press" {
			t.Errorf("manual redirect to a section: %+v", l)
		}
		if l := e.locate("https://c.test/en/ok", nil); l.Kind != "redirect" || l.Resolution.Location != "https://example.com/x" {
			t.Errorf("absolute target: %+v", l)
		}
		for _, p := range []string{"evil1", "evil2"} {
			if l := e.locate("https://c.test/en/"+p, nil); l.Kind == "redirect" {
				t.Errorf("%s: unsafe target handed out: %+v", p, l.Resolution)
			}
		}
	})
}

func BenchmarkWebLocateCollections(b *testing.B) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			clk := newClock()
			e := newPagesEnv(b, be.open(b, clk), clk, pagesOpts{channels: webChannels})
			bigTree(b, e)
			b.ResetTimer()
			for b.Loop() {
				if l, err := e.pages.Locate(ctx, "https://a.test/de/oben/bereich-3/nirgends", nil); err != nil || l.Kind != "not_found" {
					b.Fatalf("locate: %+v %v", l, err)
				}
			}
		})
	}
}
