package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var editorialTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"TrashItem", testTrashItem},
	{"TrashKeys", testTrashKeys},
	{"TrashQueries", testTrashQueries},
	{"TrashSortAndSummary", testTrashSortAndSummary},
	{"TrashExport", testTrashExport},
	{"TrashUnpublishPrimitives", testUnpublishPrimitives},
}

func setDeleted(t *testing.T, db store.DB, id ids.UUID, at *time.Time, by string) {
	t.Helper()
	withContent(t, db, func(c store.ContentStore) error { return c.SetDeleted(context.Background(), id, at, by) })
}

func testTrashItem(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, b := g.New(), g.New()
	withContent(t, db, func(c store.ContentStore) error {
		for _, it := range []store.Item{newItem(a, "article", "k"), newItem(b, "article", "")} {
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
		}
		return nil
	})
	at := t0.Add(time.Hour + 321*time.Millisecond)
	setDeleted(t, db, a, &at, "user:9")
	viewContent(t, db, func(c store.ContentStore) error {
		got, err := c.GetItem(ctx, a, false)
		if err != nil || got.DeletedAt == nil || !got.DeletedAt.Equal(at) || got.DeletedAt.Location() != time.UTC || got.DeletedBy != "user:9" {
			t.Fatalf("trashed item: %+v %v", got, err)
		}
		if got.Key != "k" || got.UpdatedBy != "user:2" {
			t.Fatalf("SetDeleted changed other columns: %+v", got)
		}
		items, err := c.GetItems(ctx, []ids.UUID{a, b})
		if err != nil || len(items) != 2 {
			t.Fatalf("GetItems returns trashed items too: %+v %v", items, err)
		}
		if _, err := c.GetItemByKey(ctx, "k"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("by key of a trashed item: %v", err)
		}
		list, err := c.ItemIDsOfType(ctx, "article", ids.UUID{}, 10)
		if err != nil || len(list) != 2 {
			t.Fatalf("ItemIDsOfType includes trashed items: %v %v", list, err)
		}
		return nil
	})

	// UpdateItem leaves the trash columns alone.
	withContent(t, db, func(c store.ContentStore) error {
		it, err := c.GetItem(ctx, a, true)
		if err != nil {
			return err
		}
		it.UpdatedBy, it.DeletedAt, it.DeletedBy = "user:5", nil, ""
		return c.UpdateItem(ctx, it)
	})
	viewContent(t, db, func(c store.ContentStore) error {
		got, _ := c.GetItem(ctx, a, false)
		if got.DeletedAt == nil || got.DeletedBy != "user:9" || got.UpdatedBy != "user:5" {
			t.Fatalf("UpdateItem touched the trash columns: %+v", got)
		}
		return nil
	})

	// Restore clears both columns; trashing again overwrites.
	setDeleted(t, db, a, nil, "ignored")
	viewContent(t, db, func(c store.ContentStore) error {
		got, _ := c.GetItem(ctx, a, false)
		if got.DeletedAt != nil || got.DeletedBy != "" {
			t.Fatalf("restored: %+v", got)
		}
		if got, err := c.GetItemByKey(ctx, "k"); err != nil || got.ID != a {
			t.Fatalf("by key after restore: %+v %v", got, err)
		}
		return nil
	})
	setDeleted(t, db, a, &at, "user:1")
	setDeleted(t, db, a, &at, "user:2")
	viewContent(t, db, func(c store.ContentStore) error {
		if got, _ := c.GetItem(ctx, a, false); got.DeletedBy != "user:2" {
			t.Fatalf("second trash: %+v", got)
		}
		return nil
	})

	if err := expectErr(db, func(c store.ContentStore) error { return c.SetDeleted(ctx, g.New(), &at, "x") }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trash unknown: %v", err)
	}
	if err := expectErr(db, func(c store.ContentStore) error { return c.SetDeleted(ctx, g.New(), nil, "") }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("restore unknown: %v", err)
	}

	// An item inserted trashed (an import) is trashed.
	n := g.New()
	it := newItem(n, "article", "")
	it.DeletedAt, it.DeletedBy = &at, "user:3"
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(ctx, it) })
	viewContent(t, db, func(c store.ContentStore) error {
		got, err := c.GetItem(ctx, n, false)
		if err != nil || !sameItem(got, it) {
			t.Fatalf("inserted trashed:\n got %+v\nwant %+v\n(%v)", got, it, err)
		}
		return nil
	})
}

