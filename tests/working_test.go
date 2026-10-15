package tests

import (
	"fmt"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

func (e *env) names(o content.ListOptions) []string {
	e.t.Helper()
	o.Type = "article"
	if o.Sort == nil {
		o.Sort = []content.SortSpec{{Field: "name"}}
	}
	res, err := e.svc.List(ctx, o)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, it := range res.Items {
		out = append(out, it.Name)
	}
	return out
}

func rated(op store.Op, v any, status store.Status) content.ListOptions {
	return content.ListOptions{Status: status, Where: []content.Filter{{Field: "rating", Op: op, Value: v}}}
}

// delivered runs a listing the way the delivery API does: through ListQuery, on the published rows.
func (e *env) delivered(o content.ListOptions) []string {
	e.t.Helper()
	o.Type = "article"
	q, err := e.svc.ListQuery(o)
	if err != nil {
		e.t.Fatal(err)
	}
	if q.Working {
		e.t.Fatalf("ListQuery asks for the working rows: %+v", q)
	}
	var res store.QueryResult
	if err := e.db.View(ctx, func(tx store.Tx) (err error) {
		res, err = tx.Content().Query(ctx, q)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, it := range res.Items {
		out = append(out, fmt.Sprint(it.PublishedVersion))
	}
	return out
}

func TestWorkingNeverPublishedItem(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Hidden gem","rating":4,"tags":["draftonly"]}`)
		e.article("Public")

		eqSlice(t, "filter", e.names(rated(store.OpEq, 4, store.StatusAny)), []string{"Hidden gem"})
		eqSlice(t, "draft status", e.names(rated(store.OpEq, 4, store.StatusDraft)), []string{"Hidden gem"})
		eqSlice(t, "contains", e.names(content.ListOptions{Where: []content.Filter{{Field: "tags", Op: store.OpContains, Value: "draftonly"}}}), []string{"Hidden gem"})
		eqSlice(t, "search", e.names(content.ListOptions{Search: "gem"}), []string{"Hidden gem"})
		eqSlice(t, "published list", e.names(rated(store.OpEq, 4, store.StatusPublished)), nil)
		eqSlice(t, "published search", e.names(content.ListOptions{Search: "gem", Status: store.StatusPublished}), nil)
		eqSlice(t, "delivery", e.delivered(rated(store.OpEq, 4, store.StatusPublished)), nil)
		eqSlice(t, "sort by indexed field", e.names(content.ListOptions{Sort: []content.SortSpec{{Field: "rating", Desc: true}}}), []string{"Hidden gem", "Public"})

		if err := e.svc.Delete(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "deleted", e.names(rated(store.OpEq, 4, store.StatusAny)), nil)
		eqSlice(t, "deleted search", e.names(content.ListOptions{Search: "gem"}), nil)
	})
}

func TestWorkingEditedPublishedItem(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Original","summary":"oldsummary","rating":1}`)
		e.publish(v.ID)
		e.update(v.ID, "en", `{"title":"Rewritten","summary":"newsummary","rating":4}`)

		eqSlice(t, "management by draft value", e.names(rated(store.OpEq, 4, store.StatusAny)), []string{"Rewritten"})
		eqSlice(t, "management by old value", e.names(rated(store.OpEq, 1, store.StatusAny)), nil)
		eqSlice(t, "management search new title", e.names(content.ListOptions{Search: "newsummary"}), []string{"Rewritten"})
		eqSlice(t, "management search old text", e.names(content.ListOptions{Search: "oldsummary"}), nil)
		eqSlice(t, "published-only by published value", e.names(rated(store.OpEq, 1, store.StatusPublished)), []string{"Original"})
		eqSlice(t, "published-only by draft value", e.names(rated(store.OpEq, 4, store.StatusPublished)), nil)
		eqSlice(t, "published-only search", e.names(content.ListOptions{Search: "oldsummary", Status: store.StatusPublished}), []string{"Original"})
		eqSlice(t, "delivery by published value", e.delivered(rated(store.OpEq, 1, store.StatusPublished)), []string{"1"})
		eqSlice(t, "delivery by draft value", e.delivered(rated(store.OpEq, 4, store.StatusPublished)), nil)
		eqSlice(t, "delivery preview listing (any status) still reads published rows", e.delivered(rated(store.OpEq, 4, store.StatusAny)), nil)

		// Discard restores the published view.
		if err := e.svc.Discard(ctx, v.ID, nil); err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "after discard, draft value", e.names(rated(store.OpEq, 4, store.StatusAny)), nil)
		eqSlice(t, "after discard, published value", e.names(rated(store.OpEq, 1, store.StatusAny)), []string{"Original"})
		eqSlice(t, "after discard, search", e.names(content.ListOptions{Search: "newsummary"}), nil)

		// Publish moves both projections.
		e.update(v.ID, "en", `{"title":"Rewritten","summary":"newsummary","rating":4}`)
		e.publish(v.ID)
		eqSlice(t, "after publish, management", e.names(rated(store.OpEq, 4, store.StatusAny)), []string{"Rewritten"})
		eqSlice(t, "after publish, published", e.names(rated(store.OpEq, 4, store.StatusPublished)), []string{"Rewritten"})
		eqSlice(t, "after publish, old value", e.names(rated(store.OpEq, 1, store.StatusAny)), nil)
	})
}

