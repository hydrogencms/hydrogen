package content

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const testSchema = `
types:
  article:
    fields:
      title:   { type: string, translatable: true }
      body:    { type: richtext, translatable: true }
      rating:  { type: number, index: true }
      day:     { type: date, index: true }
      stamp:   { type: datetime, index: true }
      on:      { type: boolean, index: true }
      slug:    { type: slug, from: title, index: true }
      author:  { type: ref, to: [person], index: true }
      tags:    { type: list, of: { type: string }, index: true }
      refs:    { type: list, of: { type: ref, to: [person, article] } }
      grp:     { type: group, fields: { who: { type: ref, to: [person] } } }
      plain:   { type: string }
  person:
    fields:
      name2: { type: string }
`

func compile(t *testing.T, yaml string) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"s/s.yaml": {Data: []byte(yaml)}}, "s"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func dec(t *testing.T, js string) map[string]any {
	t.Helper()
	d, err := schema.DecodeDoc([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func enc(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestApplyPatch(t *testing.T) {
	tests := []struct{ name, base, patch, want string }{
		{"set", `{"a":1}`, `{"b":2}`, `{"a":1,"b":2}`},
		{"remove", `{"a":1,"b":2}`, `{"a":null}`, `{"b":2}`},
		{"remove missing", `{"a":1}`, `{"z":null}`, `{"a":1}`},
		{"nested merge", `{"o":{"x":1,"y":2}}`, `{"o":{"y":null,"z":3}}`, `{"o":{"x":1,"z":3}}`},
		{"object over scalar", `{"o":1}`, `{"o":{"x":null,"y":1}}`, `{"o":{"y":1}}`},
		{"array replaced", `{"l":[1,2,3]}`, `{"l":[9]}`, `{"l":[9]}`},
		{"scalar over object", `{"o":{"x":1}}`, `{"o":5}`, `{"o":5}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, patch := dec(t, tt.base), dec(t, tt.patch)
			applyPatch(base, patch)
			if got := enc(base); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestApplyPatchDoesNotAlias(t *testing.T) {
	patch := dec(t, `{"l":[1,{"a":1}],"o":{"x":[1]}}`)
	dst := map[string]any{}
	applyPatch(dst, patch)
	dst["l"].([]any)[1].(map[string]any)["a"] = 9
	dst["o"].(map[string]any)["x"].([]any)[0] = 9
	if enc(patch) != `{"l":[1,{"a":1}],"o":{"x":[1]}}` {
		t.Errorf("patch mutated: %s", enc(patch))
	}
}

func TestSplitAndMerge(t *testing.T) {
	s := compile(t, testSchema)
	typ, _ := s.Type("article")
	doc := dec(t, `{"title":"T","rating":3,"body":{"type":"doc"},"ghost":1}`)
	shared, loc := splitDoc(typ, doc)
	if enc(shared) != `{"rating":3}` || enc(loc) != `{"body":{"type":"doc"},"title":"T"}` {
		t.Fatalf("shared=%s locale=%s", enc(shared), enc(loc))
	}
	merged := mergeParts(shared, loc)
	delete(merged, "title")
	if _, ok := loc["title"]; !ok {
		t.Error("mergeParts shares the top-level map")
	}
	if len(merged) != 2 {
		t.Errorf("merged = %v", merged)
	}
}

func TestEqualJSON(t *testing.T) {
	tests := []struct {
		a, b any
		want bool
	}{
		{json.Number("1"), json.Number("1.0"), true},
		{json.Number("1"), float64(1), true},
		{json.Number("1"), "1", false},
		{map[string]any{"a": []any{1, "x"}}, map[string]any{"a": []any{json.Number("1"), "x"}}, true},
		{map[string]any{"a": 1}, map[string]any{"a": 1, "b": nil}, false},
		{[]any{1, 2}, []any{2, 1}, false},
		{nil, nil, true},
		{nil, "", false},
		{true, false, false},
	}
	for i, tt := range tests {
		if got := equalJSON(tt.a, tt.b); got != tt.want {
			t.Errorf("%d: equalJSON(%v, %v) = %v", i, tt.a, tt.b, got)
		}
	}
}

func TestPartDiffers(t *testing.T) {
	mk := func(shared string, locs map[string]string) *pubVersion {
		v := store.Version{Shared: []byte(shared)}
		for code, js := range locs {
			v.Locales = append(v.Locales, store.VersionLocale{Locale: code, Data: []byte(js)})
		}
		return newPubVersion(v)
	}
	a := mk(`{"r":1}`, map[string]string{"en": `{"t":"x"}`})
	b := mk(`{"r":1.0}`, map[string]string{"en": `{"t":"x"}`, "de": `{"t":"y"}`})
	tests := []struct {
		x, y *pubVersion
		part string
		want bool
	}{
		{a, b, "", false},
		{a, b, "en", false},
		{a, b, "de", true},
		{nil, a, "", true},
		{nil, nil, "", false},
		{nil, a, "en", true},
		{a, mk(`{"r":2}`, nil), "", true},
	}
	for i, tt := range tests {
		got, err := partDiffers(tt.x, tt.y, tt.part)
		if err != nil || got != tt.want {
			t.Errorf("%d: got %v, %v want %v", i, got, err, tt.want)
		}
	}
}

func TestDiffSchemas(t *testing.T) {
	parse := func(y string) *compiledSchema {
		b, err := compile(t, y).MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		cs, err := parseCompiled(b)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	old := parse(`
groups:
  g: { fields: { a: { type: string }, b: { type: string } } }
types:
  one:
    fields:
      f1: { type: string }
      f2: { type: string }
      f3: { type: string }
      f4: { type: string }
      grp: { type: group, use: g }
      lst: { type: list, of: { type: group, fields: { z: { type: string } } } }
  two:
    fields:
      x: { type: string }
`)
	cur := parse(`
groups:
  g: { fields: { a: { type: number } } }
types:
  one:
    fields:
      f1: { type: string }
      f3: { type: number }
      f4: { type: string, translatable: true }
      grp: { type: group, use: g }
      lst: { type: list, of: { type: group, fields: { y: { type: string } } } }
      added: { type: string }
`)
	got := diffSchemas(old, cur)
	want := []string{"one.f2: removed", "one.f3: type changed", "one.f4: translatable changed",
		"one.grp.a: type changed", "one.grp.b: removed", "one.lst.z: removed", "two: removed"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	left := unaccounted(got, []string{"one.f2", "one.grp", "two"})
	if !slices.Equal(left, []string{"one.f3: type changed", "one.f4: translatable changed", "one.lst.z: removed"}) {
		t.Errorf("unaccounted = %q", left)
	}
	if len(diffSchemas(old, old)) != 0 {
		t.Error("identical schemas differ")
	}
}

func TestListQuery(t *testing.T) {
	s, err := New(Options{DB: nopDB{}, Schema: compile(t, testSchema), Outbox: nonNilOutbox()})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.UUID{1}
	q, err := s.query(ListOptions{Type: "article", Limit: 9999, Offset: -3, Where: []Filter{
		{Field: "rating", Op: store.OpGte, Value: "2.5"},
		{Field: "rating", Op: store.OpIn, Value: "1, 2"},
		{Field: "day", Op: store.OpEq, Value: "2026-02-03"},
		{Field: "stamp", Op: store.OpLt, Value: "2026-02-03T10:00:00+02:00"},
		{Field: "on", Op: store.OpEq, Value: "true"},
		{Field: "author", Op: store.OpEq, Value: id.String()},
		{Field: "tags", Op: store.OpContains, Value: "go"},
		{Field: "slug", Op: store.OpPrefix, Value: "a"},
		{Field: "author", Op: store.OpExists, Value: "false"},
	}, Sort: []SortSpec{{Field: "rating", Desc: true}, {Field: "name"}}})
	if err != nil {
		t.Fatal(err)
	}
	if q.Limit != 500 || q.Offset != 0 || q.Locale != "en" {
		t.Errorf("paging = %+v", q)
	}
	stamp := time.Date(2026, 2, 3, 8, 0, 0, 0, time.UTC)
	want := []store.Cond{
		{Field: "rating", Column: store.ColNum, Op: store.OpGte, Value: 2.5},
		{Field: "rating", Column: store.ColNum, Op: store.OpIn, Value: []any{1.0, 2.0}},
		{Field: "day", Column: store.ColTime, Op: store.OpEq, Value: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)},
		{Field: "stamp", Column: store.ColTime, Op: store.OpLt, Value: stamp},
		{Field: "on", Column: store.ColBool, Op: store.OpEq, Value: true},
		{Field: "author", Column: store.ColRef, Op: store.OpEq, Value: id},
		{Field: "tags", Column: store.ColText, Op: store.OpContains, Value: "go"},
		{Field: "slug", Column: store.ColText, Op: store.OpPrefix, Value: "a"},
		{Field: "author", Column: store.ColRef, Op: store.OpExists, Value: false},
	}
	if !reflect.DeepEqual(q.Where, want) {
		t.Errorf("where = %#v\nwant %#v", q.Where, want)
	}
	if !reflect.DeepEqual(q.Sort, []store.Sort{{Field: "rating", Column: store.ColNum, Desc: true}, {Field: "name"}}) {
		t.Errorf("sort = %+v", q.Sort)
	}
	d, _ := s.query(ListOptions{})
	if d.Limit != 50 {
		t.Errorf("default limit = %d", d.Limit)
	}
}

func TestListQueryRejects(t *testing.T) {
	s, _ := New(Options{DB: nopDB{}, Schema: compile(t, testSchema), Outbox: nonNilOutbox()})
	bad := []Filter{
		{Field: "plain", Op: store.OpEq, Value: "x"},
		{Field: "title", Op: store.OpEq, Value: "x"},
		{Field: "ghost", Op: store.OpEq, Value: "x"},
		{Field: "rating", Op: store.OpEq, Value: "abc"},
		{Field: "rating", Op: store.OpContains, Value: 1},
		{Field: "rating", Op: store.OpPrefix, Value: "1"},
		{Field: "on", Op: store.OpGt, Value: true},
		{Field: "day", Op: store.OpEq, Value: "yesterday"},
		{Field: "author", Op: store.OpEq, Value: "nope"},
		{Field: "on", Op: store.OpEq, Value: "maybe"},
		{Field: "rating", Op: "weird", Value: 1},
	}
	for _, f := range bad {
		if _, err := s.query(ListOptions{Type: "article", Where: []Filter{f}}); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: err = %v", f, err)
		}
	}
	if _, err := s.query(ListOptions{Where: []Filter{{Field: "rating", Op: store.OpEq, Value: 1}}}); !errors.Is(err, ErrBadRequest) {
		t.Error("filter without a type accepted")
	}
	if _, err := s.query(ListOptions{Type: "article", Sort: []SortSpec{{Field: "plain"}}}); !errors.Is(err, ErrBadRequest) {
		t.Error("sort by unindexed field accepted")
	}
	if _, err := s.query(ListOptions{Type: "nope"}); !errors.Is(err, ErrUnknownType) {
		t.Error("unknown type accepted")
	}
}

func TestSlugSources(t *testing.T) {
	s, _ := New(Options{DB: nopDB{}, Schema: compile(t, testSchema), Outbox: nonNilOutbox()})
	typ, _ := s.o.Schema.Type("article")
	doc := map[string]any{"title": "Hello, World"}
	s.fillSlugs(typ, doc)
	if doc["slug"] != "hello-world" {
		t.Errorf("slug = %v", doc["slug"])
	}
	doc = map[string]any{"title": "X", "slug": "mine"}
	s.fillSlugs(typ, doc)
	if doc["slug"] != "mine" {
		t.Errorf("slug overwritten: %v", doc["slug"])
	}
}

func TestCapSearchText(t *testing.T) {
	short := "hello world"
	if capSearchText(short) != short {
		t.Error("short text changed")
	}
	long := strings.Repeat("word ", maxSearchRunes)
	got := capSearchText(long)
	if n := len([]rune(got)); n > maxSearchRunes || !strings.HasSuffix(got, "word") {
		t.Errorf("len %d, suffix %q", n, got[len(got)-6:])
	}
}

type typedDoc struct {
	Title  string   `json:"title,omitempty"`
	Count  int      `json:"count"`
	Big    *int64   `json:"big,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Inline *struct {
		A string `json:"a"`
	} `json:"inline,omitempty"`
}

func TestTypedHookWriteBack(t *testing.T) {
	big := int64(9007199254740993)
	doc := map[string]any{"title": "T", "keep": json.Number("12345678901234567890"), "big": json.Number("9007199254740993")}
	c := &hooks.Context{Doc: doc}
	fn := typedFunc(func(_ *hooks.Context, d *typedDoc) error {
		if d.Title != "T" || d.Big == nil || *d.Big != big {
			t.Errorf("decoded = %+v", d)
		}
		d.Title = "changed"
		d.Tags = []string{"a"}
		d.Big = nil
		return nil
	})
	if err := fn(c); err != nil {
		t.Fatal(err)
	}
	if enc(doc) != `{"keep":12345678901234567890,"tags":["a"],"title":"changed"}` {
		t.Errorf("doc = %s", enc(doc))
	}
	if _, ok := doc["count"]; ok {
		t.Error("untouched zero field written back")
	}
	if _, ok := doc["tags"].([]any); !ok {
		t.Errorf("tags type = %T", doc["tags"])
	}
	boom := errors.New("boom")
	if err := typedFunc(func(*hooks.Context, *typedDoc) error { return boom })(&hooks.Context{Doc: map[string]any{}}); !errors.Is(err, boom) {
		t.Errorf("hook error = %v", err)
	}
	if err := typedFunc(func(*hooks.Context, *typedDoc) error { return nil })(&hooks.Context{}); err != nil {
		t.Errorf("nil doc: %v", err)
	}
}

func TestToMapKeepsNumbers(t *testing.T) {
	big := int64(9007199254740993)
	m, err := toMap(&typedDoc{Big: &big})
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := m["big"].(json.Number); !ok || n != "9007199254740993" {
		t.Errorf("big = %#v", m["big"])
	}
}

func TestHandleRegisterPanicsOnBadHook(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic for an invalid hook")
		}
	}()
	var r hooks.Registry
	Handle[typedDoc]{Type: "x"}.AfterPublish(reg{&r}, func(*hooks.Context, *typedDoc) error { return nil }, Async(""))
}

type reg struct{ r *hooks.Registry }

func (r reg) AddHook(h hooks.Hook) error { return r.r.Add(h) }

func TestSortMigrations(t *testing.T) {
	up := func(*MigrationDoc) error { return nil }
	got, err := sortMigrations([]Migration{{Name: "b", Type: "t", Up: up}, {Name: "a", Type: "t", Up: up}})
	if err != nil || got[0].Name != "a" {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range [][]Migration{
		{{Name: "a", Type: "t", Up: up}, {Name: "a", Type: "t", Up: up}},
		{{Name: "a", Type: "t"}},
		{{Type: "t", Up: up}},
	} {
		if _, err := sortMigrations(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestDiffBlocksAndTokens(t *testing.T) {
	parse := func(y string) *compiledSchema {
		b, err := compile(t, y).MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		cs, err := parseCompiled(b)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	old := parse(`
tokens:
  tone: [light, dark, brand]
  space: [sm, md]
blocks:
  hero:
    props:
      heading: { type: string }
      sub: { type: string }
      tone: { type: token, set: tone }
  cards:
    props:
      title: { type: string }
    slots:
      items: { allow: [card, hero], min: 1, max: 6 }
      extra: { allow: [card] }
  card:
    props: { t: { type: string } }
  gone:
    props: { t: { type: string } }
types:
  page:
    fields:
      body: { type: blocks, allow: [hero, cards, card, shared], max: 20 }
      side: { type: blocks, allow: [hero] }
  shell:
    blockset: true
    fields:
      c: { type: blocks, allow: [hero] }
`)
	cur := parse(`
tokens:
  tone: [light, dark]
  tone2: [a]
blocks:
  hero:
    props:
      heading: { type: number }
      tone: { type: token, set: tone2 }
  cards:
    props:
      title: { type: string }
    slots:
      items: { allow: [card], min: 2, max: 3 }
  card:
    props: { t: { type: string } }
types:
  page:
    fields:
      body: { type: blocks, allow: [hero, cards], max: 10 }
      side: { type: blocks, allow: [hero], min: 1 }
  shell:
    fields:
      c: { type: blocks, allow: [hero] }
`)
	got := diffSchemas(old, cur)
	for _, want := range []string{
		"block:gone: removed",
		"block:hero.props.heading: type changed",
		"block:hero.props.sub: removed",
		"block:hero.props.tone: token set changed",
		"block:cards.slots.extra: removed",
		"block:cards.slots.items: allow narrowed (removed hero)",
		"block:cards.slots.items: min tightened",
		"block:cards.slots.items: max tightened",
		"page.body: allow narrowed (removed card, shared)",
		"page.body: max tightened",
		"page.side: min tightened",
		"shell: blockset removed",
		"token:space: removed",
		"token:tone: values removed (brand)",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	left := unaccounted(got, []string{"block:hero", "block:cards", "block:gone", "block:shared", "token:space", "token:tone", "page.body", "page.side", "shell"})
	if len(left) != 0 {
		t.Errorf("unaccounted = %q", left)
	}
	if len(diffSchemas(old, old)) != 0 {
		t.Error("identical schemas differ")
	}
}
