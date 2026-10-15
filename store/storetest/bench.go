package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/store"
)

// BenchItems is the size of the catalog SeedBench creates.
const BenchItems = 10_000

// SeedBench fills an empty, migrated database with BenchItems articles. Each has three indexed fields:
// category (text, 20 values), rating (num, 0..99) and published (time).
func SeedBench(tb testing.TB, db store.DB) {
	tb.Helper()
	ctx := context.Background()
	g := newIDs()
	if err := db.Tx(ctx, func(tx store.Tx) error {
		c := tx.Content()
		for i := range BenchItems {
			id := g.New()
			it := newItem(id, "article", "")
			it.PublishedVersion = 1
			it.AlwaysAvailable = i%10 == 0
			it.UpdatedAt = t0.Add(time.Duration(i) * time.Minute)
			if err := c.InsertItem(ctx, it); err != nil {
				return err
			}
			snaps := []store.Snapshot{snap(id, "article", "", "en", 1, `{}`)}
			if i%2 == 0 {
				snaps = append(snaps, snap(id, "article", "", "de", 1, `{}`))
			}
			if err := c.PutSnapshots(ctx, id, snaps); err != nil {
				return err
			}
			published := t0.Add(time.Duration(i*37%BenchItems) * time.Hour)
			if err := c.ReplaceIndex(ctx, id, []store.IndexRow{
				txt("", "category", fmt.Sprintf("c%02d", i%20)),
				num("", "rating", float64(i*7%100)),
				tim("", "published", published),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		tb.Fatal(err)
	}
	// A freshly filled Postgres has no statistics until autovacuum analyses it, and plans without them
	// are far from what a running system gets. (SQLite plans without statistics by design.)
	if db.Dialect() == "postgres" {
		if err := db.Tx(ctx, func(tx store.Tx) error {
			_, err := tx.SQL().Exec(ctx, "ANALYZE")
			return err
		}); err != nil {
			tb.Fatal(err)
		}
	}
}

// withAvailable is q: only published items with a snapshot in "en" or "de", or always available.
func withAvailable(q store.Query) store.Query {
	q.Available = []string{"en", "de"}
	return q
}

// BenchQueries are the query shapes the adapters benchmark.
func BenchQueries() map[string]store.Query {
	base := map[string]store.Query{
		"FilterSortLimit": {
			Type: "article", Locale: "en", Limit: 50,
			Where: []store.Cond{
				{Field: "category", Column: store.ColText, Op: store.OpEq, Value: "c07"},
				{Field: "rating", Column: store.ColNum, Op: store.OpGte, Value: 50.0},
			},
			Sort: []store.Sort{{Field: "published", Column: store.ColTime, Desc: true}},
		},
		"Default": {Type: "article", Locale: "en", Limit: 50},
		"SortByIndexField": {
			Type: "article", Locale: "en", Limit: 50,
			Sort: []store.Sort{{Field: "rating", Column: store.ColNum, Desc: true}},
		},
		"Deep": {
			Type: "article", Locale: "en", Limit: 50, Offset: 2000,
			Where: []store.Cond{{Field: "rating", Column: store.ColNum, Op: store.OpLt, Value: 50.0}},
		},
	}
	base["FilterSortLimitAvailable"] = withAvailable(base["FilterSortLimit"])
	base["SortByIndexFieldAvailable"] = withAvailable(base["SortByIndexField"])
	return base
}

// BenchmarkQuery runs BenchQueries against a database seeded with SeedBench.
func BenchmarkQuery(b *testing.B, db store.DB) {
	SeedBench(b, db)
	ctx := context.Background()
	for _, name := range []string{"FilterSortLimit", "Default", "SortByIndexField", "Deep", "FilterSortLimitAvailable", "SortByIndexFieldAvailable"} {
		q := BenchQueries()[name]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := db.View(ctx, func(tx store.Tx) error {
					res, err := tx.Content().Query(ctx, q)
					if err == nil && len(res.Items) == 0 {
						err = fmt.Errorf("no items")
					}
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
