package schema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const hardenSchema = `
types:
  person:
    fields: { n: { type: string } }
  a:
    name: "{title} - {sub} - {extra}"
    fields:
      title: { type: string }
      sub:   { type: string }
      extra: { type: string }
      slug:  { type: slug, from: title }
      one:   { type: ref, to: [person], on_delete: nullify }
      many:  { type: list, of: { type: ref, to: [person] } }
      g:     { type: group, fields: { r: { type: ref, to: [person] }, i: { type: number, integer: true, min: -5, max: 100 } } }
      n:     { type: number, integer: true, min: -5, max: 100 }
      ns:    { type: list, of: { type: number, integer: true } }
      big:   { type: number, integer: true, max: 100 }
      low:   { type: number, integer: true, min: -100 }
      body:  { type: richtext }
      secret: { type: string, ai: false }
      kind:  { type: enum, values: [a, b] }
      web:   { type: url }
      h:     { type: group, fields: { t: { type: string }, hid: { type: text, ai: false } } }
      note:  { type: text }
`

func hardenType(t *testing.T) (*Schema, *Type) {
	t.Helper()
	s := mustCompile(t, hardenSchema)
	a, _ := s.Type("a")
	return s, a
}

func TestRichTextNUL(t *testing.T) {
	s, a := hardenType(t)
	d := doc(t, `{"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a\u0000b"}]}]}}`)
	errs := s.Validate(a, d, ModeDraft)
	if len(errs) != 1 || errs[0].Code != "format" || errs[0].Message != "must not contain NUL characters" ||
		errs[0].Path != "body.content[0].content[0]" {
		t.Fatalf("errs = %+v", errs)
	}
}

func TestNormalizeRefsLowerCase(t *testing.T) {
	s, a := hardenType(t)
	up := strings.ToUpper(id1)
	d := doc(t, `{"one":"`+up+`","many":["`+up+`"],"g":{"r":"`+up+`"},
		"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"item:`+up+`"}}]}]}]}}`)
	s.Normalize(a, d)
	if d["one"] != id1 || d["many"].([]any)[0] != id1 || d["g"].(map[string]any)["r"] != id1 {
		t.Fatalf("refs not lower-cased: %v", d)
	}
	refs := s.Refs(a, d)
	for _, r := range refs {
		if r.To != id1 {
			t.Fatalf("ref %+v", r)
		}
	}
	if len(refs) != 4 {
		t.Fatalf("refs = %+v", refs)
	}
	if errs := s.Validate(a, d, ModeDraft); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestRemoveRefCaseInsensitiveAndFiltered(t *testing.T) {
	s, a := hardenType(t)
	mk := func() map[string]any {
		return doc(t, `{"one":"`+strings.ToUpper(id1)+`","many":["`+id1+`","`+id2+`"],"g":{"r":"`+id1+`"}}`)
	}
	d := mk()
	if !s.RemoveRef(a, d, id1) {
		t.Fatal("no change")
	}
	if _, ok := d["one"]; ok || !reflect.DeepEqual(d["many"], []any{id2}) || len(d["g"].(map[string]any)) != 0 {
		t.Fatalf("d = %v", d)
	}

	d = mk()
	var seen []string
	if s.RemoveRefIn(a, d, id1, func(f *Field) bool { seen = append(seen, f.Name+":"+f.Type); return false }) {
		t.Fatal("changed although nothing matched")
	}
	if !reflect.DeepEqual(seen, []string{"one:ref", ":ref", "r:ref"}) {
		t.Fatalf("match saw %v", seen)
	}
	if !reflect.DeepEqual(d, mk()) {
		t.Fatal("doc changed")
	}

	d = mk()
	if !s.RemoveRefIn(a, d, id1, func(f *Field) bool { _, ok := s.Option(f, "on_delete"); return ok }) {
		t.Fatal("no change")
	}
	if _, ok := d["one"]; ok {
		t.Fatal("one (on_delete set) kept")
	}
	if len(d["many"].([]any)) != 2 || d["g"].(map[string]any)["r"] != id1 {
		t.Fatalf("other refs touched: %v", d)
	}
}

func TestOption(t *testing.T) {
	s, a := hardenType(t)
	f, _ := a.Field("slug")
	if v, ok := s.Option(f, "from"); !ok || v != "title" {
		t.Fatalf("from = %v %v", v, ok)
	}
	one, _ := a.Field("one")
	if v, ok := s.Option(one, "on_delete"); !ok || v != "nullify" {
		t.Fatalf("on_delete = %v %v", v, ok)
	}
	if _, ok := s.Option(one, "nope"); ok {
		t.Fatal("unknown option found")
	}
	if _, ok := s.Option(&Field{}, "from"); ok {
		t.Fatal("foreign field has options")
	}
}

func TestReportIsBounded(t *testing.T) {
	s, a := hardenType(t)
	kids := make([]any, 100_000)
	for i := range kids {
		kids[i] = map[string]any{"type": "bogus"}
	}
	d := map[string]any{"body": map[string]any{"type": "doc", "content": kids}}
	start := time.Now()
	errs := s.Validate(a, d, ModeDraft)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("took %v", el)
	}
	if len(errs) != maxReportErrors+1 {
		t.Fatalf("len = %d", len(errs))
	}
	if last := errs[len(errs)-1]; last.Path != "" || last.Code != "too_many_errors" || last.Message == "" {
		t.Fatalf("last = %+v", last)
	}

	// list elements and unknown fields stop too
	many := make([]any, 5000)
	for i := range many {
		many[i] = json.Number("1.5")
	}
	errs = s.Validate(a, map[string]any{"ns": many}, ModeDraft)
	if len(errs) != maxReportErrors+1 || errs[len(errs)-1].Code != "too_many_errors" {
		t.Fatalf("list: len = %d", len(errs))
	}
	unk := map[string]any{}
	for i := range 1000 {
		unk[strings.Repeat("k", 1+i%7)+string(rune('A'+i%26))+string(rune('a'+i/26))] = 1
	}
	errs = s.Validate(a, unk, ModeDraft)
	if len(errs) != maxReportErrors+1 {
		t.Fatalf("unknown: len = %d", len(errs))
	}

	// exactly the cap is not truncated
	var r Report
	for range maxReportErrors {
		r.Add("x", "c", "m")
	}
	if len(r.Errors()) != maxReportErrors {
		t.Fatal("capped early")
	}
	r.Add("x", "c", "m")
	r.Add("x", "c", "m")
	if len(r.Errors()) != maxReportErrors+1 {
		t.Fatalf("len = %d", len(r.Errors()))
	}
}

