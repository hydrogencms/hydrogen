package sqlite

import (
	"context"
	"testing"

	"github.com/hydrogencms/hydrogen/core/store"

	"github.com/hydrogencms/hydrogen/core/store/storetest"
)

func BenchmarkContentQuery(b *testing.B) {
	db, err := Open(context.Background(), "sqlite:"+b.TempDir()+"/bench.db", store.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background(), nil); err != nil {
		b.Fatal(err)
	}
	storetest.BenchmarkQuery(b, db)
}

func BenchmarkSearch(b *testing.B) {
	db, err := Open(context.Background(), "sqlite:"+b.TempDir()+"/bench.db", store.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background(), nil); err != nil {
		b.Fatal(err)
	}
	storetest.BenchmarkSearch(b, db, false)
}

func BenchmarkSearchTiered(b *testing.B) {
	db, err := Open(context.Background(), "sqlite:"+b.TempDir()+"/bench.db", store.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background(), nil); err != nil {
		b.Fatal(err)
	}
	storetest.BenchmarkSearch(b, db, true)
}
