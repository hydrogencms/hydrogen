package storetest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

var previewTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"PreviewShares", testPreviewShares},
	{"PreviewRevoke", testPreviewRevoke},
	{"PreviewDeleteExpired", testPreviewDeleteExpired},
	{"PreviewRelay", testPreviewRelay},
	{"PreviewRelayStale", testPreviewRelayStale},
}

func previewDo(t *testing.T, db store.DB, fn func(store.PreviewStore) error) error {
	t.Helper()
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Preview()) })
}

func previewMust(t *testing.T, db store.DB, fn func(store.PreviewStore) error) {
	t.Helper()
	if err := previewDo(t, db, fn); err != nil {
		t.Fatal(err)
	}
}

func newShare(g *ids.Generator, item ids.UUID, hash byte, created time.Time, ttl time.Duration) store.ShareSession {
	return store.ShareSession{
		ID: g.New(), TokenHash: bytes.Repeat([]byte{hash}, 32), Item: item, Channel: "web", Locale: "en",
		CreatedBy: "user:" + string(rune('a'+hash)), CreatedAt: created, ExpiresAt: created.Add(ttl),
	}
}

func sameShare(t *testing.T, got, want store.ShareSession) {
	t.Helper()
	if got.ID != want.ID || got.Item != want.Item || !bytes.Equal(got.TokenHash, want.TokenHash) ||
		got.Channel != want.Channel || got.Locale != want.Locale || got.CreatedBy != want.CreatedBy ||
		!got.CreatedAt.Equal(want.CreatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) ||
		(got.RevokedAt == nil) != (want.RevokedAt == nil) || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("share = %+v, want %+v", got, want)
	}
}

