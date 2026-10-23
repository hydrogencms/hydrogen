package tests

import (
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/modules/pages"
)

func (e *pagesEnv) childNames(node pages.Node) []string {
	e.t.Helper()
	ts, err := e.pages.Tree(ctx, "main", "en", &node.ID, 1)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, n := range ts {
		out = append(out, n.Name)
	}
	return out
}

func TestPagesPlaceAppends(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		home, err := e.pages.Node(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		names := func() []string { return e.childNames(*home) }

		// Without a position, nodes land last: created in order, listed in order.
		var firstN pages.Node
		for _, title := range []string{"One", "Two", "Three"} {
			item := e.page("section", title)
			n, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root})
			if err != nil {
				t.Fatal(err)
			}
			if title == "One" {
				firstN = *n
			}
		}
		if got := names(); !slices.Equal(got, []string{"One", "Two", "Three"}) {
			t.Fatalf("appended = %q", got)
		}

		// First puts a node at the head, After right behind a sibling.
		item := e.page("section", "Zero")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root, First: true}); err != nil {
			t.Fatal(err)
		}
		item = e.page("section", "OneAndAHalf")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root, After: &firstN.ID}); err != nil {
			t.Fatal(err)
		}
		if got := names(); !slices.Equal(got, []string{"Zero", "One", "OneAndAHalf", "Two", "Three"}) {
			t.Errorf("positioned = %q", got)
		}

		// Both at once is a mistake.
		item = e.page("section", "Both")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &root, First: true, After: &firstN.ID}); err == nil {
			t.Error("first together with after was accepted")
		}

		// Mounts and links append too.
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{
			Label: map[string]string{"en": "Out"}, URL: "/out"}}); err != nil {
			t.Fatal(err)
		}
		e.mount(root, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}", Label: map[string]string{"en": "News"}})
		got := names()
		if n := len(got); n < 2 || got[n-2] != "Out" || got[n-1] != "News" {
			t.Errorf("link and mount = %q", got)
		}
	})
}
