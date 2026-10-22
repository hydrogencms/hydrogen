package schema

import (
	"reflect"
	"strings"
	"testing"
)

const richDecl = `
richtext:
  nodes:
    callout:
      label: Callout
      tag: aside
      content: blocks
      attrs:
        variant: {type: enum, values: [info, warning, tip], default: info}
    details:
      tag: details
      content: [detailsSummary, detailsBody]
    detailsSummary: {tag: summary, content: inline}
    detailsBody: {content: blocks}
    embed:
      tag: figure
      content: none
      attrs:
        url: {type: url, required: true}
        caption: {type: string, max: 10}
        size: {type: int, min: 1, max: 5, default: 2}
        wide: {type: bool}
    footnoteRef:
      inline: true
      content: none
      attrs:
        n: {type: int, min: 1, max: 999, required: true}
    note: {content: inline}
  marks:
    highlight:
      tag: mark
      attrs:
        tone: {type: enum, values: [yellow, green]}
    plain: {}
types:
  page:
    fields:
      body: {type: richtext}
      small: {type: richtext, nodes: [paragraph, callout], marks: [bold, highlight]}
      none: {type: richtext, nodes: [paragraph], marks: [bold]}
`

func TestRichDeclCompile(t *testing.T) {
	s := mustCompile(t, richDecl)
	rt := s.RichText()
	if len(rt.Nodes) != 7 || len(rt.Marks) != 2 {
		t.Fatalf("nodes %d marks %d", len(rt.Nodes), len(rt.Marks))
	}
	for i := 1; i < len(rt.Nodes); i++ {
		if rt.Nodes[i-1].Name >= rt.Nodes[i].Name {
			t.Fatalf("not sorted: %s %s", rt.Nodes[i-1].Name, rt.Nodes[i].Name)
		}
	}
	c, _ := rt.Node("callout")
	if c.Tag != "aside" || c.Label != "Callout" || c.Content != "blocks" || c.Part || c.Inline {
		t.Fatalf("callout %+v", c)
	}
	if a := c.Attrs[0]; a.Name != "variant" || a.Type != "enum" || a.Default != "info" || !reflect.DeepEqual(a.Values, []string{"info", "warning", "tip"}) {
		t.Fatalf("variant %+v", a)
	}
	d, _ := rt.Node("details")
	if d.Content != "parts" || !reflect.DeepEqual(d.Parts, []string{"detailsSummary", "detailsBody"}) {
		t.Fatalf("details %+v", d)
	}
	if p, _ := rt.Node("detailsBody"); !p.Part || p.Tag != "div" {
		t.Fatalf("part %+v", p)
	}
	f, _ := rt.Node("footnoteRef")
	if !f.Inline || f.Tag != "span" || f.Label != "Footnote ref" {
		t.Fatalf("footnoteRef %+v", f)
	}
	e, _ := rt.Node("embed")
	if e.Attrs[0].Type != "url" || !e.Attrs[0].Required || e.Attrs[1].MaxLen != 10 || *e.Attrs[2].Min != 1 || *e.Attrs[2].Max != 5 || e.Attrs[2].Default != int64(2) {
		t.Fatalf("embed %+v %+v", e.Attrs[0], e.Attrs[2])
	}
	if h, _ := rt.Mark("highlight"); h.Tag != "mark" {
		t.Fatalf("highlight %+v", h)
	}
	if m, _ := rt.Mark("plain"); m.Tag != "span" {
		t.Fatalf("plain %+v", m)
	}
	if _, ok := rt.Node("nope"); ok {
		t.Fatal("found nope")
	}
	if got := mustCompile(t, "types:\n  a:\n    fields:\n      t: {type: string}\n").RichText(); got == nil || len(got.Nodes) != 0 {
		t.Fatalf("undeclared = %+v", got)
	}
	// the declaration is part of the schema hash
	if s.Hash() == mustCompile(t, strings.Replace(richDecl, "tag: aside", "tag: section", 1)).Hash() {
		t.Fatal("hash ignores the declaration")
	}
}

