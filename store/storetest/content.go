package storetest

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

var contentTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"ContentSchemaRevs", testSchemaRevs},
	{"ContentItems", testItems},
	{"ContentVersions", testVersions},
	{"ContentDraftParts", testDraftParts},
	{"ContentDraftPartsOf", testDraftPartsOf},
	{"ContentPublishedLocalesOf", testPublishedLocalesOf},
	{"ContentMainNamesOf", testMainNamesOf},
	{"ContentRefs", testRefs},
	{"ContentIndex", testIndexReplace},
	{"ContentSearch", testSearch},
	{"ContentSearchHuge", testSearchHuge},
	{"ContentQueryFilters", testQueryFilters},
	{"ContentQuerySort", testQuerySort},
	{"ContentQueryPaging", testQueryPaging},
	{"ContentQueryWalk", testQueryWalk},
	{"ContentQueryAvailable", testQueryAvailable},
	{"ContentQueryNames", testQueryNames},
	{"ContentQueryInvalid", testQueryInvalid},
	{"ContentDeleteCascade", testDeleteCascade},
	{"ContentMigrations", testContentMigrations},
	{"ContentItemIDsOfType", testItemIDsOfType},
	{"ContentWorkingIndex", testWorkingIndex},
	{"ContentWorkingSearch", testWorkingSearch},
	{"ContentWorkingNames", testWorkingNames},
	{"ContentModuleIndex", testModuleIndex},
	{"ContentQueryScope", testQueryScope},
}

func withContent(t *testing.T, db store.DB, fn func(store.ContentStore) error) {
	t.Helper()
	if err := db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Content()) }); err != nil {
		t.Fatal(err)
	}
}

func viewContent(t *testing.T, db store.DB, fn func(store.ContentStore) error) {
	t.Helper()
	if err := db.View(context.Background(), func(tx store.Tx) error { return fn(tx.Content()) }); err != nil {
		t.Fatal(err)
	}
}

// expectErr runs fn in its own transaction and returns the error, so a failure never poisons a later step
// (Postgres aborts a transaction on any error).
func expectErr(db store.DB, fn func(store.ContentStore) error) error {
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Content()) })
}

func newIDs() *ids.Generator { return ids.NewGenerator(clock.NewManual(t0), nil) }

func str(s string) *string { return &s }
func txt(loc, field, v string) store.IndexRow {
	return store.IndexRow{Locale: loc, Field: field, Text: &v}
}
func num(loc, field string, v float64) store.IndexRow {
	return store.IndexRow{Locale: loc, Field: field, Num: &v}
}
func tim(loc, field string, v time.Time) store.IndexRow {
	return store.IndexRow{Locale: loc, Field: field, Time: &v}
}
func bln(loc, field string, v bool) store.IndexRow {
	return store.IndexRow{Locale: loc, Field: field, Bool: &v}
}
func rfr(loc, field string, v ids.UUID) store.IndexRow {
	return store.IndexRow{Locale: loc, Field: field, Ref: &v}
}

func newItem(id ids.UUID, typ, key string) store.Item {
	return store.Item{
		ID: id, Key: key, Type: typ, MainLocale: "en", AlwaysAvailable: true,
		CreatedAt: t0.Add(123 * time.Millisecond), UpdatedAt: t0.Add(456 * time.Millisecond),
		CreatedBy: "user:1", UpdatedBy: "user:2",
	}
}

func testSchemaRevs(t *testing.T, db store.DB) {
	ctx := context.Background()
	withContent(t, db, func(c store.ContentStore) error {
		if rev, hash, compiled, err := c.LatestSchemaRev(ctx); err != nil || rev != 0 || hash != "" || compiled != nil {
			t.Fatalf("empty latest: %d %q %q %v", rev, hash, compiled, err)
		}
		if _, ok, err := c.SchemaRev(ctx, "h1"); err != nil || ok {
			t.Fatalf("unknown hash: %v %v", ok, err)
		}
		r1, err := c.AddSchemaRev(ctx, "h1", []byte(`{"types":{"a":1}}`), t0)
		if err != nil || r1 != 1 {
			t.Fatalf("first rev: %d %v", r1, err)
		}
		if again, err := c.AddSchemaRev(ctx, "h1", []byte(`{"other":true}`), t0.Add(time.Hour)); err != nil || again != r1 {
			t.Fatalf("AddSchemaRev must be idempotent on hash: %d %v", again, err)
		}
		r2, err := c.AddSchemaRev(ctx, "h2", []byte(`{"types":{"b":2}}`), t0)
		if err != nil || r2 != 2 {
			t.Fatalf("second rev: %d %v", r2, err)
		}
		rev, hash, compiled, err := c.LatestSchemaRev(ctx)
		if err != nil || rev != 2 || hash != "h2" || !jsonEqual(compiled, `{"types":{"b":2}}`) {
			t.Fatalf("latest: %d %q %s %v", rev, hash, compiled, err)
		}
		if rev, ok, err := c.SchemaRev(ctx, "h1"); err != nil || !ok || rev != 1 {
			t.Fatalf("lookup h1: %d %v %v", rev, ok, err)
		}
		return nil
	})
}

func testItems(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, b, c3, c4 := g.New(), g.New(), g.New(), g.New()

	ia := newItem(a, "article", "first")
	ia.PublishedVersion = 3
	withContent(t, db, func(c store.ContentStore) error {
		for _, it := range []store.Item{ia, newItem(b, "article", ""), newItem(c3, "article", "")} {
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
		}
		return nil
	})

	if err := expectErr(db, func(c store.ContentStore) error { return c.InsertItem(ctx, newItem(c4, "page", "first")) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("duplicate key insert: %v", err)
	}

	viewContent(t, db, func(c store.ContentStore) error {
		got, err := c.GetItem(ctx, a, false)
		if err != nil || !sameItem(got, ia) {
			t.Fatalf("round trip:\n got %+v\nwant %+v\n(%v)", got, ia, err)
		}
		if got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(t0.Add(123*time.Millisecond)) {
			t.Fatalf("created at: %v", got.CreatedAt)
		}
		if got, err := c.GetItemByKey(ctx, "first"); err != nil || got.ID != a {
			t.Fatalf("by key: %+v %v", got, err)
		}
		if got, err := c.GetItem(ctx, b, false); err != nil || got.Key != "" {
			t.Fatalf("keyless item: %+v %v", got, err)
		}
		if _, err := c.GetItem(ctx, c4, false); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing item: %v", err)
		}
		if _, err := c.GetItemByKey(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing key: %v", err)
		}
		if _, err := c.GetItemByKey(ctx, ""); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("empty key must not match keyless items: %v", err)
		}
		many, err := c.GetItems(ctx, []ids.UUID{c3, c4, a})
		if err != nil || len(many) != 2 {
			t.Fatalf("GetItems skips missing ids: %+v %v", many, err)
		}
		slices.SortFunc(many, func(x, y store.Item) int { return strings.Compare(x.ID.String(), y.ID.String()) })
		if many[0].ID != a || many[1].ID != c3 {
			t.Fatalf("GetItems: %+v", many)
		}
		if none, err := c.GetItems(ctx, nil); err != nil || len(none) != 0 {
			t.Fatalf("GetItems(nil): %+v %v", none, err)
		}
		return nil
	})

	upd := ia
	upd.Key = "renamed"
	upd.MainLocale = "de"
	upd.AlwaysAvailable = false
	upd.PublishedVersion = 4
	upd.UpdatedAt = t0.Add(time.Hour)
	upd.UpdatedBy = "user:3"
	withContent(t, db, func(c store.ContentStore) error { return c.UpdateItem(ctx, upd) })
	viewContent(t, db, func(c store.ContentStore) error {
		got, err := c.GetItem(ctx, a, false)
		if err != nil || !sameItem(got, upd) {
			t.Fatalf("after update:\n got %+v\nwant %+v\n(%v)", got, upd, err)
		}
		if _, err := c.GetItemByKey(ctx, "first"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("old key still resolves: %v", err)
		}
		return nil
	})

	clash := upd
	clash.ID = b
	clash.Key = "renamed"
	if err := expectErr(db, func(c store.ContentStore) error { return c.UpdateItem(ctx, clash) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("update to a taken key: %v", err)
	}
	missing := upd
	missing.ID = c4
	if err := expectErr(db, func(c store.ContentStore) error { return c.UpdateItem(ctx, missing) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update of a missing item: %v", err)
	}

	// Clearing a key frees it.
	upd.Key = ""
	withContent(t, db, func(c store.ContentStore) error { return c.UpdateItem(ctx, upd) })
	withContent(t, db, func(c store.ContentStore) error {
		it := newItem(c4, "page", "renamed")
		return c.InsertItem(ctx, it)
	})

	// forUpdate returns the same row.
	withContent(t, db, func(c store.ContentStore) error {
		got, err := c.GetItem(ctx, a, true)
		if err != nil || got.ID != a {
			t.Fatalf("GetItem forUpdate: %+v %v", got, err)
		}
		return nil
	})

	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, a) })
	if err := expectErr(db, func(c store.ContentStore) error { return c.DeleteItem(ctx, a) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		if _, err := c.GetItem(ctx, a, false); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted item: %v", err)
		}
		return nil
	})
}

