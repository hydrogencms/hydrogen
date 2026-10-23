package storeutil

import (
	"testing"

	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
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

func TestMatchWhereGroups(t *testing.T) {
	s, n := "news", 3.0
	vals := []schema.IndexValue{{Field: "kind", Text: &s}, {Field: "rating", Num: &n}}
	kind := func(v string) store.Cond { return store.Cond{Field: "kind", Op: store.OpEq, Value: v} }
	rating := func(v float64) store.Cond { return store.Cond{Field: "rating", Op: store.OpEq, Value: v} }
	cases := []struct {
		name string
		c    store.Cond
		want bool
	}{
		{"or first group", store.Cond{Op: store.OpOr, Groups: [][]store.Cond{{kind("news")}, {kind("video")}}}, true},
		{"or no group", store.Cond{Op: store.OpOr, Groups: [][]store.Cond{{kind("video")}, {kind("news"), rating(4)}}}, false},
		{"or none", store.Cond{Op: store.OpOr}, false},
		{"not satisfied", store.Cond{Op: store.OpNot, Groups: [][]store.Cond{{kind("news")}}}, false},
		{"not and group", store.Cond{Op: store.OpNot, Groups: [][]store.Cond{{kind("news"), rating(4)}}}, true},
		{"not absent field", store.Cond{Op: store.OpNot, Groups: [][]store.Cond{{{Field: "gone", Op: store.OpEq, Value: "x"}}}}, true},
		{"nested", store.Cond{Op: store.OpOr, Groups: [][]store.Cond{{{Op: store.OpNot, Groups: [][]store.Cond{{kind("news")}}}}, {rating(3)}}}, true},
	}
	for _, c := range cases {
		if got := MatchWhere([]store.Cond{c.c}, vals); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