func testPreviewShares(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := ids.NewGenerator(clock.NewManual(t0), nil)
	itemA, itemB := g.New(), g.New() // no item rows: no foreign key
	s1 := newShare(g, itemA, 1, t0, time.Hour)
	s2 := newShare(g, itemA, 2, t0.Add(time.Minute), 24*time.Hour)
	s3 := newShare(g, itemB, 3, t0, time.Hour)
	for _, s := range []store.ShareSession{s1, s2, s3} {
		previewMust(t, db, func(p store.PreviewStore) error { return p.CreateShare(ctx, s) })
	}

	dup := newShare(g, itemB, 1, t0, time.Hour) // same token hash as s1
	if err := previewDo(t, db, func(p store.PreviewStore) error { return p.CreateShare(ctx, dup) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("duplicate hash: %v", err)
	}

	previewMust(t, db, func(p store.PreviewStore) error {
		got, err := p.ShareByHash(ctx, s2.TokenHash)
		if err != nil {
			return err
		}
		sameShare(t, got, s2)
		got, err = p.GetShare(ctx, s1.ID)
		if err != nil {
			return err
		}
		sameShare(t, got, s1)
		if _, err := p.ShareByHash(ctx, bytes.Repeat([]byte{9}, 32)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown hash: %v", err)
		}
		if _, err := p.GetShare(ctx, g.New()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown id: %v", err)
		}
		list, err := p.ListShares(ctx, itemA)
		if err != nil {
			return err
		}
		if len(list) != 2 {
			t.Fatalf("list = %d shares", len(list))
		}
		sameShare(t, list[0], s2) // newest first
		sameShare(t, list[1], s1)
		list, err = p.ListShares(ctx, g.New())
		if len(list) != 0 {
			t.Fatalf("list of unknown item = %d", len(list))
		}
		return err
	})
}

func testPreviewRevoke(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := ids.NewGenerator(clock.NewManual(t0), nil)
	s := newShare(g, g.New(), 1, t0, time.Hour)
	previewMust(t, db, func(p store.PreviewStore) error { return p.CreateShare(ctx, s) })

	at := t0.Add(10 * time.Minute)
	previewMust(t, db, func(p store.PreviewStore) error { return p.RevokeShare(ctx, s.ID, at) })
	previewMust(t, db, func(p store.PreviewStore) error { return p.RevokeShare(ctx, s.ID, at.Add(time.Hour)) }) // idempotent
	previewMust(t, db, func(p store.PreviewStore) error {
		got, err := p.GetShare(ctx, s.ID)
		if err != nil {
			return err
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
			t.Fatalf("revokedAt = %v, want the first time %v", got.RevokedAt, at)
		}
		byHash, err := p.ShareByHash(ctx, s.TokenHash)
		if err != nil || byHash.RevokedAt == nil {
			t.Fatalf("by hash after revoke: %+v %v", byHash, err)
		}
		return nil
	})
	if err := previewDo(t, db, func(p store.PreviewStore) error { return p.RevokeShare(ctx, g.New(), at) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
}

func testPreviewDeleteExpired(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := ids.NewGenerator(clock.NewManual(t0), nil)
	item := g.New()
	expired := newShare(g, item, 1, t0, time.Hour)        // expires t0+1h
	live := newShare(g, item, 2, t0, 48*time.Hour)        // expires t0+48h
	revoked := newShare(g, item, 3, t0, 48*time.Hour)     // revoked t0+2h
	revokedLate := newShare(g, item, 4, t0, 48*time.Hour) // revoked t0+10h
	for _, s := range []store.ShareSession{expired, live, revoked, revokedLate} {
		previewMust(t, db, func(p store.PreviewStore) error { return p.CreateShare(ctx, s) })
	}
	previewMust(t, db, func(p store.PreviewStore) error {
		if err := p.RevokeShare(ctx, revoked.ID, t0.Add(2*time.Hour)); err != nil {
			return err
		}
		return p.RevokeShare(ctx, revokedLate.ID, t0.Add(10*time.Hour))
	})

	previewMust(t, db, func(p store.PreviewStore) error {
		n, err := p.DeleteExpiredShares(ctx, t0.Add(30*time.Minute))
		if err != nil || n != 0 {
			t.Fatalf("deleted %d before anything expired", n)
		}
		if n, err = p.DeleteExpiredShares(ctx, t0.Add(3*time.Hour)); err != nil || n != 2 {
			t.Fatalf("deleted %d, %v; want the expired and the early revoked share", n, err)
		}
		list, err := p.ListShares(ctx, item)
		if err != nil || len(list) != 2 {
			t.Fatalf("remaining %d, %v", len(list), err)
		}
		if n, err = p.DeleteExpiredShares(ctx, t0.Add(3*time.Hour)); err != nil || n != 0 {
			t.Fatalf("second run deleted %d, %v", n, err)
		}
		if _, err := p.GetShare(ctx, expired.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expired share still there: %v", err)
		}
		return nil
	})
}

func relayFieldsEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("fields %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if !bytes.Equal(ga, gb) {
		t.Fatalf("fields = %s, want %s", got, want)
	}
}

func testPreviewRelay(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := ids.NewGenerator(clock.NewManual(t0), nil)
	item := g.New()
	put := func(locale, fields, by string, at time.Time) int64 {
		var rev int64
		previewMust(t, db, func(p store.PreviewStore) (err error) {
			rev, err = p.PutRelay(ctx, store.RelayDraft{Item: item, Locale: locale, Fields: json.RawMessage(fields), UpdatedBy: by, UpdatedAt: at})
			return err
		})
		return rev
	}

	if err := previewDo(t, db, func(p store.PreviewStore) error { _, err := p.GetRelay(ctx, item, "en"); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if rev := put("en", `{"title":"one","n":1}`, "user:a", t0); rev != 1 {
		t.Fatalf("first rev = %d", rev)
	}
	if rev := put("en", `{"title":"two"}`, "user:b", t0.Add(time.Second)); rev != 2 {
		t.Fatalf("second rev = %d", rev)
	}
	if rev := put("de", `{"title":"eins"}`, "user:a", t0); rev != 1 {
		t.Fatalf("other locale rev = %d", rev)
	}
	previewMust(t, db, func(p store.PreviewStore) error {
		d, err := p.GetRelay(ctx, item, "en")
		if err != nil {
			return err
		}
		if d.Item != item || d.Locale != "en" || d.Rev != 2 || d.UpdatedBy != "user:b" || !d.UpdatedAt.Equal(t0.Add(time.Second)) {
			t.Fatalf("relay = %+v", d)
		}
		relayFieldsEqual(t, d.Fields, `{"title":"two"}`)
		de, err := p.GetRelay(ctx, item, "de")
		if err != nil || de.Rev != 1 {
			t.Fatalf("de = %+v, %v", de, err)
		}
		return nil
	})

	previewMust(t, db, func(p store.PreviewStore) error {
		other := ids.NewGenerator(clock.NewManual(t0), nil).New()
		got, err := p.GetRelays(ctx, []ids.UUID{item, other})
		if err != nil || len(got) != 2 {
			t.Fatalf("GetRelays = %+v, %v; want the two locales of the item", got, err)
		}
		for _, d := range got {
			if d.Item != item || (d.Locale == "en") != (d.Rev == 2) {
				t.Fatalf("GetRelays row = %+v", d)
			}
		}
		if got, err := p.GetRelays(ctx, nil); err != nil || len(got) != 0 {
			t.Fatalf("GetRelays(nil) = %+v, %v", got, err)
		}
		return nil
	})
	previewMust(t, db, func(p store.PreviewStore) error { return p.DeleteRelay(ctx, item, "en") })
	previewMust(t, db, func(p store.PreviewStore) error { return p.DeleteRelay(ctx, item, "en") }) // idempotent
	if err := previewDo(t, db, func(p store.PreviewStore) error { _, err := p.GetRelay(ctx, item, "en"); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if rev := put("en", `{}`, "user:a", t0.Add(time.Minute)); rev != 1 {
		t.Fatalf("rev after delete = %d, want a fresh 1", rev)
	}
	previewMust(t, db, func(p store.PreviewStore) error {
		if d, err := p.GetRelay(ctx, item, "de"); err != nil || d.Rev != 1 {
			t.Fatalf("de after deleting en: %+v, %v", d, err)
		}
		return nil
	})
}

func testPreviewRelayStale(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := ids.NewGenerator(clock.NewManual(t0), nil)
	old, fresh := g.New(), g.New()
	previewMust(t, db, func(p store.PreviewStore) error {
		if _, err := p.PutRelay(ctx, store.RelayDraft{Item: old, Locale: "en", Fields: json.RawMessage(`{}`), UpdatedAt: t0}); err != nil {
			return err
		}
		_, err := p.PutRelay(ctx, store.RelayDraft{Item: fresh, Locale: "en", Fields: json.RawMessage(`{}`), UpdatedAt: t0.Add(2 * time.Hour)})
		return err
	})
	previewMust(t, db, func(p store.PreviewStore) error {
		if n, err := p.DeleteStaleRelays(ctx, t0); err != nil || n != 0 {
			t.Fatalf("deleted %d, %v; the boundary row is not before", n, err)
		}
		if n, err := p.DeleteStaleRelays(ctx, t0.Add(time.Hour)); err != nil || n != 1 {
			t.Fatalf("deleted %d, %v", n, err)
		}
		if _, err := p.GetRelay(ctx, old, "en"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("stale relay still there: %v", err)
		}
		if _, err := p.GetRelay(ctx, fresh, "en"); err != nil {
			t.Fatalf("fresh relay: %v", err)
		}
		return nil
	})
}