func testVersions(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	id := g.New()
	empty := g.New()
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.InsertItem(ctx, newItem(id, "article", "")); err != nil {
			return err
		}
		if err := c.InsertItem(ctx, newItem(empty, "article", "")); err != nil {
			return err
		}
		if _, err := c.AddSchemaRev(ctx, "h", []byte(`{}`), t0); err != nil {
			return err
		}
		v1 := store.Version{ItemID: id, Version: 1, SchemaRev: 1, Shared: []byte(`{"rating":4,"big":12345678901234567,"tags":["a","b"],"nested":{"x":null,"y":[1.5,true]}}`),
			Locales: []store.VersionLocale{
				{Locale: "en", Name: "Hello", Data: []byte(`{"title":"Hello \"quoted\" é <b>"}`)},
				{Locale: "de", Name: "Hallo", Data: []byte(`{"title":"Hallo"}`)},
			},
			CreatedAt: t0.Add(time.Second + 7*time.Millisecond), CreatedBy: "user:1"}
		v2 := store.Version{ItemID: id, Version: 2, SchemaRev: 1, Shared: []byte(`{}`),
			Locales:   []store.VersionLocale{{Locale: "en", Name: "Hello 2", Data: []byte(`{}`)}},
			CreatedAt: t0.Add(2 * time.Second), CreatedBy: "user:2"}
		for _, v := range []store.Version{v1, v2, {ItemID: empty, Version: 1, SchemaRev: 1, Shared: []byte(`{}`), CreatedAt: t0}} {
			if err := c.InsertVersion(ctx, v); err != nil {
				return err
			}
		}
		return nil
	})

	viewContent(t, db, func(c store.ContentStore) error {
		v, err := c.GetVersion(ctx, id, 1)
		if err != nil {
			return err
		}
		if v.ItemID != id || v.Version != 1 || v.SchemaRev != 1 || v.CreatedBy != "user:1" || !v.CreatedAt.Equal(t0.Add(time.Second+7*time.Millisecond)) || v.CreatedAt.Location() != time.UTC {
			t.Fatalf("version meta: %+v", v)
		}
		if !jsonEqual(v.Shared, `{"rating":4,"big":12345678901234567,"tags":["a","b"],"nested":{"x":null,"y":[1.5,true]}}`) {
			t.Fatalf("shared: %s", v.Shared)
		}
		if len(v.Locales) != 2 || v.Locales[0].Locale != "de" || v.Locales[1].Locale != "en" {
			t.Fatalf("locales must be sorted: %+v", v.Locales)
		}
		if v.Locales[0].Name != "Hallo" || !jsonEqual(v.Locales[0].Data, `{"title":"Hallo"}`) ||
			!jsonEqual(v.Locales[1].Data, `{"title":"Hello \"quoted\" é <b>"}`) {
			t.Fatalf("locale documents: %+v", v.Locales)
		}
		if _, err := c.GetVersion(ctx, id, 3); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing version: %v", err)
		}
		if bare, err := c.GetVersion(ctx, empty, 1); err != nil || len(bare.Locales) != 0 {
			t.Fatalf("version without locales: %+v %v", bare, err)
		}

		list, err := c.ListVersions(ctx, id)
		if err != nil || len(list) != 2 {
			t.Fatalf("list: %+v %v", list, err)
		}
		if list[0].Version != 2 || list[1].Version != 1 || !slices.Equal(list[0].Locales, []string{"en"}) ||
			!slices.Equal(list[1].Locales, []string{"de", "en"}) || list[0].CreatedBy != "user:2" || list[1].SchemaRev != 1 {
			t.Fatalf("list newest first: %+v", list)
		}
		if l, err := c.ListVersions(ctx, empty); err != nil || len(l) != 1 || len(l[0].Locales) != 0 {
			t.Fatalf("list of a version without locales: %+v %v", l, err)
		}
		if l, err := c.ListVersions(ctx, g.New()); err != nil || len(l) != 0 {
			t.Fatalf("list of unknown item: %+v %v", l, err)
		}
		return nil
	})

	if err := expectErr(db, func(c store.ContentStore) error {
		return c.InsertVersion(ctx, store.Version{ItemID: id, Version: 1, SchemaRev: 1, Shared: []byte(`{}`), CreatedAt: t0})
	}); err == nil {
		t.Fatal("duplicate version accepted")
	}
}

func testDraftParts(t *testing.T, db store.DB) {
	ctx := context.Background()
	id, other := newIDs().New(), ids.UUID{9}
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.InsertItem(ctx, newItem(id, "article", "")); err != nil {
			return err
		}
		return c.InsertItem(ctx, newItem(other, "article", ""))
	})
	part := func(item ids.UUID, p, name, data string, base int) store.DraftPart {
		return store.DraftPart{ItemID: item, Part: p, BaseVersion: base, Name: name, Data: []byte(data),
			UpdatedAt: t0.Add(1500 * time.Millisecond), UpdatedBy: "user:1"}
	}
	withContent(t, db, func(c store.ContentStore) error {
		for _, p := range []store.DraftPart{
			part(id, "en", "Hello", `{"title":"Hello"}`, 0), part(id, "", "", `{"rating":1}`, 0),
			part(id, "de", "Hallo", `{"title":"Hallo"}`, 2), part(other, "en", "Other", `{}`, 0),
		} {
			if err := c.PutDraftPart(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
	get := func(item ids.UUID) []store.DraftPart {
		var out []store.DraftPart
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			out, err = c.GetDraftParts(ctx, item)
			return err
		})
		return out
	}
	parts := func(list []store.DraftPart) []string {
		out := make([]string, len(list))
		for i, p := range list {
			out[i] = p.Part
		}
		return out
	}
	got := get(id)
	if !slices.Equal(parts(got), []string{"", "de", "en"}) {
		t.Fatalf("sorted by part: %v", parts(got))
	}
	if got[1].BaseVersion != 2 || got[1].Name != "Hallo" || !jsonEqual(got[1].Data, `{"title":"Hallo"}`) ||
		!got[1].UpdatedAt.Equal(t0.Add(1500*time.Millisecond)) || got[1].UpdatedBy != "user:1" || got[1].ItemID != id {
		t.Fatalf("part round trip: %+v", got[1])
	}

	upsert := part(id, "en", "Hello again", `{"title":"Hello again"}`, 3)
	upsert.UpdatedAt = t0.Add(time.Minute)
	withContent(t, db, func(c store.ContentStore) error { return c.PutDraftPart(ctx, upsert) })
	got = get(id)
	if len(got) != 3 || got[2].Name != "Hello again" || got[2].BaseVersion != 3 || !got[2].UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("upsert: %+v", got)
	}

	withContent(t, db, func(c store.ContentStore) error { return c.DeleteDraftParts(ctx, id, []string{}) })
	if len(get(id)) != 3 {
		t.Fatal("deleting an empty list of parts removed something")
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteDraftParts(ctx, id, []string{"de", "fr"}) })
	if got := parts(get(id)); !slices.Equal(got, []string{"", "en"}) {
		t.Fatalf("after deleting de: %v", got)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteDraftParts(ctx, id, nil) })
	if got := get(id); len(got) != 0 {
		t.Fatalf("nil deletes all parts: %+v", got)
	}
	if len(get(other)) != 1 {
		t.Fatal("another item's parts were deleted")
	}
}

func testDraftPartsOf(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, b, none, missing := g.New(), g.New(), g.New(), g.New()
	withContent(t, db, func(c store.ContentStore) error {
		for _, id := range []ids.UUID{a, b, none} {
			if err := c.InsertItem(ctx, newItem(id, "article", "")); err != nil {
				return err
			}
		}
		for _, p := range []store.DraftPart{{ItemID: a, Part: "fr"}, {ItemID: a, Part: ""}, {ItemID: a, Part: "de"}, {ItemID: b, Part: "en"}} {
			p.Data, p.UpdatedAt = []byte(`{}`), t0
			if err := c.PutDraftPart(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
	var got map[ids.UUID][]string
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		got, err = c.DraftPartsOf(ctx, []ids.UUID{a, b, none, missing})
		return err
	})
	if len(got) != 2 || !slices.Equal(got[a], []string{"", "de", "fr"}) || !slices.Equal(got[b], []string{"en"}) {
		t.Fatalf("draft parts of: %v", got)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		got, err = c.DraftPartsOf(ctx, nil)
		return err
	})
	if len(got) != 0 {
		t.Fatalf("no items: %v", got)
	}
}

func testPublishedLocalesOf(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, never, missing := g.New(), g.New(), g.New()
	withContent(t, db, func(c store.ContentStore) error {
		if _, err := c.AddSchemaRev(ctx, "h", []byte(`{}`), t0); err != nil {
			return err
		}
		pub := newItem(a, "article", "")
		pub.PublishedVersion = 1
		if err := c.InsertItem(ctx, pub); err != nil {
			return err
		}
		if err := c.InsertItem(ctx, newItem(never, "article", "")); err != nil {
			return err
		}
		v := store.Version{ItemID: a, Version: 1, SchemaRev: 1, Shared: []byte(`{}`), CreatedAt: t0}
		for _, loc := range []string{"fr", "de", "en"} {
			v.Locales = append(v.Locales, store.VersionLocale{Locale: loc, Name: loc, Data: []byte(`{}`)})
		}
		return c.InsertVersion(ctx, v)
	})
	var got map[ids.UUID][]string
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		got, err = c.PublishedLocalesOf(ctx, []ids.UUID{a, never, missing})
		return err
	})
	if len(got) != 1 || !slices.Equal(got[a], []string{"de", "en", "fr"}) {
		t.Fatalf("published locales of: %v", got)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		got, err = c.PublishedLocalesOf(ctx, nil)
		return err
	})
	if len(got) != 0 {
		t.Fatalf("no items: %v", got)
	}
}

