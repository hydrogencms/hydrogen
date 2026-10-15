// Package storetest is the conformance suite for database adapters. Every adapter runs it from its own tests:
//
//	func TestConformance(t *testing.T) { storetest.Run(t, openScratchDB) }
//
// open must return a new, empty database for each call; the suite migrates it.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

type Opener func(t *testing.T) store.DB

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// widgets is a module migration set written in SQL both adapters accept.
var widgets = store.MigrationSet{Source: "storetest", FS: fstest.MapFS{
	"0001_widgets.sql": {Data: []byte("CREATE TABLE storetest_widget (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n")},
}}

func Run(t *testing.T, open Opener) {
	tests := []struct {
		name string
		fn   func(*testing.T, store.DB)
	}{
		{"Migrate", testMigrate},
		{"TxCommitRollback", testTx},
		{"Audit", testAudit},
		{"OutboxClaim", testOutboxClaim},
		{"OutboxConcurrentClaim", testOutboxConcurrentClaim},
		{"OutboxDelivery", testOutboxDelivery},
		{"Notify", testNotify},
		{"Locks", testLocks},
	}
	tests = append(tests, contentTests...)
	tests = append(tests, snapshotTests...)
	tests = append(tests, authTests...)
	tests = append(tests, sqlTests...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := open(t)
			t.Cleanup(func() { _ = db.Close() })
			if tc.name != "Migrate" {
				if err := db.Migrate(context.Background(), nil); err != nil {
					t.Fatalf("migrate: %v", err)
				}
			}
			tc.fn(t, db)
		})
	}
}