func TestIntegers(t *testing.T) {
	s, a := hardenType(t)
	d := doc(t, `{"n":5.0,"ns":[1e3,-0,7,2.50e1],"g":{"i":1E1}}`)
	if errs := s.Validate(a, d, ModeDraft); len(errs) != 0 {
		t.Fatal(errs)
	}
	s.Normalize(a, d)
	if d["n"] != json.Number("5") || !reflect.DeepEqual(d["ns"], []any{json.Number("1000"), json.Number("0"), json.Number("7"), json.Number("25")}) ||
		d["g"].(map[string]any)["i"] != json.Number("10") {
		t.Fatalf("d = %v", d)
	}

	cases := []struct {
		doc, want string
	}{
		{`{"n":101}`, "n:max"},
		{`{"n":1.01e2}`, "n:max"},
		{`{"n":-6.0}`, "n:min"},
		{`{"big":9223372036854775808}`, "big:max"},
		{`{"big":1e30}`, "big:max"},
		{`{"low":-9223372036854775809}`, "low:min"},
		{`{"n":9223372036854775808}`, "n:max"},
		{`{"ns":[9223372036854775808]}`, "ns[0]:type"},
		{`{"ns":[-1e19]}`, "ns[0]:type"},
		{`{"ns":[0.5]}`, "ns[0]:type"},
		{`{"ns":["1"]}`, "ns[0]:type"},
	}
	for _, c := range cases {
		errs := codes(s.Validate(a, doc(t, c.doc), ModeDraft))
		if len(errs) != 1 || errs[0] != c.want {
			t.Errorf("%s: %v, want %s", c.doc, errs, c.want)
		}
	}
	// no float rounding: 9007199254740993 stays exact and in range of max 100? it exceeds max.
	ex := s.Validate(a, doc(t, `{"low":9007199254740993}`), ModeDraft)
	if len(ex) != 0 {
		t.Fatal(ex)
	}
	d = doc(t, `{"n":9223372036854775807}`)
	s.Normalize(a, d)
	if d["n"] != json.Number("9223372036854775807") {
		t.Fatalf("d = %v", d)
	}
	// min/max messages are unchanged
	e := s.Validate(a, doc(t, `{"n":101}`), ModeDraft)
	if e[0].Message != "must be at most 100" {
		t.Fatalf("%+v", e)
	}
}

func TestCmpIntFloat(t *testing.T) {
	for _, c := range []struct {
		n    int64
		f    float64
		want int
	}{
		{5, 5, 0}, {5, 5.5, -1}, {5, 4.5, 1}, {-5, -5.5, 1}, {-5, -4.5, -1},
		{1 << 62, 1e30, -1}, {-1 << 62, -1e30, 1}, {9007199254740993, 9007199254740992, 1},
	} {
		if got := cmpIntFloat(c.n, c.f); got != c.want {
			t.Errorf("cmpIntFloat(%d, %v) = %d, want %d", c.n, c.f, got, c.want)
		}
	}
}