func testMainNamesOf(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	drafted, published, both, bare, missing := g.New(), g.New(), g.New(), g.New(), g.New()
	withContent(t, db, func(c store.ContentStore) error {
		if _, err := c.AddSchemaRev(ctx, "h", []byte(`{}`), t0); err != nil {
			return err
		}
		for _, id := range []ids.UUID{drafted, published, both, bare} {
			it := newItem(id, "article", "")
			if id == published || id == both {
				it.PublishedVersion = 1
			}
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
		}
		for _, id := range []ids.UUID{published, both} {
			v := store.Version{ItemID: id, Version: 1, SchemaRev: 1, Shared: []byte(`{}`), CreatedAt: t0,
				Locales: []store.VersionLocale{{Locale: "en", Name: "Published", Data: []byte(`{}`)}, {Locale: "de", Name: "Veroeffentlicht", Data: []byte(`{}`)}}}
			if err := c.InsertVersion(ctx, v); err != nil {
				return err
			}
		}
		for id, name := range map[ids.UUID]string{drafted: "Drafted", both: "Draft name"} {
			if err := c.PutDraftPart(ctx, store.DraftPart{ItemID: id, Part: "en", Name: name, Data: []byte(`{}`), UpdatedAt: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	var got map[ids.UUID]string
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		got, err = c.MainNamesOf(ctx, []ids.UUID{drafted, published, both, bare, missing})
		return err
	})
	want := map[ids.UUID]string{drafted: "Drafted", published: "Published", both: "Draft name", bare: ""}
	if len(got) != len(want) {
		t.Fatalf("main names: %v", got)
	}
	for id, n := range want {
		if got[id] != n {
			t.Fatalf("main name of %s = %q, want %q", id, got[id], n)
		}
	}
}

func testRefs(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	a, b, target, target2 := g.New(), g.New(), g.New(), g.New()
	withContent(t, db, func(c store.ContentStore) error {
		for _, id := range []ids.UUID{a, b} {
			if err := c.InsertItem(ctx, newItem(id, "article", "")); err != nil {
				return err
			}
		}
		for _, r := range []struct {
			from ids.UUID
			refs []store.Ref
		}{
			{b, []store.Ref{{FieldPath: "related[1]", To: target, Kind: "field", OnDelete: "warn"}}},
			{a, []store.Ref{
				{FieldPath: "body.link", To: target, Kind: "link", OnDelete: "warn"},
				{FieldPath: "author", To: target, Kind: "field", OnDelete: "block"},
				{FieldPath: "other", To: target2, Kind: "field", OnDelete: "nullify"},
			}},
		} {
			if err := c.ReplaceRefs(ctx, r.from, r.refs); err != nil {
				return err
			}
		}
		return nil
	})
	incoming := func(to ids.UUID) []store.Ref {
		var out []store.Ref
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			out, err = c.IncomingRefs(ctx, to)
			return err
		})
		return out
	}
	got := incoming(target)
	want := []store.Ref{
		{From: a, FieldPath: "author", To: target, Kind: "field", OnDelete: "block"},
		{From: a, FieldPath: "body.link", To: target, Kind: "link", OnDelete: "warn"},
		{From: b, FieldPath: "related[1]", To: target, Kind: "field", OnDelete: "warn"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("incoming sorted by from, field_path:\n got %+v\nwant %+v", got, want)
	}

	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceRefs(ctx, a, []store.Ref{{FieldPath: "author", To: target2, Kind: "field", OnDelete: "block"}})
	})
	if got := incoming(target); len(got) != 1 || got[0].From != b {
		t.Fatalf("after replace, target: %+v", got)
	}
	if got := incoming(target2); len(got) != 1 || got[0].From != a || got[0].FieldPath != "author" {
		t.Fatalf("after replace, target2: %+v", got)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.ReplaceRefs(ctx, a, nil) })
	if got := incoming(target2); len(got) != 0 {
		t.Fatalf("after clearing: %+v", got)
	}
	if got := incoming(g.New()); len(got) != 0 {
		t.Fatalf("unreferenced: %+v", got)
	}
}

func testIndexReplace(t *testing.T, db store.DB) {
	ctx := context.Background()
	id := newIDs().New()
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.InsertItem(ctx, newItem(id, "article", "")); err != nil {
			return err
		}
		return c.ReplaceIndex(ctx, id, store.ProjectionPublished, []store.IndexRow{num("", "rating", 5), txt("en", "title", "Old")})
	})
	count := func(q store.Query) int {
		q.Type = "article"
		q.Locale = "en"
		var res store.QueryResult
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			res, err = c.Query(ctx, q)
			return err
		})
		return res.Total
	}
	if count(store.Query{Where: []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpEq, Value: 5.0}}}) != 1 {
		t.Fatal("initial rows not queryable")
	}
	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceIndex(ctx, id, store.ProjectionPublished, []store.IndexRow{num("", "rating", 6)})
	})
	if count(store.Query{Where: []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpEq, Value: 5.0}}}) != 0 ||
		count(store.Query{Where: []store.Cond{{Field: "title", Column: store.ColText, Op: store.OpExists, Value: true}}}) != 0 ||
		count(store.Query{Where: []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpEq, Value: 6.0}}}) != 1 {
		t.Fatal("ReplaceIndex did not replace the old rows")
	}
	withContent(t, db, func(c store.ContentStore) error { return c.ReplaceIndex(ctx, id, store.ProjectionPublished, nil) })
	if count(store.Query{Where: []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpExists, Value: true}}}) != 0 {
		t.Fatal("rows remain after clearing")
	}

	two := store.IndexRow{Field: "x", Text: str("a"), Num: new(float64)}
	for name, row := range map[string]store.IndexRow{"two values": two, "no value": {Field: "x"}} {
		if err := expectErr(db, func(c store.ContentStore) error {
			return c.ReplaceIndex(ctx, id, store.ProjectionPublished, []store.IndexRow{row})
		}); err == nil {
			t.Fatalf("row with %s accepted", name)
		}
	}
}

// catalog is the dataset the query tests share. Letters name the items; ids ascend A..E, then P (a person).
//
//	A  Alpha    published, rating 5, title apple,    published 2026-01-03, tags go+db, featured, author X, key a-key
//	B  Bravo    published, rating 3, title banana,   published 2026-01-01, tags go,    not featured,       key b-key
//	C  Charlie  published, rating 3, title 100%_sure, published 2026-01-02, no tags
//	D  delta    draft only (never published), title a_b%c, tags db
//	E  Echo     published en+de, a draft, rating 8, title en "Apple pie" / de "Apfelkuchen", tags rust, key e-key
//	P  person   rating 3, title glob*?[x]
type catalog struct {
	ids   map[byte]ids.UUID
	label map[ids.UUID]byte
	x, y  ids.UUID // ref targets
}

var (
	pub1 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	pub2 = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	pub3 = time.Date(2026, 1, 3, 10, 0, 0, 123_000_000, time.UTC)
	pub5 = time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
)

func seedCatalog(t *testing.T, db store.DB) *catalog {
	t.Helper()
	ctx := context.Background()
	g := newIDs()
	cat := &catalog{ids: map[byte]ids.UUID{}, label: map[ids.UUID]byte{}, x: g.New(), y: g.New()}
	type spec struct {
		label    byte
		typ, key string
		pubNames map[string]string // locale → name of the published version; nil: never published
		drafts   []store.DraftPart
		index    []store.IndexRow
	}
	specs := []spec{
		{'A', "article", "a-key", map[string]string{"en": "Alpha"}, nil, []store.IndexRow{
			num("", "rating", 5), txt("", "title", "apple"), tim("", "pub", pub3), txt("", "tags", "go"), txt("", "tags", "db"),
			bln("", "featured", true), rfr("", "author", cat.x)}},
		{'B', "article", "b-key", map[string]string{"en": "Bravo"}, nil, []store.IndexRow{
			num("", "rating", 3), txt("", "title", "banana"), tim("", "pub", pub1), txt("", "tags", "go"), bln("", "featured", false)}},
		{'C', "article", "", map[string]string{"en": "Charlie"}, nil, []store.IndexRow{
			num("", "rating", 3), txt("", "title", "100%_sure"), tim("", "pub", pub2)}},
		{'D', "article", "", nil, []store.DraftPart{{Part: "en", Name: "delta"}}, []store.IndexRow{
			txt("", "title", "a_b%c"), txt("", "tags", "db")}},
		{'E', "article", "e-key", map[string]string{"en": "Echo", "de": "Aaron"},
			[]store.DraftPart{{Part: "", Name: ""}, {Part: "de", Name: "Draft Aaron"}}, []store.IndexRow{
				num("", "rating", 8), txt("en", "title", "Apple pie"), txt("de", "title", "Apfelkuchen"),
				tim("", "pub", pub5), txt("", "tags", "rust")}},
		{'P', "person", "", map[string]string{"en": "Pat"}, nil, []store.IndexRow{
			num("", "rating", 3), txt("", "title", "glob*?[x]")}},
	}
	withContent(t, db, func(c store.ContentStore) error {
		if _, err := c.AddSchemaRev(ctx, "h", []byte(`{}`), t0); err != nil {
			return err
		}
		for i, s := range specs {
			id := g.New()
			cat.ids[s.label], cat.label[id] = id, s.label
			it := newItem(id, s.typ, s.key)
			at := t0.Add(time.Duration(i+1) * time.Minute)
			it.CreatedAt, it.UpdatedAt = at, at
			if s.pubNames != nil {
				it.PublishedVersion = 1
			}
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
			if s.pubNames != nil {
				v := store.Version{ItemID: id, Version: 1, SchemaRev: 1, Shared: []byte(`{}`), CreatedAt: at}
				for _, loc := range []string{"en", "de"} {
					if name, ok := s.pubNames[loc]; ok {
						v.Locales = append(v.Locales, store.VersionLocale{Locale: loc, Name: name, Data: []byte(`{}`)})
					}
				}
				if err := c.InsertVersion(ctx, v); err != nil {
					return err
				}
			}
			for _, p := range s.drafts {
				p.ItemID, p.Data, p.UpdatedAt = id, []byte(`{}`), at
				if err := c.PutDraftPart(ctx, p); err != nil {
					return err
				}
			}
			if err := c.ReplaceIndex(ctx, id, store.ProjectionPublished, s.index); err != nil {
				return err
			}
		}
		return nil
	})
	return cat
}

