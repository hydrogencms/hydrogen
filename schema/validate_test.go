package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	id1 = "0190a000-0000-7000-8000-000000000001"
	id2 = "0190a000-0000-7000-8000-000000000002"
)

func codes(errs []FieldError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Path + ":" + e.Code
	}
	return out
}

const typesFixture = `
types:
  person:
    fields: { n: { type: string } }
  all:
    fields:
      s:    { type: string, min: 2, max: 5, pattern: "^[a-z]+$" }
      t:    { type: text, max: 10 }
      n:    { type: number, min: 1, max: 10 }
      i:    { type: number, integer: true }
      b:    { type: boolean }
      e:    { type: enum, values: [a, b] }
      d:    { type: date, min: "2020-01-01", max: "2030-01-01" }
      dt:   { type: datetime, min: "2020-01-01T00:00:00Z" }
      sl:   { type: slug, max: 10 }
      em:   { type: email }
      u:    { type: url }
      r:    { type: ref, to: [person] }
      l:    { type: list, of: { type: string, max: 3 }, min: 2, max: 3 }
      lg:   { type: list, of: { type: group, fields: { x: { type: string, required: true } } } }
      g:    { type: group, fields: { x: { type: string, required: true }, y: { type: number } } }
      j:    { type: json }
      rt:   { type: richtext }
      req:  { type: string, required: true }
      kind: { type: enum, values: [a, b], default: a }
      cond: { type: string, required: true, when: { field: kind, eq: b } }
      cne:  { type: string, when: { field: kind, ne: a } }
      cin:  { type: string, when: { field: kind, in: [a, b] } }
      cem:  { type: string, when: { field: s, empty: true } }
`

