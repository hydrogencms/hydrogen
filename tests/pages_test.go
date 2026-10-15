package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// routes lists an item's route rows as "locale state path" strings, sorted.
func (e *pagesEnv) routes(item ids.UUID) []string {
	e.t.Helper()
	rs, err := e.pages.Paths(ctx, item)
	if err != nil {
		e.t.Fatal(err)
	}
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Locale + " " + r.State + " " + r.Path
	}
	slices.Sort(out)
	return out
}

func (e *pagesEnv) wantRoutes(item ids.UUID, want ...string) {
	e.t.Helper()
	slices.Sort(want)
	if got := e.routes(item); !slices.Equal(got, want) {
		e.t.Errorf("routes = %q, want %q", got, want)
	}
}

func (e *pagesEnv) wantPage(locale, path string, item ids.UUID) *pages.Resolution {
	e.t.Helper()
	r := e.resolve(locale, path)
	if r.Kind != pages.ResolvedPage || r.Status != 200 || r.Item == nil || *r.Item != item {
		e.t.Errorf("resolve %s %s = %+v, want page %s", locale, path, r, item)
	}
	return r
}

func (e *pagesEnv) wantRedirect(locale, path, to string, status int) {
	e.t.Helper()
	r := e.resolve(locale, path)
	if r.Kind != pages.ResolvedRedirect || r.Status != status || r.Location != to {
		e.t.Errorf("resolve %s %s = %+v, want %d redirect to %s", locale, path, r, status, to)
	}
}

func (e *pagesEnv) wantNotFound(locale, path string) {
	e.t.Helper()
	if r := e.resolve(locale, path); r.Kind != pages.ResolvedNotFound || r.Status != 404 {
		e.t.Errorf("resolve %s %s = %+v, want not found", locale, path, r)
	}
}

func (e *pagesEnv) translate(id ids.UUID, locale, title string) {
	e.t.Helper()
	e.update(id, locale, fmt.Sprintf(`{"title":%q}`, title))
	e.mustPublish(id, locale)
}

func (e *pagesEnv) move(node ids.UUID, parent ids.UUID, after *ids.UUID) {
	e.t.Helper()
	if _, err := e.pages.Update(ctx, node, pages.NodeUpdate{Move: &pages.Move{Parent: parent, After: after}}); err != nil {
		e.t.Fatalf("move: %v", err)
	}
}

func (e *pagesEnv) article(locale, js string) ids.UUID {
	e.t.Helper()
	id := e.create("article", locale, js)
	e.mustPublish(id)
	return id
}

func (e *pagesEnv) mount(parent ids.UUID, spec pages.MountSpec, after ...ids.UUID) *pages.Node {
	e.t.Helper()
	m := pages.Mount{Parent: parent, Spec: spec}
	if len(after) > 0 {
		m.After = &after[0]
	}
	n, err := e.pages.AddMount(ctx, "main", m)
	if err != nil {
		e.t.Fatalf("mount: %v", err)
	}
	return n
}

func validation(t *testing.T, err error) []string {
	t.Helper()
	var ve *content.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a validation error", err)
	}
	var out []string
	for _, f := range ve.Errors {
		out = append(out, f.Path+":"+f.Code)
	}
	return out
}

func TestPagesPathsPerLocale(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		home, root := e.placed("home", "Home", nil, nil)
		about, aboutN := e.placed("section", "About us", &root, nil)
		e.translate(about, "de", "Ueber uns")
		team, _ := e.placed("landing", "Team", &aboutN, nil)
		e.translate(team, "de", "Mannschaft")
		press, pressN := e.placed("section", "Press", &root, &aboutN)
		// Published in German only, main locale de: the English-only parent press falls back to its main slug.
		kontakt := e.create("landing", "de", `{"title":"Kontakt"}`)
		e.mustPublish(kontakt)
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: kontakt, Parent: &pressN}); err != nil {
			t.Fatal(err)
		}

		e.wantRoutes(home, "en current /")
		e.wantRoutes(about, "en current /about-us", "de current /ueber-uns")
		e.wantRoutes(team, "en current /about-us/team", "de current /ueber-uns/mannschaft")
		e.wantRoutes(press, "en current /press")
		e.wantRoutes(kontakt, "de current /press/kontakt")

		e.wantPage("en", "/", home)
		e.wantPage("de", "/ueber-uns/mannschaft", team)
		e.wantNotFound("en", "/ueber-uns")
		e.wantNotFound("en", "/press/kontakt")
		e.wantNotFound("de", "/about-us")
		e.wantNotFound("en", "/About-Us")
		e.wantNotFound("en", "/about-us/")
		e.wantNotFound("en", "no-slash")

		// A translation published later gets its path, and the English one is untouched.
		e.translate(press, "de", "Presse")
		e.wantRoutes(press, "en current /press", "de current /presse")
		e.wantRoutes(kontakt, "de current /presse/kontakt", "de redirect /press/kontakt")
		e.wantRedirect("de", "/press/kontakt", "/presse/kontakt", 301)
		// Changing the main-locale slug moves the English descendants; German ones have their own ancestors' slugs.
		e.update(about, "en", `{"slug":"about"}`)
		e.mustPublish(about, "en")
		e.wantRoutes(team, "en current /about/team", "en redirect /about-us/team", "de current /ueber-uns/mannschaft")
	})
}

