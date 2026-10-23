package tests

import (
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/modules/pages"
)

func crumbPaths(r *pages.Resolution) []string {
	var out []string
	for _, c := range r.Breadcrumbs {
		out = append(out, c.Path)
	}
	return out
}

func TestPagesCanonicalPath(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		seo, seoN := e.placed("section", "SEO", &root, nil)
		team, _ := e.placed("landing", "Team", &seoN, nil)
		e.wantRoutes(seo, "en current /seo")
		e.wantRoutes(team, "en current /seo/team")

		// The canonical path is the page's current route; the tree path redirects to it and children build on it.
		e.update(seo, "en", `{"canonicalPath":"/services/search"}`)
		e.mustPublish(seo)
		e.wantRoutes(seo, "en current /services/search", "en redirect /seo")
		e.wantPage("en", "/services/search", seo)
		e.wantRedirect("en", "/seo", "/services/search", 301)
		e.wantRoutes(team, "en current /services/search/team", "en redirect /seo/team")
		r := e.wantPage("en", "/services/search/team", team)
		if got := crumbPaths(r); !slices.Equal(got, []string{"/", "/services/search", "/services/search/team"}) {
			t.Errorf("breadcrumbs = %q", got)
		}
		e.wantRedirect("en", "/seo/team", "/services/search/team", 301)

		ns, err := e.pages.Tree(ctx, "main", "en", &seoN, 1)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, n := range ns {
			paths = append(paths, n.Path)
		}
		if !slices.Equal(paths, []string{"/services/search/team"}) {
			t.Errorf("tree paths = %q", paths)
		}

		// A page published with an override from the start gets its tree path as a redirect too.
		fresh := e.create("landing", "en", `{"title":"Fresh","canonicalPath":"/promo/fresh"}`)
		e.mustPublish(fresh)
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: fresh, Parent: &root}); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(fresh, "en current /promo/fresh", "en redirect /fresh")
		e.wantRedirect("en", "/fresh", "/promo/fresh", 301)

		// Changing the override leaves a history redirect and moves the children.
		e.update(seo, "en", `{"canonicalPath":"/services/seo"}`)
		e.mustPublish(seo)
		e.wantRoutes(seo, "en current /services/seo", "en redirect /seo", "en redirect /services/search")
		e.wantRedirect("en", "/services/search", "/services/seo", 301)
		e.wantRedirect("en", "/services/search/team", "/services/seo/team", 301)
		e.wantPage("en", "/services/seo/team", team)

		// A path another page holds is refused, as are malformed and root paths.
		other, _ := e.placed("landing", "Other", &root, nil)
		e.update(other, "en", `{"canonicalPath":"/services/seo"}`)
		if got := validation(t, e.publish(other)); !slices.Equal(got, []string{"canonicalPath:slug_taken"}) {
			t.Errorf("taken override: %v", got)
		}
		e.update(other, "en", `{"canonicalPath":"/"}`)
		if got := validation(t, e.publish(other)); !slices.Equal(got, []string{"canonicalPath:invalid_path"}) {
			t.Errorf("root override: %v", got)
		}
		e.update(other, "en", `{"canonicalPath":"services/x/"}`)
		if got := validation(t, e.publish(other)); !slices.Equal(got, []string{"canonicalPath:invalid_path"}) {
			t.Errorf("malformed override: %v", got)
		}

		// A tree path that redirects to a canonical path does not block another page from taking it.
		e.update(other, "en", `{"canonicalPath":"","slug":"seo"}`)
		e.mustPublish(other)
		e.wantRoutes(other, "en current /seo", "en redirect /other")
		e.wantPage("en", "/seo", other)
		e.wantRoutes(seo, "en current /services/seo", "en redirect /services/search")

		// Removing the override puts the page back in the tree.
		e.update(seo, "en", `{"canonicalPath":"","slug":"seo-old"}`)
		e.mustPublish(seo)
		e.wantRoutes(seo, "en current /seo-old", "en redirect /services/search", "en redirect /services/seo")
		e.wantPage("en", "/seo-old/team", team)
	})
}

func TestPagesCanonicalPathPerLocale(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, aboutN := e.placed("section", "About us", &root, nil)
		e.translate(about, "de", "Ueber uns")
		team, _ := e.placed("landing", "Team", &aboutN, nil)
		e.translate(team, "de", "Mannschaft")

		// The override belongs to one locale; the other keeps the tree.
		e.update(about, "de", `{"canonicalPath":"/firma"}`)
		e.mustPublish(about, "de")
		e.wantRoutes(about, "en current /about-us", "de current /firma", "de redirect /ueber-uns")
		e.wantRoutes(team, "en current /about-us/team", "de current /firma/mannschaft", "de redirect /ueber-uns/mannschaft")
		e.wantPage("de", "/firma/mannschaft", team)
		e.wantPage("en", "/about-us/team", team)
	})
}
