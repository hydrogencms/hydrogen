package tests

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/site"
)

func (e *siteEnv) publishJSON(typ, js string) ids.UUID {
	e.t.Helper()
	e.clk.Advance(1e9)
	v, err := e.c.Create(ctx, content.CreateInput{Type: typ, Locale: "en", Fields: doc(e.t, js)})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.Publish(ctx, v.ID, content.PublishOptions{Locales: []string{"en"}}); err != nil {
		e.t.Fatal(err)
	}
	return v.ID
}

func (e *siteEnv) wantRedirect(path string, status int, location string) {
	e.t.Helper()
	if r := e.get(path); r.status != status || r.header.Get("Location") != location {
		e.t.Errorf("GET %s = %d %q, want %d %q", path, r.status, r.header.Get("Location"), status, location)
	}
}

func TestSiteNotFoundHook(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnv(t, b.open(t, clk), clk, "routes", func(r *siteRegistrar) {
			site.UseTheme(r, routesTheme())
			site.Handle(r, "GET /search", func(*site.Request) (*site.Response, error) {
				return &site.Response{Template: "routes/info.html", Data: "search"}, nil
			})
			site.NotFound(r, func(q *site.Request) (*site.Response, error) {
				switch {
				case q.Path == "/old-about":
					return &site.Response{Redirect: q.URL("/about")}, nil
				case strings.HasPrefix(q.Path, "/legacy/"):
					return &site.Response{Redirect: "/en/new", Status: http.StatusMovedPermanently}, nil
				case q.Path == "/gone":
					return &site.Response{Template: "routes/info.html", Title: "Gone", Data: "gone", Status: http.StatusGone}, nil
				case q.Path == "/soft":
					return &site.Response{Template: "routes/info.html", Title: "Soft", Data: "soft"}, nil
				case q.Path == "/boom":
					return nil, errors.New("boom")
				case q.Path == "/__hydrogen/404":
					return &site.Response{Redirect: "/hook"}, nil
				}
				return nil, site.ErrNotFound
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, root := e.place("home", "Home", nil)
		e.place("section", "About", &root)

		e.wantRedirect("/en/old-about", http.StatusFound, "/en/about")
		e.wantRedirect("/en/legacy/x/y", http.StatusMovedPermanently, "/en/new")
		r := e.get("/en/old-about")
		if r.header.Get("Cache-Control") != "no-store" {
			t.Errorf("hook answers are not cached: %v", r.header)
		}

		r = e.get("/en/gone")
		if r.status != http.StatusGone || !strings.Contains(r.body, `data-status="410"`) || !strings.Contains(r.body, "gone") {
			t.Errorf("custom status: %d %s", r.status, r.body)
		}
		if r = e.get("/en/soft"); r.status != http.StatusNotFound || !strings.Contains(r.body, "soft") {
			t.Errorf("the status defaults to 404: %d %s", r.status, r.body)
		}
		if r = e.get("/en/boom"); r.status != http.StatusInternalServerError {
			t.Errorf("hook error: %d", r.status)
		}
		// ErrNotFound is the ordinary 404 page; pages, custom routes and the export's 404 file never reach the hook.
		if r = e.get("/en/nothing"); r.status != http.StatusNotFound || !strings.Contains(r.body, "<html") {
			t.Errorf("default 404: %d %s", r.status, r.body)
		}
		if r = e.get("/en/about"); r.status != http.StatusOK {
			t.Errorf("page: %d", r.status)
		}
		if r = e.get("/en/search"); r.status != http.StatusOK || !strings.Contains(r.body, "search") {
			t.Errorf("custom route: %d", r.status)
		}
		if r = e.get("/en/__hydrogen/404"); r.status != http.StatusNotFound {
			t.Errorf("export 404 file: %d %q", r.status, r.header.Get("Location"))
		}
		// A listing page past the end is a miss too.
		if r = e.get("/en/posts/page/99"); r.status != http.StatusNotFound {
			t.Errorf("listing miss: %d", r.status)
		}
	})
}

func TestSiteNotFoundHookTwice(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		_, err := newSiteEnv(t, b.open(t, clk), clk, "starter", func(r *siteRegistrar) {
			site.NotFound(r, func(*site.Request) (*site.Response, error) { return nil, nil })
			site.NotFound(r, func(*site.Request) (*site.Response, error) { return nil, nil })
		})
		if err == nil || !strings.Contains(err.Error(), "twice") {
			t.Fatalf("err = %v", err)
		}
	})
}