func TestPagesMountDatePattern(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, news := e.placed("section", "News", &root, nil)
		m := e.mount(news, pages.MountSpec{Segment: "articles", Collection: "article", Route: "{publishDate:yyyy}/{publishDate:mm}/{slug}",
			Filter: map[string]any{"category": []any{"tech", "science"}}})
		a := e.article("en", `{"title":"Go 2","publishDate":"2026-03-09T10:00:00Z","category":"tech"}`)
		b := e.article("en", `{"title":"Lunch","publishDate":"2026-03-09T10:00:00Z","category":"food"}`)
		e.wantRoutes(a, "en current /news/articles/2026/03/go-2")
		e.wantRoutes(b)

		r := e.resolve("en", "/news/articles/2026/03/go-2")
		if r.Kind != pages.ResolvedItem || r.Status != 200 || *r.Item != a || *r.Node != m.ID || r.Path != "/news/articles/2026/03/go-2" {
			t.Fatalf("resolve item = %+v", r)
		}
		var names []string
		for _, c := range r.Breadcrumbs {
			names = append(names, c.Name+"="+c.Path)
		}
		if got, want := strings.Join(names, " "), "Home=/ News=/news articles=/news/articles Go 2=/news/articles/2026/03/go-2"; got != want {
			t.Errorf("breadcrumbs = %q, want %q", got, want)
		}

		// An item that starts matching on its next publish gets routed; one that stops loses its path to a redirect.
		e.update(b, "en", `{"category":"science"}`)
		e.mustPublish(b)
		e.wantRoutes(b, "en current /news/articles/2026/03/lunch")
		e.update(a, "en", `{"category":"food"}`)
		e.mustPublish(a)
		e.wantRoutes(a, "en redirect /news/articles/2026/03/go-2")
		e.wantNotFound("en", "/news/articles/2026/03/go-2")

		// A date outside the pattern's reach gives no route.
		c := e.create("article", "en", `{"title":"Undated","category":"tech"}`)
		e.mustPublish(c)
		e.wantRoutes(c)
	})
}

func TestPagesCanonicalAndAliasMounts(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		first := e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		a := e.article("en", `{"title":"Story","category":"tech"}`)
		e.wantRoutes(a, "en current /news/story")
		tech := e.mount(root, pages.MountSpec{Segment: "tech", Collection: "article", Route: "{slug}", Filter: map[string]any{"category": "tech"}}, first.ID)
		e.wantRoutes(a, "en current /news/story", "en alias /tech/story")
		canon := e.mount(root, pages.MountSpec{Segment: "stories", Collection: "article", Route: "{slug}", Canonical: true}, tech.ID)
		e.wantRoutes(a, "en alias /news/story", "en alias /tech/story", "en current /stories/story")

		r := e.resolve("en", "/news/story")
		if r.Kind != pages.ResolvedItem || r.Status != 200 || r.Path != "/stories/story" || *r.Node != first.ID {
			t.Errorf("alias resolve = %+v", r)
		}
		// Deleting the canonical mount hands the canonical role to the first one in tree order.
		if err := e.pages.DeleteNode(ctx, canon.ID); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(a, "en current /news/story", "en alias /tech/story")
		e.wantNotFound("en", "/stories/story")
	})
}

func TestPagesSlugChangeLeavesRedirects(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, aboutN := e.placed("section", "About us", &root, nil)
		team, teamN := e.placed("section", "Team", &aboutN, nil)
		bio, _ := e.placed("landing", "Bio", &teamN, nil)
		e.mount(aboutN, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		a := e.article("en", `{"title":"Opening"}`)
		e.wantRoutes(a, "en current /about-us/news/opening")

		e.update(about, "en", `{"slug":"about"}`)
		e.mustPublish(about)

		e.wantPage("en", "/about", about)
		e.wantRedirect("en", "/about-us", "/about", 301)
		e.wantRedirect("en", "/about-us/team", "/about/team", 301)
		e.wantRedirect("en", "/about-us/team/bio", "/about/team/bio", 301)
		e.wantRedirect("en", "/about-us/news/opening", "/about/news/opening", 301)
		e.wantRoutes(bio, "en current /about/team/bio", "en redirect /about-us/team/bio")
		e.wantRoutes(a, "en current /about/news/opening", "en redirect /about-us/news/opening")
		_ = team

		// Back to the old slug: the old paths are current again and the new ones become redirects.
		e.update(about, "en", `{"slug":"about-us"}`)
		e.mustPublish(about)
		e.wantRoutes(bio, "en current /about-us/team/bio", "en redirect /about/team/bio")
		e.wantPage("en", "/about-us/team/bio", bio)
		e.wantRedirect("en", "/about/team/bio", "/about-us/team/bio", 301)
		e.wantRoutes(a, "en current /about-us/news/opening", "en redirect /about/news/opening")
	})
}