func testMigrate(t *testing.T, db store.DB) {
	ctx := context.Background()
	pending, err := db.Pending(ctx, []store.MigrationSet{widgets})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) < 2 || pending[0].Source != store.CoreSource || pending[len(pending)-1].Source != "storetest" {
		t.Fatalf("pending before migrate: %+v", pending)
	}
	for range 2 {
		if err := db.Migrate(ctx, []store.MigrationSet{widgets}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	if pending, err = db.Pending(ctx, []store.MigrationSet{widgets}); err != nil || len(pending) != 0 {
		t.Fatalf("pending after migrate: %v %+v", err, pending)
	}

	edited := store.MigrationSet{Source: "storetest", FS: fstest.MapFS{
		"0001_widgets.sql": {Data: []byte("CREATE TABLE storetest_widget (id INTEGER PRIMARY KEY);\n")},
	}}
	if err := db.Migrate(ctx, []store.MigrationSet{edited}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("edited migration: %v, want a checksum error mentioning \"changed\"", err)
	}

	gap := store.MigrationSet{Source: "gap", FS: fstest.MapFS{"0002_x.sql": {Data: []byte("SELECT 1;")}}}
	if err := db.Migrate(ctx, []store.MigrationSet{gap}); err == nil {
		t.Fatal("migration numbering gap accepted")
	}

	broken := store.MigrationSet{Source: "broken", FS: fstest.MapFS{
		"0001_ok.sql":  {Data: []byte("CREATE TABLE storetest_ok (id INTEGER PRIMARY KEY);")},
		"0002_bad.sql": {Data: []byte("CREATE TABLE storetest_bad (;")},
	}}
	if err := db.Migrate(ctx, []store.MigrationSet{broken}); err == nil {
		t.Fatal("broken migration accepted")
	}
	pending, err = db.Pending(ctx, []store.MigrationSet{broken})
	if err != nil || len(pending) != 1 || pending[0].Version != 2 {
		t.Fatalf("after a failed migration, only it should be pending: %v %+v", err, pending)
	}
}

func testTx(t *testing.T, db store.DB) {
	ctx := context.Background()
	log := audit.New(clock.NewManual(t0))
	boom := errors.New("boom")
	err := db.Tx(ctx, func(tx store.Tx) error {
		if err := log.Write(ctx, tx, audit.Entry{Actor: audit.System, Action: "rolled.back"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx returned %v, want the callback's error", err)
	}
	if err := db.Tx(ctx, func(tx store.Tx) error {
		return log.Write(ctx, tx, audit.Entry{Actor: audit.System, Action: "committed"})
	}); err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEvent
	if err := db.View(ctx, func(tx store.Tx) error {
		var err error
		got, err = tx.Audit().List(ctx, store.AuditQuery{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Action != "committed" {
		t.Fatalf("after rollback and commit: %+v", got)
	}
}

func testAudit(t *testing.T, db store.DB) {
	ctx := context.Background()
	c := clock.NewManual(t0.Add(123 * time.Millisecond))
	log := audit.New(c)
	err := db.Tx(ctx, func(tx store.Tx) error {
		for i, e := range []audit.Entry{
			{Actor: audit.Actor{Kind: "user", ID: "u1"}, Action: "item.publish", Subject: "a", Data: map[string]any{"v": 1}},
			{Actor: audit.Actor{Kind: "agent", ID: "bot"}, Action: "item.update", Subject: "a"},
			{Actor: audit.System, Action: "item.publish", Subject: "b"},
		} {
			c.Advance(time.Duration(i+1) * time.Second)
			if err := log.Write(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list := func(q store.AuditQuery) []store.AuditEvent {
		var out []store.AuditEvent
		if err := db.View(ctx, func(tx store.Tx) error {
			var err error
			out, err = tx.Audit().List(ctx, q)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	all := list(store.AuditQuery{})
	if len(all) != 3 || all[0].Subject != "b" || all[2].ActorID != "u1" {
		t.Fatalf("newest first: %+v", all)
	}
	if !all[2].At.Equal(t0.Add(1123*time.Millisecond)) || all[2].At.Location() != time.UTC {
		t.Fatalf("time round trip: %v", all[2].At)
	}
	if !jsonEqual(all[2].Data, `{"v":1}`) || all[1].Data != nil {
		t.Fatalf("data round trip: %q %q", all[2].Data, all[1].Data)
	}
	if got := list(store.AuditQuery{Subject: "a"}); len(got) != 2 {
		t.Fatalf("by subject: %+v", got)
	}
	if got := list(store.AuditQuery{Action: "item.publish", Limit: 1}); len(got) != 1 || got[0].Subject != "b" {
		t.Fatalf("by action with limit: %+v", got)
	}
	if got := list(store.AuditQuery{Action: "item.*"}); len(got) != 3 {
		t.Fatalf("by action prefix: %+v", got)
	}
	if got := list(store.AuditQuery{Action: "item.p*"}); len(got) != 2 || got[0].Subject != "b" {
		t.Fatalf("by narrower prefix: %+v", got)
	}
	if got := list(store.AuditQuery{Action: "item_*"}); len(got) != 0 {
		t.Fatalf("prefix is literal: %+v", got)
	}
	if got := list(store.AuditQuery{Before: all[0].ID}); len(got) != 2 || got[0].ID != all[1].ID {
		t.Fatalf("before: %+v", got)
	}
	if got := list(store.AuditQuery{Before: all[0].ID, Action: "item.*"}); len(got) != 2 {
		t.Fatalf("before with action: %+v", got)
	}
	if got := list(store.AuditQuery{Before: all[2].ID}); len(got) != 0 {
		t.Fatalf("before the oldest: %+v", got)
	}
	if got := list(store.AuditQuery{ActorKind: "agent"}); len(got) != 1 || got[0].ActorID != "bot" {
		t.Fatalf("by actor kind: %+v", got)
	}
	if got := list(store.AuditQuery{ActorKind: "user", ActorID: "u1"}); len(got) != 1 || got[0].Subject != "a" {
		t.Fatalf("by actor: %+v", got)
	}
	if got := list(store.AuditQuery{Since: t0.Add(2123 * time.Millisecond)}); len(got) != 2 {
		t.Fatalf("since: %+v", got)
	}
	if got := list(store.AuditQuery{Limit: 1, Offset: 1}); len(got) != 1 || got[0].Subject != "a" || got[0].ActorKind != "agent" {
		t.Fatalf("offset: %+v", got)
	}
}

func jsonEqual(b []byte, want string) bool {
	var x, y any
	return json.Unmarshal(b, &x) == nil && json.Unmarshal([]byte(want), &y) == nil && mustJSON(x) == mustJSON(y)
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func inTx(t *testing.T, db store.DB, fn func(store.OutboxStore) error) {
	t.Helper()
	if err := db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Outbox()) }); err != nil {
		t.Fatal(err)
	}
}

func claim(t *testing.T, db store.DB, consumer string, now time.Time, limit int) []store.Delivery {
	t.Helper()
	var ds []store.Delivery
	inTx(t, db, func(o store.OutboxStore) error {
		var err error
		ds, err = o.Claim(context.Background(), consumer, now, time.Minute, limit)
		return err
	})
	return ds
}

func testOutboxClaim(t *testing.T, db store.DB) {
	ctx := context.Background()
	var ids []int64
	inTx(t, db, func(o store.OutboxStore) error {
		for i, topic := range []string{"a", "b", "c"} {
			id, err := o.Append(ctx, topic, []byte(`{"n":`+strconv.Itoa(i+1)+`}`), []string{"x", "y"}, t0)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		_, err := o.Append(ctx, "nobody", []byte(`{}`), nil, t0)
		return err
	})
	if ids[0] >= ids[1] || ids[1] >= ids[2] {
		t.Fatalf("event ids not increasing: %v", ids)
	}

	ds := claim(t, db, "x", t0, 2)
	if len(ds) != 2 || ds[0].Event.ID != ids[0] || ds[1].Event.ID != ids[1] || ds[0].Attempts != 1 {
		t.Fatalf("first claim: %+v", ds)
	}
	if ds[0].Event.Topic != "a" || !jsonEqual(ds[0].Event.Payload, `{"n":1}`) || !ds[0].Event.CreatedAt.Equal(t0) {
		t.Fatalf("event fields: %+v", ds[0].Event)
	}
	if ds := claim(t, db, "x", t0, 10); len(ds) != 1 || ds[0].Event.ID != ids[2] {
		t.Fatalf("leased deliveries must stay hidden: %+v", ds)
	}
	if ds := claim(t, db, "y", t0, 10); len(ds) != 3 {
		t.Fatalf("consumers are independent: %+v", ds)
	}

	inTx(t, db, func(o store.OutboxStore) error {
		if err := o.Complete(ctx, "x", ids[0], t0); err != nil {
			return err
		}
		if err := o.Retry(ctx, "x", ids[1], t0.Add(10*time.Second), "temporary"); err != nil {
			return err
		}
		return o.Fail(ctx, "x", ids[2], t0, "permanent")
	})
	if ds := claim(t, db, "x", t0.Add(5*time.Second), 10); len(ds) != 0 {
		t.Fatalf("retry before its time: %+v", ds)
	}
	ds = claim(t, db, "x", t0.Add(10*time.Second), 10)
	if len(ds) != 1 || ds[0].Event.ID != ids[1] || ds[0].Attempts != 2 {
		t.Fatalf("retry due: %+v", ds)
	}
	if ds := claim(t, db, "x", t0.Add(2*time.Minute), 10); len(ds) != 1 || ds[0].Attempts != 3 {
		t.Fatalf("expired lease must be claimable again: %+v", ds)
	}

	var st store.OutboxStats
	inTx(t, db, func(o store.OutboxStore) error {
		var err error
		st, err = o.Stats(ctx, "x")
		return err
	})
	if st != (store.OutboxStats{Pending: 1, Done: 1, Failed: 1}) {
		t.Fatalf("stats: %+v", st)
	}
}

func testOutboxConcurrentClaim(t *testing.T, db store.DB) {
	ctx := context.Background()
	const n = 40
	inTx(t, db, func(o store.OutboxStore) error {
		for range n {
			if _, err := o.Append(ctx, "t", []byte(`{}`), []string{"c"}, t0); err != nil {
				return err
			}
		}
		return nil
	})
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			for {
				var ds []store.Delivery
				err := db.Tx(ctx, func(tx store.Tx) error {
					var err error
					ds, err = tx.Outbox().Claim(ctx, "c", t0, time.Minute, 3)
					return err
				})
				if err != nil {
					errs <- err
					return
				}
				if len(ds) == 0 {
					return
				}
				mu.Lock()
				for _, d := range ds {
					seen[d.Event.ID]++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(seen) != n {
		t.Fatalf("claimed %d distinct deliveries, want %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("event %d claimed %d times", id, c)
		}
	}
}

func testOutboxDelivery(t *testing.T, db store.DB) {
	ctx := context.Background()
	c := clock.NewManual(t0)
	var got []string
	fails := 1
	ob, err := outbox.New(db, c, slog.New(slog.NewTextHandler(io.Discard, nil)), []outbox.Consumer{
		{Name: "indexer", Topics: []string{"item.*"}, Handle: func(_ context.Context, e outbox.Event) error {
			if fails > 0 {
				fails--
				return errors.New("flaky")
			}
			var p map[string]string
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return err
			}
			got = append(got, e.Topic+":"+p["id"])
			return nil
		}},
		{Name: "never", Topics: []string{"other"}, MaxAttempts: 1, Handle: func(context.Context, outbox.Event) error {
			return errors.New("always")
		}},
	}, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx store.Tx) error {
		if _, err := ob.Publish(ctx, tx, "item.published", map[string]string{"id": "a"}); err != nil {
			return err
		}
		_, err := ob.Publish(ctx, tx, "other", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := ob.Drain(ctx); err != nil || n != 2 {
		t.Fatalf("first drain: %d %v", n, err)
	}
	if len(got) != 0 {
		t.Fatalf("handler should have failed first: %v", got)
	}
	c.Advance(outbox.Backoff(1))
	if _, err := ob.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "item.published:a" {
		t.Fatalf("delivered: %v", got)
	}
	var st store.OutboxStats
	inTx(t, db, func(o store.OutboxStore) error { st, err = o.Stats(ctx, "never"); return err })
	if st.Failed != 1 {
		t.Fatalf("MaxAttempts 1 should fail at once: %+v", st)
	}
}

func testNotify(t *testing.T, db store.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := db.Notifier().Listen(ctx, "storetest_chan")
	if err != nil {
		t.Fatal(err)
	}
	// A reconnect notice (empty payload) may arrive at any time; skip those.
	next := func() (store.Notification, bool) {
		for {
			select {
			case n := <-ch:
				if n.Payload == "" {
					continue
				}
				return n, true
			case <-time.After(300 * time.Millisecond):
				return store.Notification{}, false
			}
		}
	}

	_ = db.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Notify("storetest_chan", "rolled-back"); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err := db.Tx(ctx, func(tx store.Tx) error { return tx.Notify("storetest_chan", "hello") }); err != nil {
		t.Fatal(err)
	}
	n, ok := next()
	if !ok || n.Payload != "hello" || n.Channel != "storetest_chan" {
		t.Fatalf("notification: %+v %v", n, ok)
	}
	if n, ok := next(); ok {
		t.Fatalf("unexpected notification: %+v", n)
	}
}

func testLocks(t *testing.T, db store.DB) {
	ctx := context.Background()
	l, ok, err := db.Locker().TryLock(ctx, "storetest-a")
	if err != nil || !ok {
		t.Fatalf("first lock: %v %v", ok, err)
	}
	if _, ok, err := db.Locker().TryLock(ctx, "storetest-a"); err != nil || ok {
		t.Fatalf("second lock on a held name: %v %v", ok, err)
	}
	b, ok, err := db.Locker().TryLock(ctx, "storetest-b")
	if err != nil || !ok {
		t.Fatalf("other name: %v %v", ok, err)
	}
	if err := l.Release(ctx); err != nil {
		t.Fatal(err)
	}
	l2, ok, err := db.Locker().TryLock(ctx, "storetest-a")
	if err != nil || !ok {
		t.Fatalf("relock after release: %v %v", ok, err)
	}
	_ = l2.Release(ctx)
	_ = b.Release(ctx)
}
