package tests

import (
	"errors"
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func TestPagesForcedRedirects(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		home, root := e.placed("home", "Home", nil, nil)
		about, _ := e.placed("section", "About us", &root, nil)
		e.wantPage("en", "/", home)

		// Without force a page's path is taken, the root included.
		if _, err := e.redirect(`{"from":"/","to":"/about-us"}`); !slices.Equal(validation(t, err), []string{"from:slug_taken"}) {
			t.Errorf("redirect over the root page: %v", err)
		}

		// A forced redirect wins over the page at its path; the page keeps its route.
		rootRedirect, err := e.redirect(`{"from":"/","to":"/about-us","statusCode":"302","force":true}`)
		if err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/", "/about-us", 302)
		e.wantRedirect("de", "/", "/about-us", 302)
		e.wantRoutes(home, "en current /")
		e.wantRoutes(rootRedirect, " manual /")

		// Locale-scoped, over a page; a second forced redirect on the key is refused.
		onPage, err := e.redirect(`{"from":"/about-us","to":"/elsewhere","force":true,"forLocale":"en"}`)
		if err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/about-us", "/elsewhere", 301)
		e.wantRoutes(about, "en current /about-us")
		if _, err := e.redirect(`{"from":"/about-us","to":"/other","force":true,"forLocale":"en"}`); !slices.Equal(validation(t, err), []string{"from:slug_taken"}) {
			t.Errorf("two forced redirects on a key: %v", err)
		}

		// Forced redirects take part in loop detection.
		if _, err := e.redirect(`{"from":"/elsewhere","to":"/","force":true}`); !slices.Equal(validation(t, err), []string{"to:redirect_loop"}) {
			t.Errorf("forced loop: %v", err)
		}

		// The route listing carries the flag.
		all, err := e.pages.Redirects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var forced []string
		for _, r := range all {
			if r.Force {
				forced = append(forced, r.Locale+" "+r.Path+" "+r.Target)
			}
		}
		if !slices.Equal(forced, []string{" / /about-us", "en /about-us /elsewhere"}) {
			t.Errorf("forced redirects = %q", forced)
		}

		// Dropping force moves the redirect to the route table, where the page's path refuses it...
		e.update(onPage, "en", `{"force":false}`)
		if got := validation(t, e.publish(onPage)); !slices.Equal(got, []string{"from:slug_taken"}) {
			t.Errorf("unforced over a page: %v", got)
		}
		e.wantRedirect("en", "/about-us", "/elsewhere", 301)
		// ...and on a free path it is an ordinary redirect.
		e.update(onPage, "en", `{"from":"/free"}`)
		e.mustPublish(onPage)
		e.wantPage("en", "/about-us", about)
		e.wantRedirect("en", "/free", "/elsewhere", 301)
		e.wantRoutes(onPage, "en manual /free")

		// Deleting the redirect item gives the page its path back.
		if err := e.c.Delete(ctx, rootRedirect); err != nil {
			t.Fatal(err)
		}
		e.wantPage("en", "/", home)
		if _, err := e.pages.Resolve(ctx, "main", "en", "/"); err != nil {
			t.Fatal(err)
		}
	})
}

const mountRedirectSchema = pagesSchema + "      externalUrl: { type: string }\n"