func TestPagesMoveLeavesRedirects(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, products := e.placed("section", "Products", &root, nil)
		_, company := e.placed("section", "Company", &root, &products)
		team, teamN := e.placed("section", "Team", &products, nil)
		bio, _ := e.placed("landing", "Bio", &teamN, nil)
		mn := e.mount(teamN, pages.MountSpec{Segment: "posts", Collection: "article", Route: "{slug}"})
		a := e.article("en", `{"title":"Hello"}`)
		e.wantRoutes(a, "en current /products/team/posts/hello")

		e.move(teamN, company, nil)
		e.wantPage("en", "/company/team", team)
		e.wantRoutes(bio, "en current /company/team/bio", "en redirect /products/team/bio")
		e.wantRoutes(a, "en current /company/team/posts/hello", "en redirect /products/team/posts/hello")
		e.wantRedirect("en", "/products/team/bio", "/company/team/bio", 301)
		e.wantRedirect("en", "/products/team/posts/hello", "/company/team/posts/hello", 301)
		if n, err := e.pages.Node(ctx, mn.ID); err != nil || n.Depth != 3 {
			t.Errorf("mount depth = %v, %v", n, err)
		}

		// Moving back revives the old paths.
		e.move(teamN, products, nil)
		e.wantRoutes(bio, "en current /products/team/bio", "en redirect /company/team/bio")
		e.wantRoutes(a, "en current /products/team/posts/hello", "en redirect /company/team/posts/hello")

		// Cycles, the root and foreign parents are refused.
		if _, err := e.pages.Update(ctx, products, pages.NodeUpdate{Move: &pages.Move{Parent: teamN}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("cycle: %v", err)
		}
		if _, err := e.pages.Update(ctx, root, pages.NodeUpdate{Move: &pages.Move{Parent: products}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("move root: %v", err)
		}
		// A move onto a taken path conflicts and changes nothing.
		e.placed("section", "Team", &company, nil)
		if _, err := e.pages.Update(ctx, teamN, pages.NodeUpdate{Move: &pages.Move{Parent: company}}); !errors.Is(err, content.ErrConflict) {
			t.Errorf("colliding move: %v", err)
		}
		e.wantRoutes(bio, "en current /products/team/bio", "en redirect /company/team/bio")
	})
}

func TestPagesReorder(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, a := e.placed("section", "A", &root, nil)
		_, b := e.placed("section", "B", &root, &a)
		_, c := e.placed("section", "C", &root, &b)
		order := func() string {
			ts, err := e.pages.Tree(ctx, "main", "en", nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, n := range ts {
				names = append(names, n.Name)
			}
			return strings.Join(names, "")
		}
		if got := order(); got != "ABC" {
			t.Fatalf("order = %s", got)
		}
		e.move(c, root, nil)
		if got := order(); got != "CAB" {
			t.Errorf("after moving C first: %s", got)
		}
		e.move(a, root, &b)
		if got := order(); got != "CBA" {
			t.Errorf("after moving A after B: %s", got)
		}
		if _, err := e.pages.Update(ctx, a, pages.NodeUpdate{Move: &pages.Move{Parent: root, After: &a}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("after itself: %v", err)
		}
	})
}

func TestPagesSlugTaken(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		e.placed("section", "Same", &root, nil)

		// Placing a page whose published slug is taken fails and places nothing.
		dup := e.page("section", "Same")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: dup, Parent: &root}); !errors.Is(err, content.ErrConflict) {
			t.Fatalf("place duplicate: %v", err)
		}
		if _, err := e.pages.Node(ctx, ids.Nil); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("node(nil) = %v", err)
		}

		// A placed page publishing a taken slug is a field error.
		other, _ := e.placed("section", "Other", &root, nil)
		e.update(other, "en", `{"slug":"same"}`)
		err := e.publish(other)
		if !errors.Is(err, content.ErrInvalid) || !slices.Equal(validation(t, err), []string{"slug:slug_taken"}) {
			t.Fatalf("publish duplicate: %v", err)
		}
		if !strings.Contains(err.Error(), "/same") {
			t.Errorf("message does not name the path: %v", err)
		}

		// A mounted item may not take a page's path, and a page may not take a mounted item's.
		_, news := e.placed("section", "News", &root, nil)
		e.placed("section", "2026", &news, nil)
		e.placed("landing", "Hello", mustNode(t, e, "/news/2026"), nil)
		e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{publishDate:yyyy}/{slug}"})
		art := e.create("article", "en", `{"title":"Hello","publishDate":"2026-01-02T00:00:00Z"}`)
		err = e.publish(art)
		if !slices.Equal(validation(t, err), []string{"slug:slug_taken"}) {
			t.Fatalf("mounted item over a page: %v", err)
		}
		e.update(art, "en", `{"title":"Other news"}`)
		e.update(art, "en", `{"slug":"other-news"}`)
		e.mustPublish(art)
		page := e.page("landing", "Other news")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: page, Parent: mustNode(t, e, "/news/2026")}); !errors.Is(err, content.ErrConflict) {
			t.Fatalf("page over a mounted item: %v", err)
		}
	})
}

func mustNode(t *testing.T, e *pagesEnv, path string) *ids.UUID {
	t.Helper()
	r := e.resolve("en", path)
	if r.Node == nil {
		t.Fatalf("%s resolves to %+v", path, r)
	}
	return r.Node
}