const siteRootChannels = `
languages:
  en: {}
groups:
  web:
    site: main
    render: go
    reserved: ["/", /raw/*, /search, /account/*, /tags/*, /comp, /contact, /small, /open]
channels:
  en: { group: web, languages: [en], match: { prefix: /en } }
`

func TestSiteRootCustomRoute(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnvWith(t, b.open(t, clk), clk, "routes", siteSchema, siteRootChannels, func(r *siteRegistrar) {
			site.UseTheme(r, routesTheme())
			site.Handle(r, "GET /{$}", func(*site.Request) (*site.Response, error) {
				return &site.Response{Template: "routes/info.html", Title: "Landing", Data: "root"}, nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, root := e.place("home", "Home", nil)
		e.place("section", "About", &root)

		if r := e.get("/en"); r.status != http.StatusOK || !strings.Contains(r.body, "Landing|root") {
			t.Errorf("custom root: %d %s", r.status, r.body)
		}
		if r := e.get("/en/about"); r.status != http.StatusOK || strings.Contains(r.body, "Landing|root") {
			t.Errorf("pages below the root: %d", r.status)
		}
		if l, err := e.pages.Locate(ctx, "https://x.test/en", nil); err != nil || l.Kind != pages.LocatedReserved {
			t.Errorf("delivery sees the root as reserved: %+v %v", l, err)
		}
	})
}

func TestSiteRootCustomRouteNeedsReservedRoot(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		for name, pattern := range map[string]string{"root without reserving it": "GET /{$}", "bare slash": "GET /"} {
			channels := siteChannels
			if name == "bare slash" {
				channels = siteRootChannels
			}
			_, err := newSiteEnvWith(t, b.open(t, clk), clk, "starter", siteSchema, channels, func(r *siteRegistrar) {
				site.Handle(r, pattern, func(*site.Request) (*site.Response, error) { return nil, nil })
			})
			if err == nil || !strings.Contains(err.Error(), "not on a reserved path") {
				t.Errorf("%s: err = %v", name, err)
			}
		}
	})
}

func TestSiteForcedAndMountRedirects(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := siteEnvWithLink(t, b.open(t, clk), clk)
		if err != nil {
			t.Fatal(err)
		}
		_, root := e.place("home", "Home", nil)
		_, aboutN := e.place("section", "About", &root)
		e.place("section", "Team", &aboutN)

		// The root redirects although the tree has a root page, and a retired page although its path is taken.
		e.publishJSON("redirect", `{"from":"/","to":"/about","force":true,"statusCode":"302"}`)
		e.publishJSON("redirect", `{"from":"/about","to":"https://example.com/about","force":true,"statusCode":"301"}`)
		e.drain()
		e.wantRedirect("/en", http.StatusFound, "/en/about")
		e.wantRedirect("/en/about", http.StatusMovedPermanently, "https://example.com/about")
		if r := e.get("/en/about/team"); r.status != http.StatusOK {
			t.Errorf("pages below a redirected page: %d", r.status)
		}

		// A mounted item with a redirect field.
		if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: root, Spec: pages.MountSpec{Segment: "news", Collection: "article",
			Route: "{slug}", RedirectField: "externalUrl"}}); err != nil {
			t.Fatal(err)
		}
		e.publishJSON("article", `{"title":"Out","externalUrl":"https://example.com/out"}`)
		e.publishJSON("article", `{"title":"In"}`)
		e.drain()
		e.wantRedirect("/en/news/out", http.StatusFound, "https://example.com/out")
	})
}

func siteEnvWithLink(t testing.TB, db store.DB, clk *clock.Manual) (*siteEnv, error) {
	return newSiteEnvWith(t, db, clk, "starter", siteSchema+"      externalUrl: { type: string }\n", siteChannels, func(*siteRegistrar) {})
}

func (e *siteEnv) drain() {
	e.t.Helper()
	for range 5 { // a delivery can enqueue another (page.changed → cache.purge)
		n, err := e.app.Services().Outbox.Drain(ctx)
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}
