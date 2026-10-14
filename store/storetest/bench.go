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
			it.UpdatedAt = t0.Add(time.Duration(i) * time.Minute)
			if err := c.InsertItem(ctx, it); err != nil {
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
}

// BenchQueries are the query shapes the adapters benchmark.
func BenchQueries() map[string]store.Query {
	return map[string]store.Query{
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
}

// BenchmarkQuery runs BenchQueries against a database seeded with SeedBench.
func BenchmarkQuery(b *testing.B, db store.DB) {
	SeedBench(b, db)
	ctx := context.Background()
	for _, name := range []string{"FilterSortLimit", "Default", "SortByIndexField", "Deep"} {
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