func TestValidate(t *testing.T) {
	s := mustCompile(t, typesFixture)
	typ, _ := s.Type("all")
	tests := []struct {
		name string
		doc  string
		mode Mode
		want []string
	}{
		{"empty draft", `{}`, ModeDraft, nil},
		{"empty publish", `{}`, ModePublish, []string{"req:required"}},
		{"valid", `{"s":"abc","t":"a\nb","n":5,"i":3,"b":true,"e":"a","d":"2025-05-05","dt":"2025-05-05T10:00:00+02:00","sl":"a-b","em":"a@b.co","u":"https://x.dev/p","r":"` + id1 + `","l":["a","b"],"j":{"any":[1,2]},"req":"x"}`, ModePublish, nil},
		{"string errors", `{"s":"A\nbcdefg","t":"01234567890"}`, ModeDraft, []string{"s:format", "s:max_length", "s:pattern", "t:max_length"}},
		{"string min", `{"s":"a"}`, ModeDraft, []string{"s:min_length"}},
		{"types", `{"s":1,"n":"x","b":"true","e":3,"d":1,"u":false,"l":{},"g":[],"rt":"x","lg":{}}`, ModeDraft,
			[]string{"s:type", "n:type", "b:type", "e:type", "d:type", "u:type", "l:type", "lg:type", "g:type", "rt:type"}},
		{"number range", `{"n":0.5}`, ModeDraft, []string{"n:min"}},
		{"number max", `{"n":11}`, ModeDraft, []string{"n:max"}},
		{"integer fraction", `{"i":1.5}`, ModeDraft, []string{"i:type"}},
		{"integer exponent", `{"i":1e2}`, ModeDraft, nil},
		{"integer with zero fraction", `{"i":5.0}`, ModeDraft, nil},
		{"integer exponent fraction", `{"i":1.5e1}`, ModeDraft, nil},
		{"integer negative exponent", `{"i":1e-1}`, ModeDraft, []string{"i:type"}},
		{"integer int64 max", `{"i":9223372036854775807}`, ModeDraft, nil},
		{"integer int64 min", `{"i":-9223372036854775808}`, ModeDraft, nil},
		{"integer above int64", `{"i":9223372036854775808}`, ModeDraft, []string{"i:type"}},
		{"integer below int64", `{"i":-9223372036854775809}`, ModeDraft, []string{"i:type"}},
		{"integer huge exponent", `{"i":1e999999999999}`, ModeDraft, []string{"i:type"}},
		{"integer tiny exponent", `{"i":1e-999999999999}`, ModeDraft, []string{"i:type"}},
		{"integer zero exponent", `{"i":0e999999999999}`, ModeDraft, nil},
		{"nul in string", "{\"s\":\"a\\u0000b\"}", ModeDraft, []string{"s:format"}},
		{"nul in text", "{\"t\":\"a\\u0000b\"}", ModeDraft, []string{"t:format"}},
		{"nul in slug", "{\"sl\":\"a\\u0000b\"}", ModeDraft, []string{"sl:format"}},
		{"nul in enum", "{\"e\":\"a\\u0000\"}", ModeDraft, []string{"e:format"}},
		{"nul in email", "{\"em\":\"a@b.co\\u0000\"}", ModeDraft, []string{"em:format"}},
		{"nul in url", "{\"u\":\"http://a.co/\\u0000\"}", ModeDraft, []string{"u:format"}},
		{"unicode slug", `{"sl":"привет-мир"}`, ModeDraft, nil},
		{"cjk slug", `{"sl":"日本語"}`, ModeDraft, nil},
		{"upper unicode slug", `{"sl":"Привет"}`, ModeDraft, []string{"sl:format"}},
		{"slug max counts runes", `{"sl":"привет-мир"}`, ModeDraft, nil},
		{"slug too long runes", `{"sl":"абвгдеёжзик"}`, ModeDraft, []string{"sl:max_length"}},
		{"integer exact", `{"i":9007199254740993}`, ModeDraft, nil},
		{"enum", `{"e":"c"}`, ModeDraft, []string{"e:enum"}},
		{"date", `{"d":"2025-13-01"}`, ModeDraft, []string{"d:format"}},
		{"date bounds", `{"d":"2019-01-01"}`, ModeDraft, []string{"d:min"}},
		{"date max", `{"d":"2031-01-01"}`, ModeDraft, []string{"d:max"}},
		{"datetime", `{"dt":"2025-01-01"}`, ModeDraft, []string{"dt:format"}},
		{"datetime min", `{"dt":"2010-01-01T00:00:00Z"}`, ModeDraft, []string{"dt:min"}},
		{"slug", `{"sl":"Bad Slug"}`, ModeDraft, []string{"sl:format"}},
		{"slug dash", `{"sl":"a--b"}`, ModeDraft, []string{"sl:format"}},
		{"slug long", `{"sl":"abcdefghijk"}`, ModeDraft, []string{"sl:max_length"}},
		{"email", `{"em":"A <a@b.co>"}`, ModeDraft, []string{"em:format"}},
		{"url", `{"u":"ftp://x"}`, ModeDraft, []string{"u:format"}},
		{"url item", `{"u":"item:` + id1 + `"}`, ModeDraft, nil},
		{"url bad item", `{"u":"item:nope"}`, ModeDraft, []string{"u:format"}},
		{"ref", `{"r":"nope"}`, ModeDraft, []string{"r:format"}},
		{"list", `{"l":["abcd",1,null]}`, ModeDraft, []string{"l[0]:max_length", "l[1]:type", "l[2]:type"}},
		{"list max", `{"l":["a","b","c","d"]}`, ModeDraft, []string{"l:max_items"}},
		{"list min draft ok", `{"l":["a"]}`, ModeDraft, nil},
		{"list min publish", `{"l":["a"],"req":"x"}`, ModePublish, []string{"l:min_items"}},
		{"group required", `{"g":{"y":"s"},"req":"x"}`, ModePublish, []string{"g.x:required", "g.y:type"}},
		{"list of groups", `{"lg":[{"x":"a"},{}],"req":"x"}`, ModePublish, []string{"lg[1].x:required"}},
		{"unknown sorted", `{"zz":1,"aa":2,"g":{"q":1}}`, ModeDraft, []string{"g.q:unknown_field", "aa:unknown_field", "zz:unknown_field"}},
		{"null", `{"s":null}`, ModeDraft, []string{"s:type"}},
		{"required empty string", `{"req":""}`, ModePublish, []string{"req:required"}},
		{"required wrong type once", `{"req":3}`, ModePublish, []string{"req:type"}},
		{"cond hidden not required", `{"req":"x","kind":"a"}`, ModePublish, nil},
		{"cond visible required", `{"req":"x","kind":"b"}`, ModePublish, []string{"cond:required"}},
		{"cond empty", `{"req":"x","s":"abc"}`, ModePublish, nil},
		{"richtext", `{"rt":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]}}`, ModeDraft, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codes(s.Validate(typ, doc(t, tc.doc), tc.mode))
			if !reflect.DeepEqual(got, tc.want) && (len(got) > 0 || len(tc.want) > 0) {
				t.Fatalf("errors = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateProgrammaticNumbers(t *testing.T) {
	s := mustCompile(t, typesFixture)
	typ, _ := s.Type("all")
	for _, v := range []any{3, int64(3), float64(3), json.Number("3"), json.Number("3.0"), json.Number("3e0")} {
		if errs := s.Validate(typ, map[string]any{"i": v}, ModeDraft); len(errs) != 0 {
			t.Errorf("%T: %v", v, errs)
		}
	}
	for _, v := range []any{2.5, float32(2.5), json.Number("3.5"), "3", true} {
		if errs := s.Validate(typ, map[string]any{"i": v}, ModeDraft); len(errs) != 1 {
			t.Errorf("%T %v accepted", v, v)
		}
	}
}

func richDoc(t *testing.T, content string) map[string]any {
	return doc(t, `{"rt":{"type":"doc","content":`+content+`}}`)
}

func TestRichText(t *testing.T) {
	s := mustCompile(t, typesFixture+"  rich:\n    fields:\n      rt: { type: richtext, marks: [bold], nodes: [paragraph, bulletList] }\n")
	typ, _ := s.Type("all")
	text := func(m string) string {
		return `{"type":"text","text":"x"` + m + `}`
	}
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"all nodes", `[{"type":"heading","attrs":{"level":2},"content":[` + text(`,"marks":[{"type":"bold"},{"type":"italic"},{"type":"code"},{"type":"link","attrs":{"href":"https://a.b/c"}}]`) + `,{"type":"hardBreak"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph"}]}]},
			{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[` + text(``) + `]}]}]},
			{"type":"blockquote","content":[{"type":"paragraph"}]},
			{"type":"codeBlock","attrs":{"language":"go"},"content":[` + text(``) + `]},{"type":"horizontalRule"}]`, nil},
		{"mailto and item", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"link","attrs":{"href":"mailto:a@b.co"}}]`) + `,` + text(`,"marks":[{"type":"link","attrs":{"href":"item:`+id1+`"}}]`) + `]}]`, nil},
		{"unknown node", `[{"type":"table"}]`, []string{"rt.content[0]:richtext"}},
		{"bad child", `[{"type":"listItem"}]`, []string{"rt.content[0]:richtext"}},
		{"text at root", `[` + text(``) + `]`, []string{"rt.content[0]:richtext"}},
		{"empty text", `[{"type":"paragraph","content":[{"type":"text","text":""}]}]`, []string{"rt.content[0].content[0]:richtext"}},
		{"heading level", `[{"type":"heading","attrs":{"level":7}}]`, []string{"rt.content[0]:richtext"}},
		{"heading no attrs", `[{"type":"heading"}]`, []string{"rt.content[0]:richtext"}},
		{"heading level float", `[{"type":"heading","attrs":{"level":2.5}}]`, []string{"rt.content[0]:richtext"}},
		{"bad attr", `[{"type":"paragraph","attrs":{"x":1}}]`, []string{"rt.content[0]:richtext"}},
		{"bad language", `[{"type":"codeBlock","attrs":{"language":1}}]`, []string{"rt.content[0]:richtext"}},
		{"unknown key", `[{"type":"paragraph","bogus":1}]`, []string{"rt.content[0]:richtext"}},
		{"bad link", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"link","attrs":{"href":"javascript:x"}}]`) + `]}]`, []string{"rt.content[0].content[0].marks[0]:richtext"}},
		{"link no attrs", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"link"}]`) + `]}]`, []string{"rt.content[0].content[0].marks[0]:richtext"}},
		{"unknown mark", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"blink"}]`) + `]}]`, []string{"rt.content[0].content[0].marks[0]:richtext"}},
		{"dup mark", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"bold"},{"type":"bold"}]`) + `]}]`, []string{"rt.content[0].content[0].marks[1]:richtext"}},
		{"mark attrs", `[{"type":"paragraph","content":[` + text(`,"marks":[{"type":"bold","attrs":{}}]`) + `]}]`, []string{"rt.content[0].content[0].marks[0]:richtext"}},
		{"marks on block", `[{"type":"paragraph","marks":[{"type":"bold"}]}]`, []string{"rt.content[0]:richtext"}},
		{"leaf content", `[{"type":"horizontalRule","content":[` + text(``) + `]}]`, []string{"rt.content[0]:richtext"}},
		{"content not list", `[{"type":"paragraph","content":1}]`, []string{"rt.content[0]:richtext"}},
		{"node not object", `[1]`, []string{"rt.content[0]:richtext"}},
		{"text with content", `[{"type":"paragraph","content":[{"type":"text","text":"a","content":[]}]}]`, []string{"rt.content[0].content[0]:richtext"}},
		{"marks not list", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":1}]}]`, []string{"rt.content[0].content[0]:richtext"}},
		{"mark not object", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[1]}]}]`, []string{"rt.content[0].content[0].marks[0]:richtext"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codes(s.Validate(typ, richDoc(t, tc.content), ModeDraft))
			for i := range got {
				got[i] = strings.Replace(got[i], "richtext", "richtext", 1)
			}
			if !reflect.DeepEqual(got, tc.want) && (len(got) > 0 || len(tc.want) > 0) {
				t.Fatalf("errors = %v, want %v", got, tc.want)
			}
		})
	}

	for name, rt := range map[string]string{
		"bad root":  `{"type":"x"}`,
		"extra key": `{"type":"doc","extra":1}`,
		"bad body":  `{"type":"doc","content":3}`,
	} {
		if errs := s.Validate(typ, doc(t, `{"rt":`+rt+`}`), ModeDraft); len(errs) != 1 || errs[0].Code != "richtext" {
			t.Errorf("%s: %v", name, errs)
		}
	}

	rich, _ := s.Type("rich")
	for name, content := range map[string]string{
		"heading": `[{"type":"heading","attrs":{"level":1}}]`,
		"italic":  `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"italic"}]}]}]`,
		"codeblk": `[{"type":"codeBlock"}]`,
	} {
		if errs := s.Validate(rich, richDoc(t, content), ModeDraft); len(errs) != 1 {
			t.Errorf("allow-list %s: %v", name, errs)
		}
	}
	ok := `[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph"}]}]}]`
	if errs := s.Validate(rich, richDoc(t, ok), ModeDraft); len(errs) != 0 {
		t.Errorf("allowed list: %v", errs)
	}
	ordered := `[{"type":"orderedList","content":[{"type":"listItem"}]}]`
	if errs := s.Validate(rich, richDoc(t, ordered), ModeDraft); len(errs) != 1 {
		t.Errorf("orderedList allowed: %v", errs)
	}
}