func (cat *catalog) run(t *testing.T, db store.DB, q store.Query) store.QueryResult {
	t.Helper()
	var res store.QueryResult
	viewContent(t, db, func(c store.ContentStore) error {
		var err error
		res, err = c.Query(context.Background(), q)
		return err
	})
	return res
}

func (cat *catalog) letters(res store.QueryResult) string {
	var b strings.Builder
	for _, it := range res.Items {
		b.WriteByte(cat.label[it.ID])
	}
	return b.String()
}

// sorted returns the matching letters in alphabetical order, for queries whose order is not under test.
func (cat *catalog) sorted(t *testing.T, db store.DB, q store.Query) string {
	t.Helper()
	q.Limit = 500
	if q.Sort == nil {
		q.Sort = []store.Sort{{Field: "createdAt"}}
	}
	res := cat.run(t, db, q)
	if res.Total != len(res.Items) {
		t.Fatalf("total %d for %d items", res.Total, len(res.Items))
	}
	return cat.letters(res)
}

func where(field string, col store.Column, op store.Op, v any) []store.Cond {
	return []store.Cond{{Field: field, Column: col, Op: op, Value: v}}
}

func testQueryFilters(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	x, y := cat.x, cat.y
	num, txtc, tm, bl, rf := store.ColNum, store.ColText, store.ColTime, store.ColBool, store.ColRef
	tests := []struct {
		name  string
		query store.Query
		want  string
	}{
		{"type only", store.Query{Locale: "en"}, "ABCDEP"},
		{"article only", store.Query{Type: "article"}, "ABCDE"},
		{"person only", store.Query{Type: "person"}, "P"},
		{"num eq", store.Query{Type: "article", Where: where("rating", num, store.OpEq, 3.0)}, "BC"},
		{"num eq int value", store.Query{Type: "article", Where: where("rating", num, store.OpEq, 3)}, "BC"},
		{"num ne includes items without the field", store.Query{Type: "article", Where: where("rating", num, store.OpNe, 3.0)}, "ADE"},
		{"num lt", store.Query{Type: "article", Where: where("rating", num, store.OpLt, 5.0)}, "BC"},
		{"num lte", store.Query{Type: "article", Where: where("rating", num, store.OpLte, 5.0)}, "ABC"},
		{"num gt", store.Query{Type: "article", Where: where("rating", num, store.OpGt, 3.0)}, "AE"},
		{"num gte", store.Query{Type: "article", Where: where("rating", num, store.OpGte, 5.0)}, "AE"},
		{"num in", store.Query{Type: "article", Where: where("rating", num, store.OpIn, []float64{3, 8, 99})}, "BCE"},
		{"num in empty", store.Query{Type: "article", Where: where("rating", num, store.OpIn, []float64{})}, ""},
		{"num exists", store.Query{Type: "article", Where: where("rating", num, store.OpExists, true)}, "ABCE"},
		{"num not exists", store.Query{Type: "article", Where: where("rating", num, store.OpExists, false)}, "D"},
		{"text eq is exact", store.Query{Type: "article", Where: where("title", txtc, store.OpEq, "apple")}, "A"},
		{"text ne", store.Query{Type: "article", Where: where("title", txtc, store.OpNe, "apple")}, "BCDE"},
		{"text lt (byte order)", store.Query{Type: "article", Where: where("title", txtc, store.OpLt, "b")}, "ACDE"},
		{"text lte", store.Query{Type: "article", Where: where("title", txtc, store.OpLte, "apple")}, "ACDE"},
		{"text gt", store.Query{Type: "article", Where: where("title", txtc, store.OpGt, "apple")}, "B"},
		{"text gte", store.Query{Type: "article", Where: where("title", txtc, store.OpGte, "apple")}, "AB"},
		{"text in", store.Query{Type: "article", Where: where("title", txtc, store.OpIn, []string{"banana", "a_b%c", "zzz"})}, "BD"},
		{"text in quotes", store.Query{Type: "article", Where: where("title", txtc, store.OpIn, []string{"it's", `"x"`})}, ""},
		{"text exists", store.Query{Type: "article", Where: where("title", txtc, store.OpExists, true)}, "ABCDE"},
		{"text prefix is case sensitive", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "a")}, "AD"},
		{"text prefix upper", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "Apple")}, "E"},
		{"text prefix percent is literal", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "100%")}, "C"},
		{"text prefix percent underscore", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "100%_")}, "C"},
		{"text prefix underscore is literal", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "a_")}, "D"},
		{"text prefix mid wildcard", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "a_b%")}, "D"},
		{"text prefix lone percent", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "%")}, ""},
		{"text prefix empty matches every value", store.Query{Type: "article", Where: where("title", txtc, store.OpPrefix, "")}, "ABCDE"},
		{"text prefix glob chars", store.Query{Type: "person", Where: where("title", txtc, store.OpPrefix, "glob*?[")}, "P"},
		{"text prefix glob star not wildcard", store.Query{Type: "person", Where: where("title", txtc, store.OpPrefix, "gl*")}, ""},
		{"text prefix glob question not wildcard", store.Query{Type: "person", Where: where("title", txtc, store.OpPrefix, "glo?")}, ""},
		{"text eq with sql metacharacters", store.Query{Type: "article", Where: where("title", txtc, store.OpEq, "' OR 1=1 --")}, ""},
		{"time eq (ms precision)", store.Query{Type: "article", Where: where("pub", tm, store.OpEq, pub3)}, "A"},
		{"time ne", store.Query{Type: "article", Where: where("pub", tm, store.OpNe, pub3)}, "BCDE"},
		{"time lt", store.Query{Type: "article", Where: where("pub", tm, store.OpLt, pub2)}, "B"},
		{"time lte", store.Query{Type: "article", Where: where("pub", tm, store.OpLte, pub2)}, "BC"},
		{"time gt", store.Query{Type: "article", Where: where("pub", tm, store.OpGt, pub2)}, "AE"},
		{"time gte", store.Query{Type: "article", Where: where("pub", tm, store.OpGte, pub2)}, "ACE"},
		{"time in", store.Query{Type: "article", Where: where("pub", tm, store.OpIn, []time.Time{pub1, pub5})}, "BE"},
		{"time exists", store.Query{Type: "article", Where: where("pub", tm, store.OpExists, false)}, "D"},
		{"time zone is irrelevant", store.Query{Type: "article", Where: where("pub", tm, store.OpEq, pub1.In(time.FixedZone("x", 3600)))}, "B"},
		{"bool eq true", store.Query{Type: "article", Where: where("featured", bl, store.OpEq, true)}, "A"},
		{"bool eq false", store.Query{Type: "article", Where: where("featured", bl, store.OpEq, false)}, "B"},
		{"bool ne true", store.Query{Type: "article", Where: where("featured", bl, store.OpNe, true)}, "BCDE"},
		{"bool in", store.Query{Type: "article", Where: where("featured", bl, store.OpIn, []bool{true, false})}, "AB"},
		{"bool exists", store.Query{Type: "article", Where: where("featured", bl, store.OpExists, true)}, "AB"},
		{"ref eq", store.Query{Type: "article", Where: where("author", rf, store.OpEq, x)}, "A"},
		{"ref eq other", store.Query{Type: "article", Where: where("author", rf, store.OpEq, y)}, ""},
		{"ref ne", store.Query{Type: "article", Where: where("author", rf, store.OpNe, x)}, "BCDE"},
		{"ref in", store.Query{Type: "article", Where: where("author", rf, store.OpIn, []ids.UUID{y, x})}, "A"},
		{"ref exists", store.Query{Type: "article", Where: where("author", rf, store.OpExists, true)}, "A"},
		{"ref string value", store.Query{Type: "article", Where: where("author", rf, store.OpEq, x.String())}, "A"},
		{"contains on a list", store.Query{Type: "article", Where: where("tags", txtc, store.OpContains, "db")}, "AD"},
		{"contains first element", store.Query{Type: "article", Where: where("tags", txtc, store.OpContains, "go")}, "AB"},
		{"contains nothing", store.Query{Type: "article", Where: where("tags", txtc, store.OpContains, "zzz")}, ""},
		{"eq on a list matches any element", store.Query{Type: "article", Where: where("tags", txtc, store.OpEq, "db")}, "AD"},
		{"ne on a list excludes items with the element", store.Query{Type: "article", Where: where("tags", txtc, store.OpNe, "go")}, "CDE"},
		{"list exists", store.Query{Type: "article", Where: where("tags", txtc, store.OpExists, false)}, "C"},
		{"exists without a column", store.Query{Type: "article", Where: where("tags", "", store.OpExists, true)}, "ABDE"},
		{"in over a list", store.Query{Type: "article", Where: where("tags", txtc, store.OpIn, []string{"db", "rust"})}, "ADE"},
		{"conditions are ANDed", store.Query{Type: "article", Where: []store.Cond{
			{Field: "rating", Column: num, Op: store.OpGte, Value: 3.0}, {Field: "tags", Column: txtc, Op: store.OpContains, Value: "go"}}}, "AB"},
		{"same field twice", store.Query{Type: "article", Where: []store.Cond{
			{Field: "rating", Column: num, Op: store.OpGt, Value: 3.0}, {Field: "rating", Column: num, Op: store.OpLt, Value: 8.0}}}, "A"},
		{"locale row hidden from another locale", store.Query{Type: "article", Locale: "de", Where: where("title", txtc, store.OpEq, "Apple pie")}, ""},
		{"locale row visible in its locale", store.Query{Type: "article", Locale: "en", Where: where("title", txtc, store.OpEq, "Apple pie")}, "E"},
		{"other locale row", store.Query{Type: "article", Locale: "de", Where: where("title", txtc, store.OpEq, "Apfelkuchen")}, "E"},
		{"locale row hidden from en", store.Query{Type: "article", Locale: "en", Where: where("title", txtc, store.OpEq, "Apfelkuchen")}, ""},
		{"shared row visible in any locale", store.Query{Type: "article", Locale: "de", Where: where("rating", num, store.OpEq, 8.0)}, "E"},
		{"shared and locale titles in de", store.Query{Type: "article", Locale: "de", Where: where("title", txtc, store.OpExists, true)}, "ABCDE"},
		{"status published", store.Query{Type: "article", Status: store.StatusPublished}, "ABCE"},
		{"status draft", store.Query{Type: "article", Status: store.StatusDraft}, "DE"},
		{"status any", store.Query{Type: "article", Status: store.StatusAny}, "ABCDE"},
		{"status published with a filter", store.Query{Type: "article", Status: store.StatusPublished, Where: where("tags", txtc, store.OpContains, "db")}, "A"},
		{"status draft with a filter", store.Query{Type: "article", Status: store.StatusDraft, Where: where("tags", txtc, store.OpContains, "db")}, "D"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.query
			if q.Locale == "" {
				q.Locale = "en"
			}
			if got := cat.sorted(t, db, q); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func testQuerySort(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	asc := func(f string, c store.Column) store.Sort { return store.Sort{Field: f, Column: c} }
	desc := func(f string, c store.Column) store.Sort { return store.Sort{Field: f, Column: c, Desc: true} }
	// The id tiebreak runs in the direction of the last sort key, and ids ascend with the letters.
	tests := []struct {
		name   string
		locale string
		sort   []store.Sort
		want   string
	}{
		{"default is updatedAt desc", "en", nil, "EDCBA"},
		{"updatedAt asc", "en", []store.Sort{asc("updatedAt", "")}, "ABCDE"},
		{"createdAt desc", "en", []store.Sort{desc("createdAt", "")}, "EDCBA"},
		{"createdAt asc", "en", []store.Sort{asc("createdAt", "")}, "ABCDE"},
		{"name asc (byte order, draft name for unpublished)", "en", []store.Sort{asc("name", "")}, "ABCED"},
		{"name desc", "en", []store.Sort{desc("name", "")}, "DECBA"},
		{"name in de prefers the de name", "de", []store.Sort{asc("name", "")}, "EABCD"},
		{"key asc, nulls last", "en", []store.Sort{asc("key", "")}, "ABECD"},
		{"key desc, nulls last", "en", []store.Sort{desc("key", "")}, "EBADC"},
		{"num asc, nulls last, ties by id", "en", []store.Sort{asc("rating", store.ColNum)}, "BCAED"},
		{"num desc, nulls last, ties by id desc", "en", []store.Sort{desc("rating", store.ColNum)}, "EACBD"},
		{"num desc then name asc", "en", []store.Sort{desc("rating", store.ColNum), asc("name", "")}, "EABCD"},
		{"num asc then createdAt desc", "en", []store.Sort{asc("rating", store.ColNum), desc("createdAt", "")}, "CBAED"},
		{"text asc", "en", []store.Sort{asc("title", store.ColText)}, "CEDAB"},
		{"text desc", "en", []store.Sort{desc("title", store.ColText)}, "BADEC"},
		{"text in de uses the de row", "de", []store.Sort{asc("title", store.ColText)}, "CEDAB"},
		{"time asc", "en", []store.Sort{asc("pub", store.ColTime)}, "BCAED"},
		{"time desc", "en", []store.Sort{desc("pub", store.ColTime)}, "EACBD"},
		{"bool asc", "en", []store.Sort{asc("featured", store.ColBool)}, "BACDE"},
		{"bool desc", "en", []store.Sort{desc("featured", store.ColBool)}, "ABEDC"},
		{"ref asc", "en", []store.Sort{asc("author", store.ColRef)}, "ABCDE"},
		{"list asc sorts by the smallest element", "en", []store.Sort{asc("tags", store.ColText)}, "ADBEC"},
		{"list desc sorts by the largest element", "en", []store.Sort{desc("tags", store.ColText)}, "EBADC"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := cat.run(t, db, store.Query{Type: "article", Locale: tc.locale, Sort: tc.sort})
			if got := cat.letters(res); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if res.Total != 5 {
				t.Fatalf("total %d", res.Total)
			}
		})
	}
}

