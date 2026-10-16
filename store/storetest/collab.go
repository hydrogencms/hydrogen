package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

var collabTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"CollabAppendSince", testCollabAppendSince},
	{"CollabConcurrentAppend", testCollabConcurrentAppend},
	{"CollabNoGapForReaders", testCollabNoGap},
	{"CollabCompact", testCollabCompact},
	{"CollabClaims", testCollabClaims},
	{"CollabClaimRace", testCollabClaimRace},
	{"CollabClients", testCollabClients},
	{"CollabStale", testCollabStale},
	{"CollabIdleSessions", testCollabIdle},
	{"CollabCascade", testCollabCascade},
	{"CollabLogSize", testCollabLogSize},
	{"CollabSession", testCollabSession},
}

func collabDo(db store.DB, fn func(store.CollabStore) error) error {
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Collab()) })
}

func collabMust(t *testing.T, db store.DB, fn func(store.CollabStore) error) {
	t.Helper()
	if err := collabDo(db, fn); err != nil {
		t.Fatal(err)
	}
}

func collabItem(t *testing.T, db store.DB) ids.UUID {
	t.Helper()
	id := newIDs().New()
	withContent(t, db, func(c store.ContentStore) error { return c.InsertItem(context.Background(), newItem(id, "page", "")) })
	return id
}

func collabAppend(t *testing.T, db store.DB, item ids.UUID, client, data string, init ...string) int64 {
	t.Helper()
	var seq int64
	collabMust(t, db, func(c store.CollabStore) (err error) {
		seq, err = c.AppendUpdate(context.Background(), store.CollabUpdate{Item: item, Client: client, Data: []byte(data), Init: init, At: t0})
		return
	})
	return seq
}

