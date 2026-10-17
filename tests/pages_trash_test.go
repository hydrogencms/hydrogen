package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func treeNames(t *testing.T, e *pagesEnv) []string {
	t.Helper()
	nodes, err := e.pages.Tree(ctx, "main", "en", nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func([]pages.TreeNode)
	walk = func(ns []pages.TreeNode) {
		for _, n := range ns {
			out = append(out, n.Name)
			walk(n.Nodes)
		}
	}
	walk(nodes)
	return out
}

func TestPagesTrashAndRestore(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		parent, parentN := e.placed("section", "Docs", &root, nil)
		leaf, leafN := e.placed("landing", "Guide", &parentN, nil)
		e.wantPage("en", "/docs/guide", leaf)

		// A page with children refuses both: its children's paths hang below it.
		for what, err := range map[string]error{
			"trash":     e.c.Trash(ctx, parent),
			"unpublish": unpublishOnly(e, parent),
		} {
			var coded interface{ APICode() string }
			if !errors.Is(err, content.ErrConflict) || !errors.As(err, &coded) || coded.APICode() != "has_children" ||
				!strings.Contains(err.Error(), "child") {
				t.Errorf("%s with children: %v", what, err)
			}
		}
		e.wantPage("en", "/docs", parent)
		e.wantPage("en", "/docs/guide", leaf)

		if err := e.c.Trash(ctx, leaf); err != nil {
			t.Fatal(err)
		}
		e.drain()
		e.wantRoutes(leaf)
		e.wantNotFound("en", "/docs/guide")
		if names := treeNames(t, e); strings.Join(names, ",") != "Docs" {
			t.Errorf("tree after trash = %v", names)
		}
		// The node stays, so restoring and publishing puts the page back where it was.
		if _, err := e.pages.Node(ctx, leafN); err != nil {
			t.Errorf("node of a trashed page: %v", err)
		}
		if _, err := e.c.Restore(ctx, leaf); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(leaf)
		e.wantNotFound("en", "/docs/guide")
		if names := treeNames(t, e); strings.Join(names, ",") != "Docs,Guide" {
			t.Errorf("tree after restore = %v", names)
		}
		e.mustPublish(leaf)
		e.wantPage("en", "/docs/guide", leaf)
		e.wantRoutes(leaf, "en current /docs/guide")

		// A trashed page cannot be placed anywhere; a purged one frees its node.
		if err := e.c.Trash(ctx, leaf); err != nil {
			t.Fatal(err)
		}
		if err := e.c.Purge(ctx, leaf); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Node(ctx, leafN); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("node after purge: %v", err)
		}
		e.drain()
		hidden := e.create("landing", "en", `{"title":"Hidden"}`)
		if err := e.c.Trash(ctx, hidden); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: hidden, Parent: &parentN}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("placing a trashed page: %v", err)
		}
		// The parent has no children left now.
		if err := e.c.Trash(ctx, parent); err != nil {
			t.Fatal(err)
		}
		e.wantNotFound("en", "/docs")
	})
}

func unpublishOnly(e *pagesEnv, id ids.UUID) error {
	_, err := e.c.Unpublish(ctx, id)
	return err
}

func TestPagesUnpublish(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		page, _ := e.placed("landing", "About", &root, nil)
		art := e.article("en", `{"title":"Post"}`)
		e.mount(root, pages.MountSpec{Segment: "blog", Collection: "article", Route: "{slug}"})
		e.wantPage("en", "/about", page)
		e.wantRoutes(art, "en current /blog/post")

		if _, err := e.c.Unpublish(ctx, page); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(page)
		e.wantNotFound("en", "/about")
		if _, err := e.c.Unpublish(ctx, art); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art)
		e.wantNotFound("en", "/blog/post")
		e.drain() // the reconcile consumer finds nothing to bring back

		e.wantRoutes(page)
		e.mustPublish(page)
		e.wantPage("en", "/about", page)
		e.mustPublish(art)
		e.wantRoutes(art, "en current /blog/post")

		// Trashing a mounted item removes its routes too, and restoring it does not bring them back.
		if err := e.c.Trash(ctx, art); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(art)
		e.wantNotFound("en", "/blog/post")
		if _, err := e.c.Restore(ctx, art); err != nil {
			t.Fatal(err)
		}
		e.drain()
		e.wantRoutes(art)
		e.mustPublish(art)
		e.wantRoutes(art, "en current /blog/post")
	})
}

func TestPagesRefuseTrashedParent(t *testing.T) {
	runPages(t, pagesOpts{}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		parent, parentN := e.placed("section", "Docs", &root, nil)
		other, otherN := e.placed("section", "Other", &root, nil)
		_ = other
		if err := e.c.Trash(ctx, parent); err != nil {
			t.Fatal(err)
		}
		item := e.page("landing", "Guide")
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &parentN}); !errors.Is(err, content.ErrConflict) {
			t.Fatalf("place under a trashed parent: %v", err)
		}
		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: &otherN}); err != nil {
			t.Fatal(err)
		}
		n, err := e.pages.Place(ctx, "main", pages.Place{Item: e.page("landing", "Mover"), Parent: &otherN})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Update(ctx, n.ID, pages.NodeUpdate{Move: &pages.Move{Parent: parentN}}); !errors.Is(err, content.ErrConflict) {
			t.Fatalf("move under a trashed parent: %v", err)
		}
		// Restored, it takes children again.
		if _, err := e.c.Restore(ctx, parent); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pages.Update(ctx, n.ID, pages.NodeUpdate{Move: &pages.Move{Parent: parentN}}); err != nil {
			t.Fatal(err)
		}
	})
}

// An export leaves out trashed pages, their slugs and routes (history included) and what hangs below them, and
// the result imports cleanly.
func TestPagesExportSkipsTrashed(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newPagesEnv(t, b.open(t, clk), clk, pagesOpts{})
		_, root := e.placed("home", "Home", nil, nil)
		keep, _ := e.placed("section", "Keep", &root, nil)
		gone, goneN := e.placed("section", "Gone", &root, nil)
		e.update(gone, "en", `{"slug":"renamed"}`)
		e.mustPublish(gone)
		e.wantRoutes(gone, "en current /renamed", "en redirect /gone")
		if err := e.c.Trash(ctx, gone); err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(gone, "en redirect /gone") // history kept while trashed
		// A link to the trashed page loses its target in the export.
		link, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: root, Spec: pages.LinkSpec{Label: map[string]string{"en": "Gone"}, Node: &goneN}})
		if err != nil {
			t.Fatal(err)
		}
		_ = link

		dir, _ := exportTo(t, e, false)
		clk2 := newClock()
		clk2.Advance(48 * 3600e9)
		dst := newPagesEnv(t, b.open(t, clk2), clk2, pagesOpts{})
		if _, err := importFrom(t, dst, dir, false); err != nil {
			t.Fatalf("import: %v", err)
		}
		if _, err := dst.pages.Node(ctx, goneN); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("trashed page's node after import: %v", err)
		}
		dst.wantRoutes(gone)
		dst.wantNotFound("en", "/gone")
		dst.wantRoutes(keep, "en current /keep")
		dst.wantPage("en", "/keep", keep)
		n, err := dst.pages.Node(ctx, link.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n.Link == nil || n.Link.Node != nil || n.Link.Item != nil {
			t.Errorf("link after import = %+v", n.Link)
		}
	})
}
