package delivery

import (
	"cmp"
	"reflect"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// matchesWhere evaluates a listing's field conditions over the index values of a draft (schema.IndexValues,
// the projection published items are indexed by), so a draft matches as its published self would: a list
// matches when any element does, ne when none equals the value. The values come from the picked locale's
// fields; published rows take shared fields from the main locale.
func matchesWhere(conds []store.Cond, vals []schema.IndexValue) bool {
	for _, c := range conds {
		var of []schema.IndexValue
		for _, v := range vals {
			if v.Field == c.Field {
				of = append(of, v)
			}
		}
		if !matchCond(c, of) {
			return false
		}
	}
	return true
}

func matchCond(c store.Cond, vals []schema.IndexValue) bool {
	if c.Op == store.OpExists {
		want, _ := c.Value.(bool)
		return (len(vals) > 0) == want
	}
	test := func(want any, ok func(n int) bool) bool {
		for _, v := range vals {
			if n, found := compareValue(c.Column, v, want); found && ok(n) {
				return true
			}
		}
		return false
	}
	equal := func(n int) bool { return n == 0 }
	switch c.Op {
	case store.OpEq, store.OpContains:
		return test(c.Value, equal)
	case store.OpNe:
		return !test(c.Value, equal)
	case store.OpLt:
		return test(c.Value, func(n int) bool { return n < 0 })
	case store.OpLte:
		return test(c.Value, func(n int) bool { return n <= 0 })
	case store.OpGt:
		return test(c.Value, func(n int) bool { return n > 0 })
	case store.OpGte:
		return test(c.Value, func(n int) bool { return n >= 0 })
	case store.OpPrefix:
		// Case-sensitive like the adapters (GLOB on SQLite, LIKE on Postgres).
		want, _ := c.Value.(string)
		for _, v := range vals {
			if v.Text != nil && strings.HasPrefix(*v.Text, want) {
				return true
			}
		}
	case store.OpIn:
		list := reflect.ValueOf(c.Value)
		for i := 0; list.Kind() == reflect.Slice && i < list.Len(); i++ {
			if test(list.Index(i).Interface(), equal) {
				return true
			}
		}
	}
	return false
}

// compareValue orders an index value against a condition value of the column's type; false when the index
// value has nothing in that column.
func compareValue(col store.Column, v schema.IndexValue, want any) (int, bool) {
	switch col {
	case store.ColText:
		w, ok := want.(string)
		return cmpPtr(v.Text, w, ok)
	case store.ColNum:
		w, ok := want.(float64)
		return cmpPtr(v.Num, w, ok)
	case store.ColTime:
		w, ok := want.(time.Time)
		return cmpPtr(v.Time, w.UnixMilli(), ok)
	case store.ColBool:
		w, ok := want.(bool)
		if v.Bool == nil || !ok {
			return 0, false
		}
		if *v.Bool == w {
			return 0, true
		}
		return 1, true
	case store.ColRef:
		w, ok := want.(ids.UUID)
		id, err := ids.Parse(v.Ref)
		if !ok || err != nil {
			return 0, false
		}
		return strings.Compare(id.String(), w.String()), true
	}
	return 0, false
}

func cmpPtr[T cmp.Ordered](have *T, want T, ok bool) (int, bool) {
	if have == nil || !ok {
		return 0, false
	}
	return cmp.Compare(*have, want), true
}
