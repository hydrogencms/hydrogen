package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var releaseTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"ReleaseCRUD", testReleaseCRUD},
	{"ReleaseItems", testReleaseItems},
	{"ReleaseList", testReleaseList},
	{"ReleaseDue", testReleaseDue},
	{"ReleaseTransition", testReleaseTransition},
	{"ReleaseTransitionRace", testReleaseTransitionRace},
	{"ReleaseBatch", testReleaseBatch},
}

func releaseDo(db store.DB, fn func(store.ReleaseStore) error) error {
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Releases()) })
}

func releaseMust(t *testing.T, db store.DB, fn func(store.ReleaseStore) error) {
	t.Helper()
	if err := releaseDo(db, fn); err != nil {
		t.Fatal(err)
	}
}

func newRelease(g *ids.Generator, name, status string, at time.Time) store.Release {
	return store.Release{ID: g.New(), Name: name, Kind: "release", Status: status, CreatedBy: "user:1", UpdatedBy: "user:1",
		CreatedAt: at, UpdatedAt: at}
}

func sameTime(a, b *time.Time) bool { return (a == nil) == (b == nil) && (a == nil || a.Equal(*b)) }

func sameRelease(t *testing.T, got, want store.Release) {
	t.Helper()
	ok := got.ID == want.ID && got.Name == want.Name && got.Description == want.Description && got.Kind == want.Kind &&
		got.Status == want.Status && sameTime(got.ScheduledAt, want.ScheduledAt) && sameTime(got.PublishedAt, want.PublishedAt) &&
		got.CreatedBy == want.CreatedBy && got.UpdatedBy == want.UpdatedBy && got.ScheduledBy == want.ScheduledBy &&
		got.CreatedAt.Equal(want.CreatedAt) && got.UpdatedAt.Equal(want.UpdatedAt) && got.CreatedAt.Location() == time.UTC
	if ok && len(want.Errors) == 0 {
		ok = len(got.Errors) == 0
	} else if ok {
		ok = jsonEqual(got.Errors, string(want.Errors))
	}
	if !ok {
		t.Fatalf("release:\n got %+v\nwant %+v", got, want)
	}
}

