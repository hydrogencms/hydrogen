package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var reviewTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"ReviewCRUD", testReviewCRUD},
	{"ReviewTransition", testReviewTransition},
	{"ReviewTransitionRace", testReviewTransitionRace},
	{"ReviewEvents", testReviewEvents},
	{"ReviewList", testReviewList},
	{"ReviewCascade", testReviewCascade},
}

func reviewDo(db store.DB, fn func(store.ReviewStore) error) error {
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Reviews()) })
}

func reviewMust(t *testing.T, db store.DB, fn func(store.ReviewStore) error) {
	t.Helper()
	if err := reviewDo(db, fn); err != nil {
		t.Fatal(err)
	}
}

func seedReviewItems(t *testing.T, db store.DB, g *ids.Generator, types ...string) []ids.UUID {
	t.Helper()
	list := make([]ids.UUID, len(types))
	withContent(t, db, func(c store.ContentStore) error {
		for i, typ := range types {
			list[i] = g.New()
			if err := c.InsertItem(context.Background(), newItem(list[i], typ, "")); err != nil {
				return err
			}
		}
		return nil
	})
	return list
}

func sameReview(t *testing.T, got, want store.ItemReview) {
	t.Helper()
	if got.Item != want.Item || got.State != want.State || got.DraftHash != want.DraftHash || got.SubmittedBy != want.SubmittedBy ||
		!sameTime(got.SubmittedAt, want.SubmittedAt) || !slices.Equal(got.Reviewers, want.Reviewers) ||
		got.DecidedBy != want.DecidedBy || !sameTime(got.DecidedAt, want.DecidedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) ||
		got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("review:\n got %+v\nwant %+v", got, want)
	}
}

func newReview(item ids.UUID, state string, at time.Time) store.ItemReview {
	sub := at
	return store.ItemReview{Item: item, State: state, DraftHash: "h1", SubmittedBy: "user:1", SubmittedAt: &sub,
		Reviewers: []string{"user:2", "group:3"}, UpdatedAt: at.Add(5 * time.Millisecond)}
}

func testReviewCRUD(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	item := seedReviewItems(t, db, g, "page")[0]
	if err := reviewDo(db, func(s store.ReviewStore) error { _, err := s.Get(ctx, item); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get none: %v", err)
	}
	r := newReview(item, "in_review", t0.Add(time.Second))
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Put(ctx, r) })
	reviewMust(t, db, func(s store.ReviewStore) error {
		got, err := s.Get(ctx, item)
		sameReview(t, got, r)
		return err
	})
	// Upsert replaces every column; empty reviewers read back as nil, unset times as nil.
	r2 := store.ItemReview{Item: item, State: "none", UpdatedAt: t0.Add(time.Minute)}
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Put(ctx, r2) })
	reviewMust(t, db, func(s store.ReviewStore) error {
		got, err := s.Get(ctx, item)
		sameReview(t, got, r2)
		if got.Reviewers != nil || got.SubmittedAt != nil || got.DecidedAt != nil {
			t.Fatalf("zero values: %+v", got)
		}
		return err
	})
	if err := reviewDo(db, func(s store.ReviewStore) error { return s.Put(ctx, newReview(g.New(), "in_review", t0)) }); !errors.Is(err, store.ErrConstraint) {
		t.Fatalf("put for unknown item: %v", err)
	}
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Delete(ctx, item) })
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Delete(ctx, item) })
	if err := reviewDo(db, func(s store.ReviewStore) error { _, err := s.Get(ctx, item); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}

func testReviewTransition(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	item := seedReviewItems(t, db, g, "page")[0]
	r := newReview(item, "in_review", t0)
	next := r
	dec := t0.Add(time.Hour)
	next.State, next.DecidedBy, next.DecidedAt, next.UpdatedAt = "approved", "user:2", &dec, dec
	do := func(x store.ItemReview, from ...string) error {
		return reviewDo(db, func(s store.ReviewStore) error { return s.Transition(ctx, x, from) })
	}
	if err := do(next, "in_review"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no row: %v", err)
	}
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Put(ctx, r) })
	if err := do(next, "approved", "changes_requested"); !errors.Is(err, store.ErrStateChanged) {
		t.Fatalf("wrong state: %v", err)
	}
	reviewMust(t, db, func(s store.ReviewStore) error {
		got, err := s.Get(ctx, item)
		sameReview(t, got, r)
		return err
	})
	if err := do(next, "changes_requested", "in_review"); err != nil {
		t.Fatal(err)
	}
	reviewMust(t, db, func(s store.ReviewStore) error {
		got, err := s.Get(ctx, item)
		sameReview(t, got, next)
		return err
	})
	if err := do(next, "in_review"); !errors.Is(err, store.ErrStateChanged) {
		t.Fatalf("second transition: %v", err)
	}
}

