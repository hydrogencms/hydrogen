package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func TestPagesBlockedMountedItemRecovers(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, news := e.placed("section", "News", &root, nil)
		art := e.article("en", `{"title":"Hello World"}`)
		art2 := e.article("en", `{"title":"Second Post"}`)
		_, posts := e.placed("section", "Posts", &news, nil)
		hello, _ := e.placed("landing", "Hello World", &posts, nil)
		second, _ := e.placed("landing", "Second Post", &posts, nil)
		e.mount(news, pages.MountSpec{Segment: "posts", Collection: "article", Route: "{slug}"})
		// Both pages hold the paths the mount would give the articles, so the articles get no route.
		e.wantRoutes(art)
		e.wantRoutes(art2)
		e.wantPage("en", "/news/posts/hello-world", hello)

		// Renaming a page frees its path and the article takes it.
		e.update(hello, "en", `{"slug":"hello-page"}`)
		e.mustPublish(hello)
		e.wantRoutes(art, "en current /news/posts/hello-world")
		if r := e.resolve("en", "/news/posts/hello-world"); r.Kind != pages.ResolvedItem || *r.Item != art {
			t.Errorf("article path = %+v", r)
		}
		e.wantRoutes(art2)

		// Deleting a page frees its path too.
		if err := e.c.Delete(ctx, second); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art2, "en current /news/posts/second-post")
	})
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestPagesRejectsUnsafeTargets(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		for _, to := range []string{`/\evil.example`, "/a\x01b", "/a\x7fb", `https://example.com/\x`, "item:nope"} {
			_, err := e.redirect(fmt.Sprintf(`{"from":"/x","to":%s}`, jsonString(to)))
			if !slices.Equal(validation(t, err), []string{"to:invalid_target"}) {
				t.Errorf("redirect to %q: %v", to, err)
			}
		}
		for _, from := range []string{`/a\b`, "/a\tb", "/a b", "a"} {
			_, err := e.redirect(fmt.Sprintf(`{"from":%s,"to":"/y"}`, jsonString(from)))
			if !slices.Equal(validation(t, err), []string{"from:invalid_path"}) {
				t.Errorf("redirect from %q: %v", from, err)
			}
		}
		for _, u := range []string{`/\evil.example`, "/a\x01b", `https://example.com/\x`, "https://example.com/\x7f"} {
			_, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: u}})
			if !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("link %q: %v", u, err)
			}
		}
		ok, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: "/fine"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Update(ctx, ok.ID, pages.NodeUpdate{Link: &pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: `/\evil`}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("link update: %v", err)
		}
	})
}

func TestPagesRedirectLoops(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		e.placed("home", "Home", nil, nil)
		if _, err := e.redirect(`{"from":"/a","to":"/b"}`); err != nil {
			t.Fatal(err)
		}
		if _, err := e.redirect(`{"from":"/b","to":"/a"}`); !slices.Equal(validation(t, err), []string{"to:redirect_loop"}) {
			t.Errorf("two-redirect loop: %v", err)
		}
		if _, err := e.redirect(`{"from":"/self","to":"/self?x=1"}`); !slices.Equal(validation(t, err), []string{"to:redirect_loop"}) {
			t.Errorf("self loop: %v", err)
		}
		if _, err := e.redirect(`{"from":"/c","to":"/d"}`); err != nil {
			t.Fatal(err)
		}
		if _, err := e.redirect(`{"from":"/d","to":"/e"}`); err != nil {
			t.Fatal(err)
		}
		if _, err := e.redirect(`{"from":"/e","to":"/c"}`); !slices.Equal(validation(t, err), []string{"to:redirect_loop"}) {
			t.Errorf("three-redirect loop: %v", err)
		}
		// A long chain without a loop: ten hops are fine, eleven are not.
		for i := 11; i >= 2; i-- {
			if _, err := e.redirect(fmt.Sprintf(`{"from":"/chain%d","to":"/chain%d"}`, i, i+1)); err != nil {
				t.Fatalf("chain%d: %v", i, err)
			}
		}
		if _, err := e.redirect(`{"from":"/chain1","to":"/chain2"}`); !slices.Equal(validation(t, err), []string{"to:redirect_loop"}) {
			t.Errorf("long chain: %v", err)
		}
		// Republishing a redirect over its own earlier row is not a loop.
		id, err := e.redirect(`{"from":"/again","to":"/elsewhere"}`)
		if err != nil {
			t.Fatal(err)
		}
		e.update(id, "en", `{"to":"/elsewhere-2"}`)
		e.mustPublish(id)
		e.wantRedirect("en", "/again", "/elsewhere-2", 301)
	})
}

func TestPagesLegacyRedirectPaths(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		about, _ := e.placed("section", "About", &root, nil)
		for from, to := range map[string]string{"/about.html": "/about", "/Old/Page": "/about", "/old/dir/": "/about", "/index.php?id=3": "/about"} {
			if _, err := e.redirect(fmt.Sprintf(`{"from":%s,"to":%s}`, jsonString(from), jsonString(to))); err != nil {
				t.Fatalf("redirect from %s: %v", from, err)
			}
			e.wantRedirect("en", from, to, 301)
		}
		e.wantNotFound("en", "/old/page")
		e.wantNotFound("en", "/about.htm")
		e.wantNotFound("en", "/old/dir")
		e.wantPage("en", "/about", about)
		// Paths that cannot be matched at all stay not found.
		e.wantNotFound("en", "/a b")
		e.wantNotFound("en", `/a\b`)
		e.wantNotFound("en", "/"+strings.Repeat("x", 1100))
	})
}