// testTrashKeys: a key is unique among items that are not in the trash.
func testTrashKeys(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, b, c3, d := g.New(), g.New(), g.New(), g.New()
	at := t0.Add(time.Hour)
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(ctx, newItem(a, "page", "home")) })
	if err := expectErr(db, func(c store.ContentStore) error { return c.InsertItem(ctx, newItem(b, "page", "home")) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("duplicate before trash: %v", err)
	}
	setDeleted(t, db, a, &at, "user:1")
	// The key is free.
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(ctx, newItem(b, "page", "home")) })
	viewContent(t, db, func(c store.ContentStore) error {
		if got, err := c.GetItemByKey(ctx, "home"); err != nil || got.ID != b {
			t.Fatalf("by key: %+v %v", got, err)
		}
		return nil
	})
	// Restoring while the key is taken fails and changes nothing.
	if err := expectErr(db, func(c store.ContentStore) error { return c.SetDeleted(ctx, a, nil, "") }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("restore into a taken key: %v", err)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		if got, _ := c.GetItem(ctx, a, false); got.DeletedAt == nil {
			t.Fatal("failed restore changed the item")
		}
		return nil
	})
	// Two trashed items may share a key, but only one of them can come back.
	setDeleted(t, db, b, &at, "user:1")
	setDeleted(t, db, a, nil, "")
	if err := expectErr(db, func(c store.ContentStore) error { return c.SetDeleted(ctx, b, nil, "") }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("second restore: %v", err)
	}
	// Keys can be changed to a trashed item's key, and a trashed item's key can be edited freely.
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.InsertItem(ctx, newItem(d, "page", "other")); err != nil {
			return err
		}
		return c.InsertItem(ctx, newItem(c3, "page", "c3"))
	})
	setDeleted(t, db, c3, &at, "user:1")
	withContent(t, db, func(c store.ContentStore) error {
		it, err := c.GetItem(ctx, d, true)
		if err != nil {
			return err
		}
		it.Key = "tmp"
		return c.UpdateItem(ctx, it)
	})
	withContent(t, db, func(c store.ContentStore) error {
		it, err := c.GetItem(ctx, c3, true)
		if err != nil {
			return err
		}
		it.Key = "home" // taken by the live item a: allowed while trashed
		return c.UpdateItem(ctx, it)
	})
	if err := expectErr(db, func(c store.ContentStore) error {
		it, err := c.GetItem(ctx, d, true)
		if err != nil {
			return err
		}
		it.Key = "home"
		return c.UpdateItem(ctx, it)
	}); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("update into a live key: %v", err)
	}
	if err := expectErr(db, func(c store.ContentStore) error { return c.SetDeleted(ctx, c3, nil, "") }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("restore after a key edit: %v", err)
	}
}

