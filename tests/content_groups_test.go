package tests

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/store"
)

func TestListGroups(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		mk := func(title, js string) {
			v := e.create("article", "en", js)
			e.update(v.ID, "en", fmt.Sprintf(`{"title":%q}`, title))
			e.clk.Advance(time.Second)
			e.publish(v.ID)
		}
		mk("Alpha", `{"rating":1,"featured":true,"tags":["go","cms"]}`)
		mk("Beta", `{"rating":3,"tags":["go"]}`)
		mk("Gamma", `{"rating":5}`)
		e.create("article", "en", `{"title":"Draft only","tags":["cms"]}`)

		f := func(field string, op store.Op, v any) content.Filter {
			return content.Filter{Field: field, Op: op, Value: v}
		}
		or := func(gs ...[]content.Filter) content.Filter { return content.Filter{Op: store.OpOr, Groups: gs} }
		not := func(gs ...[]content.Filter) content.Filter { return content.Filter{Op: store.OpNot, Groups: gs} }
		g := func(fs ...content.Filter) []content.Filter { return fs }
		list := func(fs ...content.Filter) ([]string, error) {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Where: fs, Sort: []content.SortSpec{{Field: "name"}}})
			if err != nil {
				return nil, err
			}
			var names []string
			for _, it := range res.Items {
				names = append(names, it.Name)
			}
			return names, nil
		}
		for name, tc := range map[string]struct {
			where []content.Filter
			want  []string
		}{
			"or":               {[]content.Filter{or(g(f("rating", store.OpEq, "5")), g(f("featured", store.OpEq, "true")))}, []string{"Alpha", "Gamma"}},
			"or and plain":     {[]content.Filter{or(g(f("tags", store.OpContains, "go")), g(f("tags", store.OpContains, "cms"))), f("rating", store.OpGte, 3)}, []string{"Beta"}},
			"not":              {[]content.Filter{not(g(f("tags", store.OpContains, "go")))}, []string{"Draft only", "Gamma"}},
			"not keeps absent": {[]content.Filter{not(g(f("featured", store.OpEq, true)))}, []string{"Beta", "Draft only", "Gamma"}},
			"not of two":       {[]content.Filter{not(g(f("rating", store.OpEq, 1)), g(f("rating", store.OpEq, 5)))}, []string{"Beta", "Draft only"}},
		} {
			got, err := list(tc.where...)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("%s: %v %v, want %v", name, got, err, tc.want)
			}
		}

		// Groups hold plain filters; every condition in a group counts toward the limit.
		for name, bad := range map[string][]content.Filter{
			"nested":         {or(g(not(g(f("rating", store.OpEq, 1)))))},
			"empty or":       {or()},
			"empty group":    {or(g())},
			"unknown field":  {or(g(f("nope", store.OpEq, 1)))},
			"bad value":      {not(g(f("rating", store.OpEq, "abc")))},
			"over the limit": {or(g(slices.Repeat([]content.Filter{f("rating", store.OpGte, 0)}, content.MaxConditions+1)...))},
		} {
			if _, err := list(bad...); !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("%s: %v", name, err)
			}
		}
		atLimit := or(g(slices.Repeat([]content.Filter{f("rating", store.OpGte, 0)}, content.MaxConditions)...))
		if _, err := list(atLimit); err != nil {
			t.Errorf("a group at the limit: %v", err)
		}
		if _, err := list(atLimit, f("rating", store.OpGte, 0)); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("group and plain filters share the limit: %v", err)
		}
	})
}