func TestPagesMountRedirect(t *testing.T) {
	runPages(t, pagesOpts{schema: mountRedirectSchema, channels: webChannels}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		e.placed("section", "About us", &root, nil)
		spec := pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}", RedirectField: "externalUrl"}
		news := e.mount(root, spec)

		plain := e.article("en", `{"title":"Plain"}`)
		out := e.article("en", `{"title":"Out","externalUrl":"https://example.com/out"}`)
		inner := e.article("en", `{"title":"Inner","externalUrl":"/about-us"}`)
		bad := e.article("en", `{"title":"Bad"}`)
		e.drain()

		if r := e.resolve("en", "/news/plain"); r.Kind != pages.ResolvedItem || *r.Item != plain {
			t.Errorf("plain item: %+v", r)
		}
		e.wantRedirect("en", "/news/out", "https://example.com/out", 302)
		e.wantRedirect("en", "/news/inner", "/about-us", 302)
		if r := e.resolve("en", "/news/out"); r.Item == nil || *r.Item != out || r.Node == nil {
			t.Errorf("the redirect names the item and its mount: %+v", r)
		}
		_ = inner
		_ = bad

		// Links point at the target; Locate answers like Resolve, with a channel-relative path for site paths.
		ch := e.channel("web_en")
		if u, err := e.pages.LinkURL(ctx, ch, out); err != nil || u != "https://example.com/out" {
			t.Errorf("link of a redirecting item = %q, %v", u, err)
		}
		if u, err := e.pages.LinkURL(ctx, ch, plain); err != nil || u != "/en/news/plain" {
			t.Errorf("link of a plain item = %q, %v", u, err)
		}
		if u, ok, err := e.pages.URL(ctx, ch, out, "en"); err != nil || !ok || u != "https://example.com/out" {
			t.Errorf("url of a redirecting item = %q %v %v", u, ok, err)
		}
		if l := e.locate("https://a.test/en/news/out", nil); l.Kind != "redirect" || l.Resolution.Status != 302 ||
			l.Resolution.Location != "https://example.com/out" {
			t.Errorf("locate external: %+v", l)
		}
		if l := e.locate("https://a.test/en/news/inner", nil); l.Kind != "redirect" || l.Resolution.Location != "/en/about-us" {
			t.Errorf("locate site path: %+v", l)
		}
		if l := e.locate("https://a.test/en/news/plain", nil); l.Kind != "item" || l.Canonical == "" {
			t.Errorf("locate plain: %+v", l)
		}

		// The status is the mount's.
		spec.RedirectStatus = 308
		if _, err := e.pages.Update(ctx, news.ID, pages.NodeUpdate{Mount: &spec}); err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/news/out", "https://example.com/out", 308)

		// Clearing the field makes the item show itself again; setting it redirects.
		e.update(out, "en", `{"externalUrl":null}`)
		e.mustPublish(out)
		if r := e.resolve("en", "/news/out"); r.Kind != pages.ResolvedItem {
			t.Errorf("cleared: %+v", r)
		}
		e.update(plain, "en", `{"externalUrl":"https://example.com/plain"}`)
		e.mustPublish(plain)
		e.wantRedirect("en", "/news/plain", "https://example.com/plain", 308)

		// Removing the option from the mount ends every redirect.
		spec.RedirectField, spec.RedirectStatus = "", 0
		if _, err := e.pages.Update(ctx, news.ID, pages.NodeUpdate{Mount: &spec}); err != nil {
			t.Fatal(err)
		}
		if r := e.resolve("en", "/news/plain"); r.Kind != pages.ResolvedItem {
			t.Errorf("option removed: %+v", r)
		}
		if r := e.resolve("en", "/news/inner"); r.Kind != pages.ResolvedItem {
			t.Errorf("option removed: %+v", r)
		}

		// Validation.
		for name, s := range map[string]pages.MountSpec{
			"unknown field":    {Segment: "a", Collection: "article", Route: "{slug}", RedirectField: "nope"},
			"wrong type":       {Segment: "b", Collection: "article", Route: "{slug}", RedirectField: "publishDate"},
			"bad status":       {Segment: "c", Collection: "article", Route: "{slug}", RedirectField: "externalUrl", RedirectStatus: 303},
			"status w/o field": {Segment: "d", Collection: "article", Route: "{slug}", RedirectStatus: 301},
		} {
			if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: root, Spec: s}); !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

const rootChannels = `
channels:
  front: { languages: [en], match: { host: a.test, prefix: /en }, reserved: ["/"] }
  plain: { languages: [en], match: { host: b.test } }
`

func TestWebRootReservedAndForced(t *testing.T) {
	runPages(t, pagesOpts{channels: rootChannels}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, _ := e.placed("section", "About us", &root, nil)
		e.drain()

		// The root of a channel that reserves it belongs to the frontend, pages stay below it.
		if l := e.locate("https://a.test/en", nil); l.Kind != pages.LocatedReserved || l.Resolution != nil {
			t.Errorf("reserved root: %+v", l)
		}
		if l := e.locate("https://a.test/en/about-us", nil); l.Kind != "page" || *l.Resolution.Item != about {
			t.Errorf("page below a reserved root: %+v", l)
		}
		if l := e.locate("https://b.test/", nil); l.Kind != "page" {
			t.Errorf("root of the other channel: %+v", l)
		}

		// A forced redirect from the root redirects every channel of the site that does not reserve it.
		if _, err := e.redirect(`{"from":"/","to":"/about-us","force":true,"statusCode":"302"}`); err != nil {
			t.Fatal(err)
		}
		e.drain()
		if l := e.locate("https://b.test/", nil); l.Kind != "redirect" || l.Resolution.Status != 302 || l.Resolution.Location != "/about-us" {
			t.Errorf("forced root redirect: %+v", l)
		}
		if l := e.locate("https://a.test/en", nil); l.Kind != pages.LocatedReserved {
			t.Errorf("reserved still wins: %+v", l)
		}
		if l := e.locate("https://b.test/about-us", nil); l.Kind != "page" {
			t.Errorf("other pages unaffected: %+v", l)
		}
	})
}