// testTrashQueries: every query kind hides trashed items by default and honours Trashed.
func testTrashQueries(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	withContent(t, db, func(c store.ContentStore) error {
		for l, docs := range map[byte][]store.SearchDoc{
			'A': {{Locale: "en", Text: "quick brown fox"}},
			'B': {{Locale: "en", Text: "fox fox fox"}},
			'C': {{Locale: "en", Text: "nothing"}},
			'E': {{Locale: "en", Text: "quick fox"}},
		} {
			if err := c.ReplaceSearch(ctx, cat.ids[l], store.ProjectionPublished, docs); err != nil {
				return err
			}
			if err := c.ReplaceSearch(ctx, cat.ids[l], store.ProjectionWorking, docs); err != nil {
				return err
			}
		}
		return nil
	})
	trashed := "BE"
	at := t0.Add(time.Hour)
	for _, l := range []byte(trashed) {
		setDeleted(t, db, cat.ids[l], &at, "user:1")
	}

	rating := []store.Sort{{Field: "rating", Column: store.ColNum}}
	ratingDesc := []store.Sort{{Field: "rating", Column: store.ColNum, Desc: true}}
	scope := []store.ScopeTerm{{CreatedBy: []string{"user:1"}}}
	cases := []struct {
		name string
		q    store.Query
		all  string // matches when trashed items are included
	}{
		{"type", store.Query{Type: "article"}, "ABCDE"},
		{"every type", store.Query{}, "ABCDEP"},
		{"published", store.Query{Type: "article", Status: store.StatusPublished}, "ABCE"},
		{"draft", store.Query{Type: "article", Status: store.StatusDraft}, "DE"},
		{"eq driver", store.Query{Where: where("rating", store.ColNum, store.OpEq, 3.0)}, "BCP"},
		{"contains driver", store.Query{Where: where("tags", store.ColText, store.OpContains, "go")}, "AB"},
		{"in driver", store.Query{Where: where("title", store.ColText, store.OpIn, []string{"apple", "banana"})}, "AB"},
		{"range", store.Query{Where: where("rating", store.ColNum, store.OpGte, 3.0)}, "ABCEP"},
		{"negative", store.Query{Type: "article", Where: where("rating", store.ColNum, store.OpNe, 3.0)}, "ADE"},
		{"walk asc", store.Query{Type: "article", Sort: rating}, "ABCDE"},
		{"walk desc", store.Query{Type: "article", Sort: ratingDesc}, "ABCDE"},
		{"walk with filter", store.Query{Where: where("rating", store.ColNum, store.OpGte, 3.0), Sort: rating}, "ABCEP"},
		{"search", store.Query{Locale: "en", Search: "fox"}, "ABE"},
		{"search ranked", store.Query{Locale: "en", Search: "fox", Sort: []store.Sort{{Field: "rating", Column: store.ColNum}}}, "ABE"},
		{"working", store.Query{Type: "article", Working: true}, "ABCDE"},
		{"working search", store.Query{Locale: "en", Search: "quick", Working: true}, "AE"},
		{"scoped", store.Query{Type: "article", Scoped: true, Scope: scope}, "ABCDE"},
		{"available", store.Query{Type: "article", Available: []string{"en"}}, "ABCE"},
	}
	want := func(all, mode string) string {
		var out []byte
		for _, l := range []byte(all) {
			in := slices.Contains([]byte(trashed), l)
			if mode == "include" || mode == "only" == in {
				out = append(out, l)
			}
		}
		return string(out)
	}
	modes := []struct {
		name string
		t    store.Trashed
	}{{"exclude", store.TrashedExclude}, {"only", store.TrashedOnly}, {"include", store.TrashedInclude}}
	for _, tc := range cases {
		for _, m := range modes {
			q := tc.q
			q.Trashed = m.t
			if q.Limit == 0 {
				q.Limit = 500
			}
			exp := want(tc.all, m.name)
			res := cat.run(t, db, q)
			if got := sortString(cat.letters(res)); got != exp || res.Total != len(exp) {
				t.Errorf("%s/%s: %q total %d, want %q", tc.name, m.name, got, res.Total, exp)
			}
			// A one-row page still reports the full total.
			q.Limit = 1
			if res := cat.run(t, db, q); res.Total != len(exp) || len(res.Items) != min(1, len(exp)) {
				t.Errorf("%s/%s paged: %d items, total %d, want total %d", tc.name, m.name, len(res.Items), res.Total, len(exp))
			}
			// Past the end: the total is still right.
			q.Offset = 50
			if res := cat.run(t, db, q); len(res.Items) != 0 || res.Total != len(exp) {
				t.Errorf("%s/%s offset: %d items, total %d, want total %d", tc.name, m.name, len(res.Items), res.Total, len(exp))
			}
		}
		// The default is exclude, on the delivery reader too.
		q := tc.q
		q.Limit = 500
		res, err := db.Reader().Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if exp := want(tc.all, "exclude"); sortString(cat.letters(res)) != exp || res.Total != len(exp) {
			t.Errorf("%s default via reader: %q total %d, want %q", tc.name, cat.letters(res), res.Total, exp)
		}
	}
	if err := expectErr(db, func(c store.ContentStore) error {
		_, err := c.Query(ctx, store.Query{Trashed: 9})
		return err
	}); err == nil {
		t.Error("an unknown Trashed value is accepted")
	}
}

func testTrashSortAndSummary(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	at1, at2 := t0.Add(time.Hour+5*time.Millisecond), t0.Add(2*time.Hour)
	setDeleted(t, db, cat.ids['A'], &at1, "user:1")
	setDeleted(t, db, cat.ids['C'], &at2, "user:2")
	setDeleted(t, db, cat.ids['B'], &at2, "user:3")
	res := cat.run(t, db, store.Query{Type: "article", Trashed: store.TrashedOnly, Locale: "en",
		Sort: []store.Sort{{Field: "deletedAt", Desc: true}, {Field: "createdAt"}}})
	if got := cat.letters(res); got != "BCA" && got != "CBA" {
		t.Fatalf("by deletedAt desc: %q", got)
	}
	// Ties on deletedAt break on createdAt: B (created before C) first.
	if got := cat.letters(res); got != "BCA" {
		t.Fatalf("tie order: %q", got)
	}
	last := res.Items[2]
	if last.DeletedAt == nil || !last.DeletedAt.Equal(at1) || last.DeletedBy != "user:1" || last.Name != "Alpha" {
		t.Fatalf("summary of a trashed item: %+v", last)
	}
	res = cat.run(t, db, store.Query{Type: "article", Trashed: store.TrashedOnly, Sort: []store.Sort{{Field: "deletedAt"}}})
	if got := cat.letters(res); got[0] != 'A' {
		t.Fatalf("by deletedAt asc: %q", got)
	}
	if res = cat.run(t, db, store.Query{Type: "article"}); cat.letters(res) == "" || res.Items[0].DeletedAt != nil {
		t.Fatalf("live summaries carry DeletedAt: %+v", res.Items)
	}
}

