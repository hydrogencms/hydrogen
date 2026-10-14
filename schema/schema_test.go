package schema

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

const fixture = `
types:
  person:
    fields:
      name2: { type: string }
  article:
    label: Article
    description: News and stories.
    name: "{title} – {kind}"
    admin: { list: [title, publishDate], group: Editorial, icon: newspaper }
    ai: { instructions: "Neutral tone.", generate: [title, summary] }
    fields:
      title:       { type: string, translatable: true, required: true, max: 120 }
      slug:        { type: slug, from: title, translatable: true }
      summary:     { type: text, max: 300, description: "Two sentences shown in listings." }
      body:        { type: richtext, translatable: true }
      kind:        { type: enum, values: [news, video], default: news }
      video:       { type: url, when: { field: kind, eq: video }, required: true }
      publishDate: { type: datetime, index: true }
      rating:      { type: number, integer: true, min: 0, max: 5 }
      featured:    { type: boolean, default: false }
      author:      { type: ref, to: [person], on_delete: nullify, index: true }
      tags:        { type: list, of: { type: string, max: 30 }, max: 3, min: 1, index: true }
      seo:         { type: group, use: seo }
      address:     { type: group, fields: { street: { type: string }, city: { type: string, required: true } } }
      contact:     { type: email, ai: false }
      extra:       { type: json }
groups:
  seo:
    fields:
      metaTitle: { type: string, max: 60 }
`