func TestNameKeepsValueCharacters(t *testing.T) {
	s := mustCompile(t, `
types:
  t:
    name: "{a}"
    fields: { a: { type: string } }
  u:
    name: "{a} / {b}"
    fields: { a: { type: string }, b: { type: string } }
  v:
    name: "{a} - {b} - {c}"
    fields: { a: { type: string }, b: { type: string }, c: { type: string } }
  w:
    name: "Room: {a}, {b}"
    fields: { a: { type: string }, b: { type: string } }
`)
	tp := func(n string) *Type { x, _ := s.Type(n); return x }
	tests := []struct {
		t    string
		doc  map[string]any
		want string
	}{
		{"t", map[string]any{"a": "-5°C"}, "-5°C"},
		{"t", map[string]any{"a": "  :x| "}, ":x|"},
		{"u", map[string]any{"a": "AC/DC /", "b": ""}, "AC/DC /"},
		{"u", map[string]any{"a": "AC/DC /", "b": "live"}, "AC/DC / / live"},
		{"u", map[string]any{"a": "", "b": "-x"}, "-x"},
		{"u", map[string]any{"a": "A", "b": ""}, "A"},
		{"v", map[string]any{"a": "A", "b": "", "c": "C"}, "A - C"},
		{"v", map[string]any{"a": "", "b": "B", "c": ""}, "B"},
		{"v", map[string]any{"a": "A", "b": "", "c": ""}, "A"},
		{"v", map[string]any{"a": "", "b": "", "c": "C"}, "C"},
		{"v", map[string]any{}, ""},
		{"w", map[string]any{"a": "5", "b": ""}, "Room: 5"},
		{"w", map[string]any{"a": "", "b": "x"}, "Room: x"},
		{"w", map[string]any{"a": "", "b": ""}, "Room"},
	}
	for _, c := range tests {
		if got := s.Name(tp(c.t), c.doc); got != c.want {
			t.Errorf("Name(%s, %v) = %q, want %q", c.t, c.doc, got, c.want)
		}
	}
}

func TestSearchTextScope(t *testing.T) {
	s, a := hardenType(t)
	d := doc(t, `{"title":"T","slug":"the-slug","kind":"a","web":"https://x.example","secret":"classified","n":42,
		"note":"N","h":{"t":"inner","hid":"hidden"},"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}}`)
	if got := s.SearchText(a, d); got != "T the-slug N inner B" && got != "T the-slug inner N B" {
		// schema order: title, slug, ..., body, secret(excluded), ..., h, note
		if got != "T the-slug B inner N" {
			t.Fatalf("text = %q", got)
		}
	}
}

func TestJSONSchemaFileUsesRegistry(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(testType{}); err != nil {
		t.Fatal(err)
	}
	js := JSONSchemaFile(reg)
	field := js["$defs"].(map[string]any)["field"].(map[string]any)["properties"].(map[string]any)
	enum := field["type"].(map[string]any)["enum"].([]string)
	if len(enum) != 16 || !contains(enum, "zzmodule") {
		t.Fatalf("enum = %v", enum)
	}
	for i := 1; i < len(enum); i++ {
		if enum[i-1] >= enum[i] {
			t.Fatalf("not sorted: %v", enum)
		}
	}
	if got := JSONSchemaFile(nil)["$defs"].(map[string]any)["field"].(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)["enum"].([]string); len(got) != 15 {
		t.Fatalf("nil registry: %v", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type testType struct{}

func (testType) Name() string { return "zzmodule" }
func (testType) Compile(*Options) (Shape, Config, error) {
	return Shape{Base: BaseString}, jsonConfig{}, nil
}

func TestSlugSchemaPattern(t *testing.T) {
	s := mustCompile(t, hardenSchema)
	a, _ := s.Type("a")
	f, _ := a.Field("slug")
	if p := f.Config.JSONSchema()["pattern"]; p != slugPattern {
		t.Fatalf("pattern = %v", p)
	}
	for _, in := range []string{"", "-", "a--b", "-a", "a-", "Ab", "a b", "ǅ", "a_b"} {
		if isSlug(in) {
			t.Errorf("isSlug(%q)", in)
		}
	}
	for _, in := range []string{"a", "a-b", "日本語", "привет-мир", "x1-2y", "ß-ø"} {
		if !isSlug(in) {
			t.Errorf("!isSlug(%q)", in)
		}
	}
}

func TestReportPathsStayCorrect(t *testing.T) {
	s, a := hardenType(t)
	errs := s.Validate(a, doc(t, `{"g":{"r":"x","i":"y"},"ns":[1,"z"],"zzz":1}`), ModeDraft)
	got := codes(errs)
	want := []string{"g.r:format", "g.i:type", "ns[1]:type", "zzz:unknown_field"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