func testReleaseCRUD(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	at := t0.Add(123 * time.Millisecond)
	r := newRelease(g, "Autumn", "open", at)
	r.Description = "all the autumn pages"
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.CreateRelease(ctx, r) })
	if err := releaseDo(db, func(s store.ReleaseStore) error { return s.CreateRelease(ctx, r) }); !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("duplicate id: %v", err)
	}
	releaseMust(t, db, func(s store.ReleaseStore) error {
		got, err := s.GetRelease(ctx, r.ID)
		if err != nil {
			return err
		}
		sameRelease(t, got, r)
		return nil
	})

	sched, pub := t0.Add(48*time.Hour+7*time.Millisecond), t0.Add(49*time.Hour)
	r.Name, r.Description, r.Status, r.Kind = "Winter", "", "failed", "item"
	r.ScheduledAt, r.PublishedAt, r.ScheduledBy, r.UpdatedBy = &sched, &pub, "user:2", "user:3"
	r.UpdatedAt = at.Add(time.Minute)
	r.Errors = json.RawMessage(`[{"item":"x","code":"validation","message":"title is required","path":"title"}]`)
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.UpdateRelease(ctx, r) })
	releaseMust(t, db, func(s store.ReleaseStore) error {
		got, err := s.GetRelease(ctx, r.ID)
		if err != nil {
			return err
		}
		r.CreatedBy = "user:1" // not updatable
		sameRelease(t, got, r)
		return nil
	})

	// Clearing the optional columns.
	r.ScheduledAt, r.PublishedAt, r.Errors, r.Status = nil, nil, nil, "open"
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.UpdateRelease(ctx, r) })
	releaseMust(t, db, func(s store.ReleaseStore) error {
		got, err := s.GetRelease(ctx, r.ID)
		if err != nil {
			return err
		}
		sameRelease(t, got, r)
		return nil
	})

	other := g.New()
	if err := releaseDo(db, func(s store.ReleaseStore) error { _, err := s.GetRelease(ctx, other); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get unknown: %v", err)
	}
	if err := releaseDo(db, func(s store.ReleaseStore) error { x := r; x.ID = other; return s.UpdateRelease(ctx, x) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}
	if err := releaseDo(db, func(s store.ReleaseStore) error { return s.DeleteRelease(ctx, other) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete unknown: %v", err)
	}
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.DeleteRelease(ctx, r.ID) })
	if err := releaseDo(db, func(s store.ReleaseStore) error { _, err := s.GetRelease(ctx, r.ID); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
}

func seedReleaseItems(t *testing.T, db store.DB, g *ids.Generator, n int) []ids.UUID {
	t.Helper()
	list := make([]ids.UUID, n)
	withContent(t, db, func(c store.ContentStore) error {
		for i := range list {
			list[i] = g.New()
			if err := c.InsertItem(context.Background(), newItem(list[i], "page", "")); err != nil {
				return err
			}
		}
		return nil
	})
	return list
}

func testReleaseItems(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	list := seedReleaseItems(t, db, g, 3)
	r1, r2 := newRelease(g, "one", "open", t0), newRelease(g, "two", "open", t0)
	releaseMust(t, db, func(s store.ReleaseStore) error {
		if err := s.CreateRelease(ctx, r1); err != nil {
			return err
		}
		return s.CreateRelease(ctx, r2)
	})
	at := t0.Add(time.Second + 5*time.Millisecond)
	put := func(rel ids.UUID, item ids.UUID, action string, by string, at time.Time, locales ...string) {
		t.Helper()
		releaseMust(t, db, func(s store.ReleaseStore) error {
			return s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: rel, ItemID: item, Action: action, Locales: locales, AddedBy: by, AddedAt: at})
		})
	}
	put(r1.ID, list[1], "publish", "user:1", at.Add(time.Second), "en", "de")
	put(r1.ID, list[0], "unpublish", "user:2", at)
	put(r2.ID, list[0], "publish", "user:3", at)

	items := func(rel ids.UUID) []store.ReleaseItem {
		t.Helper()
		var out []store.ReleaseItem
		releaseMust(t, db, func(s store.ReleaseStore) error {
			var err error
			out, err = s.ReleaseItems(ctx, rel)
			return err
		})
		return out
	}
	got := items(r1.ID)
	if len(got) != 2 || got[0].ItemID != list[0] || got[1].ItemID != list[1] {
		t.Fatalf("release items (ordered by added_at): %+v", got)
	}
	if got[0].Action != "unpublish" || got[0].Locales != nil || got[0].AddedBy != "user:2" || !got[0].AddedAt.Equal(at) ||
		got[0].ReleaseID != r1.ID || got[0].AddedAt.Location() != time.UTC {
		t.Fatalf("item 0: %+v", got[0])
	}
	if !slices.Equal(got[1].Locales, []string{"en", "de"}) || got[1].Action != "publish" {
		t.Fatalf("item 1: %+v", got[1])
	}

	// Upsert by (release, item).
	put(r1.ID, list[1], "unpublish", "user:4", at.Add(time.Hour))
	got = items(r1.ID)
	if len(got) != 2 || got[1].Action != "unpublish" || got[1].Locales != nil || got[1].AddedBy != "user:4" {
		t.Fatalf("after upsert: %+v", got)
	}
	if n := len(items(r2.ID)); n != 1 {
		t.Fatalf("second release: %d items", n)
	}

	// An unknown release or item is a constraint error and leaves the transaction usable on every adapter's
	// own terms (callers check existence first).
	if err := releaseDo(db, func(s store.ReleaseStore) error {
		return s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: g.New(), ItemID: list[0], Action: "publish", AddedAt: at})
	}); !errors.Is(err, store.ErrConstraint) {
		t.Fatalf("unknown release: %v", err)
	}
	if err := releaseDo(db, func(s store.ReleaseStore) error {
		return s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: r1.ID, ItemID: g.New(), Action: "publish", AddedAt: at})
	}); !errors.Is(err, store.ErrConstraint) {
		t.Fatalf("unknown item: %v", err)
	}
	if err := releaseDo(db, func(s store.ReleaseStore) error {
		return s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: r1.ID, ItemID: list[2], Action: "explode", AddedAt: at})
	}); !errors.Is(err, store.ErrConstraint) {
		t.Fatalf("unknown action: %v", err)
	}

	// Remove is idempotent.
	releaseMust(t, db, func(s store.ReleaseStore) error {
		if err := s.RemoveReleaseItem(ctx, r1.ID, list[0]); err != nil {
			return err
		}
		return s.RemoveReleaseItem(ctx, r1.ID, list[0])
	})
	if got := items(r1.ID); len(got) != 1 || got[0].ItemID != list[1] {
		t.Fatalf("after remove: %+v", got)
	}

	// Trashing keeps release items; purging an item and deleting a release remove them.
	tat := t0.Add(time.Hour)
	setDeleted(t, db, list[1], &tat, "user:1")
	if got := items(r1.ID); len(got) != 1 {
		t.Fatalf("trashed item left the release: %+v", got)
	}
	withContent(t, db, func(c store.ContentStore) error { return c.DeleteItem(ctx, list[1]) })
	if got := items(r1.ID); len(got) != 0 {
		t.Fatalf("purged item stayed in the release: %+v", got)
	}
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.DeleteRelease(ctx, r2.ID) })
	if got := items(r2.ID); len(got) != 0 {
		t.Fatalf("deleted release kept items: %+v", got)
	}
	if got := items(g.New()); len(got) != 0 {
		t.Fatalf("unknown release has items: %+v", got)
	}
}

