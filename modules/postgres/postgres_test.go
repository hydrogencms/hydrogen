package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storetest"
)

func open(t *testing.T) store.DB {
	db, err := Open(context.Background(), pgtest.Scratch(t), store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestConformance(t *testing.T) { storetest.Run(t, open) }

// Two servers sharing one database: a commit on one wakes listeners on the other, and locks exclude each other.
func TestMultiNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbURL := pgtest.Scratch(t)
	a, err := Open(ctx, dbURL, store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := Open(ctx, dbURL, store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()

	ch, err := b.Notifier().Listen(ctx, "nodes")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Tx(ctx, func(tx store.Tx) error { return tx.Notify("nodes", "from-a") }); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-ch:
		if n.Payload != "from-a" {
			t.Fatalf("payload %q", n.Payload)
		}
	case <-ctx.Done():
		t.Fatal("notification not delivered across connections")
	}

	l, ok, err := a.Locker().TryLock(ctx, "shared")
	if err != nil || !ok {
		t.Fatalf("lock on a: %v %v", ok, err)
	}
	if _, ok, err := b.Locker().TryLock(ctx, "shared"); err != nil || ok {
		t.Fatalf("lock on b while a holds it: %v %v", ok, err)
	}
	_ = l.Release(ctx)
	if l2, ok, err := b.Locker().TryLock(ctx, "shared"); err != nil || !ok {
		t.Fatalf("lock on b after release: %v %v", ok, err)
	} else {
		_ = l2.Release(ctx)
	}
}