func TestPagesChildrenRule(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, section := e.placed("section", "Section", &root, nil)
		_, landing := e.placed("landing", "Landing", &section, nil)
		// landing allows no children; section allows section and landing, not home.
		for name, c := range map[string]struct {
			typ    string
			parent ids.UUID
		}{"under landing": {"landing", landing}, "home under section": {"home", section}} {
			item := e.page(c.typ, name)
			if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &c.parent}); !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("%s: %v", name, err)
			}
		}
		// A move is checked the same way.
		_, other := e.placed("section", "Other", &root, nil)
		if _, err := e.pages.Update(ctx, other, pages.NodeUpdate{Move: &pages.Move{Parent: landing}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("move under landing: %v", err)
		}
		// Not a page type, unknown item, unknown site, second root, double placement.
		art := e.article("en", `{"title":"A"}`)
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: art, Parent: &root}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("article: %v", err)
		}
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: ids.UUID{1}, Parent: &root}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown item: %v", err)
		}
		if _, err := e.pages.Place(ctx, "nowhere", pages.Place{Item: e.page("home", "X")}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown site: %v", err)
		}
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: e.page("home", "Second root")}); !errors.Is(err, content.ErrConflict) {
			t.Errorf("second root: %v", err)
		}
		item := e.page("section", "Twice")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root}); !errors.Is(err, content.ErrConflict) {
			t.Errorf("placed twice: %v", err)
		}
	})
}

func TestPagesVanityAlias(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		promo, _ := e.placed("landing", "Spring sale", &root, nil)
		e.update(promo, "en", `{"vanityPath":"/promo"}`)
		e.mustPublish(promo)
		e.wantRoutes(promo, "en current /spring-sale", "en alias /promo")
		r := e.wantPage("en", "/promo", promo)
		if r.Path != "/spring-sale" {
			t.Errorf("alias canonical path = %s", r.Path)
		}
		// Taken or malformed vanity paths are field errors.
		other, _ := e.placed("landing", "Other", &root, nil)
		e.update(other, "en", `{"vanityPath":"/promo"}`)
		if got := validation(t, e.publish(other)); !slices.Equal(got, []string{"vanityPath:slug_taken"}) {
			t.Errorf("duplicate vanity: %v", got)
		}
		e.update(other, "en", `{"vanityPath":"promo"}`)
		if got := validation(t, e.publish(other)); !slices.Equal(got, []string{"vanityPath:invalid_path"}) {
			t.Errorf("malformed vanity: %v", got)
		}
		// Removing the vanity path leaves a redirect.
		e.update(promo, "en", `{"vanityPath":""}`)
		e.mustPublish(promo)
		e.wantRoutes(promo, "en current /spring-sale", "en redirect /promo")
		e.wantRedirect("en", "/promo", "/spring-sale", 301)
	})
}

func (e *pagesEnv) redirect(js string) (ids.UUID, error) {
	e.t.Helper()
	id := e.create("redirect", "en", js)
	return id, e.publish(id)
}

func TestPagesManualRedirects(t *testing.T) {
	runPages(t, pagesOpts{sites: []string{"main", "shop"}}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, aboutN := e.placed("section", "About us", &root, nil)

		// Defaults: site main, status 301, every locale.
		red, err := e.redirect(`{"from":"/old-about","to":"/about-us"}`)
		if err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/old-about", "/about-us", 301)
		e.wantRedirect("de", "/old-about", "/about-us", 301)
		e.wantRoutes(red, " manual /old-about")
		if _, err := e.redirect(`{"from":"/moved","to":"https://example.com/x","statusCode":"308","forLocale":"de"}`); err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("de", "/moved", "https://example.com/x", 308)
		e.wantNotFound("en", "/moved")
		to, err := e.redirect(fmt.Sprintf(`{"from":"/by-item","to":"item:%s","statusCode":"302"}`, about))
		if err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/by-item", "/about-us", 302)
		_ = to
		// A shop-site redirect is separate.
		if _, err := e.redirect(`{"from":"/old-about","to":"/shop-home","site":"shop"}`); err != nil {
			t.Fatal(err)
		}
		if r, _ := e.pages.Resolve(ctx, "shop", "en", "/old-about"); r.Kind != pages.ResolvedRedirect || r.Location != "/shop-home" {
			t.Errorf("shop redirect = %+v", r)
		}

		// A manual redirect never takes a live path, and a page never takes a redirect's path.
		if _, err := e.redirect(`{"from":"/about-us","to":"/elsewhere"}`); !slices.Equal(validation(t, err), []string{"from:slug_taken"}) {
			t.Errorf("redirect over a page: %v", err)
		}
		if _, err := e.redirect(`{"from":"/old-about","to":"/elsewhere"}`); !slices.Equal(validation(t, err), []string{"from:slug_taken"}) {
			t.Errorf("redirect over a redirect: %v", err)
		}
		if _, err := e.redirect(`{"from":"bad","to":"nowhere"}`); !slices.Equal(validation(t, err), []string{"from:invalid_path", "to:invalid_target"}) {
			t.Errorf("malformed redirect: %v", err)
		}
		if _, err := e.redirect(`{"from":"/x","to":"/y","site":"nope"}`); !slices.Equal(validation(t, err), []string{"site:unknown_site"}) {
			t.Errorf("unknown site: %v", err)
		}
		taker := e.create("landing", "en", `{"title":"Old","slug":"old-about"}`)
		e.mustPublish(taker)
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: taker, Parent: &root}); !errors.Is(err, content.ErrConflict) {
			t.Errorf("page over a manual redirect: %v", err)
		}

		// Precedence: a manual redirect replaces a history redirect of a page, and the page's current path beats both.
		e.update(about, "en", `{"slug":"about"}`)
		e.mustPublish(about)
		e.wantRedirect("en", "/about-us", "/about", 301)
		hist, err := e.redirect(`{"from":"/about-us","to":"/somewhere-else","statusCode":"307"}`)
		if err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/about-us", "/somewhere-else", 307)
		e.wantRoutes(about, "en current /about", "en redirect /about-us")
		// Renaming back takes the path from the manual redirect? No: it is refused.
		e.update(about, "en", `{"slug":"about-us"}`)
		if got := validation(t, e.publish(about)); !slices.Equal(got, []string{"slug:slug_taken"}) {
			t.Errorf("rename onto a manual redirect: %v", got)
		}
		_ = aboutN
		// Deleting the redirect item removes its row; the page's own history shows again, and the rename now works.
		if err := e.c.Delete(ctx, hist); err != nil {
			t.Fatal(err)
		}
		e.wantRedirect("en", "/about-us", "/about", 301)
		e.mustPublish(about)
		e.wantPage("en", "/about-us", about)
	})
}

