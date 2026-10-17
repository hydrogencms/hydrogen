package storetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var snapshotTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"SnapshotRoundTrip", testSnapshotRoundTrip},
	{"SnapshotReplace", testSnapshotReplace},
	{"SnapshotLookups", testSnapshotLookups},
	{"SnapshotCascade", testSnapshotCascade},
}

func snap(item ids.UUID, typ, key, locale string, version int, payload string, refs ...ids.UUID) store.Snapshot {
	sum := sha256.Sum256([]byte(payload))
	return store.Snapshot{ItemID: item, Locale: locale, Version: version, Type: typ, Key: key, MainLocale: "en",
		AlwaysAvailable: locale == "en", Payload: []byte(payload), ETag: hex.EncodeToString(sum[:]), Refs: refs,
		PublishedAt: t0.Add(789 * time.Millisecond)}
}

func putSnaps(t *testing.T, db store.DB, item ids.UUID, snaps ...store.Snapshot) {
	t.Helper()
	withContent(t, db, func(c store.ContentStore) error { return c.PutSnapshots(context.Background(), item, snaps) })
}

func seedSnapItems(t *testing.T, db store.DB, n int) []ids.UUID {
	t.Helper()
	g := newIDs()
	list := make([]ids.UUID, n)
	withContent(t, db, func(c store.ContentStore) error {
		for i := range list {
			list[i] = g.New()
			key := ""
			if i == 0 {
				key = "home"
			}
			if err := c.InsertItem(context.Background(), newItem(list[i], "page", key)); err != nil {
				return err
			}
		}
		return nil
	})
	return list
}

func checkSnap(t *testing.T, got, want store.Snapshot) {
	t.Helper()
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("payload %q, want %q", got.Payload, want.Payload)
	}
	if !got.PublishedAt.Equal(want.PublishedAt) {
		t.Errorf("publishedAt %v, want %v", got.PublishedAt, want.PublishedAt)
	}
	if got.ItemID != want.ItemID || got.Locale != want.Locale || got.Version != want.Version || got.Type != want.Type ||
		got.Key != want.Key || got.MainLocale != want.MainLocale || got.AlwaysAvailable != want.AlwaysAvailable ||
		got.ETag != want.ETag || !slices.Equal(got.Refs, want.Refs) {
		t.Errorf("snapshot %+v, want %+v", got, want)
	}
}

func byLocale(list []store.Snapshot) map[string]store.Snapshot {
	m := map[string]store.Snapshot{}
	for _, s := range list {
		m[s.ItemID.String()+"/"+s.Locale] = s
	}
	return m
}

// testSnapshotRoundTrip stores payloads that a JSON re-encoder or jsonb would change and reads them back.
func testSnapshotRoundTrip(t *testing.T, db store.DB) {
	ctx := context.Background()
	items := seedSnapItems(t, db, 2)
	payloads := []string{
		`{"id":"x","fields":{"b":1,"a":1.50,"n":12345678901234567890123,"e":1E+2,"dup":1,"dup":2},"name":"Zoë \\u0000 <&>  "}`,
		"{ \"spaced\" :  [1, 2 ,3]\n}",
		`{"fields":{"empty":{},"long":"` + string(bytes.Repeat([]byte("é"), 50000)) + `"}}`,
	}
	refs := []ids.UUID{items[1], newIDs().New()}
	slices.SortFunc(refs, func(a, b ids.UUID) int { return bytes.Compare(a[:], b[:]) })
	var want []store.Snapshot
	for i, p := range payloads {
		want = append(want, snap(items[0], "page", "home", []string{"en", "de", "fr"}[i], 3, p, refs...))
	}
	putSnaps(t, db, items[0], want...)
	got, err := db.Reader().Snapshots(ctx, items, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("snapshots: %d %v", len(got), err)
	}
	m := byLocale(got)
	for _, w := range want {
		checkSnap(t, m[w.ItemID.String()+"/"+w.Locale], w)
	}
	// No refs and no key come back as empty values.
	bare := snap(items[1], "page", "", "en", 1, `{}`)
	putSnaps(t, db, items[1], bare)
	got, err = db.Reader().Snapshots(ctx, items[1:], []string{"en"})
	if err != nil || len(got) != 1 {
		t.Fatalf("bare: %d %v", len(got), err)
	}
	checkSnap(t, got[0], bare)
}

