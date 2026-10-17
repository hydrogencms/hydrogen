package storeutil

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func TestValidField(t *testing.T) {
	for s, want := range map[string]bool{"title": true, "a.b1": true, "": false, "1a": false, "A": false, "a b": false, "a'": false} {
		if got := ValidField(s); got != want {
			t.Errorf("ValidField(%q) = %v", s, got)
		}
	}
}

func TestNormalize(t *testing.T) {
	if q := Normalize(store.Query{Limit: 0, Offset: -3}); q.Limit != DefaultLimit || q.Offset != 0 {
		t.Errorf("%+v", q)
	}
	if q := Normalize(store.Query{Limit: 9999}); q.Limit != MaxLimit {
		t.Errorf("%+v", q)
	}
}

func TestSearchTerms(t *testing.T) {
	if got := SearchTerms(`fox, "été" a:b!!`); strings.Join(got, "|") != "fox|été|a|b" {
		t.Errorf("%q", got)
	}
}

func TestScalarAndList(t *testing.T) {
	id := ids.UUID{1}
	if v, err := Scalar(store.ColRef, id.String()); err != nil || v != id {
		t.Errorf("%v %v", v, err)
	}
	if v, err := Scalar(store.ColNum, 3); err != nil || v != 3.0 {
		t.Errorf("%v %v", v, err)
	}
	if _, err := Scalar(store.ColText, 3); err == nil {
		t.Error("want error")
	}
	if _, err := Scalar(store.ColRef, "nope"); err == nil {
		t.Error("want error")
	}
	now := time.Now()
	if l, err := List(store.ColTime, []time.Time{now}); err != nil || len(l) != 1 {
		t.Errorf("%v %v", l, err)
	}
	if _, err := List(store.ColText, "x"); err == nil {
		t.Error("want error")
	}
}

func TestSearchTexts(t *testing.T) {
	loc, txt := SearchTexts([]store.SearchDoc{{Locale: "fr", Text: "a"}, {Locale: "en", Text: "b"}, {Locale: "en", Text: "c"}})
	if strings.Join(loc, ",") != "en,fr" || txt[0] != "b\nc" || txt[1] != "a" {
		t.Errorf("%v %q", loc, txt)
	}
	_, txt = SearchTexts([]store.SearchDoc{{Locale: "en", Text: strings.Repeat("é", MaxSearchChars+10)}})
	if n := len([]rune(txt[0])); n != MaxSearchChars {
		t.Errorf("capped to %d", n)
	}
}

func TestDecorator(t *testing.T) {
	a, b := ids.UUID{1}, ids.UUID{2}
	items := []store.ItemSummary{{Item: store.Item{ID: a, MainLocale: "en"}}, {Item: store.Item{ID: b, MainLocale: "en"}}}
	d := NewDecorator("fr", items)
	d.Published(a, "fr", "Bonjour")
	d.Published(a, "en", "Hello")
	d.Published(b, "en", "Only")
	d.Draft(b, "", "")
	d.Finish()
	if items[0].Name != "Bonjour" || items[0].Locales[0] != "en" || items[1].Name != "Only" || len(items[1].Drafts) != 1 {
		t.Errorf("%+v", items)
	}
}

func TestIndexWalk(t *testing.T) {
	sorted := []store.Sort{{Field: "n", Column: store.ColNum}}
	rng := store.Cond{Field: "n", Column: store.ColNum, Op: store.OpGte, Value: 1.0}
	eq := store.Cond{Field: "n", Column: store.ColNum, Op: store.OpEq, Value: 1.0}
	for _, tc := range []struct {
		name string
		q    store.Query
		want bool
	}{
		{"indexed sort", store.Query{Sort: sorted}, true},
		{"range", store.Query{Sort: sorted, Where: []store.Cond{rng}}, true},
		{"equality", store.Query{Sort: sorted, Where: []store.Cond{rng, eq}}, false},
		{"many conditions", store.Query{Sort: sorted, Where: []store.Cond{rng, rng, rng, rng}}, false},
		{"meta sort", store.Query{Sort: []store.Sort{{Field: "createdAt"}}}, false},
		{"two keys", store.Query{Sort: append(sorted, sorted...)}, false},
		{"no sort", store.Query{}, false},
		{"search", store.Query{Sort: sorted, Search: "x"}, false},
	} {
		if got := IndexWalk(tc.q); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunWalk(t *testing.T) {
	item := func(n int) store.ItemSummary { return store.ItemSummary{Item: store.Item{Key: strconv.Itoa(n)}} }
	run := func(have, missing, limit, offset int) (string, [][2]int) {
		var missCalls [][2]int
		page := func(base, n, limit, offset int) []store.ItemSummary {
			var out []store.ItemSummary
			for i := offset; i < min(n, offset+limit); i++ {
				out = append(out, item(base+i))
			}
			return out
		}
		items, err := RunWalk(store.Query{Limit: limit, Offset: offset}, WalkPhases{
			Ordered: func(l, o int) ([]store.ItemSummary, error) { return page(0, have, l, o), nil },
			Missing: func(l, o int) ([]store.ItemSummary, error) {
				missCalls = append(missCalls, [2]int{l, o})
				return page(100, missing, l, o), nil
			},
			CountOrdered: func() (int, error) { return have, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		keys := make([]string, len(items))
		for i, it := range items {
			keys[i] = it.Key
		}
		return strings.Join(keys, ","), missCalls
	}
	for _, tc := range []struct {
		have, missing, limit, offset int
		want                         string
		miss                         [][2]int
	}{
		{5, 3, 2, 0, "0,1", nil},
		{5, 3, 4, 3, "3,4,100,101", [][2]int{{2, 0}}},
		{5, 3, 4, 5, "100,101,102", [][2]int{{4, 0}}},
		{5, 3, 4, 6, "101,102", [][2]int{{4, 1}}},
		{5, 3, 4, 20, "", [][2]int{{4, 15}}},
		{0, 3, 4, 0, "100,101,102", [][2]int{{4, 0}}},
		{0, 0, 4, 0, "", [][2]int{{4, 0}}},
	} {
		got, miss := run(tc.have, tc.missing, tc.limit, tc.offset)
		if got != tc.want || fmt.Sprint(miss) != fmt.Sprint(tc.miss) {
			t.Errorf("%+v: %q %v", tc, got, miss)
		}
	}
}

func TestLiveTerms(t *testing.T) {
	empty := store.Cond{Field: "f", Column: store.ColText, Op: store.OpIn, Value: []string{}}
	some := store.Cond{Field: "f", Column: store.ColText, Op: store.OpIn, Value: []string{"a"}}
	got := LiveTerms([]store.ScopeTerm{{Where: []store.Cond{some, empty}}, {CreatedBy: []string{"u"}}, {Where: []store.Cond{some}}})
	if len(got) != 2 || got[0].CreatedBy[0] != "u" || len(got[1].Where) != 1 {
		t.Errorf("LiveTerms = %+v", got)
	}
	if got := LiveTerms(nil); len(got) != 0 {
		t.Errorf("LiveTerms(nil) = %+v", got)
	}
}

func TestDecoratorWorkingNames(t *testing.T) {
	id := ids.UUID{1}
	for working, want := range map[bool]string{false: "Published", true: "Draft"} {
		items := []store.ItemSummary{{Item: store.Item{ID: id, MainLocale: "en"}}}
		d := NewDecorator("en", items)
		d.Working = working
		d.Published(id, "en", "Published")
		d.Draft(id, "en", "Draft")
		d.Finish()
		if items[0].Name != want {
			t.Errorf("working=%v name = %q, want %q", working, items[0].Name, want)
		}
	}
}
