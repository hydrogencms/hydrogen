package postgres

import (
	"context"
	"testing"

	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storetest"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

func BenchmarkContentQuery(b *testing.B) {
	db, err := Open(context.Background(), pgtest.Scratch(b), store.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background(), nil); err != nil {
		b.Fatal(err)
	}
	storetest.BenchmarkQuery(b, db)
}