func testReleaseList(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	list := seedReleaseItems(t, db, g, 2)
	mk := func(name, status, kind string, min int) store.Release {
		r := newRelease(g, name, status, t0.Add(time.Duration(min)*time.Minute))
		r.Kind = kind
		return r
	}
	rs := []store.Release{
		mk("a", "open", "release", 1), mk("b", "scheduled", "release", 2), mk("c", "open", "item", 3),
		mk("d", "published", "release", 4), mk("e", "scheduled", "item", 5),
	}
	releaseMust(t, db, func(s store.ReleaseStore) error {
		for _, r := range rs {
			if err := s.CreateRelease(ctx, r); err != nil {
				return err
			}
		}
		for _, p := range []struct {
			r    int
			item int
		}{{0, 0}, {2, 0}, {2, 1}, {4, 1}} {
			if err := s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: rs[p.r].ID, ItemID: list[p.item], Action: "publish", AddedAt: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	names := func(f store.ReleaseFilter, p store.Page) (string, int) {
		t.Helper()
		var out []store.Release
		var total int
		releaseMust(t, db, func(s store.ReleaseStore) error {
			var err error
			out, total, err = s.ListReleases(ctx, f, p)
			return err
		})
		var b []byte
		for _, r := range out {
			b = append(b, r.Name...)
		}
		return string(b), total
	}
	for _, tc := range []struct {
		name string
		f    store.ReleaseFilter
		p    store.Page
		want string
		n    int
	}{
		{"all, newest first", store.ReleaseFilter{}, store.Page{}, "edcba", 5},
		{"statuses", store.ReleaseFilter{Statuses: []string{"open", "scheduled"}}, store.Page{}, "ecba", 4},
		{"one status", store.ReleaseFilter{Statuses: []string{"published"}}, store.Page{}, "d", 1},
		{"kind", store.ReleaseFilter{Kind: "item"}, store.Page{}, "ec", 2},
		{"kind and status", store.ReleaseFilter{Kind: "item", Statuses: []string{"scheduled"}}, store.Page{}, "e", 1},
		{"item", store.ReleaseFilter{Item: list[0]}, store.Page{}, "ca", 2},
		{"item and status", store.ReleaseFilter{Item: list[1], Statuses: []string{"scheduled"}}, store.Page{}, "e", 1},
		{"no match", store.ReleaseFilter{Statuses: []string{"cancelled"}}, store.Page{}, "", 0},
		{"unknown item", store.ReleaseFilter{Item: g.New()}, store.Page{}, "", 0},
		{"page", store.ReleaseFilter{}, store.Page{Limit: 2, Offset: 1}, "dc", 5},
		{"past the end", store.ReleaseFilter{}, store.Page{Limit: 2, Offset: 9}, "", 5},
	} {
		if got, n := names(tc.f, tc.p); got != tc.want || n != tc.n {
			t.Errorf("%s: %q total %d, want %q total %d", tc.name, got, n, tc.want, tc.n)
		}
	}
}

func testReleaseDue(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	now := t0.Add(10 * time.Hour)
	mk := func(name, status string, sched *time.Time) store.Release {
		r := newRelease(g, name, status, t0)
		r.ScheduledAt = sched
		return r
	}
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	rs := []store.Release{
		mk("late", "scheduled", at(-time.Minute)),
		mk("early", "scheduled", at(-time.Hour)),
		mk("exact", "scheduled", at(0)),
		mk("future", "scheduled", at(time.Millisecond)),
		mk("open", "open", at(-time.Hour)),       // not scheduled
		mk("done", "published", at(-time.Hour)),  // already run
		mk("nodate", "scheduled", nil),           // cannot happen, must not match
		mk("failed", "failed", at(-2*time.Hour)), // not retried by the job
	}
	releaseMust(t, db, func(s store.ReleaseStore) error {
		for _, r := range rs {
			if err := s.CreateRelease(ctx, r); err != nil {
				return err
			}
		}
		return nil
	})
	due := func(limit int) string {
		var out []store.Release
		releaseMust(t, db, func(s store.ReleaseStore) error {
			var err error
			out, err = s.DueReleases(ctx, now, limit)
			return err
		})
		var b []string
		for _, r := range out {
			b = append(b, r.Name)
		}
		return strings.Join(b, ",")
	}
	if got := due(10); got != "early,late,exact" {
		t.Fatalf("due: %q", got)
	}
	if got := due(2); got != "early,late" {
		t.Fatalf("due limit 2: %q", got)
	}
}

func testReleaseTransition(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	r := newRelease(g, "t", "open", t0)
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.CreateRelease(ctx, r) })
	next := r
	next.Status, next.UpdatedBy, next.UpdatedAt = "publishing", "user:9", t0.Add(time.Minute)
	if err := releaseDo(db, func(s store.ReleaseStore) error {
		return s.TransitionRelease(ctx, next, []string{"scheduled", "failed"})
	}); !errors.Is(err, store.ErrStateChanged) {
		t.Fatalf("wrong status: %v", err)
	}
	releaseMust(t, db, func(s store.ReleaseStore) error {
		got, err := s.GetRelease(ctx, r.ID)
		sameRelease(t, got, r)
		return err
	})
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.TransitionRelease(ctx, next, []string{"open", "failed"}) })
	releaseMust(t, db, func(s store.ReleaseStore) error {
		got, err := s.GetRelease(ctx, r.ID)
		sameRelease(t, got, next)
		return err
	})
	if err := releaseDo(db, func(s store.ReleaseStore) error { return s.TransitionRelease(ctx, next, []string{"open"}) }); !errors.Is(err, store.ErrStateChanged) {
		t.Fatalf("second transition: %v", err)
	}
	unknown := next
	unknown.ID = g.New()
	if err := releaseDo(db, func(s store.ReleaseStore) error { return s.TransitionRelease(ctx, unknown, []string{"open"}) }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown release: %v", err)
	}
}

