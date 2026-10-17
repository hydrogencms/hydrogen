package tests

import (
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/overlays"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const ovPagesChannels = `
languages:
  en: {}
  de: {}
  de-CH: { inherits: de }
  gsw: { inherits: de-CH }
channels:
  web_de:   { languages: [de, en],        match: { host: a.test, prefix: /de } }
  web_dech: { languages: [de-CH, de, en], match: { host: a.test, prefix: /de-ch } }
  web_gsw:  { languages: [gsw, de-CH, de], match: { host: a.test, prefix: /gsw } }
  web_en:   { languages: [en],            match: { host: a.test, prefix: /en } }
`

func ovPagesOpts(extra ...kernel.Module) pagesOpts {
	return pagesOpts{channels: ovPagesChannels, extra: append([]kernel.Module{overlays.Module()}, extra...)}
}

// placedDe creates a page in de (the main locale), publishes it and places it under parent.
func (e *pagesEnv) placedDe(typ, title string, parent *ids.UUID) (ids.UUID, ids.UUID) {
	e.t.Helper()
	item := e.create(typ, "de", `{"title":"`+title+`"}`)
	e.mustPublish(item)
	n, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: parent})
	if err != nil {
		e.t.Fatalf("place %s: %v", title, err)
	}
	e.drain()
	return item, n.ID
}

func (e *pagesEnv) routeLocales(item ids.UUID) []string {
	e.t.Helper()
	rs, err := e.pages.Paths(ctx, item)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, r := range rs {
		if r.State == pages.StateCurrent && !slices.Contains(out, r.Locale) {
			out = append(out, r.Locale)
		}
	}
	slices.Sort(out)
	return out
}

// An overlay locale is routed without a document of its own: its inherited slug gives the parent's path.
func TestOverlayPagesDerivedRoutes(t *testing.T) {
	runPages(t, ovPagesOpts(), func(t *testing.T, e *pagesEnv) {
		_, home := e.placedDe("home", "Start", nil)
		sec, _ := e.placedDe("section", "Presse", &home)

		if got := e.routeLocales(sec); !slices.Equal(got, []string{"de", "de-CH", "gsw"}) {
			t.Fatalf("routes in locales %v, want de, de-CH and gsw (the chain)", got)
		}
		e.wantPage("de", "/presse", sec)
		e.wantPage("de-CH", "/presse", sec)
		e.wantPage("gsw", "/presse", sec)
		e.wantNotFound("en", "/presse")

		// The channel under the overlay prefix serves the derived locale, with hreflang to the parent.
		l := e.locate("https://a.test/de-ch/presse", nil)
		if l.Channel != "web_dech" || l.Kind != "page" || l.Locale != "de-CH" || *l.Resolution.Item != sec {
			t.Fatalf("de-ch: %+v", l)
		}
		if l.Canonical != "https://a.test/de-ch/presse" {
			t.Errorf("canonical: %s", l.Canonical)
		}
		urls := map[string]string{}
		for _, a := range l.Alternates {
			urls[a.Channel+"/"+a.Locale] = a.URL
		}
		for k, want := range map[string]string{"web_dech/de-CH": "https://a.test/de-ch/presse",
			"web_de/de": "https://a.test/de/presse", "web_gsw/gsw": "https://a.test/gsw/presse"} {
			if urls[k] != want {
				t.Errorf("alternate %s = %q, want %q (all: %v)", k, urls[k], want, urls)
			}
		}
		if l = e.locate("https://a.test/gsw/presse", nil); l.Channel != "web_gsw" || l.Locale != "gsw" {
			t.Errorf("gsw: %+v", l)
		}
		if l = e.locate("https://a.test/de-ch/", nil); l.Locale != "de-CH" || *l.Resolution.Item != e.rootItem(t, home) {
			t.Errorf("root: %+v", l)
		}
	})
}

func (e *pagesEnv) rootItem(t *testing.T, node ids.UUID) ids.UUID {
	t.Helper()
	n, err := e.pages.Node(ctx, node)
	if err != nil || n.Item == nil {
		t.Fatalf("node %s: %v", node, err)
	}
	return *n.Item
}

// A slug override gives the overlay locale its own route and history; the parent's slug changes reach the
// locales that inherit the slug and leave overridden ones alone.
func TestOverlayPagesSlugOverridesAndParentPublish(t *testing.T) {
	runPages(t, ovPagesOpts(), func(t *testing.T, e *pagesEnv) {
		_, home := e.placedDe("home", "Start", nil)
		press, pressN := e.placedDe("section", "Presse", &home)
		team, _ := e.placedDe("landing", "Team", &pressN)
		news, _ := e.placedDe("section", "Aktuell", &home)
		e.wantPage("de-CH", "/presse/team", team)

		// de-CH overrides the slug of Presse: its own path and history, and the children follow.
		e.update(press, "de-CH", `{"slug":"druck"}`)
		e.mustPublish(press, "de-CH")
		e.drain()
		e.wantPage("de-CH", "/druck", press)
		e.wantPage("de-CH", "/druck/team", team)
		e.wantRedirect("de-CH", "/presse", "/druck", 301)
		e.wantPage("de", "/presse", press)
		e.wantPage("de", "/presse/team", team)
		e.wantPage("gsw", "/druck", press) // gsw inherits de-CH's override
		e.wantNotFound("de-CH", "/druck/nope")

		// Publishing de renames an inherited slug in every descendant locale, with history in each.
		e.update(news, "de", `{"slug":"neuigkeiten"}`)
		e.mustPublish(news, "de")
		e.drain()
		for _, loc := range []string{"de", "de-CH", "gsw"} {
			e.wantPage(loc, "/neuigkeiten", news)
			e.wantRedirect(loc, "/aktuell", "/neuigkeiten", 301)
		}

		// de's new slug for Presse does not touch de-CH, which overrides it, but reaches nothing it has not.
		e.update(press, "de", `{"slug":"presse-neu"}`)
		e.mustPublish(press, "de")
		e.drain()
		e.wantPage("de", "/presse-neu/team", team)
		e.wantPage("de-CH", "/druck/team", team)
		e.wantRedirect("de", "/presse", "/presse-neu", 301)

		// Removing the override (null) inherits the parent's slug again.
		e.update(press, "de-CH", `{"slug":null}`)
		e.mustPublish(press, "de-CH")
		e.drain()
		e.wantPage("de-CH", "/presse-neu", press)
		e.wantPage("de-CH", "/presse-neu/team", team)
		e.wantRedirect("de-CH", "/druck", "/presse-neu", 301)
		e.wantPage("gsw", "/presse-neu", press)
	})
}

// Navigation labels of an overlay locale come from the resolved names.
func TestOverlayPagesTreeLabels(t *testing.T) {
	runPages(t, ovPagesOpts(), func(t *testing.T, e *pagesEnv) {
		_, home := e.placedDe("home", "Start", nil)
		press, _ := e.placedDe("section", "Presse", &home)
		names := func(locale string) map[string]string {
			ns, err := e.pages.Tree(ctx, "main", locale, &home, 1)
			if err != nil {
				t.Fatal(err)
			}
			out := map[string]string{}
			for _, n := range ns {
				out[n.Path] = n.Name
			}
			return out
		}
		if got := names("de-CH"); got["/presse"] != "Presse" {
			t.Fatalf("inherited label: %v", got)
		}
		e.update(press, "de-CH", `{"title":"Presse CH","slug":"presse"}`)
		e.mustPublish(press, "de-CH")
		e.drain()
		if got := names("de-CH"); got["/presse"] != "Presse CH" {
			t.Errorf("de-CH label: %v", got)
		}
		if got := names("gsw"); got["/presse"] != "Presse CH" {
			t.Errorf("gsw inherits de-CH's name: %v", got)
		}
		if got := names("de"); got["/presse"] != "Presse" {
			t.Errorf("de label unchanged: %v", got)
		}
		// de renames, de-CH keeps its own.
		e.update(press, "de", `{"title":"Medien","slug":"presse"}`)
		e.mustPublish(press, "de")
		e.drain()
		if got := names("de"); got["/presse"] != "Medien" {
			t.Errorf("de label: %v", got)
		}
		if got := names("de-CH"); got["/presse"] != "Presse CH" {
			t.Errorf("de-CH keeps its override: %v", got)
		}
	})
}

// Mounted collection items get a route in the overlay locale from their resolved slug.
func TestOverlayPagesMountedItems(t *testing.T) {
	runPages(t, ovPagesOpts(), func(t *testing.T, e *pagesEnv) {
		_, home := e.placedDe("home", "Start", nil)
		if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: home, Spec: pages.MountSpec{
			Segment: "news", Collection: "article", Route: "{slug}"}}); err != nil {
			t.Fatal(err)
		}
		a := e.create("article", "de", `{"title":"Hallo Welt"}`)
		e.mustPublish(a)
		e.drain()
		for _, loc := range []string{"de", "de-CH", "gsw"} {
			if r := e.resolve(loc, "/news/hallo-welt"); r.Kind != pages.ResolvedItem || *r.Item != a {
				t.Errorf("%s: %+v", loc, r)
			}
		}
		l := e.locate("https://a.test/de-ch/news/hallo-welt", nil)
		if l.Channel != "web_dech" || l.Kind != "item" || l.Locale != "de-CH" {
			t.Fatalf("located: %+v", l)
		}

		e.update(a, "de-CH", `{"slug":"hoi-zaeme"}`)
		e.mustPublish(a, "de-CH")
		e.drain()
		if r := e.resolve("de-CH", "/news/hoi-zaeme"); r.Kind != pages.ResolvedItem || *r.Item != a {
			t.Errorf("override: %+v", r)
		}
		e.wantRedirect("de-CH", "/news/hallo-welt", "/news/hoi-zaeme", 301)
		if r := e.resolve("de", "/news/hallo-welt"); r.Kind != pages.ResolvedItem {
			t.Errorf("de: %+v", r)
		}

		// Unpublishing drops the routes of every locale, derived ones included.
		if _, err := e.c.Unpublish(ctx, a); err != nil {
			t.Fatal(err)
		}
		e.drain()
		if got := e.routeLocales(a); len(got) != 0 {
			t.Errorf("routes after unpublish: %v", got)
		}
	})
}

// Moving a page recomputes the derived locales' paths too.
func TestOverlayPagesMove(t *testing.T) {
	runPages(t, ovPagesOpts(), func(t *testing.T, e *pagesEnv) {
		_, home := e.placedDe("home", "Start", nil)
		a, aN := e.placedDe("section", "Alpha", &home)
		_, bN := e.placedDe("section", "Beta", &home)
		e.wantPage("de-CH", "/alpha", a)
		if _, err := e.pages.Update(ctx, aN, pages.NodeUpdate{Move: &pages.Move{Parent: bN}}); err != nil {
			t.Fatal(err)
		}
		e.drain()
		e.wantPage("de-CH", "/beta/alpha", a)
		e.wantRedirect("de-CH", "/alpha", "/beta/alpha", 301)
	})
}