func TestRichTextEmpty(t *testing.T) {
	s := mustCompile(t, "types:\n  a:\n    fields:\n      rt: { type: richtext, required: true }\n")
	a, _ := s.Type("a")
	for text, wantErr := range map[string]bool{
		`{"type":"doc","content":[]}`:                          true,
		`{"type":"doc"}`:                                       true,
		`{"type":"doc","content":[{"type":"paragraph"}]}`:      true,
		`{"type":"doc","content":[{"type":"horizontalRule"}]}`: false,
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`: false,
	} {
		errs := s.Validate(a, doc(t, `{"rt":`+text+`}`), ModePublish)
		if (len(errs) == 1 && errs[0].Code == "required") != wantErr {
			t.Errorf("%s: %v", text, errs)
		}
	}
}

func TestDefaultsPruneNormalize(t *testing.T) {
	s, a := article(t)
	d := map[string]any{"title": "T"}
	s.ApplyDefaults(a, d)
	if d["kind"] != "news" || d["featured"] != false || d["title"] != "T" {
		t.Fatalf("defaults = %v", d)
	}
	if _, ok := d["video"]; ok {
		t.Fatal("hidden field got a value")
	}
	d["video"] = "https://v.dev/x"
	s.Prune(a, d)
	if _, ok := d["video"]; ok {
		t.Fatalf("hidden video kept: %v", d)
	}
	d["kind"] = "video"
	d["video"] = "https://v.dev/x"
	s.Prune(a, d)
	if d["video"] == nil {
		t.Fatal("visible video pruned")
	}
	if errs := s.Validate(a, map[string]any{"title": "T", "tags": []any{"a"}, "kind": "news", "video": "x"}, ModePublish); len(errs) != 1 || errs[0].Code != "format" {
		// shape of a hidden value is still checked, but it is never required
		t.Fatalf("hidden value: %v", errs)
	}

	// defaults are deep copies
	s2 := mustCompile(t, "types:\n  a:\n    fields:\n      j: { type: json, default: { k: [1, 2] } }\n      g:\n        type: group\n        fields:\n          x: { type: string, default: hi }\n")
	a2, _ := s2.Type("a")
	d1, d2 := map[string]any{}, map[string]any{"g": map[string]any{}}
	s2.ApplyDefaults(a2, d1)
	s2.ApplyDefaults(a2, d2)
	d1["j"].(map[string]any)["k"] = nil
	d3 := map[string]any{}
	s2.ApplyDefaults(a2, d3)
	if d3["j"].(map[string]any)["k"] == nil || d2["g"].(map[string]any)["x"] != "hi" {
		t.Fatal("defaults shared or nested defaults missing")
	}

	// prune inside groups and lists
	s3 := mustCompile(t, `
types:
  a:
    fields:
      l:
        type: list
        of:
          type: group
          fields:
            k: { type: enum, values: [a, b] }
            v: { type: string, when: { field: k, eq: b } }
`)
	a3, _ := s3.Type("a")
	d4 := doc(t, `{"l":[{"k":"a","v":"x"},{"k":"b","v":"y"}]}`)
	s3.Prune(a3, d4)
	l := d4["l"].([]any)
	if _, ok := l[0].(map[string]any)["v"]; ok || l[1].(map[string]any)["v"] != "y" {
		t.Fatalf("pruned = %v", d4)
	}

	// normalize
	s4 := mustCompile(t, "types:\n  a:\n    fields:\n      at: { type: datetime }\n      l: { type: list, of: { type: datetime } }\n      g: { type: group, fields: { at: { type: datetime } } }\n")
	a4, _ := s4.Type("a")
	d5 := doc(t, `{"at":"2025-01-01T10:00:00+02:00","l":["2025-01-01T00:00:00Z","bad"],"g":{"at":"2025-06-01T12:00:00.5-04:00"}}`)
	s4.Normalize(a4, d5)
	if d5["at"] != "2025-01-01T08:00:00.000Z" || d5["l"].([]any)[0] != "2025-01-01T00:00:00.000Z" ||
		d5["l"].([]any)[1] != "bad" || d5["g"].(map[string]any)["at"] != "2025-06-01T16:00:00.500Z" {
		t.Fatalf("normalized = %v", d5)
	}
}

func TestConditionOps(t *testing.T) {
	s := mustCompile(t, typesFixture)
	typ, _ := s.Type("all")
	d := map[string]any{"kind": "b", "cne": "x", "cin": "y"}
	if errs := s.Validate(typ, d, ModeDraft); len(errs) != 0 {
		t.Fatal(errs)
	}
	s.Prune(typ, d)
	if d["cne"] == nil || d["cin"] == nil || d["cem"] != nil {
		t.Fatalf("prune: %v", d)
	}
	d = map[string]any{"kind": "a", "cne": "x", "cem": "z", "s": "abc"}
	s.Prune(typ, d)
	if _, ok := d["cne"]; ok {
		t.Fatalf("ne kept: %v", d)
	}
	if _, ok := d["cem"]; ok {
		t.Fatalf("empty kept: %v", d)
	}
	if !jsonEqual(json.Number("2"), 2.0) || jsonEqual("a", 1) || !jsonEqual([]any{"a"}, []any{"a"}) ||
		jsonEqual(map[string]any{"a": 1}, map[string]any{"a": 2}) || !jsonEqual(nil, nil) {
		t.Fatal("jsonEqual")
	}
}

func TestProjections(t *testing.T) {
	s, a := article(t)
	d := doc(t, `{
		"title":"Hello",
		"slug":"hello",
		"summary":"Sum mary",
		"kind":"news",
		"publishDate":"2025-01-02T03:04:05.250Z",
		"rating":4,
		"author":"`+id1+`",
		"tags":["go","cms",""],
		"contact":"a@b.co",
		"extra":{"hidden":"nope"},
		"address":{"street":"Main","city":"Zurich"},
		"body":{"type":"doc","content":[
			{"type":"paragraph","content":[{"type":"text","text":"Some "},{"type":"text","text":"bold","marks":[{"type":"bold"}]},{"type":"text","text":" link","marks":[{"type":"link","attrs":{"href":"item:`+id2+`"}}]}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"},{"type":"hardBreak"},{"type":"text","text":"two"}]}]}]}
		]}}`)

	refs := s.Refs(a, d)
	wantRefs := []RefValue{
		{Path: "body", To: id2, Kind: "link", OnDelete: "warn"},
		{Path: "author", To: id1, Kind: "field", OnDelete: "nullify"},
	}
	// fields come in schema order: body precedes author
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Fatalf("refs = %+v", refs)
	}

	iv := s.IndexValues(a, d)
	var got []string
	for _, v := range iv {
		switch {
		case v.Text != nil:
			got = append(got, v.Field+"=text:"+*v.Text)
		case v.Num != nil:
			got = append(got, v.Field+"=num:"+fmtNum(*v.Num))
		case v.Time != nil:
			got = append(got, fmt.Sprintf("%s=time:%d", v.Field, *v.Time))
		case v.Bool != nil:
			got = append(got, fmt.Sprintf("%s=bool:%v", v.Field, *v.Bool))
		default:
			got = append(got, v.Field+"=ref:"+v.Ref)
		}
	}
	want := []string{"publishDate=time:1735787045250", "author=ref:" + id1, "tags=text:go", "tags=text:cms"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("index = %v", got)
	}

	text := s.SearchText(a, d)
	wantText := "Hello hello Sum mary Some bold link one two news Main Zurich a@b.co go cms"
	_ = wantText
	for _, w := range []string{"Hello", "Some bold link", "one two", "Zurich", "go cms"} {
		if !strings.Contains(text, w) {
			t.Errorf("search text %q lacks %q", text, w)
		}
	}
	if strings.Contains(text, "nope") || strings.Contains(text, "2025") || strings.Contains(text, "a@b.co") ||
		strings.Contains(text, "news") {
		t.Errorf("search text has json or dates: %q", text)
	}
	if s.SearchText(a, d) != text {
		t.Fatal("nondeterministic")
	}

	if got := s.Name(a, d); got != "Hello – news" {
		t.Fatalf("name = %q", got)
	}
	if got := s.Name(a, map[string]any{"title": "Only"}); got != "Only" {
		t.Fatalf("name = %q", got)
	}
}

func TestIndexTypesAndName(t *testing.T) {
	s := mustCompile(t, `
types:
  a:
    fields:
      n:  { type: number, index: true }
      b:  { type: boolean, index: true }
      d:  { type: date, index: true }
      e:  { type: enum, values: [x], index: true }
      ns: { type: list, of: { type: number }, index: true }
  plain:
    fields:
      body: { type: richtext }
      sub: { type: text }
      num: { type: number }
  pat:
    name: "{num} items/{flag}"
    fields:
      num: { type: number }
      flag: { type: boolean }
`)
	a, _ := s.Type("a")
	iv := s.IndexValues(a, doc(t, `{"n":1.5,"b":false,"d":"1970-01-02","e":"x","ns":[1,2]}`))
	if len(iv) != 6 || *iv[0].Num != 1.5 || *iv[1].Bool || *iv[2].Time != 86400000 || *iv[3].Text != "x" || *iv[5].Num != 2 {
		t.Fatalf("iv = %+v", iv)
	}
	if got := s.IndexValues(a, doc(t, `{"n":"x","b":1,"d":"nope","e":3}`)); len(got) != 0 {
		t.Fatalf("bad values indexed: %+v", got)
	}
	p, _ := s.Type("plain")
	if got := s.Name(p, map[string]any{"sub": "  Hi  "}); got != "Hi" {
		t.Fatalf("name = %q", got)
	}
	if got := s.Name(p, map[string]any{}); got != "" {
		t.Fatalf("name = %q", got)
	}
	pat, _ := s.Type("pat")
	if got := s.Name(pat, doc(t, `{"num":3,"flag":true}`)); got != "3 items/true" {
		t.Fatalf("name = %q", got)
	}
	if got := s.Name(pat, doc(t, `{"num":2.50}`)); got != "2.50 items" {
		t.Fatalf("name = %q", got)
	}
}

func TestRemoveRef(t *testing.T) {
	s := mustCompile(t, `
types:
  person:
    fields: { n: { type: string } }
  a:
    fields:
      one: { type: ref, to: [person] }
      many: { type: list, of: { type: ref, to: [person] } }
      g: { type: group, fields: { r: { type: ref, to: [person] }, keep: { type: string } } }
`)
	a, _ := s.Type("a")
	d := doc(t, `{"one":"`+id1+`","many":["`+id1+`","`+id2+`","`+id1+`"],"g":{"r":"`+id1+`","keep":"k"}}`)
	if !s.RemoveRef(a, d, id1) {
		t.Fatal("no change reported")
	}
	if _, ok := d["one"]; ok {
		t.Fatal("one kept")
	}
	if !reflect.DeepEqual(d["many"], []any{id2}) {
		t.Fatalf("many = %v", d["many"])
	}
	if g := d["g"].(map[string]any); g["r"] != nil || g["keep"] != "k" {
		t.Fatalf("g = %v", g)
	}
	if s.RemoveRef(a, d, id1) {
		t.Fatal("second pass changed")
	}
	if s.RemoveRef(a, doc(t, `{"one":"`+id2+`"}`), id1) {
		t.Fatal("other target changed")
	}
}

func TestJSONSchema(t *testing.T) {
	s, a := article(t)
	js := s.JSONSchema(a)
	if js["$schema"] != schemaDialect || js["additionalProperties"] != false || js["type"] != "object" || js["title"] != "Article" {
		t.Fatalf("root = %v", js)
	}
	if !reflect.DeepEqual(js["required"], []string{"title"}) {
		t.Fatalf("required = %v", js["required"])
	}
	props := js["properties"].(map[string]any)
	title := props["title"].(map[string]any)
	if title["type"] != "string" || title["maxLength"] != 120 {
		t.Fatalf("title = %v", title)
	}
	if props["summary"].(map[string]any)["description"] != "Two sentences shown in listings." {
		t.Fatalf("summary = %v", props["summary"])
	}
	if !reflect.DeepEqual(props["kind"].(map[string]any)["enum"], []string{"news", "video"}) || props["kind"].(map[string]any)["default"] != "news" {
		t.Fatalf("kind = %v", props["kind"])
	}
	rating := props["rating"].(map[string]any)
	if rating["type"] != "integer" || rating["minimum"] != float64(0) || rating["maximum"] != float64(5) {
		t.Fatalf("rating = %v", rating)
	}
	if props["publishDate"].(map[string]any)["format"] != "date-time" || props["contact"].(map[string]any)["format"] != "email" {
		t.Fatal("formats")
	}
	tags := props["tags"].(map[string]any)
	if tags["type"] != "array" || tags["maxItems"] != 3 || tags["items"].(map[string]any)["maxLength"] != 30 {
		t.Fatalf("tags = %v", tags)
	}
	addr := props["address"].(map[string]any)
	if addr["additionalProperties"] != false || !reflect.DeepEqual(addr["required"], []string{"city"}) {
		t.Fatalf("address = %v", addr)
	}
	if props["author"].(map[string]any)["format"] != "uuid" || props["body"].(map[string]any)["type"] != "object" {
		t.Fatal("author/body")
	}
	if _, err := json.Marshal(js); err != nil {
		t.Fatal(err)
	}

	all := mustCompile(t, typesFixture)
	at, _ := all.Type("all")
	aj := all.JSONSchema(at)["properties"].(map[string]any)
	for _, k := range []string{"s", "t", "sl", "u", "d", "j", "b", "lg"} {
		if aj[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
	if aj["s"].(map[string]any)["pattern"] != "^[a-z]+$" || aj["sl"].(map[string]any)["maxLength"] != 10 {
		t.Fatal("string schema")
	}
}

func TestJSONSchemaFile(t *testing.T) {
	js := JSONSchemaFile(NewRegistry())
	if _, err := json.Marshal(js); err != nil {
		t.Fatal(err)
	}
	defs := js["$defs"].(map[string]any)
	field := defs["field"].(map[string]any)["properties"].(map[string]any)
	for _, k := range []string{"type", "when", "values", "on_delete", "of", "fields", "use"} {
		if field[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
	types := field["type"].(map[string]any)["enum"].([]string)
	if len(types) != 15 {
		t.Fatalf("types = %v", types)
	}
	reg := NewRegistry()
	if got := len(reg.names()); got != len(types) {
		t.Fatalf("registry has %d types, file schema %d", got, len(types))
	}
}

func TestSlugify(t *testing.T) {
	long := strings.Repeat("ab ", 100)
	tests := map[string]string{
		"Hello, World!":      "hello-world",
		"  Crème brûlée  ":   "creme-brulee",
		"Straße Ørsted Æble": "strasse-orsted-aeble",
		"Łódź Škoda Çağrı":   "lodz-skoda-cagri",
		"a---b___c":          "a-b-c",
		"日本語":                "日本語",
		"Привет мир":         "привет-мир",
		"ПРИВЕТ, Мир!":       "привет-мир",
		"Ünï ☃ 日本 語":         "uni-日本-語",
		"🙂":                  "",
		"":                   "",
		"x1 2y":              "x1-2y",
		"e\u0301cole":        "ecole",
		"--lead and trail--": "lead-and-trail",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	got := Slugify(long)
	if len(got) > 120 || strings.HasSuffix(got, "-") || !isSlug(got) {
		t.Fatalf("long slug %q (%d)", got, len(got))
	}
	cyr := Slugify(strings.Repeat("щ ", 200))
	if n := utf8.RuneCountInString(cyr); n > 120 || strings.HasSuffix(cyr, "-") || !isSlug(cyr) {
		t.Fatalf("long cyrillic slug (%d runes)", n)
	}
}

func TestDecodeDoc(t *testing.T) {
	d, err := DecodeDoc([]byte(` {"a": 12345678901234567890, "b": [1.50]} `))
	if err != nil {
		t.Fatal(err)
	}
	if d["a"] != json.Number("12345678901234567890") || d["b"].([]any)[0] != json.Number("1.50") {
		t.Fatalf("d = %v", d)
	}
	for _, bad := range []string{``, `[1]`, `"x"`, `{"a":1} {}`, `{"a":1}x`, `{"a":`, `null`} {
		if _, err := DecodeDoc([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestMarshalDeterministicAndPositionFree(t *testing.T) {
	s1 := mustCompile(t, fixture)
	s2, err := compile(t, map[string]string{"schema/other/name.yaml": "\n\n\n" + fixture})
	if err != nil {
		t.Fatal(err)
	}
	if s1.Hash() != s2.Hash() {
		t.Fatal("positions leak into the hash")
	}
	b1, _ := s1.MarshalJSON()
	if !json.Valid(b1) || strings.Contains(string(b1), "schema/s.yaml") {
		t.Fatal("bad canonical json")
	}
}

func BenchmarkValidate(b *testing.B) {
	s, err := compile(&testing.T{}, map[string]string{"schema/s.yaml": benchSchema})
	if err != nil {
		b.Fatal(err)
	}
	typ, _ := s.Type("article")
	d, err := DecodeDoc([]byte(benchDoc))
	if err != nil {
		b.Fatal(err)
	}
	if errs := s.Validate(typ, d, ModePublish); len(errs) != 0 {
		b.Fatal(errs)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.Validate(typ, d, ModePublish)
	}
}

const benchSchema = `
types:
  person:
    fields: { n: { type: string } }
  article:
    fields:
      title: { type: string, required: true, max: 120 }
      slug: { type: slug }
      summary: { type: text, max: 300 }
      body: { type: richtext }
      kind: { type: enum, values: [news, video] }
      video: { type: url, when: { field: kind, eq: video } }
      publishDate: { type: datetime }
      day: { type: date }
      rating: { type: number, integer: true, min: 0, max: 5 }
      featured: { type: boolean }
      author: { type: ref, to: [person] }
      tags: { type: list, of: { type: string, max: 30 }, max: 10 }
      related: { type: list, of: { type: ref, to: [person] } }
      address: { type: group, fields: { street: { type: string }, city: { type: string } } }
      contact: { type: email }
      site: { type: url }
      extra: { type: json }
      subtitle: { type: string }
      code: { type: string, pattern: "^[A-Z]{2}-[0-9]+$" }
      views: { type: number }
`

const benchDoc = `{
 "title":"A reasonably long article title","slug":"a-reasonably-long-article-title",
 "summary":"Two sentences about this article. They appear in listings.",
 "body":{"type":"doc","content":[
  {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Heading"}]},
  {"type":"paragraph","content":[{"type":"text","text":"Some "},{"type":"text","text":"bold","marks":[{"type":"bold"}]},{"type":"text","text":" and a link","marks":[{"type":"link","attrs":{"href":"https://example.com/x"}}]}]},
  {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
  {"type":"paragraph","content":[{"type":"text","text":"Closing paragraph."}]}]},
 "kind":"news","publishDate":"2025-01-02T03:04:05.000Z","day":"2025-01-02","rating":4,"featured":true,
 "author":"0190a000-0000-7000-8000-000000000001",
 "tags":["go","cms","headless","content","schema"],
 "related":["0190a000-0000-7000-8000-000000000001","0190a000-0000-7000-8000-000000000002"],
 "address":{"street":"Main street 1","city":"Zurich"},"contact":"editor@example.com","site":"https://example.com",
 "extra":{"a":[1,2,3]},"subtitle":"Sub","code":"AB-123","views":1234
}`