func TestWorkingLocales(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Hello","rating":2}`)
		e.publish(v.ID)
		e.update(v.ID, "de", `{"title":"Hallo Welt","slug":"hallo-welt"}`)
		de := func(o content.ListOptions) []string { o.Locale = "de"; return e.names(o) }

		eqSlice(t, "draft-only locale by search", de(content.ListOptions{Search: "welt"}), []string{"Hallo Welt"})
		eqSlice(t, "draft-only locale by slug", de(content.ListOptions{Where: []content.Filter{{Field: "slug", Op: store.OpEq, Value: "hallo-welt"}}}), []string{"Hallo Welt"})
		eqSlice(t, "draft-only locale, published list", de(content.ListOptions{Search: "welt", Status: store.StatusPublished}), nil)
		eqSlice(t, "shared field in the other locale", de(rated(store.OpEq, 2, store.StatusAny)), []string{"Hallo Welt"})

		// A shared draft edit moves the shared index rows for every locale.
		e.update(v.ID, "en", `{"rating":5}`)
		eqSlice(t, "shared edit, en", e.names(rated(store.OpEq, 5, store.StatusAny)), []string{"Hello"})
		eqSlice(t, "shared edit, de", de(rated(store.OpEq, 5, store.StatusAny)), []string{"Hallo Welt"})
		eqSlice(t, "shared edit, old value", de(rated(store.OpEq, 2, store.StatusAny)), nil)

		if err := e.svc.Discard(ctx, v.ID, []string{"shared"}); err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "after discarding the shared part", e.names(rated(store.OpEq, 2, store.StatusAny)), []string{"Hello"})
		eqSlice(t, "the locale draft stays", de(rated(store.OpEq, 2, store.StatusAny)), []string{"Hallo Welt"})

		e.publish(v.ID, "de")
		eqSlice(t, "after publishing de, search", de(content.ListOptions{Search: "welt", Status: store.StatusPublished}), []string{"Hallo Welt"})
		eqSlice(t, "after publishing de, not drafts", de(content.ListOptions{Search: "welt", Status: store.StatusDraft}), nil)
	})
}

func TestWorkingSingleton(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		if _, err := e.svc.UpdateSingleton(ctx, "settings", content.UpdateInput{Fields: doc(t, `{"siteName":"Acme Corp"}`)}); err != nil {
			t.Fatal(err)
		}
		list := func(status store.Status) int {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "settings", Search: "acme", Status: status})
			if err != nil {
				t.Fatal(err)
			}
			return res.Total
		}
		eq(t, "draft singleton by search", list(store.StatusAny), 1)
		eq(t, "published singleton by search", list(store.StatusPublished), 0)
		if _, err := e.svc.PublishSingleton(ctx, "settings", content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		eq(t, "published singleton after publish", list(store.StatusPublished), 1)
	})
}

func TestWorkingUpgrade(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		pub := e.article("Published one")
		e.update(pub.ID, "en", `{"rating":3}`)
		draft := e.create("article", "en", `{"title":"Fresh draft","rating":3}`)
		wipe := func() {
			t.Helper()
			err := e.db.Tx(ctx, func(tx store.Tx) error {
				for _, id := range []ids.UUID{pub.ID, draft.ID} {
					if err := tx.Content().ReplaceIndex(ctx, id, store.ProjectionWorking, nil); err != nil {
						return err
					}
					if err := tx.Content().ReplaceSearch(ctx, id, store.ProjectionWorking, nil); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		wipe() // the state of a database that predates working rows
		eqSlice(t, "before the upgrade step", e.names(rated(store.OpEq, 3, store.StatusAny)), nil)

		built, err := e.svc.EnsureWorkingProjections(ctx)
		if err != nil || !built {
			t.Fatalf("first run = %v, %v", built, err)
		}
		eqSlice(t, "after the upgrade step", e.names(rated(store.OpEq, 3, store.StatusAny)), []string{"Fresh draft", "Published one"})
		eqSlice(t, "search after the upgrade step", e.names(content.ListOptions{Search: "fresh"}), []string{"Fresh draft"})

		wipe()
		if built, err = e.svc.EnsureWorkingProjections(ctx); err != nil || built {
			t.Fatalf("second run = %v, %v; the step is recorded and must not repeat", built, err)
		}
		eqSlice(t, "recorded step does not rebuild", e.names(rated(store.OpEq, 3, store.StatusAny)), nil)

		n, err := e.svc.Reindex(ctx, nil)
		if err != nil || n != 2 {
			t.Fatalf("reindex = %d, %v", n, err)
		}
		eqSlice(t, "after reindex", e.names(rated(store.OpEq, 3, store.StatusAny)), []string{"Fresh draft", "Published one"})
		eqSlice(t, "reindex leaves published rows", e.names(rated(store.OpEq, 3, store.StatusPublished)), nil)
		eqSlice(t, "published rows", e.names(rated(store.OpEq, 0, store.StatusPublished)), nil)
		e.checkSnapshotsComplete()
	})
}
