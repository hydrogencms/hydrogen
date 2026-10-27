package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func TestPagesUnplaced(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		list := func(c context.Context, item *ids.UUID, limit, offset int) ([]ids.UUID, int) {
			t.Helper()
			res, err := e.pages.Unplaced(c, "en", item, limit, offset)
			if err != nil {
				t.Fatal(err)
			}
			var out []ids.UUID
			for _, it := range res.Items {
				out = append(out, it.ID)
			}
			return out, res.Total
		}
		if got, total := list(ctx, nil, 50, 0); len(got) != 0 || total != 0 {
			t.Fatalf("a fully placed tree has unplaced items: %v", got)
		}

		first := e.page("landing", "First")
		draft := e.create("section", "en", `{"title":"Second"}`)
		third := e.page("section", "Third")
		gone := e.page("landing", "Gone")
		if err := e.c.Trash(ctx, gone); err != nil {
			t.Fatal(err)
		}
		e.article("en", `{"title":"Not a page"}`)

		want := []ids.UUID{third, draft, first}
		got, total := list(ctx, nil, 50, 0)
		if !slices.Equal(got, want) || total != 3 {
			t.Fatalf("unplaced = %v (%d), want %v", got, total, want)
		}
		res, _ := e.pages.Unplaced(ctx, "en", nil, 50, 0)
		if res.Items[0].Name != "Third" || res.Items[0].Status != "published" || res.Items[1].Name != "Second" || res.Items[1].Status != "draft" {
			t.Errorf("rows = %+v", res.Items)
		}

		page1, total := list(ctx, nil, 2, 0)
		page2, _ := list(ctx, nil, 2, 2)
		if total != 3 || !slices.Equal(append(page1, page2...), want) {
			t.Errorf("paging: %v + %v", page1, page2)
		}
		if one, total := list(ctx, &first, 50, 0); len(one) != 1 || one[0] != first || total != 1 {
			t.Errorf("single item: %v", one)
		}
		if one, _ := list(ctx, &e.home, 50, 0); len(one) != 0 {
			t.Errorf("a placed item is listed: %v", one)
		}

		if _, err := e.pages.Place(ctx, "main", pages.Place{Item: first, Parent: &e.aN}); err != nil {
			t.Fatal(err)
		}
		if got, _ := list(ctx, nil, 50, 0); !slices.Equal(got, []ids.UUID{third, draft}) {
			t.Errorf("after placing: %v", got)
		}

		// Readers see only items they can read: a reader limited to a site sees none of the unplaced ones.
		site := e.as("shop-only", pol("pages.read", "pages.site", "shop"), pol("content.read", "pages.site", "shop"))
		if got, total := list(site, nil, 50, 0); len(got) != 0 || total != 0 {
			t.Errorf("site-limited reader sees %v (%d)", got, total)
		}
		all := e.as("reader", pol("content.read", "collection", "section"))
		if got, total := list(all, nil, 50, 0); !slices.Equal(got, []ids.UUID{third, draft}) || total != 2 {
			t.Errorf("type-limited reader sees %v (%d)", got, total)
		}
	})
}
