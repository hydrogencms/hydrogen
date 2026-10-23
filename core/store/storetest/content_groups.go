package storetest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/store"
)

func cond(field string, col store.Column, op store.Op, v any) store.Cond {
	return store.Cond{Field: field, Column: col, Op: op, Value: v}
}

func anyOf(groups ...[]store.Cond) store.Cond { return store.Cond{Op: store.OpOr, Groups: groups} }
func noneOf(groups ...[]store.Cond) store.Cond {
	return store.Cond{Op: store.OpNot, Groups: groups}
}
func conds(cs ...store.Cond) []store.Cond { return cs }

// testQueryGroups covers boolean groups: or (any group fully satisfied), not (no group fully satisfied), and
// the rule that an item without index rows for a field satisfies the negation of any positive condition on it.
func testQueryGroups(t *testing.T, db store.DB) {
	cat := seedCatalog(t, db)
	// A: rating 5, tags go+db, featured true; B: 3, go, featured false; C: 3, no tags, no featured;
	// D: no rating, db; E: 8, rust.
	num, txtc, bl := store.ColNum, store.ColText, store.ColBool
	rating := func(op store.Op, v any) store.Cond { return cond("rating", num, op, v) }
	tag := func(v string) store.Cond { return cond("tags", txtc, store.OpContains, v) }
	featured := func(v bool) store.Cond { return cond("featured", bl, store.OpEq, v) }

	tests := []struct {
		name  string
		where []store.Cond
		want  string
	}{
		{"or across fields", conds(anyOf(conds(rating(store.OpEq, 5.0)), conds(tag("rust")))), "AE"},
		{"or of and groups", conds(anyOf(conds(rating(store.OpEq, 3.0), featured(false)), conds(tag("rust")))), "BE"},
		{"or with one group is and", conds(anyOf(conds(rating(store.OpEq, 3.0), tag("go")))), "B"},
		{"or ands with plain conditions", conds(anyOf(conds(tag("go")), conds(tag("rust"))), rating(store.OpGt, 4.0)), "AE"},
		{"or without groups matches nothing", conds(anyOf()), ""},
		{"or with an empty in list", conds(anyOf(conds(rating(store.OpIn, []float64{})), conds(tag("rust")))), "E"},
		{"or on an absent field", conds(anyOf(conds(featured(true)), conds(rating(store.OpExists, false)))), "AD"},
		{"not of one condition keeps absent", conds(noneOf(conds(rating(store.OpEq, 3.0)))), "ADE"},
		{"not contains keeps items without values", conds(noneOf(conds(tag("go")))), "CDE"},
		{"not bool keeps items without the field", conds(noneOf(conds(featured(true)))), "BCDE"},
		{"not of an and group", conds(noneOf(conds(rating(store.OpEq, 3.0), tag("go")))), "ACDE"},
		{"not of several groups is none of them", conds(noneOf(conds(rating(store.OpEq, 3.0)), conds(tag("rust")))), "AD"},
		{"not exists", conds(noneOf(conds(rating(store.OpExists, true)))), "D"},
		{"not of an empty in list is everything", conds(noneOf(conds(rating(store.OpIn, []float64{})))), "ABCDE"},
		{"not of a group nothing satisfies", conds(noneOf(conds(rating(store.OpEq, 99.0)))), "ABCDE"},
		{"not and plain", conds(noneOf(conds(tag("db"))), rating(store.OpGte, 3.0)), "BCE"},
		{"not under or", conds(anyOf(conds(noneOf(conds(tag("go"))), rating(store.OpGt, 3.0)), conds(featured(false)))), "BE"},
		{"or under not", conds(noneOf(conds(anyOf(conds(tag("go")), conds(tag("rust")))))), "CD"},
		{"two groups", conds(anyOf(conds(tag("go")), conds(tag("db"))), noneOf(conds(featured(true)))), "BD"},
		{"or of ne", conds(anyOf(conds(rating(store.OpNe, 3.0)), conds(tag("go")))), "ABDE"},
		{"prefix in a group", conds(anyOf(conds(cond("title", txtc, store.OpPrefix, "ban")), conds(cond("title", txtc, store.OpPrefix, "100%")))), "BC"},
	}
	for _, working := range []bool{false, true} {
		if working {
			for _, l := range "ABCDE" {
				copyIndex(t, db, cat, byte(l))
			}
		}
		for _, tc := range tests {
			name := tc.name
			if working {
				name += " (working)"
			}
			t.Run(name, func(t *testing.T) {
				got := cat.sorted(t, db, store.Query{Type: "article", Locale: "en", Working: working, Where: tc.where})
				if got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			})
		}
	}

	t.Run("sort, page and total", func(t *testing.T) {
		q := store.Query{Type: "article", Locale: "en", Where: conds(noneOf(conds(tag("rust")))),
			Sort: []store.Sort{{Field: "rating", Column: num}}}
		res := cat.run(t, db, q)
		if got := cat.letters(res); got != "BCAD" || res.Total != 4 {
			t.Errorf("sorted by rating: %q total %d", got, res.Total)
		}
		q.Limit, q.Offset = 2, 1
		res = cat.run(t, db, q)
		if got := cat.letters(res); got != "CA" || res.Total != 4 {
			t.Errorf("page: %q total %d", got, res.Total)
		}
		q.Sort = []store.Sort{{Field: "updatedAt", Desc: true}}
		q.Limit, q.Offset = 0, 0
		if got := cat.letters(cat.run(t, db, q)); got != "DCBA" {
			t.Errorf("sorted by meta field: %q", got)
		}
	})

	t.Run("with search and access scope", func(t *testing.T) {
		for _, l := range "ABE" {
			replaceSearch(t, db, cat.ids[byte(l)], store.ProjectionPublished, store.SearchDoc{Locale: "en", Text: "fox"})
		}
		q := store.Query{Type: "article", Locale: "en", Search: "fox", Where: conds(noneOf(conds(tag("go"))))}
		if got := cat.sorted(t, db, q); got != "E" {
			t.Errorf("search with not: %q", got)
		}
		q = store.Query{Type: "article", Locale: "en", Where: conds(anyOf(conds(tag("go")), conds(tag("rust")))),
			Scoped: true, Scope: []store.ScopeTerm{{Where: conds(rating(store.OpGte, 5.0))}, {Where: conds(noneOf(conds(rating(store.OpExists, true))))}}}
		if got := cat.sorted(t, db, q); got != "AE" {
			t.Errorf("group with a scope: %q", got)
		}
	})

	t.Run("facets", func(t *testing.T) {
		facet := func(q store.Query) string {
			var out []string
			viewContent(t, db, func(c store.ContentStore) error {
				got, err := c.Facet(context.Background(), q, "tags", 100)
				for _, f := range got {
					out = append(out, fmt.Sprintf("%s=%d", f.Value, f.Count))
				}
				return err
			})
			return strings.Join(out, " ")
		}
		q := store.Query{Type: "article", Locale: "en", Where: conds(noneOf(conds(rating(store.OpEq, 3.0))))}
		if got := facet(q); got != "db=2 go=1 rust=1" {
			t.Errorf("facets of not: %q", got)
		}
		q.Where = conds(anyOf(conds(featured(true)), conds(tag("rust"))))
		if got := facet(q); got != "db=1 go=1 rust=1" {
			t.Errorf("facets of or: %q", got)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		bad := map[string]store.Cond{
			"bad field in a group":    anyOf(conds(cond("a'b", txtc, store.OpEq, "x"))),
			"bad value in a group":    noneOf(conds(cond("rating", num, store.OpEq, "5"))),
			"unknown operator":        {Op: store.Op("xor"), Groups: [][]store.Cond{conds(rating(store.OpEq, 1.0))}},
			"nested past the maximum": anyOf(conds(anyOf(conds(anyOf(conds(anyOf(conds(rating(store.OpEq, 1.0))))))))),
		}
		for name, c := range bad {
			err := db.View(context.Background(), func(tx store.Tx) error {
				_, err := tx.Content().Query(context.Background(), store.Query{Type: "article", Locale: "en", Where: []store.Cond{c}})
				return err
			})
			if err == nil {
				t.Errorf("%s: query accepted", name)
			}
		}
	})
}

// copyIndex writes the working index rows of an item equal to its published ones.
func copyIndex(t *testing.T, db store.DB, cat *catalog, label byte) {
	t.Helper()
	var rows []store.IndexRow
	switch label {
	case 'A':
		rows = []store.IndexRow{num("", "rating", 5), txt("", "title", "apple"), txt("", "tags", "go"), txt("", "tags", "db"), bln("", "featured", true)}
	case 'B':
		rows = []store.IndexRow{num("", "rating", 3), txt("", "title", "banana"), txt("", "tags", "go"), bln("", "featured", false)}
	case 'C':
		rows = []store.IndexRow{num("", "rating", 3), txt("", "title", "100%_sure")}
	case 'D':
		rows = []store.IndexRow{txt("", "title", "a_b%c"), txt("", "tags", "db")}
	case 'E':
		rows = []store.IndexRow{num("", "rating", 8), txt("en", "title", "Apple pie"), txt("", "tags", "rust")}
	}
	replaceIndex(t, db, cat.ids[label], store.ProjectionWorking, rows...)
}