func TestRichDeclErrors(t *testing.T) {
	const types = "\ntypes:\n  a:\n    fields:\n      b: {type: richtext}\n"
	tests := []struct{ name, yaml, want string }{
		{"node name", "nodes:\n    Bad: {content: none}", `node name "Bad" must match`},
		{"builtin node", "nodes:\n    paragraph: {content: none}", `"paragraph" is a built-in node`},
		{"builtin doc", "nodes:\n    doc: {content: none}", `"doc" is a built-in node`},
		{"builtin mark", "marks:\n    bold: {}", `"bold" is a built-in mark`},
		{"bad key", "nodes:\n    x: {content: none, style: a}", `unknown key "style"`},
		{"top key", "foo: {}", `unknown key "foo" in richtext`},
		{"no content", "nodes:\n    x: {tag: div}", "content is required"},
		{"bad content", "nodes:\n    x: {content: stuff}", `content "stuff" must be`},
		{"empty parts", "nodes:\n    x: {content: []}", "at least one part"},
		{"block tag", "nodes:\n    x: {content: none, tag: p}", `tag "p" must be one of`},
		{"inline tag on block", "nodes:\n    x: {content: none, tag: span}", `tag "span" must be one of`},
		{"block tag on inline", "nodes:\n    x: {content: none, inline: true, tag: div}", `tag "div" must be one of`},
		{"mark tag", "marks:\n    m: {tag: div}", `tag "div" must be one of`},
		{"inline content", "nodes:\n    x: {content: blocks, inline: true}", "inline node needs content: none"},
		{"unknown part", "nodes:\n    x: {content: [y]}", `part "y" is not a declared node`},
		{"part twice", "nodes:\n    x: {content: [y, y]}\n    y: {content: none}", `lists part "y" twice`},
		{"part two parents", "nodes:\n    x: {content: [y]}\n    z: {content: [y]}\n    y: {content: none}", `"y" is already a part of "x"`},
		{"inline part", "nodes:\n    x: {content: [y]}\n    y: {content: none, inline: true}", "is inline and cannot be a part"},
		{"self part", "nodes:\n    x: {content: [x]}", "its own part"},
		{"part cycle", "nodes:\n    x: {content: [y]}\n    y: {content: [x]}", "contains itself"},
		{"attr no type", "nodes:\n    x:\n      content: none\n      attrs: {a: {}}", "type is required"},
		{"attr type", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: float}}", `unknown type "float"`},
		{"attr name", "nodes:\n    x:\n      content: none\n      attrs: {A: {type: bool}}", `attribute name "A"`},
		{"enum no values", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: enum}}", "needs 1 to 100 values"},
		{"enum bad value", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: enum, values: [Big]}}", `value "Big" must match`},
		{"enum dup", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: enum, values: [a, a]}}", "listed twice"},
		{"values on string", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: string, values: [a]}}", "values only apply"},
		{"min on bool", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: bool, min: 1}}", "min and max only apply"},
		{"min on string", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: string, min: 1}}", "min does not apply"},
		{"string max", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: string, max: 10001}}", "max must be from 1 to 10000"},
		{"int range", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: int, min: 5, max: 1}}", "min must not exceed max"},
		{"int limit", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: int, max: 2000000000}}", "within"},
		{"default enum", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: enum, values: [a], default: b}}", "default must be one of"},
		{"default int", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: int, max: 3, default: 4}}", "default must be from"},
		{"default type", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: bool, default: yes please}}", "default must be a bool"},
		{"default url", "nodes:\n    x:\n      content: none\n      attrs: {a: {type: url, default: javascript:x}}", "default must be an http(s) URL"},
		{"allowlist part", "nodes:\n    x: {content: [y]}\n    y: {content: none}", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, map[string]string{"schema/s.yaml": "richtext:\n  " + tc.yaml + types})
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("error %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error %v, want %q", err, tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), "schema/s.yaml:"):
				t.Fatalf("error has no position: %v", err)
			}
		})
	}

	t.Run("counts", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("richtext:\n  nodes:\n")
		for i := range 51 {
			b.WriteString("    n" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ": {content: none}\n")
		}
		_, err := compile(t, map[string]string{"schema/s.yaml": b.String() + types})
		if err == nil || !strings.Contains(err.Error(), "the most is 50") {
			t.Fatalf("err %v", err)
		}
		b.Reset()
		b.WriteString("richtext:\n  marks:\n")
		for i := range 21 {
			b.WriteString("    m" + string(rune('a'+i)) + ": {}\n")
		}
		_, err = compile(t, map[string]string{"schema/s.yaml": b.String() + types})
		if err == nil || !strings.Contains(err.Error(), "the most is 20") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("attr count", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("richtext:\n  marks:\n    m:\n      attrs:\n")
		for i := range 21 {
			b.WriteString("        a" + string(rune('a'+i)) + ": {type: bool}\n")
		}
		_, err := compile(t, map[string]string{"schema/s.yaml": b.String() + types})
		if err == nil || !strings.Contains(err.Error(), "the most is 20") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("duplicate across files", func(t *testing.T) {
		_, err := compile(t, map[string]string{
			"schema/a.yaml": "richtext:\n  marks:\n    m: {}\n",
			"schema/b.yaml": "richtext:\n  marks:\n    m: {}\n",
		})
		if err == nil || !strings.Contains(err.Error(), "already defined at schema/a.yaml:3") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("two files merge", func(t *testing.T) {
		s, err := compile(t, map[string]string{
			"schema/a.yaml": "richtext:\n  marks:\n    m: {}\n",
			"schema/b.yaml": "richtext:\n  nodes:\n    n: {content: none}\n",
		})
		if err != nil || len(s.RichText().Nodes) != 1 || len(s.RichText().Marks) != 1 {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("field allowlists", func(t *testing.T) {
		const decl = "richtext:\n  nodes:\n    x: {content: [y]}\n    y: {content: none}\n    z: {content: none}\n  marks:\n    m: {}\ntypes:\n  a:\n    fields:\n"
		for name, c := range map[string]struct{ field, want string }{
			"custom ok":    {"b: {type: richtext, nodes: [paragraph, x, z], marks: [bold, m]}", ""},
			"part":         {"b: {type: richtext, nodes: [y]}", "a part node"},
			"unknown node": {"b: {type: richtext, nodes: [w]}", `nodes names unknown "w"`},
			"unknown mark": {"b: {type: richtext, marks: [w]}", `marks names unknown "w"`},
			"custom named": {"b: {type: richtext, nodes: [x, z, quote]}", `nodes names unknown "quote"`},
		} {
			_, err := compile(t, map[string]string{"schema/s.yaml": decl + "      " + c.field + "\n"})
			if (c.want == "") != (err == nil) || (c.want != "" && !strings.Contains(err.Error(), c.want)) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

func TestRichCustomValidation(t *testing.T) {
	s := mustCompile(t, richDecl)
	typ, _ := s.Type("page")
	p := func(text string) string {
		return `{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}`
	}
	tests := []struct {
		name    string
		field   string
		content string
		want    []string
	}{
		{"callout", "body", `[{"type":"callout","attrs":{"variant":"tip"},"content":[` + p("a") + `]}]`, nil},
		{"callout default attrs", "body", `[{"type":"callout","content":[` + p("a") + `]}]`, nil},
		{"callout nested", "body", `[{"type":"callout","content":[{"type":"callout","content":[` + p("a") + `]}]}]`, nil},
		{"callout in quote", "body", `[{"type":"blockquote","content":[{"type":"callout","content":[` + p("a") + `]}]}]`, nil},
		{"callout in list", "body", `[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"callout","content":[` + p("a") + `]}]}]}]`, nil},
		{"callout empty", "body", `[{"type":"callout"}]`, []string{"body.content[0]:richtext"}},
		{"callout empty list", "body", `[{"type":"callout","content":[]}]`, []string{"body.content[0]:richtext"}},
		{"callout bad enum", "body", `[{"type":"callout","attrs":{"variant":"x"},"content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"callout bad attr", "body", `[{"type":"callout","attrs":{"nope":1},"content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"callout attrs not object", "body", `[{"type":"callout","attrs":1,"content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"callout text child", "body", `[{"type":"callout","content":[{"type":"text","text":"a"}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"callout inline child", "body", `[{"type":"callout","content":[{"type":"footnoteRef","attrs":{"n":1}}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"callout null attr", "body", `[{"type":"callout","attrs":{"variant":null},"content":[` + p("a") + `]}]`, nil},
		{"callout marks", "body", `[{"type":"callout","marks":[{"type":"bold"}],"content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"details", "body", `[{"type":"details","content":[{"type":"detailsSummary","content":[{"type":"text","text":"s"}]},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, nil},
		{"details empty summary", "body", `[{"type":"details","content":[{"type":"detailsSummary"},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, nil},
		{"details missing part", "body", `[{"type":"details","content":[{"type":"detailsSummary"}]}]`, []string{"body.content[0]:richtext"}},
		{"details no content", "body", `[{"type":"details"}]`, []string{"body.content[0]:richtext"}},
		{"details wrong order", "body", `[{"type":"details","content":[{"type":"detailsBody","content":[` + p("a") + `]},{"type":"detailsSummary"}]}]`, []string{"body.content[0].content[0]:richtext", "body.content[0].content[1]:richtext"}},
		{"details extra", "body", `[{"type":"details","content":[{"type":"detailsSummary"},{"type":"detailsBody","content":[` + p("a") + `]},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, []string{"body.content[0]:richtext", "body.content[0].content[2]:richtext"}},
		{"details non-part", "body", `[{"type":"details","content":[{"type":"paragraph"},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"part at root", "body", `[{"type":"detailsBody","content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"part in callout", "body", `[{"type":"callout","content":[{"type":"detailsSummary"}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"summary block child", "body", `[{"type":"details","content":[{"type":"detailsSummary","content":[` + p("a") + `]},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, []string{"body.content[0].content[0].content[0]:richtext"}},
		{"summary inline atom", "body", `[{"type":"details","content":[{"type":"detailsSummary","content":[{"type":"footnoteRef","attrs":{"n":2}},{"type":"hardBreak"}]},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, nil},
		{"embed", "body", `[{"type":"embed","attrs":{"url":"https://a.b/c","caption":"abc","size":3,"wide":true}}]`, nil},
		{"embed relative", "body", `[{"type":"embed","attrs":{"url":"/assets/x"}}]`, nil},
		{"embed empty content", "body", `[{"type":"embed","attrs":{"url":"/x"},"content":[]}]`, nil},
		{"embed no attrs", "body", `[{"type":"embed"}]`, []string{"body.content[0]:richtext"}},
		{"embed no url", "body", `[{"type":"embed","attrs":{"caption":"a"}}]`, []string{"body.content[0]:richtext"}},
		{"embed bad url", "body", `[{"type":"embed","attrs":{"url":"javascript:x"}}]`, []string{"body.content[0]:richtext"}},
		{"embed long caption", "body", `[{"type":"embed","attrs":{"url":"/x","caption":"12345678901"}}]`, []string{"body.content[0]:richtext"}},
		{"embed NUL", "body", `[{"type":"embed","attrs":{"url":"/x","caption":"a\u0000"}}]`, []string{"body.content[0]:richtext"}},
		{"embed int range", "body", `[{"type":"embed","attrs":{"url":"/x","size":6}}]`, []string{"body.content[0]:richtext"}},
		{"embed int float", "body", `[{"type":"embed","attrs":{"url":"/x","size":2.5}}]`, []string{"body.content[0]:richtext"}},
		{"embed bool type", "body", `[{"type":"embed","attrs":{"url":"/x","wide":"yes"}}]`, []string{"body.content[0]:richtext"}},
		{"embed content", "body", `[{"type":"embed","attrs":{"url":"/x"},"content":[` + p("a") + `]}]`, []string{"body.content[0]:richtext"}},
		{"footnote", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"footnoteRef","attrs":{"n":3}}]}]`, nil},
		{"footnote in heading", "body", `[{"type":"heading","attrs":{"level":1},"content":[{"type":"footnoteRef","attrs":{"n":3}}]}]`, nil},
		{"footnote in note", "body", `[{"type":"note","content":[{"type":"footnoteRef","attrs":{"n":3}},{"type":"text","text":"x"}]}]`, nil},
		{"footnote missing n", "body", `[{"type":"paragraph","content":[{"type":"footnoteRef"}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"footnote n range", "body", `[{"type":"paragraph","content":[{"type":"footnoteRef","attrs":{"n":0}}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"footnote as block", "body", `[{"type":"footnoteRef","attrs":{"n":1}}]`, []string{"body.content[0]:richtext"}},
		{"footnote in codeBlock", "body", `[{"type":"codeBlock","content":[{"type":"footnoteRef","attrs":{"n":1}}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"block in paragraph", "body", `[{"type":"paragraph","content":[{"type":"embed","attrs":{"url":"/x"}}]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"note block", "body", `[{"type":"note","content":[` + p("a") + `]}]`, []string{"body.content[0].content[0]:richtext"}},
		{"note empty", "body", `[{"type":"note"}]`, nil},
		{"note at root", "body", `[{"type":"note","content":[{"type":"text","text":"a"}]}]`, nil},
		{"highlight", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight","attrs":{"tone":"green"}},{"type":"bold"}]}]}]`, nil},
		{"highlight bare", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight"}]}]}]`, nil},
		{"highlight bad", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight","attrs":{"tone":"red"}}]}]}]`, []string{"body.content[0].content[0].marks[0]:richtext"}},
		{"highlight attr", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight","attrs":{"x":"red"}}]}]}]`, []string{"body.content[0].content[0].marks[0]:richtext"}},
		{"highlight twice", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight"},{"type":"highlight"}]}]}]`, []string{"body.content[0].content[0].marks[1]:richtext"}},
		{"plain mark attrs", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"plain","attrs":{"a":1}}]}]}]`, []string{"body.content[0].content[0].marks[0]:richtext"}},
		{"unknown mark still", "body", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"blink"}]}]}]`, []string{"body.content[0].content[0].marks[0]:richtext"}},
		{"allow callout", "small", `[{"type":"callout","content":[` + p("a") + `]}]`, nil},
		{"allow highlight", "small", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight"}]}]}]`, nil},
		{"allow embed denied", "small", `[{"type":"embed","attrs":{"url":"/x"}}]`, []string{"small.content[0]:richtext"}},
		{"allow details denied", "small", `[{"type":"details","content":[{"type":"detailsSummary"},{"type":"detailsBody","content":[` + p("a") + `]}]}]`, []string{"small.content[0]:richtext"}},
		{"allow footnote denied", "small", `[{"type":"paragraph","content":[{"type":"footnoteRef","attrs":{"n":1}}]}]`, []string{"small.content[0].content[0]:richtext"}},
		{"allow mark denied", "none", `[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"highlight"}]}]}]`, []string{"none.content[0].content[0].marks[0]:richtext"}},
		{"allow callout denied", "none", `[{"type":"callout","content":[` + p("a") + `]}]`, []string{"none.content[0]:richtext"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(t, `{"`+tc.field+`":{"type":"doc","content":`+tc.content+`}}`)
			got := codes(s.Validate(typ, d, ModeDraft))
			if !reflect.DeepEqual(got, tc.want) && (len(got) > 0 || len(tc.want) > 0) {
				t.Fatalf("errors = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRichCustomIsEmpty(t *testing.T) {
	s := mustCompile(t, richDecl)
	typ, _ := s.Type("page")
	f, _ := typ.Field("body")
	empty := func(content string) bool {
		return f.Config.IsEmpty(doc(t, `{"body":{"type":"doc","content":`+content+`}}`)["body"])
	}
	if !empty(`[{"type":"callout","content":[{"type":"paragraph"}]}]`) {
		t.Error("an empty callout counts as content")
	}
	if empty(`[{"type":"embed","attrs":{"url":"/x"}}]`) {
		t.Error("a leaf block is content")
	}
	if empty(`[{"type":"paragraph","content":[{"type":"footnoteRef","attrs":{"n":1}}]}]`) {
		t.Error("an inline atom is content")
	}
	if empty(`[{"type":"callout","content":[{"type":"embed","attrs":{"url":"/x"}}]}]`) {
		t.Error("a nested leaf is content")
	}
	if !empty(`[{"type":"details","content":[{"type":"detailsSummary"},{"type":"detailsBody","content":[{"type":"paragraph"}]}]}]`) {
		t.Error("details without text is empty")
	}
}

func TestRichCoreAdditions(t *testing.T) {
	s := mustCompile(t, "types:\n  a:\n    fields:\n      body: {type: richtext}\n")
	typ, _ := s.Type("a")
	link := func(href, extra string) string {
		return `[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"` + href + `"` + extra + `}}]}]}]`
	}
	tests := []struct {
		name, content string
		bad           bool
	}{
		{"align", `[{"type":"paragraph","attrs":{"align":"center"}},{"type":"heading","attrs":{"level":2,"align":"justify"}}]`, false},
		{"align left", `[{"type":"paragraph","attrs":{"align":"left"}}]`, false},
		{"align bad", `[{"type":"paragraph","attrs":{"align":"start"}}]`, true},
		{"align css", `[{"type":"paragraph","attrs":{"align":"center;color:red"}}]`, true},
		{"align number", `[{"type":"paragraph","attrs":{"align":1}}]`, true},
		{"id", `[{"type":"paragraph","attrs":{"id":"intro"}},{"type":"heading","attrs":{"level":1,"id":"A_b-9"}}]`, false},
		{"id digit first", `[{"type":"paragraph","attrs":{"id":"1a"}}]`, true},
		{"id space", `[{"type":"paragraph","attrs":{"id":"a b"}}]`, true},
		{"id empty", `[{"type":"paragraph","attrs":{"id":""}}]`, true},
		{"id long", `[{"type":"paragraph","attrs":{"id":"a` + strings.Repeat("b", 64) + `"}}]`, true},
		{"id max", `[{"type":"paragraph","attrs":{"id":"a` + strings.Repeat("b", 63) + `"}}]`, false},
		{"id duplicate", `[{"type":"paragraph","attrs":{"id":"x"}},{"type":"blockquote","content":[{"type":"heading","attrs":{"level":1,"id":"x"}}]}]`, true},
		{"id case differs", `[{"type":"paragraph","attrs":{"id":"x"}},{"type":"paragraph","attrs":{"id":"X"}}]`, false},
		{"id not string", `[{"type":"paragraph","attrs":{"id":5}}]`, true},
		{"start", `[{"type":"orderedList","attrs":{"start":5},"content":[{"type":"listItem"}]}]`, false},
		{"start negative", `[{"type":"orderedList","attrs":{"start":-1000000},"content":[{"type":"listItem"}]}]`, false},
		{"start range", `[{"type":"orderedList","attrs":{"start":1000001},"content":[{"type":"listItem"}]}]`, true},
		{"start float", `[{"type":"orderedList","attrs":{"start":1.5},"content":[{"type":"listItem"}]}]`, true},
		{"start on bullet", `[{"type":"bulletList","attrs":{"start":2},"content":[{"type":"listItem"}]}]`, true},
		{"tight", `[{"type":"bulletList","attrs":{"tight":true},"content":[{"type":"listItem"}]},{"type":"orderedList","attrs":{"tight":false},"content":[{"type":"listItem"}]}]`, false},
		{"tight string", `[{"type":"bulletList","attrs":{"tight":"yes"},"content":[{"type":"listItem"}]}]`, true},
		{"old order attr", `[{"type":"orderedList","attrs":{"order":2},"content":[{"type":"listItem"}]}]`, true},
		{"target", link("https://a.b", `,"target":"_blank"`), false},
		{"target bad", link("https://a.b", `,"target":"_self"`), true},
		{"target null", link("https://a.b", `,"target":null`), false},
		{"link extra attr", link("https://a.b", `,"rel":"x"`), true},
		{"anchor", link("#intro", ``), false},
		{"anchor bad", link("#1", ``), true},
		{"anchor empty", link("#", ``), true},
		{"item anchor", link("item:"+id1+"#intro", ``), false},
		{"item anchor bad", link("item:"+id1+"#", ``), true},
		{"item anchor space", link("item:"+id1+"#a b", ``), true},
		{"item plain", link("item:"+id1, ``), false},
		{"item bad id anchor", link("item:nope#a", ``), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := s.Validate(typ, doc(t, `{"body":{"type":"doc","content":`+tc.content+`}}`), ModeDraft)
			if tc.bad != (len(errs) > 0) {
				t.Fatalf("errors %v", errs)
			}
		})
	}
}

func TestRichFragmentsInRefs(t *testing.T) {
	s := mustCompile(t, "types:\n  a:\n    fields:\n      body: {type: richtext}\n")
	typ, _ := s.Type("a")
	up := strings.ToUpper(id1)
	d := doc(t, `{"body":{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"item:`+up+`#Sec_1"}}]},
		{"type":"text","text":"y","marks":[{"type":"link","attrs":{"href":"item:`+id1+`"}}]},
		{"type":"text","text":"z","marks":[{"type":"link","attrs":{"href":"#local"}}]}]}]}}`)
	s.Normalize(typ, d)
	refs := s.Refs(typ, d)
	if len(refs) != 2 || refs[0].To != id1 || refs[1].To != id1 || refs[0].Kind != "link" {
		t.Fatalf("refs %+v", refs)
	}
	marks := d["body"].(map[string]any)["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["marks"].([]any)
	if got := marks[0].(map[string]any)["attrs"].(map[string]any)["href"]; got != "item:"+id1+"#Sec_1" {
		t.Fatalf("href %v", got)
	}
	if errs := s.Validate(typ, d, ModeDraft); len(errs) != 0 {
		t.Fatalf("errors %v", errs)
	}
}

func TestRichDeclJSON(t *testing.T) {
	s := mustCompile(t, richDecl)
	b, err := s.MarshalJSON()
	if err != nil || !strings.Contains(string(b), `"richtext":{"marks"`) {
		t.Fatalf("%s %v", b, err)
	}
	if plain := mustCompile(t, "types:\n  a:\n    fields:\n      t: {type: string}\n"); strings.Contains(func() string { b, _ := plain.MarshalJSON(); return string(b) }(), "richtext") {
		t.Fatal("undeclared schema marshals richtext")
	}
}

func TestRichCustomSearchText(t *testing.T) {
	s := mustCompile(t, richDecl)
	typ, _ := s.Type("page")
	d := doc(t, `{"body":{"type":"doc","content":[
		{"type":"details","content":[{"type":"detailsSummary","content":[{"type":"text","text":"Sum"},{"type":"hardBreak"},{"type":"text","text":"mary"}]},
			{"type":"detailsBody","content":[{"type":"paragraph","content":[{"type":"text","text":"Inside"}]}]}]},
		{"type":"callout","content":[{"type":"paragraph","content":[{"type":"text","text":"Boxed"}]}]}]}}`)
	if got := s.SearchText(typ, d); got != "Sum mary Inside Boxed" {
		t.Fatalf("search text %q", got)
	}
}