func testSnapshotReplace(t *testing.T, db store.DB) {
	ctx := context.Background()
	items := seedSnapItems(t, db, 2)
	putSnaps(t, db, items[0], snap(items[0], "page", "home", "en", 1, `{"v":1}`), snap(items[0], "page", "home", "de", 1, `{"v":1}`))
	putSnaps(t, db, items[1], snap(items[1], "page", "", "en", 1, `{"other":true}`))
	// A second put replaces every locale of the item and leaves other items alone.
	next := snap(items[0], "page", "home", "en", 2, `{"v":2}`)
	putSnaps(t, db, items[0], next)
	got, err := db.Reader().Snapshots(ctx, items, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("after replace: %+v %v", got, err)
	}
	m := byLocale(got)
	checkSnap(t, m[items[0].String()+"/en"], next)
	if _, ok := m[items[0].String()+"/de"]; ok {
		t.Error("the de snapshot survived a replace")
	}
	if _, ok := m[items[1].String()+"/en"]; !ok {
		t.Error("another item's snapshot was removed")
	}
	// Putting nothing and deleting both clear the item.
	putSnaps(t, db, items[0])
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteSnapshots(ctx, items[1]) })
	if got, err = db.Reader().Snapshots(ctx, items, nil); err != nil || len(got) != 0 {
		t.Fatalf("after clearing: %+v %v", got, err)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteSnapshots(ctx, items[1]) }) // no rows is fine
}

func testSnapshotLookups(t *testing.T, db store.DB) {
	ctx := context.Background()
	items := seedSnapItems(t, db, 4)
	r := db.Reader()
	for i, id := range items[:3] {
		key := ""
		if i == 0 {
			key = "home"
		}
		putSnaps(t, db, id, snap(id, "page", key, "en", 1, `{"i":`+strconv.Itoa(i)+`}`), snap(id, "page", key, "de", 1, `{"i":0}`))
	}
	missing := newIDs().New()
	got, err := r.Snapshots(ctx, []ids.UUID{items[0], items[2], items[3], missing}, []string{"en", "fr"})
	if err != nil || len(got) != 2 {
		t.Fatalf("items+locales: %d %v", len(got), err)
	}
	for _, s := range got {
		if s.Locale != "en" || (s.ItemID != items[0] && s.ItemID != items[2]) {
			t.Errorf("unexpected row %s/%s", s.ItemID, s.Locale)
		}
	}
	if got, err = r.Snapshots(ctx, items[:3], nil); err != nil || len(got) != 6 {
		t.Fatalf("all locales: %d %v", len(got), err)
	}
	if got, err = r.Snapshots(ctx, nil, []string{"en"}); err != nil || len(got) != 0 {
		t.Fatalf("no items: %d %v", len(got), err)
	}
	if got, err = r.SnapshotsByKey(ctx, "home", []string{"de"}); err != nil || len(got) != 1 || got[0].ItemID != items[0] || got[0].Locale != "de" {
		t.Fatalf("by key: %+v %v", got, err)
	}
	if got, err = r.SnapshotsByKey(ctx, "home", nil); err != nil || len(got) != 2 {
		t.Fatalf("by key, all locales: %d %v", len(got), err)
	}
	for _, key := range []string{"nope", ""} {
		if got, err = r.SnapshotsByKey(ctx, key, nil); err != nil || len(got) != 0 {
			t.Fatalf("by key %q: %d %v", key, len(got), err)
		}
	}
	// More items than SQLite binds in one statement.
	g := newIDs()
	many := make([]ids.UUID, 0, 40001)
	for range 40000 {
		many = append(many, g.New())
	}
	many = append(many, items[1])
	if got, err = r.Snapshots(ctx, many, []string{"en"}); err != nil || len(got) != 1 {
		t.Fatalf("many items: %d %v", len(got), err)
	}
}

func testSnapshotCascade(t *testing.T, db store.DB) {
	ctx := context.Background()
	items := seedSnapItems(t, db, 2)
	putSnaps(t, db, items[0], snap(items[0], "page", "home", "en", 1, `{}`))
	putSnaps(t, db, items[1], snap(items[1], "page", "", "en", 1, `{}`))
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, items[0]) })
	got, err := db.Reader().Snapshots(ctx, items, nil)
	if err != nil || len(got) != 1 || got[0].ItemID != items[1] {
		t.Fatalf("after item delete: %+v %v", got, err)
	}
	if got, err = db.Reader().SnapshotsByKey(ctx, "home", nil); err != nil || len(got) != 0 {
		t.Fatalf("key after item delete: %+v %v", got, err)
	}
}
