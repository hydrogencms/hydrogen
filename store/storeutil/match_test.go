package storeutil

import (
	"testing"

	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

func TestMatchWhereWithoutColumn(t *testing.T) {
	s, n := "news", 3.0
	vals := []schema.IndexValue{{Field: "kind", Text: &s}, {Field: "rating", Num: &n}}
	cases := []struct {
		c    store.Cond
		want bool
	}{
		{store.Cond{Field: "kind", Op: store.OpEq, Value: "news"}, true},
		{store.Cond{Field: "kind", Op: store.OpEq, Value: "video"}, false},
		{store.Cond{Field: "kind", Op: store.OpIn, Value: []string{"a", "news"}}, true},
		{store.Cond{Field: "rating", Op: store.OpGte, Value: 3}, true},
		{store.Cond{Field: "rating", Op: store.OpGt, Value: int64(3)}, false},
		{store.Cond{Field: "gone", Op: store.OpNe, Value: "x"}, true},
	}
	for i, c := range cases {
		if got := MatchWhere([]store.Cond{c.c}, vals); got != c.want {
			t.Errorf("%d: %v", i, got)
		}
	}
}