func TestPagesTree(t *testing.T) {
	pr := &probe{topics: []string{"page.*"}}
	runPages(t, pagesOpts{extra: []kernel.Module{pr}}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, aboutN := e.placed("section", "About us", &root, nil)
		e.translate(about, "de", "Ueber uns")
		_, teamN := e.placed("landing", "Team", &aboutN, nil)
		draft := e.create("landing", "en", `{"title":"Coming soon"}`)
		draftN, err := e.pages.Place(ctx, "main", pages.Place{Item: draft, Parent: &aboutN, After: &teamN})
		if err != nil {
			t.Fatal(err)
		}
		news := e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}",
			Label: map[string]string{"en": "News", "de": "Nachrichten"}}, aboutN)
		art := e.article("en", `{"title":"First"}`)
		link := func(spec pages.LinkSpec, after ids.UUID) *pages.Node {
			n, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, After: &after, Spec: spec})
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		l1 := link(pages.LinkSpec{Label: map[string]string{"en": "Art"}, Item: &art}, news.ID)
		l2 := link(pages.LinkSpec{Label: map[string]string{"en": "Team page"}, Node: &teamN}, l1.ID)
		l3 := link(pages.LinkSpec{Label: map[string]string{"en": "Docs", "de": "Doku"}, URL: "https://example.com/docs"}, l2.ID)
		if _, err := e.pages.Update(ctx, aboutN, pages.NodeUpdate{Hidden: new(true)}); err != nil {
			t.Fatal(err)
		}
		e.update(about, "en", `{"title":"About us!"}`)

		ts, err := e.pages.Tree(ctx, "main", "en", nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		type row struct {
			name, kind, path, status, target string
			hidden                           bool
			children                         int
		}
		var got []row
		for _, n := range ts {
			got = append(got, row{n.Name, n.Kind, n.Path, n.Status, n.Target, n.Hidden, n.Children})
			if len(n.Nodes) != 0 {
				t.Errorf("depth 1 returned nested nodes for %s", n.Name)
			}
		}
		want := []row{
			{"About us", pages.KindPage, "/about-us", content.StatusChanged, "", true, 2},
			{"News", pages.KindMount, "/news", "", "", false, 0},
			{"Art", pages.KindLink, "", "", "/news/first", false, 0},
			{"Team page", pages.KindLink, "", "", "/about-us/team", false, 0},
			{"Docs", pages.KindLink, "", "", "https://example.com/docs", false, 0},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("tree = %+v\nwant %+v", got, want)
		}
		if ts[0].Type != "section" || ts[1].Mount == nil || ts[2].Link == nil || ts[4].Link.URL == "" {
			t.Errorf("types/specs: %+v", ts)
		}

		// Depth 2 nests, locales fall back to the main locale, drafts show their draft name.
		ts, err = e.pages.Tree(ctx, "main", "de", nil, 2)
		if err != nil {
			t.Fatal(err)
		}
		if ts[0].Name != "Ueber uns" || ts[0].Path != "/ueber-uns" || len(ts[0].Nodes) != 2 {
			t.Fatalf("de tree = %+v", ts[0])
		}
		team, soon := ts[0].Nodes[0], ts[0].Nodes[1]
		if team.Name != "Team" || team.Path != "" || soon.Name != "Coming soon" || soon.Status != content.StatusDraft || soon.ID != draftN.ID {
			t.Errorf("children = %+v / %+v", team, soon)
		}
		if ts[1].Name != "Nachrichten" || ts[4].Name != "Doku" {
			t.Errorf("labels = %q %q", ts[1].Name, ts[4].Name)
		}
		sub, err := e.pages.Tree(ctx, "main", "en", &aboutN, 1)
		if err != nil || len(sub) != 2 || sub[0].Name != "Team" {
			t.Errorf("subtree = %+v, %v", sub, err)
		}
		if _, err := e.pages.Tree(ctx, "other", "en", nil, 1); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown site: %v", err)
		}
		if _, err := e.pages.Tree(ctx, "main", "en", &l3.ID, 1); err != nil {
			t.Errorf("links have no children: %v", err)
		}

		// Spec updates: a link's target and a mount's pattern.
		if _, err := e.pages.Update(ctx, l1.ID, pages.NodeUpdate{Link: &pages.LinkSpec{Label: map[string]string{"en": "Art"}, URL: "/x", Item: &art}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("link with two targets: %v", err)
		}
		if _, err := e.pages.Update(ctx, l1.ID, pages.NodeUpdate{Link: &pages.LinkSpec{Label: map[string]string{"en": "Art"}, URL: "/x"}}); err != nil {
			t.Error(err)
		}
		if _, err := e.pages.Update(ctx, l1.ID, pages.NodeUpdate{Mount: &pages.MountSpec{}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("mount spec on a link: %v", err)
		}
		spec := *news.Mount
		spec.Route = "p/{slug}"
		if _, err := e.pages.Update(ctx, news.ID, pages.NodeUpdate{Mount: &spec}); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art, "en current /news/p/first", "en redirect /news/first")
		if _, err := e.pages.Update(ctx, aboutN, pages.NodeUpdate{}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("empty update: %v", err)
		}

		// Every mutation is audited and queued as page.changed.
		if got := e.audit(news.ID); !slices.Equal(got, []string{"page.mount", "page.update"}) {
			t.Errorf("audit of the mount = %v", got)
		}
		if got := e.audit(aboutN); !slices.Equal(got, []string{"page.place", "page.update"}) {
			t.Errorf("audit of about = %v", got)
		}
		e.drain()
		evs := pr.take()
		var payload struct {
			Site string
			Node *ids.UUID
		}
		if len(evs) == 0 || evs[0].Topic != "page.changed" || json.Unmarshal(evs[0].Payload, &payload) != nil || payload.Site != "main" || payload.Node == nil {
			t.Errorf("events = %+v", evs)
		}
	})
}