func files(m map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for k, v := range m {
		fsys[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return fsys
}

func compile(t *testing.T, m map[string]string) (*Schema, error) {
	t.Helper()
	b := NewBuilder(NewRegistry())
	if err := b.LoadFS(files(m), "schema"); err != nil {
		return nil, err
	}
	return b.Compile()
}

func mustCompile(t *testing.T, yamlText string) *Schema {
	t.Helper()
	s, err := compile(t, map[string]string{"schema/s.yaml": yamlText})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func article(t *testing.T) (*Schema, *Type) {
	t.Helper()
	s := mustCompile(t, fixture)
	a, ok := s.Type("article")
	if !ok {
		t.Fatal("no article")
	}
	return s, a
}

func doc(t *testing.T, text string) map[string]any {
	t.Helper()
	d, err := DecodeDoc([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCompileFixture(t *testing.T) {
	s, a := article(t)
	if len(s.Types()) != 2 || s.Types()[0].Name != "article" {
		t.Fatalf("types not sorted: %v", s.Types())
	}
	if len(s.Hash()) != 64 {
		t.Fatalf("hash %q", s.Hash())
	}
	f, _ := a.Field("publishDate")
	if f.Label != "Publish date" || f.Shape.Format != "datetime" || !f.Index || f.Type != "datetime" {
		t.Fatalf("publishDate = %+v", f)
	}
	seo, _ := a.Field("seo")
	if seo.Group != "seo" || len(seo.Fields) != 1 || seo.Shape.Base != BaseGroup {
		t.Fatalf("seo = %+v", seo)
	}
	tags, _ := a.Field("tags")
	if tags.Of == nil || tags.Of.Shape.Base != BaseString {
		t.Fatalf("tags = %+v", tags)
	}
	if f, _ := a.Field("slug"); f.Pos.Line == 0 || f.Pos.File != "schema/s.yaml" {
		t.Fatalf("slug pos = %v", f.Pos)
	}
	if len(s.Groups()) != 1 || s.Groups()[0].Name != "seo" {
		t.Fatalf("groups = %v", s.Groups())
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatal(err)
	}
}

func TestHashIgnoresLoadOrder(t *testing.T) {
	person := "types:\n  person:\n    fields:\n      name2: { type: string }\n"
	seo := "groups:\n  seo:\n    fields:\n      metaTitle: { type: string, max: 60 }\n"
	art := `
types:
  article:
    fields:
      title: { type: string }
      seo: { type: group, use: seo }
      author: { type: ref, to: [person] }
`
	orders := []map[string]string{
		{"schema/a.yaml": person, "schema/b.yaml": seo, "schema/c.yaml": art},
		{"schema/c.yaml": person, "schema/b.yaml": seo, "schema/a.yaml": art},
		{"schema/x/z.yaml": person, "schema/a.yml": seo, "schema/m.yaml": art},
	}
	var hashes []string
	for _, o := range orders {
		s, err := compile(t, o)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, s.Hash())
	}
	if hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Fatalf("hashes differ: %v", hashes)
	}
	s2, err := compile(t, map[string]string{"schema/a.yaml": person, "schema/b.yaml": seo,
		"schema/c.yaml": strings.Replace(art, "title: { type: string }", "title: { type: string, max: 5 }", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Hash() == hashes[0] {
		t.Fatal("hash ignores options")
	}
}

func TestCompileErrors(t *testing.T) {
	person := "  person:\n    fields: { n: { type: string } }\n"
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{"max not number", "types:\n  a:\n    fields:\n      rating: { type: number, max: x }\n",
			[]string{`schema/s.yaml:4: field "rating": max must be a number`}},
		{"unknown option", "types:\n  a:\n    fields:\n      t: { type: string, bogus: 1 }\n",
			[]string{`schema/s.yaml:4: field "t": unknown option "bogus" for type "string" (options: max, min, pattern)`}},
		{"unknown type", "types:\n  a:\n    fields:\n      t: { type: nope }\n", []string{`unknown field type "nope"`}},
		{"missing type", "types:\n  a:\n    fields:\n      t: { label: x }\n", []string{"type is required"}},
		{"unknown top key", "foo: 1\n", []string{`schema/s.yaml:1: unknown key "foo"`}},
		{"unknown type key", "types:\n  a:\n    bogus: 1\n", []string{`unknown key "bogus" in type "a"`}},
		{"bad type name", "types:\n  Bad:\n    fields: {}\n", []string{`type name "Bad" must match`}},
		{"reserved", "types:\n  a:\n    fields:\n      id: { type: string }\n", []string{`field name "id" is reserved`}},
		{"bad field name", "types:\n  a:\n    fields:\n      Bad: { type: string }\n", []string{`field name "Bad" must match`}},
		{"duplicate key", "types:\n  a:\n    fields:\n      t: { type: string }\n      t: { type: string }\n", []string{`duplicate key "t"`}},
		{"translatable in group", "types:\n  a:\n    fields:\n      g: { type: group, fields: { x: { type: string, translatable: true } } }\n",
			[]string{"translatable is only allowed on top-level fields"}},
		{"index on richtext", "types:\n  a:\n    fields:\n      b: { type: richtext, index: true }\n", []string{"index is only allowed"}},
		{"index in group", "types:\n  a:\n    fields:\n      g: { type: group, fields: { x: { type: string, index: true } } }\n", []string{"index is only allowed"}},
		{"index list of list", "types:\n  a:\n    fields:\n      g: { type: list, of: { type: list, of: { type: string } }, index: true }\n", []string{"index is only allowed"}},
		{"ref unknown type", "types:\n  a:\n    fields:\n      r: { type: ref, to: [ghost] }\n", []string{`to names unknown type "ghost"`}},
		{"ref no to", "types:\n  a:\n    fields:\n      r: { type: ref }\n", []string{"to is required"}},
		{"ref bad on_delete", "types:\n  a:\n    fields:\n      r: { type: ref, to: [a], on_delete: x }\n", []string{"on_delete must be"}},
		{"slug from missing", "types:\n  a:\n    fields:\n      s: { type: slug, from: nope }\n", []string{`from must name a sibling string or text field`}},
		{"slug from number", "types:\n  a:\n    fields:\n      n: { type: number }\n      s: { type: slug, from: n }\n", []string{"from must name"}},
		{"enum empty", "types:\n  a:\n    fields:\n      e: { type: enum, values: [] }\n", []string{"values must not be empty"}},
		{"enum dup", "types:\n  a:\n    fields:\n      e: { type: enum, values: [x, x] }\n", []string{"values must be unique"}},
		{"enum missing", "types:\n  a:\n    fields:\n      e: { type: enum }\n", []string{"values is required"}},
		{"bad default", "types:\n  a:\n    fields:\n      e: { type: enum, values: [x], default: y }\n", []string{"default is invalid"}},
		{"null default", "types:\n  a:\n    fields:\n      e: { type: string, default: null }\n", []string{"default must not be null"}},
		{"when later sibling", "types:\n  a:\n    fields:\n      x: { type: string, when: { field: y, eq: a } }\n      y: { type: string }\n", []string{"must be a sibling declared before"}},
		{"when no op", "types:\n  a:\n    fields:\n      y: { type: string }\n      x: { type: string, when: { field: y } }\n", []string{"needs exactly one of"}},
		{"when in scalar", "types:\n  a:\n    fields:\n      y: { type: string }\n      x: { type: string, when: { field: y, in: a } }\n", []string{"in must be a list"}},
		{"list no of", "types:\n  a:\n    fields:\n      l: { type: list }\n", []string{"of is required"}},
		{"list item required", "types:\n  a:\n    fields:\n      l: { type: list, of: { type: string, required: true } }\n", []string{"required is not allowed on list items"}},
		{"group neither", "types:\n  a:\n    fields:\n      g: { type: group }\n", []string{"a group needs fields or use"}},
		{"group both", "types:\n  a:\n    fields:\n      g: { type: group, use: x, fields: {} }\ngroups:\n  x: { fields: {} }\n", []string{"mutually exclusive"}},
		{"group unknown", "types:\n  a:\n    fields:\n      g: { type: group, use: nope }\n", []string{`unknown group "nope"`}},
		{"group cycle", "groups:\n  x:\n    fields:\n      y: { type: group, use: x }\n", []string{`group "x" is invalid or uses itself`}},
		{"admin list unknown", "types:\n  a:\n    admin: { list: [nope] }\n    fields:\n      t: { type: string }\n", []string{`admin.list names "nope"`}},
		{"ai generate unknown", "types:\n  a:\n    ai: { generate: [nope] }\n    fields:\n      t: { type: string }\n", []string{`ai.generate names "nope"`}},
		{"name pattern unknown", "types:\n  a:\n    name: \"{nope}\"\n    fields:\n      t: { type: string }\n", []string{`name pattern uses "nope"`}},
		{"name pattern unclosed", "types:\n  a:\n    name: \"{t\"\n    fields:\n      t: { type: string }\n", []string{"unclosed"}},
		{"bad pattern", "types:\n  a:\n    fields:\n      t: { type: string, pattern: \"(\" }\n", []string{"not a valid regular expression"}},
		{"min over max", "types:\n  a:\n    fields:\n      t: { type: string, min: 5, max: 2 }\n", []string{"min must not exceed max"}},
		{"bad date min", "types:\n  a:\n    fields:\n      d: { type: date, min: soon }\n", []string{"min must be a date"}},
		{"richtext bad mark", "types:\n  a:\n    fields:\n      d: { type: richtext, marks: [blink] }\n", []string{`names unknown "blink"`}},
		{"ai bad key", "types:\n  a:\n    fields:\n      t: { type: string, ai: { x: 1 } }\n", []string{`unknown key "x" in ai`}},
		{"admin bad key", "types:\n  a:\n    fields:\n      t: { type: string, admin: { x: 1 } }\n", []string{`unknown key "x" in admin`}},
		{"syntax", "types: [\n", []string{"schema/s.yaml"}},
		{"field not mapping", "types:\n  a:\n    fields:\n      t: string\n", []string{"expected a mapping"}},
		{"duplicate group", "groups:\n  g: { fields: {} }\n  h: { fields: {} }\n", nil},
	}
	_ = person
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, map[string]string{"schema/s.yaml": tc.yaml})
			if tc.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestDuplicateTypeAcrossFiles(t *testing.T) {
	_, err := compile(t, map[string]string{
		"schema/a.yaml": "types:\n  x:\n    fields: {}\n",
		"schema/b.yaml": "types:\n  y: {}\n  x:\n    fields: {}\n",
	})
	want := `schema/b.yaml:3: type "x" is already defined at schema/a.yaml:2`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	_, err = compile(t, map[string]string{
		"schema/a.yaml": "groups:\n  g: {}\n", "schema/b.yaml": "groups:\n  g: {}\n",
	})
	if err == nil || !strings.Contains(err.Error(), `group "g" is already defined at schema/a.yaml:2`) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsSortedAndJoined(t *testing.T) {
	_, err := compile(t, map[string]string{
		"schema/b.yaml": "types:\n  b:\n    fields:\n      x: { type: nope }\n",
		"schema/a.yaml": "types:\n  a:\n    fields:\n      y: { type: nope }\n      z: { type: nope }\n",
	})
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "schema/a.yaml:4") || !strings.HasPrefix(lines[2], "schema/b.yaml:4") {
		t.Fatalf("errors:\n%v", err)
	}
}

func TestLoadDir(t *testing.T) {
	b := NewBuilder(NewRegistry())
	if err := b.LoadDir(t.TempDir() + "/missing"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil || len(s.Types()) != 0 {
		t.Fatalf("empty schema: %v %v", s, err)
	}
	dir := t.TempDir()
	writeFile(t, dir+"/sub/a.yml", "types:\n  a:\n    fields: {}\n")
	writeFile(t, dir+"/notes.txt", "ignored")
	writeFile(t, dir+"/empty.yaml", "")
	b = NewBuilder(NewRegistry())
	if err := b.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Type("a"); !ok {
		t.Fatal("type a not loaded")
	}
	writeFile(t, dir+"/bad.yaml", "types: [\n")
	b = NewBuilder(NewRegistry())
	err = b.LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Fatalf("err = %v", err)
	}
	if err := NewBuilder(NewRegistry()).LoadDir(dir + "/notes.txt"); err == nil {
		t.Fatal("file as dir accepted")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stringType{name: "string"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Register(stringType{}); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := r.Register(stringType{name: "shortcode"}); err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(r)
	if err := b.AddType("a", Spec{"fields": Spec{"c": Spec{"type": "shortcode", "max": 3}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Compile(); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderProgrammatic(t *testing.T) {
	b := NewBuilder(NewRegistry())
	if err := b.LoadFS(files(map[string]string{"schema/a.yaml": "types:\n  page:\n    page: true\n    fields:\n      title: { type: string }\n"}), "schema"); err != nil {
		t.Fatal(err)
	}
	b.SetOrigin("module seo")
	if err := b.AddGroup("seo", Spec{"fields": map[string]Spec{"metaTitle": {"type": "string", "max": 60}}}); err != nil {
		t.Fatal(err)
	}
	for _, tb := range b.Types() {
		if tb.Page() && !tb.HasField("seo") {
			if err := tb.AddField("seo", Spec{"type": "group", "use": "seo"}); err != nil {
				t.Fatal(err)
			}
			if err := tb.AddField("seo", Spec{"type": "group", "use": "seo"}); err == nil {
				t.Fatal("duplicate field accepted")
			}
		}
	}
	if err := b.AddType("page", Spec{}); err == nil {
		t.Fatal("duplicate type accepted")
	}
	if err := b.AddType("bad name", Spec{}); err == nil {
		t.Fatal("bad name accepted")
	}
	if err := b.AddType("withUnknown", Spec{"nope": 1}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, ok := b.Type("withUnknown"); ok {
		t.Fatal("failed type was kept")
	}
	if err := b.AddType("single", Spec{"singleton": true, "fields": Spec{"n": Spec{"type": "number", "default": json.Number("3"), "min": 1.5}}}); err != nil {
		t.Fatal(err)
	}
	if tb, _ := b.Type("single"); !tb.Singleton() || tb.Name() != "single" {
		t.Fatal("single")
	}
	tb, _ := b.Type("page")
	if err := tb.AddField("Bad", Spec{"type": "string"}); err == nil {
		t.Fatal("bad field name accepted")
	}
	if err := tb.AddField("x", Spec{"type": make(chan int)}); err == nil {
		t.Fatal("unsupported value accepted")
	}
	if err := tb.AddField("broken", Spec{"type": "number", "max": "x"}); err != nil {
		t.Fatal(err)
	}
	_, err := b.Compile()
	if err == nil || !strings.Contains(err.Error(), `module seo: field "broken": max must be a number`) {
		t.Fatalf("err = %v", err)
	}
	if len(b.Types()) != 2 {
		t.Fatalf("types = %d", len(b.Types()))
	}
}

func TestFieldOrderAndProgrammaticAppend(t *testing.T) {
	b := NewBuilder(NewRegistry())
	_ = b.AddType("a", Spec{"fields": Spec{"z": Spec{"type": "string"}, "y": Spec{"type": "boolean", "default": true}}})
	tb, _ := b.Type("a")
	_ = tb.AddField("first", Spec{"type": "string"})
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Type("a")
	var names []string
	for _, f := range a.Fields {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "y,z,first" {
		t.Fatalf("order = %v", names)
	}
}

func TestSplitNameAndPos(t *testing.T) {
	if got := SplitName("blogPost"); got != "Blog post" {
		t.Fatal(got)
	}
	if (Pos{File: "x"}).String() != "x" || (Pos{File: "x", Line: 2}).String() != "x:2" {
		t.Fatal("pos")
	}
	e := &Error{Message: "m"}
	if e.Error() != "m" {
		t.Fatal(e.Error())
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := mkWrite(path, data); err != nil {
		t.Fatal(err)
	}
}
