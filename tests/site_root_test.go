package tests

import (
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/modules/site"
)

const siteRootChannels = `
languages:
  en: {}
groups:
  web:
    site: main
    render: go
channels:
  global-en: { group: web, languages: [en], match: { prefix: /en }, root_reserved: [/legacy, /ajax/*] }
`

func TestSiteRootLevelRoutes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnvWith(t, b.open(t, clk), clk, "routes", siteRootChannels, func(r *siteRegistrar) {
			site.UseTheme(r, routesTheme())
			site.Handle(r, "GET /legacy", func(q *site.Request) (*site.Response, error) {
				return &site.Response{Template: "routes/info.html", Title: "Legacy", Data: "ch=" + q.URL("/x")}, nil
			}).Sample(func() *site.Response { return &site.Response{Template: "routes/info.html"} })
			site.Handle(r, "POST /ajax/save", func(q *site.Request) (*site.Response, error) {
				return &site.Response{JSON: map[string]any{"ok": true}}, nil
			}).Sample(func() *site.Response { return &site.Response{JSON: 1} })
		})
		if err != nil {
			t.Fatal(err)
		}
		e.place("home", "Home", nil)

		g := e.get("/legacy")
		if g.status != 200 || !strings.Contains(g.body, `Legacy|ch=/en/x|`) {
			t.Errorf("/legacy: %d %s", g.status, g.body)
		}
		if p := e.do("POST", "/ajax/save", strings.NewReader("")); p.status != 200 || !strings.Contains(p.body, `"ok":true`) {
			t.Errorf("POST /ajax/save: %d %s", p.status, p.body)
		}
		if p := e.get("/ajax/save"); p.status != 405 {
			t.Errorf("GET /ajax/save: %d", p.status)
		}
		// Unclaimed root paths stay unmatched; the prefix still serves the pages.
		if g := e.get("/other"); g.status != 404 {
			t.Errorf("/other: %d", g.status)
		}
		if g := e.get("/en"); g.status != 200 {
			t.Errorf("/en: %d", g.status)
		}
		// A root path is reserved inside the prefix as well, so the same route answers there.
		if g := e.get("/en/legacy"); g.status != 200 {
			t.Errorf("/en/legacy: %d", g.status)
		}
	})
}