func TestPagesDeleteNodes(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		parent, parentN := e.placed("section", "Parent", &root, nil)
		child, childN := e.placed("landing", "Child", &parentN, nil)
		m := e.mount(parentN, pages.MountSpec{Segment: "posts", Collection: "article", Route: "{slug}"})
		art := e.article("en", `{"title":"Post"}`)
		l, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: parentN, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: "/x"}})
		if err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art, "en current /parent/posts/post")

		// A page with children (pages, mounts or links) cannot be deleted; a page node cannot be deleted directly.
		if err := e.c.Delete(ctx, parent); !errors.Is(err, content.ErrConflict) || !strings.Contains(err.Error(), "children") {
			t.Fatalf("delete with children: %v", err)
		}
		if err := e.pages.DeleteNode(ctx, parentN); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("delete page node: %v", err)
		}
		if err := e.pages.DeleteNode(ctx, ids.UUID{9}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("delete unknown node: %v", err)
		}

		if err := e.pages.DeleteNode(ctx, m.ID); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art)
		e.wantNotFound("en", "/parent/posts/post")
		if err := e.pages.DeleteNode(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.c.Delete(ctx, parent); !errors.Is(err, content.ErrConflict) {
			t.Fatalf("delete with a page child: %v", err)
		}

		e.wantRoutes(child, "en current /parent/child")
		if err := e.c.Delete(ctx, child); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(child)
		e.wantNotFound("en", "/parent/child")
		if _, err := e.pages.Node(ctx, childN); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("child node after delete: %v", err)
		}
		if err := e.c.Delete(ctx, parent); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(parent)
		e.drain()

		// Deleting a mounted item removes its routes and history.
		m2 := e.mount(root, pages.MountSpec{Segment: "blog", Collection: "article", Route: "{slug}"})
		e.update(art, "en", `{"slug":"renamed"}`)
		e.mustPublish(art)
		e.wantRoutes(art, "en current /blog/renamed", "en redirect /blog/post")
		e.update(art, "en", `{"slug":"again"}`)
		e.mustPublish(art)
		e.wantRoutes(art, "en current /blog/again", "en redirect /blog/renamed", "en redirect /blog/post")
		if err := e.c.Delete(ctx, art); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art)
		e.wantNotFound("en", "/blog/renamed")
		_ = m2
	})
}