func testQueryPaging(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	by := []store.Sort{{Field: "createdAt"}}
	page := func(limit, offset int) store.QueryResult {
		return cat.run(t, db, store.Query{Type: "article", Locale: "en", Sort: by, Limit: limit, Offset: offset})
	}
	for _, tc := range []struct {
		limit, offset int
		want          string
		total         int
	}{
		{2, 0, "AB", 5}, {2, 2, "CD", 5}, {2, 4, "E", 5}, {2, 5, "", 5}, {2, 50, "", 5},
		{5, 0, "ABCDE", 5}, {6, 0, "ABCDE", 5}, {0, 0, "ABCDE", 5}, {1, 3, "D", 5}, {-1, -4, "ABCDE", 5},
		{100000, 1, "BCDE", 5},
	} {
		res := page(tc.limit, tc.offset)
		if got := cat.letters(res); got != tc.want || res.Total != tc.total {
			t.Errorf("limit %d offset %d: %q total %d, want %q total %d", tc.limit, tc.offset, got, res.Total, tc.want, tc.total)
		}
	}

	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Sort: by, Limit: 1,
		Where: where("rating", store.ColNum, store.OpGte, 3.0)})
	if res.Total != 4 || cat.letters(res) != "A" {
		t.Errorf("filtered total: %q %d", cat.letters(res), res.Total)
	}
	none := cat.run(t, db, store.Query{Type: "article", Locale: "en", Where: where("rating", store.ColNum, store.OpGt, 100.0)})
	if none.Total != 0 || len(none.Items) != 0 {
		t.Errorf("no match: %+v", none)
	}
	// Walking the pages reproduces the full ordering.
	var walked string
	for off := 0; ; off += 2 {
		p := page(2, off)
		if len(p.Items) == 0 {
			break
		}
		walked += cat.letters(p)
	}
	if walked != "ABCDE" {
		t.Errorf("walked pages: %q", walked)
	}
}

