package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// BenchmarkSQLRoundTrip reads one row by id through store.SQL, converting a uuid, a string and a time.
func BenchmarkSQLRoundTrip(b *testing.B) {
	ctx := context.Background()
	db, err := Open(ctx, "sqlite:"+filepath.Join(b.TempDir(), "bench.db"), store.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	set := store.MigrationSet{Source: "bench", FS: fstest.MapFS{
		"0001_t.sql": {Data: []byte(`CREATE TABLE bench_t (id BLOB PRIMARY KEY, name TEXT NOT NULL, ts INTEGER NOT NULL);`)},
	}}
	if err := db.Migrate(ctx, []store.MigrationSet{set}); err != nil {
		b.Fatal(err)
	}
	id := ids.UUID{1, 2, 3}
	err = db.Tx(ctx, func(tx store.Tx) error {
		_, err := tx.SQL().Exec(ctx, `INSERT INTO bench_t (id, name, ts) VALUES (?, ?, ?)`, id, "name", time.Now())
		return err
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Run("QueryRow", func(b *testing.B) {
		b.ReportAllocs()
		err := db.View(ctx, func(tx store.Tx) error {
			q := tx.SQL()
			for range b.N {
				var got ids.UUID
				var name string
				var ts time.Time
				if err := q.QueryRow(ctx, `SELECT id, name, ts FROM bench_t WHERE id = ?`, id).Scan(&got, &name, &ts); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	})
	b.Run("Exec", func(b *testing.B) {
		b.ReportAllocs()
		err := db.Tx(ctx, func(tx store.Tx) error {
			q := tx.SQL()
			for range b.N {
				if _, err := q.Exec(ctx, `UPDATE bench_t SET ts = ? WHERE id = ?`, time.Now(), id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	})
}