func TestPagesMountValidation(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, lastPage := e.placed("section", "S", &root, nil)
		art := pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"}
		for name, mod := range map[string]func(*pages.MountSpec){
			"empty segment":      func(s *pages.MountSpec) { s.Segment = "" },
			"slash in segment":   func(s *pages.MountSpec) { s.Segment = "a/b" },
			"bad segment":        func(s *pages.MountSpec) { s.Segment = "News Room" },
			"unknown collection": func(s *pages.MountSpec) { s.Collection = "nope" },
			"page collection":    func(s *pages.MountSpec) { s.Collection = "section" },
			"redirects":          func(s *pages.MountSpec) { s.Collection = "redirect" },
			"bad route":          func(s *pages.MountSpec) { s.Route = "news/" },
			"no placeholder":     func(s *pages.MountSpec) { s.Route = "news" },
			"unknown route field": func(s *pages.MountSpec) {
				s.Route = "{nope}"
			},
			"unknown filter field": func(s *pages.MountSpec) { s.Filter = map[string]any{"nope": "x"} },
			"object filter":        func(s *pages.MountSpec) { s.Filter = map[string]any{"category": map[string]any{"a": 1}} },
		} {
			spec := art
			mod(&spec)
			if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: root, Spec: spec}); !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: ids.UUID{3}, Spec: art}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown parent: %v", err)
		}
		linkNode, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: "/x"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: linkNode.ID, Spec: art}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("mount under a link: %v", err)
		}
		e.mount(root, art)
		if _, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: root, Spec: art}); !errors.Is(err, content.ErrConflict) {
			t.Errorf("same segment twice: %v", err)
		}
		_ = lastPage
		for name, spec := range map[string]pages.LinkSpec{
			"no target":  {Label: map[string]string{"en": "x"}},
			"no label":   {URL: "/x"},
			"bad url":    {Label: map[string]string{"en": "x"}, URL: "ftp://x"},
			"protocol":   {Label: map[string]string{"en": "x"}, URL: "//evil.example"},
			"both nodes": {Label: map[string]string{"en": "x"}, Node: &root, URL: "/x"},
		} {
			if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: spec}); !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("link %s: %v", name, err)
			}
		}
		missing := ids.UUID{7}
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, Item: &missing}}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("link to a missing item: %v", err)
		}
	})
}

func TestPagesFilterAndPatternValues(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		e.mount(root, pages.MountSpec{Segment: "x", Collection: "article", Route: "{category}/{id}", Filter: map[string]any{"kind": "news"}})
		a := e.article("en", `{"title":"A","category":"Big Data","kind":"news"}`)
		if r := e.routes(a); len(r) != 1 || !strings.HasPrefix(r[0], "en current /x/big-data/") || !strings.HasSuffix(r[0], a.String()) {
			t.Errorf("routes = %v", r)
		}
		e.wantRoutes(e.article("en", `{"title":"B","kind":"other","category":"c"}`))
		e.wantRoutes(e.article("en", `{"title":"C","kind":"news"}`)) // empty category: no route
	})
}

func TestPagesReconcileAfterContentMigration(t *testing.T) {
	suffix := func(d *content.MigrationDoc) error {
		d.Fields["slug"] = d.Fields["slug"].(string) + "-v2"
		return nil
	}
	mig := migrationModule{
		{Name: "0001_section_slug", Type: "section", Up: suffix},
		{Name: "0002_article_slug", Type: "article", Up: suffix},
	}
	runPages(t, pagesOpts{extra: []kernel.Module{mig}}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		sec, secN := e.placed("section", "News desk", &root, nil)
		e.mount(secN, pages.MountSpec{Segment: "all", Collection: "article", Route: "{slug}"})
		art := e.article("en", `{"title":"Scoop"}`)
		e.wantRoutes(sec, "en current /news-desk")
		e.drain()
		e.wantRoutes(sec, "en current /news-desk") // the hooks did the work; the consumer finds nothing to do
		e.wantRoutes(art, "en current /news-desk/all/scoop")

		done, err := e.c.Migrate(ctx)
		if err != nil || len(done) != 2 {
			t.Fatalf("migrate: %v %v", done, err)
		}
		e.wantRoutes(sec, "en current /news-desk") // no hooks ran
		e.drain()
		e.wantRoutes(sec, "en current /news-desk-v2", "en redirect /news-desk")
		e.wantRoutes(art, "en current /news-desk-v2/all/scoop-v2", "en redirect /news-desk/all/scoop", "en redirect /news-desk-v2/all/scoop")
		e.wantPage("en", "/news-desk-v2", sec)
		e.wantRedirect("en", "/news-desk/all/scoop", "/news-desk-v2/all/scoop-v2", 301)
		e.drain() // again: nothing changes
		e.wantRoutes(sec, "en current /news-desk-v2", "en redirect /news-desk")
	})
}

func TestPagesSites(t *testing.T) {
	runPages(t, pagesOpts{sites: []string{"main", "shop"}}, func(t *testing.T, e *pagesEnv) {
		if got := e.pages.Sites(); !slices.Equal(got, []string{"main", "shop"}) {
			t.Fatalf("sites = %v", got)
		}
		a := e.page("home", "Main home")
		b := e.page("home", "Shop home")
		na, err := e.pages.Place(ctx, "main", pages.Place{Item: a})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Place(ctx, "shop", pages.Place{Item: b}); err != nil {
			t.Fatal(err)
		}
		c := e.page("section", "Shared name")
		if _, err := e.pages.Place(ctx, "shop", pages.Place{Item: c, Parent: &na.ID}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("parent of another site: %v", err)
		}
		e.wantRoutes(a, "en current /")
		e.wantRoutes(b, "en current /")
		if r, err := e.pages.Resolve(ctx, "shop", "en", "/"); err != nil || r.Item == nil || *r.Item != b {
			t.Errorf("shop root = %+v, %v", r, err)
		}
	})
}

