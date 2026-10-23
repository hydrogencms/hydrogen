package pages

import (
	"encoding/json"
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
)

func TestMatches(t *testing.T) {
	doc := map[string]any{"category": "news", "n": json.Number("3"), "on": true, "tags": []any{"a", "b"}}
	for _, c := range []struct {
		name   string
		filter map[string]any
		want   bool
	}{
		{"empty filter", nil, true},
		{"string", map[string]any{"category": "news"}, true},
		{"other string", map[string]any{"category": "blog"}, false},
		{"any of", map[string]any{"category": []any{"blog", "news"}}, true},
		{"none of", map[string]any{"category": []any{"blog", "docs"}}, false},
		{"number across decodings", map[string]any{"n": float64(3)}, true},
		{"number mismatch", map[string]any{"n": float64(4)}, false},
		{"bool", map[string]any{"on": true}, true},
		{"type mismatch", map[string]any{"on": "true"}, false},
		{"list field contains", map[string]any{"tags": "b"}, true},
		{"list field misses", map[string]any{"tags": "c"}, false},
		{"missing field", map[string]any{"nope": "x"}, false},
		{"all conditions", map[string]any{"category": "news", "on": true}, true},
		{"one condition fails", map[string]any{"category": "news", "on": false}, false},
	} {
		if got := matches(c.filter, doc); got != c.want {
			t.Errorf("%s: matches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheckFilterValue(t *testing.T) {
	for v, ok := range map[string]bool{"a": true, "": true} {
		if (checkFilterValue(v) == nil) != ok {
			t.Errorf("%q", v)
		}
	}
	for _, v := range []any{[]any{}, []any{map[string]any{}}, map[string]any{}, nil, []any{[]any{"x"}}} {
		if checkFilterValue(v) == nil {
			t.Errorf("%#v should be rejected", v)
		}
	}
	for _, v := range []any{1, float64(2), json.Number("3"), true, []any{"a", float64(1)}} {
		if err := checkFilterValue(v); err != nil {
			t.Errorf("%#v: %v", v, err)
		}
	}
}

func TestRelPath(t *testing.T) {
	pat, err := route.Parse("{publishDate:yyyy}/{category}/{id}")
	if err != nil {
		t.Fatal(err)
	}
	id := ids.UUID{1}
	rel, ok := relPath(pat, id, map[string]any{"publishDate": "2026-10-14T09:00:00Z", "category": "Big Data"}, nil)
	if !ok || rel != "2026/big-data/"+id.String() {
		t.Errorf("rel = %q, %v", rel, ok)
	}
	for _, doc := range []map[string]any{
		{"publishDate": "2026-10-14", "category": ""},
		{"publishDate": "2026-10-14"},
		{"category": "x"},
		{"publishDate": "yesterday", "category": "x"},
		{"publishDate": "2026-10-14", "category": []any{"x"}},
	} {
		if rel, ok := relPath(pat, id, doc, nil); ok {
			t.Errorf("%v: rel %q, want none", doc, rel)
		}
	}
}

func TestCheckTarget(t *testing.T) {
	for to, ok := range map[string]bool{
		"/about":               true,
		"/a?b=c#d":             true,
		"https://example.com":  true,
		"http://example.com/x": true,
		"item:01a139a3-de38-74ec-b7d7-dc3a1bfc4913": true,
		"item:nope":      false,
		"//evil.example": false,
		"ftp://x":        false,
		"about":          false,
		"/with space":    false,
		"":               false,
		"https://":       false,
		"javascript:x":   false,
	} {
		if err := checkTarget(to); (err == nil) != ok {
			t.Errorf("%q: %v", to, err)
		}
	}
}

func TestSameMemberships(t *testing.T) {
	a, b := ids.UUID{1}, ids.UUID{2}
	x := []membership{{Node: a, Rel: "x"}, {Node: b, Rel: "y"}}
	if !sameMemberships(x, []membership{{Node: b, Rel: "y"}, {Node: a, Rel: "x"}}) {
		t.Error("order must not matter")
	}
	if sameMemberships(x, []membership{{Node: a, Rel: "x"}, {Node: b, Rel: "z"}}) || sameMemberships(x, x[:1]) {
		t.Error("differences must show")
	}
}