func TestPagesDepthAndPathLength(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, parent := e.placed("home", "Home", nil, nil)
		// The root is at depth 0; 32 levels below it are allowed.
		var chain []ids.UUID
		for i := range 32 {
			_, n := e.placed("section", fmt.Sprintf("S%d", i), &parent, nil)
			chain = append(chain, n)
			parent = n
		}
		extra := e.page("section", "Too deep")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: extra, Parent: &parent}); !errors.Is(err, content.ErrBadRequest) ||
			!strings.Contains(err.Error(), "levels deep") {
			t.Errorf("place at depth 33: %v", err)
		}
		// Moving a subtree down past the limit fails as well.
		_, side := e.placed("section", "Side", &chain[0], nil)
		_, sideKid := e.placed("section", "Side kid", &side, nil)
		if _, err := e.pages.Update(ctx, side, pages.NodeUpdate{Move: &pages.Move{Parent: chain[30]}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("move past depth 32: %v", err)
		}
		if _, err := e.pages.Update(ctx, sideKid, pages.NodeUpdate{Move: &pages.Move{Parent: chain[30]}}); err != nil {
			t.Errorf("move to depth 32: %v", err)
		}
	})
}

func TestPagesPathLength(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		long := func(i int) string { return strings.Repeat("a", 119) + fmt.Sprint(i) }
		_, root := e.placed("home", "Home", nil, nil)
		top, topN := e.placed("section", "T", &root, nil)
		parent := topN
		var deepest ids.UUID
		for i := range 8 {
			deepest, parent = e.placed("section", long(i), &parent, nil)
		}
		// 970 bytes now. One more level, or a longer top slug, would pass 1024.
		extra := e.page("section", long(9))
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: extra, Parent: &parent}); !errors.Is(err, content.ErrBadRequest) ||
			!strings.Contains(err.Error(), "1024") {
			t.Errorf("place with a 1091 byte path: %v", err)
		}
		e.update(top, "en", fmt.Sprintf(`{"slug":%q}`, long(0)))
		if got := validation(t, e.publish(top)); !slices.Equal(got, []string{"slug:path_too_long"}) {
			t.Errorf("publish over the limit: %v", got)
		}
		e.wantRoutes(deepest, "en current /t/"+strings.Join(slices.Collect(func(yield func(string) bool) {
			for i := range 8 {
				if !yield(long(i)) {
					return
				}
			}
		}), "/"))
		// Moving a subtree to where its paths pass the limit is refused.
		_, other := e.placed("section", long(10), &root, nil)
		_, mid := e.placed("section", long(11), &other, nil)
		if _, err := e.pages.Update(ctx, topN, pages.NodeUpdate{Move: &pages.Move{Parent: mid}}); !errors.Is(err, content.ErrBadRequest) ||
			!strings.Contains(err.Error(), "1024") {
			t.Errorf("move past the path limit: %v", err)
		}
	})
}

func TestPagesBounds(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		spec := func(mod func(*pages.MountSpec)) error {
			s := pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"}
			mod(&s)
			_, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: root, Spec: s})
			return err
		}
		labels := map[string]string{}
		for i := range 51 {
			labels[fmt.Sprintf("l%02d", i)] = "x"
		}
		for name, mod := range map[string]func(*pages.MountSpec){
			"51 label locales": func(s *pages.MountSpec) { s.Label = labels },
			"long label":       func(s *pages.MountSpec) { s.Label = map[string]string{"en": strings.Repeat("é", 201)} },
			"17 filter keys": func(s *pages.MountSpec) {
				s.Filter = map[string]any{}
				for i := range 17 {
					s.Filter[fmt.Sprintf("f%d", i)] = "x"
				}
			},
		} {
			if err := spec(mod); !errors.Is(err, content.ErrBadRequest) || !strings.Contains(err.Error(), "at most") && !strings.Contains(err.Error(), "longer") {
				t.Errorf("%s: %v", name, err)
			}
		}
		if err := spec(func(s *pages.MountSpec) { s.Label = map[string]string{"en": strings.Repeat("é", 200)} }); err != nil {
			t.Errorf("a 200 character label: %v", err)
		}
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{URL: "/x", Label: labels}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("link with 51 label locales: %v", err)
		}
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{URL: "/x", Label: map[string]string{"en": strings.Repeat("x", 201)}}}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("link with a long label: %v", err)
		}
		if _, err := e.pages.Tree(ctx, "main", "en", nil, 11); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("tree depth 11: %v", err)
		}
		if _, err := e.pages.Tree(ctx, "main", "en", nil, 10); err != nil {
			t.Errorf("tree depth 10: %v", err)
		}
	})
}