// walkCatalog seeds 40 items shaped to break an index-ordered walk: tied values, list fields (with equal
// duplicates), values in the shared row and in locale rows, items without the field, and a spread of types and
// statuses. It returns the ids in creation order.
func walkCatalog(t *testing.T, db store.DB) []ids.UUID {
	t.Helper()
	ctx := context.Background()
	g := newIDs()
	var out []ids.UUID
	withContent(t, db, func(c store.ContentStore) error {
		if _, err := c.AddSchemaRev(ctx, "h", []byte(`{}`), t0); err != nil {
			return err
		}
		for i := range 40 {
			id := g.New()
			out = append(out, id)
			typ := "article"
			if i%7 == 3 {
				typ = "page"
			}
			it := newItem(id, typ, "")
			at := t0.Add(time.Duration(i*i%11) * time.Minute) // ties in updatedAt
			it.CreatedAt, it.UpdatedAt = at, at
			if i%3 != 0 {
				it.PublishedVersion = 1
			}
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
			if i%3 == 0 && i%2 == 0 {
				if err := c.PutDraftPart(ctx, store.DraftPart{ItemID: id, Part: "en", Name: "d", Data: []byte(`{}`), UpdatedAt: at}); err != nil {
					return err
				}
			}
			var rows []store.IndexRow
			if i%5 != 0 { // 8 items have no score
				rows = append(rows, num("", "score", float64(i%7)))
				switch i % 4 {
				case 0:
					rows = append(rows, num("", "score", float64(i%7+3)), num("", "score", float64(i%7))) // list, equal duplicate
				case 1:
					rows = append(rows, num("en", "score", float64(i%7))) // shared and locale row, same value
				}
			}
			switch i % 4 {
			case 0:
				rows = append(rows, txt("", "label", string(rune('a'+i%6))))
			case 1:
				rows = append(rows, txt("en", "label", string(rune('a'+i%6))), txt("de", "label", string(rune('f'-i%6))))
			case 2:
				rows = append(rows, txt("", "label", string(rune('a'+i%6))), txt("", "label", string(rune('a'+(i+2)%6))))
			}
			if i%6 != 0 {
				rows = append(rows, tim("", "when", t0.Add(time.Duration(i%9)*time.Hour)), bln("", "flag", i%2 == 0))
			}
			if err := c.ReplaceIndex(ctx, id, store.ProjectionPublished, rows); err != nil {
				return err
			}
		}
		return nil
	})
	return out
}

// testQueryWalk compares the index-ordered plan with the compiled sort plan on the same data. A query with
// more than MaxWalkConds conditions never walks, so padding it with always-true ones forces the other plan.
func testQueryWalk(t *testing.T, db store.DB) {
	walkCatalog(t, db)
	ctx := context.Background()
	pad := func(q store.Query) store.Query {
		w := slices.Clone(q.Where)
		for range storeutil.MaxWalkConds + 1 {
			w = append(w, store.Cond{Field: "nosuchfield", Op: store.OpExists, Value: false})
		}
		q.Where = w
		return q
	}
	run := func(q store.Query) store.QueryResult {
		var tx store.QueryResult
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			tx, err = c.Query(ctx, q)
			return err
		})
		return tx
	}
	viaReader := func(q store.Query) store.QueryResult {
		rd, err := db.Reader().Query(ctx, q)
		if err != nil {
			t.Fatalf("reader: %v", err)
		}
		return rd
	}
	filters := map[string][]store.Cond{
		"none":          nil,
		"score>=2":      where("score", store.ColNum, store.OpGte, 2.0),
		"score<5":       where("score", store.ColNum, store.OpLt, 5.0),
		"score!=3":      where("score", store.ColNum, store.OpNe, 3.0),
		"label exists":  {{Field: "label", Column: store.ColText, Op: store.OpExists, Value: true}},
		"score missing": {{Field: "score", Op: store.OpExists, Value: false}},
		"two":           {{Field: "score", Column: store.ColNum, Op: store.OpGt, Value: 0.0}, {Field: "when", Op: store.OpExists, Value: true}},
		"eq (no walk)":  where("score", store.ColNum, store.OpEq, 3.0),
	}
	filterNames := make([]string, 0, len(filters))
	for n := range filters {
		filterNames = append(filterNames, n)
	}
	slices.Sort(filterNames)
	sorts := []store.Sort{
		{Field: "score", Column: store.ColNum}, {Field: "score", Column: store.ColNum, Desc: true},
		{Field: "label", Column: store.ColText}, {Field: "label", Column: store.ColText, Desc: true},
		{Field: "when", Column: store.ColTime}, {Field: "flag", Column: store.ColBool, Desc: true},
		{Field: "nosuchfield", Column: store.ColNum},
	}
	pages := [][2]int{{5, 0}, {7, 13}, {50, 0}, {4, 26}, {5, 100}}
	nonEmpty := 0
	for _, fn := range filterNames {
		for _, srt := range sorts {
			for _, loc := range []string{"en", "de"} {
				if loc == "de" && srt.Field != "label" {
					continue // only locale rows depend on the locale
				}
				statuses := []store.Status{store.StatusPublished}
				if fn == "none" {
					statuses = []store.Status{store.StatusPublished, store.StatusAny, store.StatusDraft}
				}
				for _, st := range statuses {
					for pi, pg := range pages {
						q := store.Query{Type: "article", Locale: loc, Status: st, Where: filters[fn], Sort: []store.Sort{srt}, Limit: pg[0], Offset: pg[1]}
						want := run(pad(q))
						results := []store.QueryResult{run(q)}
						if pi < 2 {
							results = append(results, viaReader(q))
						}
						for i, got := range results {
							if !slices.Equal(idsOf(got), idsOf(want)) || got.Total != want.Total {
								t.Fatalf("%s sort %+v locale %s status %d page %v (reader %v): got %d items total %d, want %d items total %d",
									fn, srt, loc, st, pg, i == 1, len(got.Items), got.Total, len(want.Items), want.Total)
							}
						}
						if len(want.Items) > 0 {
							nonEmpty++
						}
					}
				}
			}
		}
	}
	if nonEmpty < 200 {
		t.Fatalf("only %d queries returned items", nonEmpty)
	}

	// Every item once, whatever the plan: pages of 3 over the whole set reproduce the single page.
	for _, srt := range sorts[:4] {
		q := store.Query{Locale: "en", Status: store.StatusAny, Sort: []store.Sort{srt}, Limit: 500}
		all := run(q)
		if len(all.Items) != 40 || all.Total != 40 {
			t.Fatalf("sort %+v: %d items, total %d", srt, len(all.Items), all.Total)
		}
		seen := map[ids.UUID]bool{}
		for _, it := range all.Items {
			if seen[it.ID] {
				t.Fatalf("sort %+v: item listed twice", srt)
			}
			seen[it.ID] = true
		}
		var paged []ids.UUID
		for off := 0; off < 40; off += 3 {
			q.Limit, q.Offset = 3, off
			p := run(q)
			paged = append(paged, idsOf(p)...)
		}
		if !slices.Equal(paged, idsOf(all)) {
			t.Fatalf("sort %+v: paging differs from one page", srt)
		}
	}
}

func idsOf(res store.QueryResult) []ids.UUID {
	out := make([]ids.UUID, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.ID
	}
	return out
}

func testQueryNames(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	byLetter := func(locale string) map[byte]store.ItemSummary {
		out := map[byte]store.ItemSummary{}
		for _, it := range cat.run(t, db, store.Query{Locale: locale}).Items {
			out[cat.label[it.ID]] = it
		}
		return out
	}
	en, de, fr := byLetter("en"), byLetter("de"), byLetter("fr")
	names := func(m map[byte]store.ItemSummary) string {
		var parts []string
		for _, l := range []byte("ABCDEP") {
			parts = append(parts, m[l].Name)
		}
		return strings.Join(parts, "|")
	}
	if got := names(en); got != "Alpha|Bravo|Charlie|delta|Echo|Pat" {
		t.Errorf("en names: %s", got)
	}
	if got := names(de); got != "Alpha|Bravo|Charlie|delta|Aaron|Pat" {
		t.Errorf("de names (locale name, else main locale, else draft): %s", got)
	}
	if got := names(fr); got != "Alpha|Bravo|Charlie|delta|Echo|Pat" {
		t.Errorf("fr names fall back to the main locale: %s", got)
	}
	if got := en['E'].Locales; !slices.Equal(got, []string{"de", "en"}) {
		t.Errorf("E locales: %v", got)
	}
	if got := en['E'].Drafts; !slices.Equal(got, []string{"", "de"}) {
		t.Errorf("E drafts: %v", got)
	}
	if got := en['D'].Drafts; !slices.Equal(got, []string{"en"}) {
		t.Errorf("D drafts: %v", got)
	}
	if len(en['D'].Locales) != 0 || len(en['A'].Drafts) != 0 {
		t.Errorf("D locales %v, A drafts %v", en['D'].Locales, en['A'].Drafts)
	}
	if got := en['A']; got.Key != "a-key" || got.Type != "article" || got.PublishedVersion != 1 || got.MainLocale != "en" || !got.AlwaysAvailable {
		t.Errorf("summary carries the item: %+v", got.Item)
	}

	// An item without any name still lists, with an empty one.
	id := newIDs().New()
	id[0] = 0xff
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(context.Background(), newItem(id, "blank", "")) })
	res := cat.run(t, db, store.Query{Type: "blank", Locale: "en", Sort: []store.Sort{{Field: "name"}}})
	if len(res.Items) != 1 || res.Items[0].Name != "" {
		t.Errorf("unnamed item: %+v", res.Items)
	}
}