// Two transactions race for the same transition: exactly one wins, the other sees ErrStateChanged.
func testReleaseTransitionRace(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	r := newRelease(g, "race", "scheduled", t0)
	releaseMust(t, db, func(s store.ReleaseStore) error { return s.CreateRelease(ctx, r) })
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
			x.Status, x.UpdatedBy = "publishing", "user:"+string(rune('a'+i))
			x.UpdatedAt = t0.Add(time.Minute)
			errs[i] = releaseDo(db, func(s store.ReleaseStore) error { return s.TransitionRelease(ctx, x, []string{"scheduled"}) })
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

func testReleaseBatch(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := newIDs()
	list := seedReleaseItems(t, db, g, 3)
	r1, r2, r3 := newRelease(g, "1", "open", t0), newRelease(g, "2", "open", t0), newRelease(g, "3", "open", t0)
	releaseMust(t, db, func(s store.ReleaseStore) error {
		for _, r := range []store.Release{r1, r2, r3} {
			if err := s.CreateRelease(ctx, r); err != nil {
				return err
			}
		}
		for _, p := range []struct {
			r  ids.UUID
			it ids.UUID
			a  string
		}{{r1.ID, list[0], "publish"}, {r1.ID, list[1], "publish"}, {r1.ID, list[2], "unpublish"}, {r2.ID, list[0], "unpublish"}} {
			if err := s.PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: p.r, ItemID: p.it, Action: p.a, AddedAt: t0}); err != nil {
				return err
			}
		}
		counts, err := s.ReleaseItemCounts(ctx, []ids.UUID{r1.ID, r2.ID, r3.ID, g.New()})
		if err != nil {
			return err
		}
		if len(counts) != 2 || counts[r1.ID] != 3 || counts[r2.ID] != 1 {
			t.Fatalf("counts: %v", counts)
		}
		if counts, err = s.ReleaseItemCounts(ctx, nil); err != nil || len(counts) != 0 {
			t.Fatalf("no releases: %v %v", counts, err)
		}
		of, err := s.ReleaseItemsOfItem(ctx, list[0])
		if err != nil {
			return err
		}
		if len(of) != 2 || of[0].ItemID != list[0] || of[1].ItemID != list[0] {
			t.Fatalf("items of item: %+v", of)
		}
		acts := map[ids.UUID]string{}
		for _, x := range of {
			acts[x.ReleaseID] = x.Action
		}
		if acts[r1.ID] != "publish" || acts[r2.ID] != "unpublish" {
			t.Fatalf("actions: %v", acts)
		}
		return nil
	})
}
