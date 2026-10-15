package cachetag

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestItems(t *testing.T) {
	m := Items(func(typ string) bool { return typ == "settings" })
	for _, tc := range []struct {
		topic, payload string
		want           []string
	}{
		{"item.published", `{"id":"abc","type":"article","version":2,"locales":["en"]}`, []string{"i:abc", "c:article"}},
		{"item.deleted", `{"id":"abc","type":"settings"}`, []string{"i:abc", "s:settings"}},
		{"item.created", `{"id":"x","type":"gone"}`, []string{"i:x", "c:gone"}},
		{"item.updated", `{"id":"x","type":"gone","draft":true}`, nil},
		{"item.updated", `{"id":"x","type":"gone","draft":false}`, []string{"i:x", "c:gone"}},
		{"page.changed", `{"id":"x","type":"a"}`, nil},
		{"item.updated", `not json`, nil},
	} {
		if got := m(tc.topic, json.RawMessage(tc.payload)); !slices.Equal(got, tc.want) {
			t.Errorf("%s %s: %v, want %v", tc.topic, tc.payload, got, tc.want)
		}
	}
}

func TestTags(t *testing.T) {
	a := func(string, json.RawMessage) []string { return []string{"b", "a", "", "bad tag", "a"} }
	b := func(string, json.RawMessage) []string { return []string{"c", "b"} }
	if got := Tags([]Mapper{a, b}, "t", nil); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("tags = %v", got)
	}
	all := func(string, json.RawMessage) []string { return []string{"x", All} }
	if got := Tags([]Mapper{a, all}, "t", nil); !slices.Equal(got, []string{All}) {
		t.Errorf("all = %v", got)
	}
}

func TestChunks(t *testing.T) {
	if got := Chunks([]string{"b", "a", "b"}, 100); !slices.Equal(got, []string{"a b"}) {
		t.Errorf("chunks = %v", got)
	}
	if got := Chunks([]string{"a", "*"}, 100); !slices.Equal(got, []string{"*"}) {
		t.Errorf("all = %v", got)
	}
	if got := Chunks(nil, 100); len(got) != 0 {
		t.Errorf("empty = %v", got)
	}
	var tags []string
	for i := range 1000 {
		tags = append(tags, Item(strings.Repeat("x", 30)+string(rune('a'+i%26))+strings.Repeat("y", i/26)))
	}
	got := Chunks(tags, MaxPayload)
	n := 0
	for _, c := range got {
		if len(c) > MaxPayload {
			t.Errorf("chunk of %d bytes", len(c))
		}
		n += len(strings.Fields(c))
	}
	if len(got) < 2 || n != len(slices.Compact(slices.Sorted(slices.Values(tags)))) {
		t.Errorf("%d chunks, %d tags", len(got), n)
	}
	if got := Chunks([]string{"aaaaaaaaaa", "b"}, 3); !slices.Equal(got, []string{"aaaaaaaaaa", "b"}) {
		t.Errorf("long tag = %v", got)
	}
}