func testQueryInvalid(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	bad := map[string]store.Query{
		"field with a quote":   {Type: "article", Where: where("a'b", store.ColText, store.OpEq, "x")},
		"field with a space":   {Type: "article", Where: where("a b", store.ColText, store.OpEq, "x")},
		"field starting upper": {Type: "article", Where: where("Title", store.ColText, store.OpEq, "x")},
		"empty field":          {Type: "article", Where: where("", store.ColText, store.OpEq, "x")},
		"field injection":      {Type: "article", Where: where("a;DROP TABLE item", store.ColText, store.OpEq, "x")},
		"unknown op":           {Type: "article", Where: where("title", store.ColText, store.Op("like"), "x")},
		"unknown column":       {Type: "article", Where: where("title", store.Column("v_text; --"), store.OpEq, "x")},
		"missing column":       {Type: "article", Where: where("title", "", store.OpEq, "x")},
		"text value for num":   {Type: "article", Where: where("rating", store.ColNum, store.OpEq, "5")},
		"num value for text":   {Type: "article", Where: where("title", store.ColText, store.OpEq, 5.0)},
		"prefix on a number":   {Type: "article", Where: where("rating", store.ColNum, store.OpPrefix, "5")},
		"prefix with a number": {Type: "article", Where: where("title", store.ColText, store.OpPrefix, 5)},
		"exists needs a bool":  {Type: "article", Where: where("title", store.ColText, store.OpExists, "yes")},
		"in needs a list":      {Type: "article", Where: where("title", store.ColText, store.OpIn, "x")},
		"in with a wrong type": {Type: "article", Where: where("rating", store.ColNum, store.OpIn, []string{"1"})},
		"bad ref string":       {Type: "article", Where: where("author", store.ColRef, store.OpEq, "not-a-uuid")},
		"sort field injection": {Type: "article", Sort: []store.Sort{{Field: "x; DROP", Column: store.ColText}}},
		"unknown meta sort":    {Type: "article", Sort: []store.Sort{{Field: "published_version"}}},
		"sort unknown column":  {Type: "article", Sort: []store.Sort{{Field: "rating", Column: store.Column("x")}}},
		"unknown status":       {Type: "article", Status: store.Status(9)},
	}
	for name, q := range bad {
		t.Run(name, func(t *testing.T) {
			q.Locale = "en"
			err := db.View(context.Background(), func(tx store.Tx) error {
				_, err := tx.Content().Query(context.Background(), q)
				return err
			})
			if err == nil {
				t.Fatal("query accepted")
			}
		})
	}
	// Hostile data is just data.
	for _, s := range []string{`"`, `' OR 1=1 --`, `fox" OR "`, `*`, `-`, `NEAR(`, `a:b`, `(`, `\`, "!!!"} {
		res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: s})
		if len(res.Items) != 0 || res.Total != 0 {
			t.Errorf("search %q matched %+v", s, res)
		}
	}
}

// testSearchHuge: ReplaceSearch accepts a megabyte of distinct words (Postgres rejects a tsvector over 1 MB, so
// the adapters bound what they index); the start of the text stays searchable.
func testSearchHuge(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	var b strings.Builder
	b.WriteString("zebrafish ")
	for i := 0; b.Len() < 1_000_000; i++ {
		b.WriteString("w")
		b.WriteString(strconv.Itoa(i * 7919))
		b.WriteByte('x')
		b.WriteString(strconv.FormatInt(int64(i)*31337, 36))
		b.WriteByte(' ')
	}
	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceSearch(context.Background(), cat.ids['B'], store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: b.String()}})
	})
	res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "zebrafish"})
	if got := cat.letters(res); got != "B" {
		t.Errorf("huge document: zebrafish matched %q", got)
	}
}

func testSearch(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	docs := map[byte][]store.SearchDoc{
		'A': {{Locale: "en", Text: "a quick brown fox jumps over many other animals today"}, {Locale: "", Text: "sharedword"}},
		'B': {{Locale: "en", Text: "fox fox fox fox fox"}},
		'C': {{Locale: "en", Text: "nothing to see here"}},
		'E': {{Locale: "en", Text: "quick reference guide"}, {Locale: "de", Text: "schneller fuchs"}},
	}
	withContent(t, db, func(c store.ContentStore) error {
		for _, l := range []byte("ABCE") {
			if err := c.ReplaceSearch(ctx, cat.ids[l], store.ProjectionPublished, docs[l]); err != nil {
				return err
			}
		}
		return nil
	})
	search := func(locale, text string, sort ...store.Sort) store.QueryResult {
		return cat.run(t, db, store.Query{Type: "article", Locale: locale, Search: text, Sort: sort})
	}
	for _, tc := range []struct {
		locale, text, want string
	}{
		{"en", "fox", "BA"},
		{"en", "FOX", "BA"},
		{"en", "quick", "AE"},
		{"en", "quick fox", "A"},
		{"en", "qui", "AE"},
		{"en", "fox!!", "BA"},
		{"en", "nothing", "C"},
		{"en", "missing", ""},
		{"en", "fuchs", ""},
		{"de", "fuchs", "E"},
		{"de", "schnell", "E"},
		{"de", "quick", ""},
		{"en", "sharedword", "A"},
		{"de", "sharedword", "A"},
		{"fr", "sharedword", "A"},
		{"fr", "fox", ""},
	} {
		res := search(tc.locale, tc.text)
		got := sortString(cat.letters(res))
		if got != sortString(tc.want) || res.Total != len(tc.want) {
			t.Errorf("search %q in %s: %q total %d, want %q", tc.text, tc.locale, got, res.Total, tc.want)
		}
	}

	for _, hostile := range []string{`fox" OR "`, `fox*`, `NEAR(fox`, `"`, `a:b`, `-fox`, `fox AND`, `(fox`} {
		search("en", hostile)
	}

	ranked := search("en", "fox")
	if len(ranked.Items) != 2 || !(ranked.Items[0].Searched > ranked.Items[1].Searched) || ranked.Items[1].Searched <= 0 {
		t.Errorf("rank order and Searched must be best first and positive: %+v", ranked.Items)
	}
	if got := cat.letters(search("en", "fox", store.Sort{Field: "name"})); got != "AB" {
		t.Errorf("explicit sort overrides rank: %q", got)
	}
	if got := cat.letters(search("en", "fox", store.Sort{Field: "name", Desc: true})); got != "BA" {
		t.Errorf("explicit sort desc: %q", got)
	}
	if res := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "quick", Status: store.StatusDraft}); cat.letters(res) != "E" || res.Total != 1 {
		t.Errorf("search with status: %q", cat.letters(res))
	}
	filtered := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "quick", Where: where("rating", store.ColNum, store.OpGt, 5.0)})
	if cat.letters(filtered) != "E" || filtered.Total != 1 {
		t.Errorf("search with filter: %q", cat.letters(filtered))
	}
	if page := cat.run(t, db, store.Query{Type: "article", Locale: "en", Search: "quick", Limit: 1}); len(page.Items) != 1 || page.Total != 2 {
		t.Errorf("search paging: %d items, total %d", len(page.Items), page.Total)
	}
	if res := cat.run(t, db, store.Query{Locale: "en", Search: "quick"}); res.Total != 2 {
		t.Errorf("search across types: %d", res.Total)
	}
	if res := search("en", ""); res.Total != 5 {
		t.Errorf("empty search is no search: %d", res.Total)
	}

	// Replacing removes the old text; a locale given twice is merged.
	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceSearch(ctx, cat.ids['B'], store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: "kiwi"}, {Locale: "en", Text: "papaya"}})
	})
	if got := cat.letters(search("en", "fox")); got != "A" {
		t.Errorf("after replace, fox: %q", got)
	}
	if got := cat.letters(search("en", "kiwi papaya")); got != "B" {
		t.Errorf("after replace, merged docs: %q", got)
	}
	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceSearch(ctx, cat.ids['B'], store.ProjectionPublished, nil)
	})
	if got := cat.letters(search("en", "kiwi")); got != "" {
		t.Errorf("after clearing: %q", got)
	}
	// Replacing again after deletes still works (no stale rows from earlier documents).
	withContent(t, db, func(c store.ContentStore) error {
		return c.ReplaceSearch(ctx, cat.ids['B'], store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: "fox again"}})
	})
	if got := cat.letters(search("en", "fox")); got != "BA" && got != "AB" {
		t.Errorf("re-added: %q", got)
	}
}

func sortString(s string) string {
	b := []byte(s)
	slices.Sort(b)
	return string(b)
}

func testDeleteCascade(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	ctx := context.Background()
	a, e := cat.ids['A'], cat.ids['E']
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.ReplaceRefs(ctx, a, []store.Ref{{FieldPath: "author", To: e, Kind: "field", OnDelete: "block"}}); err != nil {
			return err
		}
		return c.ReplaceSearch(ctx, a, store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: "doomed"}})
	})
	viewContent(t, db, func(c store.ContentStore) error {
		if refs, err := c.IncomingRefs(ctx, e); err != nil || len(refs) != 1 {
			t.Fatalf("setup: %+v %v", refs, err)
		}
		return nil
	})
	// E has versions, drafts, index rows; delete both.
	withContent(t, db, func(c store.ContentStore) error {
		if err := c.DeleteItem(ctx, a); err != nil {
			return err
		}
		return c.DeleteItem(ctx, e)
	})
	viewContent(t, db, func(c store.ContentStore) error {
		if _, err := c.GetVersion(ctx, e, 1); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("version survived: %v", err)
		}
		if l, err := c.ListVersions(ctx, e); err != nil || len(l) != 0 {
			t.Errorf("versions survived: %+v %v", l, err)
		}
		if d, err := c.GetDraftParts(ctx, e); err != nil || len(d) != 0 {
			t.Errorf("drafts survived: %+v %v", d, err)
		}
		if refs, err := c.IncomingRefs(ctx, e); err != nil || len(refs) != 0 {
			t.Errorf("refs survived: %+v %v", refs, err)
		}
		return nil
	})
	if got := cat.sorted(t, db, store.Query{Type: "article", Locale: "en"}); got != "BCD" {
		t.Errorf("remaining articles: %q", got)
	}
	if got := cat.sorted(t, db, store.Query{Locale: "en", Where: where("title", store.ColText, store.OpExists, true)}); got != "BCDP" {
		t.Errorf("index rows survived: %q", got)
	}
	if got := cat.sorted(t, db, store.Query{Locale: "en", Search: "doomed"}); got != "" {
		t.Errorf("search rows survived: %q", got)
	}
	// Recreating an item with the same id starts clean.
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(ctx, newItem(e, "article", "e-key")) })
	viewContent(t, db, func(c store.ContentStore) error {
		if d, err := c.GetDraftParts(ctx, e); err != nil || len(d) != 0 {
			t.Errorf("recreated item inherited drafts: %+v %v", d, err)
		}
		return nil
	})
}