// Several transactions race for the same transition: exactly one wins, the others see ErrStateChanged.
func testReviewTransitionRace(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	item := seedReviewItems(t, db, g, "page")[0]
	r := newReview(item, "in_review", t0)
	reviewMust(t, db, func(s store.ReviewStore) error { return s.Put(ctx, r) })
	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			x := r
			x.State, x.DecidedBy = "approved", "user:"+string(rune('a'+i))
			errs[i] = reviewDo(db, func(s store.ReviewStore) error { return s.Transition(ctx, x, []string{"in_review"}) })
		}()
	}
	close(start)
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, store.ErrStateChanged):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d transitions won, want 1: %v", wins, errs)
	}
}

func testReviewEvents(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	items := seedReviewItems(t, db, g, "page", "page")
	item, other := items[0], items[1]
	var evs []store.ReviewEvent
	for i := range 5 {
		e := store.ReviewEvent{ID: g.New(), Item: item, Kind: "comment", Comment: "c" + string(rune('0'+i)), Actor: "user:1",
			At: t0.Add(time.Duration(i/2) * time.Second).Add(time.Millisecond)} // pairs share a timestamp
		if i == 1 {
			e.Data = json.RawMessage(`{"from":"a","n":3}`)
		}
		evs = append(evs, e)
	}
	oe := store.ReviewEvent{ID: g.New(), Item: other, Kind: "submitted", Actor: "user:9", At: t0}
	reviewMust(t, db, func(s store.ReviewStore) error {
		for _, e := range evs {
			if err := s.AddEvent(ctx, e); err != nil {
				return err
			}
		}
		return s.AddEvent(ctx, oe)
	})
	if err := reviewDo(db, func(s store.ReviewStore) error { return s.AddEvent(ctx, evs[0]) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("duplicate id: %v", err)
	}
	bad := oe
	bad.ID, bad.Item = g.New(), g.New()
	if err := reviewDo(db, func(s store.ReviewStore) error { return s.AddEvent(ctx, bad) }); !errors.Is(err, store.ErrConstraint) {
		t.Fatalf("unknown item: %v", err)
	}
	want := slices.Clone(evs)
	slices.Reverse(want) // newest first; ids are increasing so ties break by id desc as well
	var all []store.ReviewEvent
	reviewMust(t, db, func(s store.ReviewStore) (err error) { all, err = s.Events(ctx, item, 0, ids.UUID{}); return })
	if len(all) != len(want) {
		t.Fatalf("got %d events, want %d", len(all), len(want))
	}
	for i := range want {
		g, w := all[i], want[i]
		if g.ID != w.ID || g.Item != w.Item || g.Kind != w.Kind || g.Comment != w.Comment || g.Actor != w.Actor ||
			!g.At.Equal(w.At) || g.At.Location() != time.UTC || !sameData(g.Data, w.Data) {
			t.Fatalf("event %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
	// Paging by the last id of the previous page, across a shared timestamp.
	var got []ids.UUID
	var before ids.UUID
	for range 10 {
		var page []store.ReviewEvent
		reviewMust(t, db, func(s store.ReviewStore) (err error) { page, err = s.Events(ctx, item, 2, before); return })
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			got = append(got, e.ID)
		}
		before = page[len(page)-1].ID
	}
	var wantIDs []ids.UUID
	for _, e := range want {
		wantIDs = append(wantIDs, e.ID)
	}
	if !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("paged ids %v, want %v", got, wantIDs)
	}
	if err := reviewDo(db, func(s store.ReviewStore) error { _, err := s.Events(ctx, item, 2, oe.ID); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("before of another item: %v", err)
	}
	reviewMust(t, db, func(s store.ReviewStore) error {
		l, err := s.Events(ctx, g.New(), 10, ids.UUID{})
		if len(l) != 0 {
			t.Fatalf("unknown item events: %v", l)
		}
		return err
	})
}

func testReviewList(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	items := seedReviewItems(t, db, g, "article", "page", "article", "page", "article")
	put := func(i int, state string, at time.Time, reviewers ...string) store.ItemReview {
		r := newReview(items[i], state, at)
		r.Reviewers = reviewers
		reviewMust(t, db, func(s store.ReviewStore) error { return s.Put(ctx, r) })
		return r
	}
	r0 := put(0, "in_review", t0.Add(1*time.Second), "user:2", "group:3")
	r1 := put(1, "in_review", t0.Add(2*time.Second), "group:3")
	r2 := put(2, "approved", t0.Add(3*time.Second), "user:22")
	put(3, "changes_requested", t0.Add(4*time.Second))
	r4 := put(4, "none", t0.Add(5*time.Second))
	withContent(t, db, func(c store.ContentStore) error {
		return c.SetDeleted(ctx, items[3], &t0, "user:1")
	})
	list := func(f store.ReviewFilter, p store.Page) ([]store.ReviewRow, int) {
		t.Helper()
		var rows []store.ReviewRow
		var total int
		reviewMust(t, db, func(s store.ReviewStore) (err error) { rows, total, err = s.List(ctx, f, p); return })
		return rows, total
	}
	check := func(name string, f store.ReviewFilter, want ...store.ItemReview) {
		t.Helper()
		rows, total := list(f, store.Page{})
		if total != len(want) || len(rows) != len(want) {
			t.Fatalf("%s: total %d rows %d, want %d", name, total, len(rows), len(want))
		}
		for i, w := range want {
			sameReview(t, rows[i].ItemReview, w)
		}
	}
	// Newest submission first; the trashed item (r3) is hidden.
	check("all", store.ReviewFilter{}, r4, r2, r1, r0)
	check("states", store.ReviewFilter{States: []string{"in_review", "approved"}}, r2, r1, r0)
	check("types", store.ReviewFilter{Types: []string{"page"}}, r1)
	check("state and type", store.ReviewFilter{States: []string{"in_review"}, Types: []string{"article"}}, r0)
	check("reviewer user", store.ReviewFilter{Reviewers: []string{"user:2"}}, r0)
	check("reviewer user or group", store.ReviewFilter{Reviewers: []string{"user:2", "group:3"}}, r1, r0)
	check("reviewer is exact", store.ReviewFilter{Reviewers: []string{"user:2x", "user:"}})
	check("reviewer user:22", store.ReviewFilter{Reviewers: []string{"user:22"}}, r2)
	rows, total := list(store.ReviewFilter{}, store.Page{Limit: 2, Offset: 1})
	if total != 4 || len(rows) != 2 || rows[0].Item != items[2] || rows[0].Type != "article" || rows[1].Item != items[1] || rows[1].Type != "page" {
		t.Fatalf("page: total %d %+v", total, rows)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.SetDeleted(ctx, items[3], nil, "") })
	if _, total := list(store.ReviewFilter{States: []string{"changes_requested"}}, store.Page{}); total != 1 {
		t.Fatalf("restored item should list again, total %d", total)
	}
}

func testReviewCascade(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	item := seedReviewItems(t, db, g, "page")[0]
	reviewMust(t, db, func(s store.ReviewStore) error {
		if err := s.Put(ctx, newReview(item, "in_review", t0)); err != nil {
			return err
		}
		return s.AddEvent(ctx, store.ReviewEvent{ID: g.New(), Item: item, Kind: "submitted", Actor: "user:1", At: t0})
	})
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, item) })
	reviewMust(t, db, func(s store.ReviewStore) error {
		if _, err := s.Get(ctx, item); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("review survived purge: %v", err)
		}
		evs, err := s.Events(ctx, item, 10, ids.UUID{})
		if len(evs) != 0 {
			t.Fatalf("events survived purge: %v", evs)
		}
		return err
	})
}

func sameData(got, want json.RawMessage) bool {
	if len(want) == 0 {
		return len(got) == 0
	}
	return jsonEqual(got, string(want))
}