func TestPagesLinksToDeletedTargetsAreCleared(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		page, pageN := e.placed("landing", "Landing", &root, nil)
		mount := e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		art := e.article("en", `{"title":"Story"}`)
		add := func(label string, spec pages.LinkSpec) *pages.Node {
			spec.Label = map[string]string{"en": label}
			n, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: spec})
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		lItem := add("item", pages.LinkSpec{Item: &art})
		lPage := add("page", pages.LinkSpec{Node: &pageN})
		lMount := add("mount", pages.LinkSpec{Node: &mount.ID})
		lPageItem := add("page item", pages.LinkSpec{Item: &page})
		lURL := add("url", pages.LinkSpec{URL: "/x"})
		targets := func() map[string]pages.TreeNode {
			ts, err := e.pages.Tree(ctx, "main", "en", nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			out := map[string]pages.TreeNode{}
			for _, n := range ts {
				out[n.Name] = n
			}
			return out
		}
		got := targets()
		for _, name := range []string{"item", "page", "mount", "page item", "url"} {
			if got[name].Target == "" || got[name].Broken {
				t.Errorf("before: %s link = %+v", name, got[name])
			}
		}
		hardDelete(t, e.c, art)
		if err := e.pages.DeleteNode(ctx, mount.ID); err != nil {
			t.Fatal(err)
		}
		hardDelete(t, e.c, page)
		got = targets()
		for _, name := range []string{"item", "page", "mount", "page item"} {
			n := got[name]
			if n.Target != "" || !n.Broken || n.Link == nil || n.Link.Item != nil || n.Link.Node != nil {
				t.Errorf("after: %s link = %+v", name, n)
			}
		}
		if n := got["url"]; n.Target != "/x" || n.Broken {
			t.Errorf("url link = %+v", n)
		}
		for _, l := range []*pages.Node{lItem, lPage, lMount, lPageItem, lURL} {
			if _, err := e.pages.Node(ctx, l.ID); err != nil {
				t.Errorf("link %s: %v", l.ID, err)
			}
		}
		// A broken link can be pointed somewhere else.
		other := e.page("landing", "Other")
		on, err := e.pages.Place(ctx, "main", pages.Place{Item: other, Parent: &root})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Update(ctx, lItem.ID, pages.NodeUpdate{Link: &pages.LinkSpec{Label: map[string]string{"en": "item"}, Node: &on.ID}}); err != nil {
			t.Fatal(err)
		}
		if n := targets()["item"]; n.Target != "/other" || n.Broken {
			t.Errorf("repointed link = %+v", n)
		}
	})
}

func (e *pagesEnv) sql(query string, args ...any) (int64, error) {
	var n int64
	err := e.db.Tx(ctx, func(tx store.Tx) error {
		var err error
		n, err = tx.SQL().Exec(ctx, query, args...)
		return err
	})
	return n, err
}

func TestPagesReorderRecomputesNothing(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		_, a := e.placed("section", "Alpha", &root, nil)
		kid, _ := e.placed("landing", "Kid", &a, nil)
		_, b := e.placed("section", "Beta", &root, &a)
		// Break the child's route behind the module's back: a recomputation would put it back.
		if n, err := e.sql("DELETE FROM route WHERE item_id = ?", kid); err != nil || n != 1 {
			t.Fatalf("delete route: %d, %v", n, err)
		}
		e.move(a, root, &b)
		e.wantRoutes(kid)
		e.move(b, root, nil)
		e.wantRoutes(kid)
		// A real move does recompute.
		e.move(a, b, nil)
		e.wantRoutes(kid, "en current /beta/alpha/kid")
	})
}

func TestPagesParentForeignKeyRestricts(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		parent, parentN := e.placed("section", "Parent", &root, nil)
		e.placed("landing", "Kid", &parentN, nil)
		_, err := e.sql("DELETE FROM page_node WHERE id = ?", parentN)
		if !errors.Is(err, store.ErrConstraint) {
			t.Errorf("raw delete of a parent: %v", err)
		}
		if err := e.c.Delete(ctx, parent); !errors.Is(err, content.ErrConflict) || !strings.Contains(err.Error(), "children") {
			t.Errorf("content delete of a parent: %v", err)
		}
	})
}

func TestPagesMountsSeenAfterChange(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		// The publish hooks cache mounts per collection; this node's own changes must show at once.
		before := e.article("en", `{"title":"Before"}`)
		e.wantRoutes(before)
		m := e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		after := e.article("en", `{"title":"After"}`)
		e.wantRoutes(before, "en current /news/before")
		e.wantRoutes(after, "en current /news/after")
		if _, err := e.pages.Update(ctx, m.ID, pages.NodeUpdate{Mount: &pages.MountSpec{Segment: "blog", Collection: "article", Route: "{slug}"}}); err != nil {
			t.Fatal(err)
		}
		third := e.article("en", `{"title":"Third"}`)
		e.wantRoutes(third, "en current /blog/third")
		if err := e.pages.DeleteNode(ctx, m.ID); err != nil {
			t.Fatal(err)
		}
		fourth := e.article("en", `{"title":"Fourth"}`)
		e.wantRoutes(fourth)
	})
}