func testContentMigrations(t *testing.T, db store.DB) {
	ctx := context.Background()
	withContent(t, db, func(c store.ContentStore) error {
		if got, err := c.AppliedContentMigrations(ctx); err != nil || len(got) != 0 {
			t.Fatalf("initially: %v %v", got, err)
		}
		for _, name := range []string{"0002_b", "0001_a", "0002_b"} {
			if err := c.RecordContentMigration(ctx, name, t0); err != nil {
				return err
			}
		}
		return nil
	})
	viewContent(t, db, func(c store.ContentStore) error {
		if got, err := c.AppliedContentMigrations(ctx); err != nil || !slices.Equal(got, []string{"0001_a", "0002_b"}) {
			t.Fatalf("applied: %v %v", got, err)
		}
		return nil
	})
}

func testItemIDsOfType(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	var want []ids.UUID
	withContent(t, db, func(c store.ContentStore) error {
		for i := range 8 {
			id := g.New()
			typ := "thing"
			if i%4 == 3 {
				typ = "other"
			} else {
				want = append(want, id)
			}
			if err := c.InsertItem(ctx, newItem(id, typ, "")); err != nil {
				return err
			}
		}
		return nil
	})
	var got []ids.UUID
	var after ids.UUID
	viewContent(t, db, func(c store.ContentStore) error {
		for range 10 {
			page, err := c.ItemIDsOfType(ctx, "thing", after, 4)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			if len(page) > 4 {
				t.Fatalf("page of %d", len(page))
			}
			got = append(got, page...)
			after = page[len(page)-1]
		}
		return nil
	})
	if !slices.Equal(got, want) {
		t.Fatalf("paged ids:\n got %v\nwant %v", got, want)
	}
	viewContent(t, db, func(c store.ContentStore) error {
		if page, err := c.ItemIDsOfType(ctx, "nothing", ids.Nil, 10); err != nil || len(page) != 0 {
			t.Fatalf("unknown type: %v %v", page, err)
		}
		return nil
	})
}

func sameItem(a, b store.Item) bool {
	ta, tb := [2]time.Time{a.CreatedAt, a.UpdatedAt}, [2]time.Time{b.CreatedAt, b.UpdatedAt}
	da, db := a.DeletedAt, b.DeletedAt
	a.CreatedAt, a.UpdatedAt, b.CreatedAt, b.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	a.DeletedAt, b.DeletedAt = nil, nil
	return a == b && ta[0].Equal(tb[0]) && ta[1].Equal(tb[1]) && (da == nil) == (db == nil) && (da == nil || da.Equal(*db))
}

// testQueryAvailable checks Query.Available on every plan: eight kinds of item, ten of each.
func testQueryAvailable(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	// kind: snapshot locales, published, always available.
	kinds := []struct {
		locales          []string
		published, alway bool
	}{
		0: {[]string{"en"}, true, false},
		1: {[]string{"de"}, true, false},
		2: {[]string{"en", "de"}, true, false},
		3: {[]string{"fr"}, true, false},
		4: {nil, true, true},
		5: {nil, true, false},
		6: {[]string{"en", "fr"}, false, true}, // unpublished: a stale snapshot must not count
		7: {[]string{"en", "fr"}, true, false},
	}
	idxOf := map[ids.UUID]int{}
	withContent(t, db, func(c store.ContentStore) error {
		for i := range 80 {
			k := kinds[i%8]
			id := g.New()
			idxOf[id] = i
			it := newItem(id, "avail", "")
			it.AlwaysAvailable = k.alway
			if k.published {
				it.PublishedVersion = 1
			}
			it.UpdatedAt = t0.Add(time.Duration(i) * time.Minute)
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
			var snaps []store.Snapshot
			for _, l := range k.locales {
				snaps = append(snaps, snap(id, "avail", "", l, 1, `{}`))
			}
			if err := c.PutSnapshots(ctx, id, snaps); err != nil {
				return err
			}
			rows := []store.IndexRow{num("", "rating", float64(i))}
			if i%3 != 0 { // some items have no value, for the walk's second phase
				rows = append(rows, txt("", "title", "x"))
			}
			if err := c.ReplaceIndex(ctx, id, store.ProjectionPublished, rows); err != nil {
				return err
			}
			text := "gadget"
			if i%2 == 1 {
				text = "other"
			}
			if err := c.ReplaceSearch(ctx, id, store.ProjectionPublished, []store.SearchDoc{{Locale: "en", Text: text}}); err != nil {
				return err
			}
		}
		return nil
	})
	run := func(q store.Query) (res store.QueryResult) {
		viewContent(t, db, func(c store.ContentStore) error {
			var err error
			res, err = c.Query(ctx, q)
			return err
		})
		return res
	}
	has := func(kind int, avail []string) bool {
		k := kinds[kind]
		return k.published && (k.alway || slices.ContainsFunc(k.locales, func(l string) bool { return slices.Contains(avail, l) }))
	}
	rating := store.Sort{Field: "rating", Column: store.ColNum}
	ge20 := []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpGte, Value: 20.0}}
	eq := []store.Cond{{Field: "title", Column: store.ColText, Op: store.OpEq, Value: "x"}}
	type shape struct {
		name string
		q    store.Query
		keep func(i int) bool // which of the 80 items match apart from Available
	}
	all := func(int) bool { return true }
	shapes := []shape{
		{"default", store.Query{}, all},
		{"walk", store.Query{Sort: []store.Sort{rating}}, all},
		{"walk desc filter", store.Query{Sort: []store.Sort{{Field: "rating", Column: store.ColNum, Desc: true}}, Where: ge20}, func(i int) bool { return i >= 20 }},
		{"eq", store.Query{Where: eq}, func(i int) bool { return i%3 != 0 }},
		{"eq sorted", store.Query{Where: eq, Sort: []store.Sort{rating}}, func(i int) bool { return i%3 != 0 }},
		{"search", store.Query{Search: "gadget"}, func(i int) bool { return i%2 == 0 }},
		{"search sorted", store.Query{Search: "gadget", Sort: []store.Sort{{Field: "createdAt"}}}, func(i int) bool { return i%2 == 0 }},
	}
	for _, avail := range [][]string{{"fr"}, {"en"}, {"de", "fr"}, {"es"}, {}} {
		for _, sh := range shapes {
			want := map[ids.UUID]bool{}
			for id, i := range idxOf {
				if sh.keep(i) && has(i%8, avail) {
					want[id] = true
				}
			}
			if len(want) == 0 && len(avail) > 0 && avail[0] != "es" {
				t.Fatalf("%v %s: empty expectation", avail, sh.name)
			}
			for _, limit := range []int{500, 7} {
				q := sh.q
				q.Type, q.Locale, q.Status, q.Available, q.Limit = "avail", "en", store.StatusPublished, avail, limit
				got := map[ids.UUID]bool{}
				for off := 0; ; off += limit {
					q.Offset = off
					res := run(q)
					viaReader, err := db.Reader().Query(ctx, q)
					if err != nil {
						t.Fatal(err)
					}
					if res.Total != len(want) || viaReader.Total != len(want) || !slices.Equal(idsOf(res), idsOf(viaReader)) {
						t.Fatalf("%v %s limit %d offset %d: total %d/%d, want %d", avail, sh.name, limit, off, res.Total, viaReader.Total, len(want))
					}
					for _, it := range res.Items {
						if !want[it.ID] || got[it.ID] {
							t.Fatalf("%v %s: unexpected or repeated item of kind %d", avail, sh.name, idxOf[it.ID]%8)
						}
						got[it.ID] = true
					}
					if len(res.Items) < limit {
						break
					}
				}
				if len(got) != len(want) {
					t.Fatalf("%v %s limit %d: paged %d items, want %d", avail, sh.name, limit, len(got), len(want))
				}
			}
		}
	}
	// Unset keeps everything of the type that is published, snapshot or not.
	res := run(store.Query{Type: "avail", Locale: "en", Status: store.StatusPublished, Limit: 1})
	if res.Total != 70 {
		t.Fatalf("without Available: total %d, want 70", res.Total)
	}
}