func TestPagesPlacedBeforeFirstPublish(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		draft := e.create("section", "en", `{"title":"Soon"}`)
		n, err := e.pages.Place(ctx, "main", pages.Place{Item: draft, Parent: &root})
		if err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(draft)
		child := e.page("landing", "Child")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: child, Parent: &n.ID}); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(child) // the parent has no slug yet
		e.mustPublish(draft)
		e.wantRoutes(draft, "en current /soon")
		e.wantRoutes(child, "en current /soon/child")

		// A name-only republish and an unplaced page's publish change no routes.
		e.update(draft, "en", `{"title":"Soon!"}`)
		e.update(draft, "en", `{"slug":"soon"}`)
		e.mustPublish(draft)
		e.wantRoutes(draft, "en current /soon")
		if ts, _ := e.pages.Tree(ctx, "main", "en", nil, 1); len(ts) != 1 || ts[0].Name != "Soon!" {
			t.Errorf("tree = %+v", ts)
		}
		loose := e.page("landing", "Loose")
		e.wantRoutes(loose)
	})
}

// wantCurrent checks the current routes of an item, ignoring the history behind them.
func (e *pagesEnv) wantCurrent(item ids.UUID, want ...string) {
	e.t.Helper()
	got := []string{}
	for _, r := range e.routes(item) {
		if strings.Contains(r, " current ") {
			got = append(got, r)
		}
	}
	if !slices.Equal(got, want) {
		e.t.Errorf("current routes = %q, want %q", got, want)
	}
}

// runPagesPostgres runs fn on Postgres only: the concurrency tests need real parallel transactions.
func runPagesPostgres(t *testing.T, o pagesOpts, fn func(t *testing.T, e *pagesEnv)) {
	t.Helper()
	clk := newClock()
	fn(t, newPagesEnv(t, openPostgres(t, clk), clk, o))
}

// parallel runs the functions at once and returns their errors.
func parallel(fns ...func() error) []error {
	errs := make([]error, len(fns))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

func TestPagesConcurrentCrossMoves(t *testing.T) {
	runPagesPostgres(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		ia, a := e.placed("section", "Alpha", &root, nil)
		ib, b := e.placed("section", "Beta", &root, nil)
		moveTo := func(node, parent ids.UUID) func() error {
			return func() error {
				_, err := e.pages.Update(ctx, node, pages.NodeUpdate{Move: &pages.Move{Parent: parent}})
				return err
			}
		}
		for round := range 30 {
			errs := parallel(moveTo(a, b), moveTo(b, a))
			wins := 0
			for _, err := range errs {
				if err == nil {
					wins++
				} else if !errors.Is(err, content.ErrBadRequest) {
					t.Fatalf("round %d: unexpected error %v", round, err)
				}
			}
			if wins != 1 {
				t.Fatalf("round %d: %d moves succeeded (%v), want exactly one", round, wins, errs)
			}
			na, err := e.pages.Node(ctx, a)
			if err != nil {
				t.Fatal(err)
			}
			nb, err := e.pages.Node(ctx, b)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case *na.Parent == b && *nb.Parent == root && na.Depth == 2 && nb.Depth == 1:
				e.wantCurrent(ia, "en current /beta/alpha")
				e.wantCurrent(ib, "en current /beta")
				e.move(a, root, nil)
			case *nb.Parent == a && *na.Parent == root && nb.Depth == 2 && na.Depth == 1:
				e.wantCurrent(ib, "en current /alpha/beta")
				e.wantCurrent(ia, "en current /alpha")
				e.move(b, root, nil)
			default:
				t.Fatalf("round %d: inconsistent tree: alpha %+v beta %+v", round, na, nb)
			}
		}
	})
}

func TestPagesConcurrentPublishAndMove(t *testing.T) {
	runPagesPostgres(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, target := e.placed("section", "Target", &root, nil)
		top, topN := e.placed("section", "Top", &root, nil)
		child, _ := e.placed("landing", "Child", &topN, nil)
		under := false
		for round := range 12 {
			slug := fmt.Sprintf("top-%d", round)
			e.update(top, "en", fmt.Sprintf(`{"slug":%q}`, slug))
			parent, base := root, ""
			if !under {
				parent, base = target, "/target"
			}
			under = !under
			errs := parallel(
				func() error { return e.publish(top) },
				func() error {
					_, err := e.pages.Update(ctx, topN, pages.NodeUpdate{Move: &pages.Move{Parent: parent}})
					return err
				})
			for _, err := range errs {
				if err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
			}
			e.wantPage("en", base+"/"+slug, top)
			e.wantPage("en", base+"/"+slug+"/child", child)
			var current []string
			for _, r := range e.routes(child) {
				if strings.HasPrefix(r, "en current ") {
					current = append(current, r)
				}
			}
			if want := "en current " + base + "/" + slug + "/child"; !slices.Equal(current, []string{want}) {
				t.Fatalf("round %d: child routes %q, want %q", round, current, want)
			}
		}
	})
}