func collabSince(t *testing.T, db store.DB, item ids.UUID, after int64, limit int) []store.CollabUpdate {
	t.Helper()
	var out []store.CollabUpdate
	if err := db.View(context.Background(), func(tx store.Tx) (err error) {
		out, err = tx.Collab().UpdatesSince(context.Background(), item, after, limit)
		return
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func testCollabAppendSince(t *testing.T, db store.DB) {
	ctx := context.Background()
	a, b := collabItem(t, db), collabItem(t, db)
	var seqs []int64
	for i := range 5 {
		seqs = append(seqs, collabAppend(t, db, a, "c1", fmt.Sprintf("u%d", i)))
		collabAppend(t, db, b, "c2", "other")
	}
	if !slices.IsSorted(seqs) || slices.Compact(slices.Clone(seqs))[len(seqs)-1] != seqs[4] || len(slices.Compact(slices.Clone(seqs))) != 5 {
		t.Fatalf("seqs not strictly increasing: %v", seqs)
	}
	all := collabSince(t, db, a, 0, 0)
	if len(all) != 5 {
		t.Fatalf("updates = %d, want 5 (the other item's are not included)", len(all))
	}
	for i, u := range all {
		if u.Seq != seqs[i] || u.Item != a || u.Client != "c1" || string(u.Data) != fmt.Sprintf("u%d", i) || len(u.Init) != 0 || !u.At.Equal(t0) {
			t.Fatalf("update %d = %+v", i, u)
		}
	}
	if got := collabSince(t, db, a, seqs[1], 0); len(got) != 3 || got[0].Seq != seqs[2] {
		t.Fatalf("since seq 2: %+v", got)
	}
	if got := collabSince(t, db, a, 0, 2); len(got) != 2 || got[1].Seq != seqs[1] {
		t.Fatalf("limit 2: %+v", got)
	}
	if got := collabSince(t, db, a, seqs[4], 0); len(got) != 0 {
		t.Fatalf("since last: %+v", got)
	}
	// binary data and init parts round trip
	bin := []byte{0, 1, 2, 255, 0, 7}
	seq := int64(0)
	collabMust(t, db, func(c store.CollabStore) (err error) {
		seq, err = c.AppendUpdate(ctx, store.CollabUpdate{Item: a, Client: "c3", Data: bin, Init: []string{"shared", "l:en"}, At: t0.Add(time.Second)})
		return
	})
	last := collabSince(t, db, a, seqs[4], 0)
	if len(last) != 1 || last[0].Seq != seq || !bytes.Equal(last[0].Data, bin) || !slices.Equal(last[0].Init, []string{"shared", "l:en"}) || !last[0].At.Equal(t0.Add(time.Second)) {
		t.Fatalf("binary update: %+v", last)
	}
	collabMust(t, db, func(c store.CollabStore) error {
		if got, err := c.LastSeq(ctx, a); err != nil || got != seq {
			t.Fatalf("LastSeq = %d, %v, want %d", got, err, seq)
		}
		if got, err := c.LastSeq(ctx, ids.UUID{9}); err != nil || got != 0 {
			t.Fatalf("LastSeq of nothing = %d, %v", got, err)
		}
		return nil
	})
	// an unknown item is refused, and the failure does not poison other transactions
	err := collabDo(db, func(c store.CollabStore) error {
		_, err := c.AppendUpdate(ctx, store.CollabUpdate{Item: ids.UUID{9}, Client: "c", Data: []byte("x"), At: t0})
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("append to a missing item: %v, want ErrNotFound", err)
	}
}

func testCollabConcurrentAppend(t *testing.T, db store.DB) {
	a, b := collabItem(t, db), collabItem(t, db)
	const writers, per = 6, 15
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for w := range writers {
		for _, item := range []ids.UUID{a, b} {
			wg.Go(func() {
				for i := range per {
					err := collabDo(db, func(c store.CollabStore) error {
						_, err := c.AppendUpdate(context.Background(), store.CollabUpdate{Item: item, Client: fmt.Sprint("w", w), Data: []byte(fmt.Sprint(i)), At: t0})
						return err
					})
					if err != nil {
						errs <- err
						return
					}
				}
			})
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for _, item := range []ids.UUID{a, b} {
		all := collabSince(t, db, item, 0, 0)
		if len(all) != writers*per {
			t.Fatalf("updates = %d, want %d", len(all), writers*per)
		}
		next := map[string]int{}
		for i, u := range all {
			if i > 0 && u.Seq <= all[i-1].Seq {
				t.Fatalf("seq %d after %d", u.Seq, all[i-1].Seq)
			}
			if string(u.Data) != fmt.Sprint(next[u.Client]) {
				t.Fatalf("client %s: update %s out of its own order", u.Client, u.Data)
			}
			next[u.Client]++
		}
	}
}

// testCollabNoGap follows the log the way an SSE relay does (always from the last seq it saw) while writers append
// concurrently: if a lower seq could commit after a higher one, the follower would skip it for good.
func testCollabNoGap(t *testing.T, db store.DB) {
	a, b := collabItem(t, db), collabItem(t, db)
	const writers, per = 5, 20
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for w := range writers {
		for _, item := range []ids.UUID{a, b} {
			wg.Go(func() {
				for i := range per {
					err := collabDo(db, func(c store.CollabStore) error {
						_, err := c.AppendUpdate(context.Background(), store.CollabUpdate{Item: item, Client: fmt.Sprint("w", w), Data: []byte(fmt.Sprint(w, "/", i)), At: t0})
						return err
					})
					if err != nil {
						errs <- err
						return
					}
				}
			})
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	seen := map[string]bool{}
	var last int64
	deadline := time.Now().Add(60 * time.Second)
	finished := false
	for len(seen) < writers*per {
		if time.Now().After(deadline) {
			t.Fatalf("follower saw %d of %d updates", len(seen), writers*per)
		}
		got := collabSince(t, db, a, last, 7)
		for _, u := range got {
			if u.Seq <= last {
				t.Fatalf("seq %d after %d", u.Seq, last)
			}
			last = u.Seq
			seen[string(u.Data)] = true
		}
		if len(got) == 0 {
			select {
			case <-done:
				if finished {
					t.Fatalf("writers done but follower saw %d of %d: a seq was skipped", len(seen), writers*per)
				}
				finished = true
			case <-time.After(2 * time.Millisecond):
			}
		}
	}
	<-done
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func testCollabCompact(t *testing.T, db store.DB) {
	ctx := context.Background()
	item := collabItem(t, db)
	collabMust(t, db, func(c store.CollabStore) error {
		if n, err := c.Compact(ctx, item, 5, []byte("s"), "c", t0); err != nil || n != 0 {
			t.Fatalf("compact of an empty log = %d, %v", n, err)
		}
		return nil
	})
	s := make([]int64, 5)
	s[0] = collabAppend(t, db, item, "a", "1", "shared")
	s[1] = collabAppend(t, db, item, "a", "2")
	s[2] = collabAppend(t, db, item, "b", "3", "l:en")
	s[3] = collabAppend(t, db, item, "b", "4")
	s[4] = collabAppend(t, db, item, "a", "5")
	other := collabItem(t, db)
	collabAppend(t, db, other, "z", "keep")

	collabMust(t, db, func(c store.CollabStore) error {
		if n, err := c.Compact(ctx, item, s[2], []byte("S123"), "b", t0.Add(time.Minute)); err != nil || n != s[2] {
			t.Fatalf("compact = %d, %v, want %d", n, err, s[2])
		}
		return nil
	})
	all := collabSince(t, db, item, 0, 0)
	if len(all) != 3 || all[0].Seq != s[2] || string(all[0].Data) != "S123" || all[0].Client != "b" ||
		!slices.Equal(all[0].Init, []string{"l:en", "shared"}) || all[1].Seq != s[3] || all[2].Seq != s[4] {
		t.Fatalf("after compact: %+v", all)
	}
	if got := collabSince(t, db, item, s[2], 0); len(got) != 2 {
		t.Fatalf("a reader at the compacted seq gets %d updates, want 2", len(got))
	}
	if got := collabSince(t, db, item, s[1], 0); len(got) != 3 || string(got[0].Data) != "S123" {
		t.Fatalf("a reader before the compacted seq gets the state first: %+v", got)
	}
	if got := collabSince(t, db, other, 0, 0); len(got) != 1 {
		t.Fatalf("another item's log was touched: %+v", got)
	}
	// upto beyond the log clamps to the last seq; the next append still gets a higher seq
	collabMust(t, db, func(c store.CollabStore) error {
		if n, err := c.Compact(ctx, item, s[4]+1000, []byte("ALL"), "a", t0); err != nil || n != s[4] {
			t.Fatalf("clamped compact = %d, %v, want %d", n, err, s[4])
		}
		if n, err := c.Compact(ctx, item, 0, []byte("x"), "a", t0); err != nil || n != 0 {
			t.Fatalf("compact upto 0 = %d, %v", n, err)
		}
		return nil
	})
	if all = collabSince(t, db, item, 0, 0); len(all) != 1 || string(all[0].Data) != "ALL" || all[0].Seq != s[4] {
		t.Fatalf("after full compact: %+v", all)
	}
	if next := collabAppend(t, db, item, "a", "6"); next <= s[4] {
		t.Fatalf("append after compact got seq %d <= %d", next, s[4])
	}
}

func collabClaim(t *testing.T, db store.DB, item ids.UUID, parts []string, client string, now time.Time) store.CollabClaim {
	t.Helper()
	var out store.CollabClaim
	collabMust(t, db, func(c store.CollabStore) (err error) {
		out, err = c.ClaimParts(context.Background(), item, parts, client, now, 15*time.Second)
		return
	})
	return out
}

func testCollabClaims(t *testing.T, db store.DB) {
	ctx := context.Background()
	item := collabItem(t, db)
	parts := []string{"shared", "l:en"}

	got := collabClaim(t, db, item, parts, "a", t0)
	if !slices.Equal(got.Claimed, parts) || len(got.Pending) != 0 || len(got.Seeded) != 0 {
		t.Fatalf("first claim: %+v", got)
	}
	// b waits; a asking again keeps its claim, but does not extend it
	if got = collabClaim(t, db, item, parts, "b", t0.Add(5*time.Second)); len(got.Claimed) != 0 || !slices.Equal(got.Pending, parts) {
		t.Fatalf("b during a's claim: %+v", got)
	}
	if got = collabClaim(t, db, item, []string{"shared"}, "a", t0.Add(6*time.Second)); !slices.Equal(got.Claimed, []string{"shared"}) {
		t.Fatalf("a re-claims its own: %+v", got)
	}
	collabMust(t, db, func(c store.CollabStore) error {
		ps, err := c.Parts(ctx, item)
		if err != nil || len(ps) != 2 || !ps[1].ClaimedAt.Equal(t0) || ps[1].ClaimedBy != "a" {
			t.Fatalf("a re-claim must keep the first claimed_at: %+v, %v", ps, err)
		}
		return nil
	})
	// both claims (t0) are expired at t0+16s, however often a asked: b takes both over
	got = collabClaim(t, db, item, parts, "b", t0.Add(16*time.Second))
	if !slices.Equal(got.Claimed, parts) || len(got.Pending) != 0 {
		t.Fatalf("b after expiry of the claims: %+v", got)
	}
	// a lost both: its MarkSeeded does not count; b's does
	collabMust(t, db, func(c store.CollabStore) error {
		if m, err := c.MarkSeeded(ctx, item, parts, "a"); err != nil || len(m) != 0 {
			t.Fatalf("a marks %v, %v; want nothing", m, err)
		}
		if m, err := c.MarkSeeded(ctx, item, []string{"shared"}, "b"); err != nil || !slices.Equal(m, []string{"shared"}) {
			t.Fatalf("b marks %v, %v; want shared", m, err)
		}
		if m, err := c.MarkSeeded(ctx, item, []string{"shared"}, "b"); err != nil || len(m) != 0 {
			t.Fatalf("marking twice: %v, %v", m, err)
		}
		if m, err := c.MarkSeeded(ctx, item, []string{"nope"}, "b"); err != nil || len(m) != 0 {
			t.Fatalf("marking an unclaimed part: %v, %v", m, err)
		}
		return nil
	})
	// a client whose own claim expired claims afresh (and holds it for a full expiry again)
	collabClaim(t, db, item, []string{"l:fr"}, "e", t0.Add(time.Minute))
	if got = collabClaim(t, db, item, []string{"l:fr"}, "e", t0.Add(time.Minute+20*time.Second)); !slices.Equal(got.Claimed, []string{"l:fr"}) {
		t.Fatalf("e re-claims after expiry: %+v", got)
	}
	if got = collabClaim(t, db, item, []string{"l:fr"}, "f", t0.Add(time.Minute+30*time.Second)); !slices.Equal(got.Pending, []string{"l:fr"}) {
		t.Fatalf("e's fresh claim must hold: %+v", got)
	}
	collabMust(t, db, func(c store.CollabStore) error { return c.ReleaseClaims(ctx, item, "e") })
	// seeded parts are never claimed again, however old
	got = collabClaim(t, db, item, []string{"shared", "l:en"}, "c", t0.Add(24*time.Hour))
	if !slices.Equal(got.Seeded, []string{"shared"}) || !slices.Equal(got.Claimed, []string{"l:en"}) {
		t.Fatalf("claim of a seeded part: %+v", got)
	}
	collabMust(t, db, func(c store.CollabStore) error {
		ps, err := c.Parts(ctx, item)
		if err != nil || len(ps) != 2 || ps[0].Part != "l:en" || ps[0].ClaimedBy != "c" || ps[0].Seeded || !ps[0].ClaimedAt.Equal(t0.Add(24*time.Hour)) ||
			ps[1].Part != "shared" || ps[1].ClaimedBy != "b" || !ps[1].Seeded || ps[1].Item != item {
			t.Fatalf("parts: %+v, %v", ps, err)
		}
		// ReleaseClaims drops only the unseeded claims of that client
		if err := c.ReleaseClaims(ctx, item, "c"); err != nil {
			return err
		}
		if err := c.ReleaseClaims(ctx, item, "b"); err != nil {
			return err
		}
		ps, _ = c.Parts(ctx, item)
		if len(ps) != 1 || ps[0].Part != "shared" {
			t.Fatalf("after release: %+v", ps)
		}
		return nil
	})
	// ReleaseExpiredClaims
	collabClaim(t, db, item, []string{"l:de"}, "d", t0.Add(time.Hour))
	collabMust(t, db, func(c store.CollabStore) error {
		if items, err := c.ReleaseExpiredClaims(ctx, t0.Add(time.Hour-time.Second)); err != nil || len(items) != 0 {
			t.Fatalf("early release: %v, %v", items, err)
		}
		if items, err := c.ReleaseExpiredClaims(ctx, t0.Add(time.Hour)); err != nil || len(items) != 1 || items[0] != item {
			t.Fatalf("release: %v, %v", items, err)
		}
		if ps, _ := c.Parts(ctx, item); len(ps) != 1 {
			t.Fatalf("seeded part must stay: %+v", ps)
		}
		return nil
	})
	err := collabDo(db, func(c store.CollabStore) error {
		_, err := c.ClaimParts(ctx, ids.UUID{9}, []string{"shared"}, "a", t0, time.Second)
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("claim on a missing item: %v", err)
	}
}

func testCollabClaimRace(t *testing.T, db store.DB) {
	item := collabItem(t, db)
	parts := []string{"shared", "l:en", "l:de"}
	const n = 8
	var wg sync.WaitGroup
	results := make([]store.CollabClaim, n)
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			err := collabDo(db, func(c store.CollabStore) (err error) {
				results[i], err = c.ClaimParts(context.Background(), item, parts, fmt.Sprint("c", i), t0, 15*time.Second)
				return
			})
			if err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for _, p := range parts {
		winners, pending := 0, 0
		for _, r := range results {
			if slices.Contains(r.Claimed, p) {
				winners++
			}
			if slices.Contains(r.Pending, p) {
				pending++
			}
		}
		if winners != 1 || pending != n-1 {
			t.Fatalf("part %s: %d claimers and %d pending, want 1 and %d", p, winners, pending, n-1)
		}
	}
}

func collabClient(item ids.UUID, id, name string, joined time.Time) store.CollabClient {
	return store.CollabClient{Client: id, Item: item, Principal: "user:" + name, Name: name, Locale: "en", Focus: "title", Idle: false,
		Edit: true, Color: "#e11d48", JoinedAt: joined, SeenAt: joined}
}

func testCollabClients(t *testing.T, db store.DB) {
	ctx := context.Background()
	item, other := collabItem(t, db), collabItem(t, db)
	a, b := collabClient(item, "ca", "Ann", t0), collabClient(item, "cb", "Bob", t0.Add(time.Second))
	b.Edit, b.Focus = false, ""
	collabMust(t, db, func(c store.CollabStore) error {
		for _, cl := range []store.CollabClient{b, a, collabClient(other, "cz", "Zed", t0)} {
			if err := c.PutClient(ctx, cl); err != nil {
				return err
			}
		}
		return nil
	})
	collabMust(t, db, func(c store.CollabStore) error {
		got, err := c.Clients(ctx, item)
		if err != nil || len(got) != 2 || got[0] != a || got[1] != b {
			t.Fatalf("clients: %+v, %v, want [%+v %+v]", got, err, a, b)
		}
		if g, err := c.GetClient(ctx, "cb"); err != nil || g != b {
			t.Fatalf("get: %+v, %v", g, err)
		}
		if _, err := c.GetClient(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("get unknown: %v", err)
		}
		if err := c.TouchClient(ctx, "ca", "de", "body[2].props.heading", true, t0.Add(time.Minute)); err != nil {
			return err
		}
		g, _ := c.GetClient(ctx, "ca")
		if g.Locale != "de" || g.Focus != "body[2].props.heading" || !g.Idle || !g.SeenAt.Equal(t0.Add(time.Minute)) || !g.JoinedAt.Equal(t0) || !g.Edit || g.Name != "Ann" {
			t.Fatalf("after touch: %+v", g)
		}
		if err := c.TouchClient(ctx, "nope", "en", "", false, t0); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("touch unknown: %v", err)
		}
		// PutClient again replaces the presence data and keeps joined_at
		a2 := a
		a2.Name, a2.JoinedAt, a2.SeenAt = "Ann B", t0.Add(time.Hour), t0.Add(time.Hour)
		if err := c.PutClient(ctx, a2); err != nil {
			return err
		}
		if g, _ = c.GetClient(ctx, "ca"); g.Name != "Ann B" || !g.JoinedAt.Equal(t0) || !g.SeenAt.Equal(t0.Add(time.Hour)) {
			t.Fatalf("after re-put: %+v", g)
		}
		return nil
	})
	// removing a client frees its unseeded claims and is idempotent; a wrong item removes nothing
	collabClaim(t, db, item, []string{"shared", "l:en"}, "cb", t0)
	collabMust(t, db, func(c store.CollabStore) error {
		if _, err := c.MarkSeeded(ctx, item, []string{"shared"}, "cb"); err != nil {
			return err
		}
		if err := c.RemoveClient(ctx, other, "cb"); err != nil {
			return err
		}
		if _, err := c.GetClient(ctx, "cb"); err != nil {
			t.Fatalf("removed through the wrong item: %v", err)
		}
		for range 2 {
			if err := c.RemoveClient(ctx, item, "cb"); err != nil {
				return err
			}
		}
		if _, err := c.GetClient(ctx, "cb"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("still there: %v", err)
		}
		if ps, _ := c.Parts(ctx, item); len(ps) != 1 || ps[0].Part != "shared" {
			t.Fatalf("parts after remove: %+v", ps)
		}
		if err := c.PutClient(ctx, collabClient(ids.UUID{9}, "cx", "X", t0)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("put for a missing item: %v", err)
		}
		return nil
	})
}

func testCollabStale(t *testing.T, db store.DB) {
	ctx := context.Background()
	i1, i2, i3 := collabItem(t, db), collabItem(t, db), collabItem(t, db)
	collabMust(t, db, func(c store.CollabStore) error {
		for _, cl := range []store.CollabClient{
			collabClient(i1, "a1", "A", t0), collabClient(i1, "a2", "B", t0.Add(time.Second)),
			collabClient(i2, "b1", "C", t0), collabClient(i3, "c1", "D", t0.Add(time.Hour)),
		} {
			if err := c.PutClient(ctx, cl); err != nil {
				return err
			}
		}
		return nil
	})
	collabClaim(t, db, i2, []string{"shared"}, "b1", t0)
	collabMust(t, db, func(c store.CollabStore) error {
		if items, err := c.RemoveStaleClients(ctx, t0); err != nil || len(items) != 0 {
			t.Fatalf("boundary is not stale: %v, %v", items, err)
		}
		items, err := c.RemoveStaleClients(ctx, t0.Add(time.Minute))
		slices.SortFunc(items, func(x, y ids.UUID) int { return bytes.Compare(x[:], y[:]) })
		want := []ids.UUID{i1, i2}
		slices.SortFunc(want, func(x, y ids.UUID) int { return bytes.Compare(x[:], y[:]) })
		if err != nil || !slices.Equal(items, want) {
			t.Fatalf("stale items = %v, %v, want %v", items, err, want)
		}
		if cl, _ := c.Clients(ctx, i1); len(cl) != 0 {
			t.Fatalf("i1 clients: %+v", cl)
		}
		if cl, _ := c.Clients(ctx, i3); len(cl) != 1 {
			t.Fatalf("i3 clients: %+v", cl)
		}
		if ps, _ := c.Parts(ctx, i2); len(ps) != 0 {
			t.Fatalf("stale client's claim survived: %+v", ps)
		}
		return nil
	})
}

func testCollabIdle(t *testing.T, db store.DB) {
	ctx := context.Background()
	idle, busy, fresh := collabItem(t, db), collabItem(t, db), collabItem(t, db)
	collabAppend(t, db, idle, "x", "u")
	collabAppend(t, db, busy, "x", "u")
	collabAppend(t, db, fresh, "x", "u") // at t0 as well
	collabClaim(t, db, fresh, []string{"shared"}, "x", t0.Add(time.Hour))
	collabMust(t, db, func(c store.CollabStore) error {
		return c.PutClient(ctx, collabClient(busy, "cb", "B", t0))
	})
	collabMust(t, db, func(c store.CollabStore) error {
		items, err := c.DropIdleSessions(ctx, t0.Add(time.Minute))
		if err != nil || len(items) != 1 || items[0] != idle {
			t.Fatalf("dropped %v, %v, want only the idle item", items, err)
		}
		if n, _, _ := c.LogSize(ctx, idle); n != 0 {
			t.Fatalf("idle log not deleted: %d", n)
		}
		if n, _, _ := c.LogSize(ctx, busy); n != 1 {
			t.Fatalf("a session with a client must stay: %d", n)
		}
		if n, _, _ := c.LogSize(ctx, fresh); n != 1 {
			t.Fatalf("recent activity (the claim) keeps the session: %d", n)
		}
		return nil
	})
}

func testCollabCascade(t *testing.T, db store.DB) {
	ctx := context.Background()
	item, keep := collabItem(t, db), collabItem(t, db)
	for _, it := range []ids.UUID{item, keep} {
		collabAppend(t, db, it, "c", "u")
		collabClaim(t, db, it, []string{"shared"}, "c", t0)
		collabMust(t, db, func(c store.CollabStore) error { return c.PutClient(ctx, collabClient(it, "c-"+it.String(), "N", t0)) })
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, item) })
	collabMust(t, db, func(c store.CollabStore) error {
		if n, _, _ := c.LogSize(ctx, item); n != 0 {
			t.Fatalf("updates survived the item: %d", n)
		}
		if ps, _ := c.Parts(ctx, item); len(ps) != 0 {
			t.Fatalf("parts survived: %+v", ps)
		}
		if cl, _ := c.Clients(ctx, item); len(cl) != 0 {
			t.Fatalf("clients survived: %+v", cl)
		}
		if _, err := c.GetClient(ctx, "c-"+item.String()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("client survived: %v", err)
		}
		if n, _, _ := c.LogSize(ctx, keep); n != 1 {
			t.Fatalf("the other item lost its log: %d", n)
		}
		// DeleteSession removes everything of one item and is idempotent
		for range 2 {
			if err := c.DeleteSession(ctx, keep); err != nil {
				return err
			}
		}
		if cl, _ := c.Clients(ctx, keep); len(cl) != 0 {
			t.Fatalf("clients after DeleteSession: %+v", cl)
		}
		if ps, _ := c.Parts(ctx, keep); len(ps) != 0 {
			t.Fatalf("parts after DeleteSession: %+v", ps)
		}
		if n, _, _ := c.LogSize(ctx, keep); n != 0 {
			t.Fatalf("log after DeleteSession: %d", n)
		}
		return nil
	})
}

func testCollabLogSize(t *testing.T, db store.DB) {
	ctx := context.Background()
	item := collabItem(t, db)
	collabMust(t, db, func(c store.CollabStore) error {
		if n, b, err := c.LogSize(ctx, item); err != nil || n != 0 || b != 0 {
			t.Fatalf("empty: %d, %d, %v", n, b, err)
		}
		return nil
	})
	collabAppend(t, db, item, "c", "12345")
	collabAppend(t, db, item, "c", "")
	collabAppend(t, db, item, "c", "abc")
	collabMust(t, db, func(c store.CollabStore) error {
		if n, b, err := c.LogSize(ctx, item); err != nil || n != 3 || b != 8 {
			t.Fatalf("size: %d, %d, %v, want 3, 8", n, b, err)
		}
		return nil
	})
}

func testCollabSession(t *testing.T, db store.DB) {
	ctx := context.Background()
	item := collabItem(t, db)
	collabMust(t, db, func(c store.CollabStore) error {
		if _, err := c.GetSession(ctx, item); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session before: %v", err)
		}
		if err := c.LockSession(ctx, item); err != nil {
			t.Fatal(err)
		}
		s, created, err := c.EnsureSession(ctx, item, "e1", t0)
		if err != nil || !created || s.Etag != "e1" || !s.StartedAt.Equal(t0) || s.CompactClient != "" || !s.CompactAt.IsZero() {
			t.Fatalf("ensure: %+v %v %v", s, created, err)
		}
		// A second ensure keeps the row (and its etag).
		s, created, err = c.EnsureSession(ctx, item, "e2", t0.Add(time.Second))
		if err != nil || created || s.Etag != "e1" || !s.StartedAt.Equal(t0) {
			t.Fatalf("ensure again: %+v %v %v", s, created, err)
		}
		s.Etag, s.CompactClient, s.CompactUpto, s.CompactAt = "e3", "cl", 7, t0.Add(2*time.Second)
		if err := c.UpdateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
		got, err := c.GetSession(ctx, item)
		if err != nil || got != s {
			t.Fatalf("get: %+v, %v (want %+v)", got, err, s)
		}
		if _, _, err := c.EnsureSession(ctx, ids.UUID{9}, "x", t0); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("ensure on a missing item: %v", err)
		}
		if err := c.UpdateSession(ctx, store.CollabSession{Item: ids.UUID{9}}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("update of a missing session: %v", err)
		}
		if err := c.PutClient(ctx, collabClient(item, "cl-edit", "a", t0)); err != nil {
			t.Fatal(err)
		}
		if err := c.SetClientEdit(ctx, "cl-edit", true); err != nil {
			t.Fatal(err)
		}
		if cl, _ := c.GetClient(ctx, "cl-edit"); !cl.Edit {
			t.Error("edit not set")
		}
		if err := c.SetClientEdit(ctx, "cl-edit", false); err != nil {
			t.Fatal(err)
		}
		if cl, _ := c.GetClient(ctx, "cl-edit"); cl.Edit {
			t.Error("edit not cleared")
		}
		if err := c.SetClientEdit(ctx, "nobody", true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("set edit of an unknown client: %v", err)
		}
		return nil
	})
	// A session row alone makes the session live until it is idle; DeleteSession removes it.
	collabMust(t, db, func(c store.CollabStore) error {
		if err := c.RemoveClient(ctx, item, "cl-edit"); err != nil {
			return err
		}
		if items, err := c.DropIdleSessions(ctx, t0); err != nil || len(items) != 0 {
			t.Fatalf("early idle drop: %v, %v", items, err)
		}
		items, err := c.DropIdleSessions(ctx, t0.Add(time.Hour))
		if err != nil || len(items) != 1 || items[0] != item {
			t.Fatalf("idle drop: %v, %v", items, err)
		}
		if _, err := c.GetSession(ctx, item); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session after idle drop: %v", err)
		}
		if _, _, err := c.EnsureSession(ctx, item, "e", t0); err != nil {
			return err
		}
		if err := c.DeleteSession(ctx, item); err != nil {
			return err
		}
		if _, err := c.GetSession(ctx, item); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session after delete: %v", err)
		}
		return nil
	})
}