func testTrashExport(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	list := make([]ids.UUID, 5)
	withContent(t, db, func(c store.ContentStore) error {
		for i := range list {
			list[i] = g.New()
			if err := c.InsertItem(ctx, newItem(list[i], "article", "")); err != nil {
				return err
			}
		}
		return nil
	})
	at := t0.Add(time.Hour)
	setDeleted(t, db, list[1], &at, "user:1")
	setDeleted(t, db, list[3], &at, "user:1")
	err := db.View(ctx, func(tx store.Tx) error {
		ex, err := store.ExportOf(tx)
		if err != nil {
			return err
		}
		n, err := ex.CountItems(ctx)
		if err != nil || n != 3 {
			t.Fatalf("CountItems = %d, %v; want 3 (trashed items are not exported)", n, err)
		}
		var got []ids.UUID
		for after := (ids.UUID{}); ; {
			page, err := ex.ItemsAfter(ctx, after, 2)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			for _, it := range page {
				got = append(got, it.ID)
			}
			after = page[len(page)-1].ID
		}
		want := []ids.UUID{list[0], list[2], list[4]}
		slices.SortFunc(want, func(a, b ids.UUID) int { return slices.Compare(a[:], b[:]) })
		if !slices.Equal(got, want) {
			t.Fatalf("ItemsAfter = %v, want %v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// testUnpublishPrimitives checks that the existing store calls remove everything delivery reads, which is how
// the content service unpublishes (there is no ContentStore.Unpublish).
func testUnpublishPrimitives(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	a, x := cat.ids['A'], cat.x
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.PutSnapshots(ctx, a, []store.Snapshot{snap(a, "article", "a-key", "en", 1, `{"a":1}`, x)}); err != nil {
			return err
		}
		if err := c.ReplaceRefs(ctx, a, []store.Ref{{From: a, FieldPath: "author", To: x, Kind: "field", OnDelete: "block"}}); err != nil {
			return err
		}
		return c.ReplaceSearch(ctx, a, store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: "unpublishable"}})
	})
	published := func(q store.Query) bool {
		res, err := db.Reader().Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(res.Items, func(s store.ItemSummary) bool { return s.ID == a })
	}
	byTitle := store.Query{Where: where("title", store.ColText, store.OpEq, "apple"), Status: store.StatusPublished}
	bySearch := store.Query{Locale: "en", Search: "unpublishable", Status: store.StatusPublished}
	if !published(byTitle) || !published(bySearch) {
		t.Fatal("seed: item not found while published")
	}
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.DeleteSnapshots(ctx, a); err != nil {
			return err
		}
		if err := c.ReplaceRefs(ctx, a, nil); err != nil {
			return err
		}
		if err := c.ReplaceIndex(ctx, a, store.ProjectionPublished, nil); err != nil {
			return err
		}
		if err := c.ReplaceSearch(ctx, a, store.ProjectionPublished, nil); err != nil {
			return err
		}
		it, err := c.GetItem(ctx, a, true)
		if err != nil {
			return err
		}
		it.PublishedVersion = 0
		return c.UpdateItem(ctx, it)
	})
	if published(byTitle) || published(bySearch) {
		t.Error("an unpublished item is still found")
	}
	if snaps, err := db.Reader().Snapshots(ctx, []ids.UUID{a}, nil); err != nil || len(snaps) != 0 {
		t.Errorf("snapshots after unpublish: %v %v", snaps, err)
	}
	if snaps, err := db.Reader().SnapshotsByKey(ctx, "a-key", nil); err != nil || len(snaps) != 0 {
		t.Errorf("snapshots by key after unpublish: %v %v", snaps, err)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		if refs, err := c.IncomingRefs(ctx, x); err != nil || len(refs) != 0 {
			t.Errorf("refs after unpublish: %v %v", refs, err)
		}
		// Versions and drafts stay.
		if vs, err := c.ListVersions(ctx, a); err != nil || len(vs) != 1 {
			t.Errorf("versions after unpublish: %v %v", vs, err)
		}
		return nil
	})
}
