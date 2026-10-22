package site

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/schema"
)

type fixtureCase struct {
	Name    string          `json:"name"`
	Doc     any             `json:"doc"`
	Options json.RawMessage `json:"options"`
	HTML    string          `json:"html"`
}

// vocabOf reads the TypeScript RichTextSchema shape of the parity fixture; attributes keep their declared order.
func vocabOf(t *testing.T, raw json.RawMessage) *richVocab {
	t.Helper()
	var o struct {
		Schema struct {
			Nodes map[string]json.RawMessage `json:"nodes"`
			Marks map[string]json.RawMessage `json:"marks"`
		} `json:"schema"`
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	rt := &schema.RichText{}
	decl := func(name string, raw json.RawMessage) (tag string, inline bool, attrs []*schema.RichAttr) {
		var d struct {
			Tag    string                     `json:"tag"`
			Inline bool                       `json:"inline"`
			Attrs  map[string]json.RawMessage `json:"attrs"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		for _, k := range orderedKeys(t, raw, "attrs") {
			var a struct {
				Type    string `json:"type"`
				Default any    `json:"default"`
			}
			if err := json.Unmarshal(d.Attrs[k], &a); err != nil {
				t.Fatal(err)
			}
			if f, ok := a.Default.(float64); ok {
				a.Default = int64(f)
			}
			attrs = append(attrs, &schema.RichAttr{Name: k, Type: a.Type, Default: a.Default})
		}
		return d.Tag, d.Inline, attrs
	}
	for name, raw := range o.Schema.Nodes {
		tag, inline, attrs := decl(name, raw)
		rt.Nodes = append(rt.Nodes, &schema.RichNode{Name: name, Tag: tag, Inline: inline, Attrs: attrs})
	}
	for name, raw := range o.Schema.Marks {
		tag, _, attrs := decl(name, raw)
		rt.Marks = append(rt.Marks, &schema.RichMark{Name: name, Tag: tag, Attrs: attrs})
	}
	return newRichVocab(rt)
}

// orderedKeys lists the keys of the object under field in raw, in document order.
func orderedKeys(t *testing.T, raw json.RawMessage, field string) []string {
	t.Helper()
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		t.Fatal(err)
	}
	in, ok := outer[field]
	if !ok {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(in))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// TestRichTextParity renders the fixture every renderer shares (packages/client/test/fixtures).
func TestRichTextParity(t *testing.T) {
	data, err := os.ReadFile("../../packages/client/test/fixtures/richtext-ext.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var o struct {
				Classes map[string]string `json:"classes"`
				Links   map[string]string `json:"links"`
			}
			if len(c.Options) > 0 {
				if err := json.Unmarshal(c.Options, &o); err != nil {
					t.Fatal(err)
				}
			}
			got, err := renderRich(c.Doc, richConfig{
				link: func(id string) string { return o.Links[id] }, rt: vocabOf(t, c.Options), opts: RichTextOptions{Classes: o.Classes}})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.HTML {
				t.Errorf("got  %s\nwant %s", got, c.HTML)
			}
		})
	}
}

func richDoc(t *testing.T, src string) any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

const richSample = `{"type":"doc","content":[
 {"type":"paragraph","content":[{"type":"text","text":"one "},{"type":"text","text":"two","marks":[{"type":"bold"}]}]},
 {"type":"callout","attrs":{"tone":"warn"},"content":[{"type":"paragraph","content":[{"type":"text","text":"in"}]}]},
 {"type":"paragraph","content":[{"type":"text","text":"hl","marks":[{"type":"highlight","attrs":{"tone":"g"}}]}]}]}`

func richVocabSample() *richVocab {
	return newRichVocab(&schema.RichText{
		Nodes: []*schema.RichNode{{Name: "callout", Tag: "aside", Attrs: []*schema.RichAttr{
			{Name: "tone", Type: "enum", Default: "info"}, {Name: "size", Type: "int", Default: int64(2)}}}},
		Marks: []*schema.RichMark{{Name: "highlight", Tag: "mark", Attrs: []*schema.RichAttr{{Name: "tone", Type: "string"}}}},
	})
}

func richPage(t *testing.T, files map[string]string) *page {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		writeFile(t, dir, "templates/"+n, c)
	}
	chain, _ := newExtensions().chain("starter")
	e, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := &View{Locale: "en", Fields: map[string]any{}, Params: map[string]string{}, Channel: ChannelView{Home: "/"}}
	p := &page{eng: e, ch: &channel.Channel{Name: "t", Languages: []string{"en"}}, locale: "en", def: "en", view: v, strict: true, links: map[string]string{}}
	p.rtv = richVocabSample()
	return p
}

func execT(t *testing.T, p *page, name string, data any) (string, error) {
	t.Helper()
	if err := p.ensureSet(); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	err := p.t.ExecuteTemplate(&sb, name, data)
	return sb.String(), err
}

func TestRichTextTemplateOptions(t *testing.T) {
	p := richPage(t, map[string]string{
		"partials/a.html": `{{richtext . (dict "classes" (dict "paragraph" "lead" "bold" "x y") "wrap" "section" "wrapClass" "prose")}}`,
		"partials/b.html": `{{richtext . (dict "wrap" "script" "wrapClass" "a\"b")}}`,
		"partials/c.html": `{{richtext . (dict "wrap" "div")}}|`,
		"partials/d.html": `{{richtext . (dict "bogus" "1")}}`,
	})
	doc := richDoc(t, richSample)
	got, err := execT(t, p, "partials/a.html", doc)
	want := `<section class="prose"><p class="lead">one <strong class="x y">two</strong></p><aside class="rt-callout" data-tone="warn" data-size="2"><p class="lead">in</p></aside>` +
		`<p class="lead"><mark class="rt-highlight" data-tone="g">hl</mark></p></section>`
	if err != nil || got != want {
		t.Errorf("got %s %v\nwant %s", got, err, want)
	}
	if got, _ := execT(t, p, "partials/b.html", richDoc(t, `{"type":"doc","content":[{"type":"paragraph"}]}`)); got != "<p></p>" {
		t.Errorf("ignored wrap = %s", got)
	}
	if got, _ := execT(t, p, "partials/c.html", richDoc(t, `{"type":"doc","content":[]}`)); got != "|" {
		t.Errorf("empty output wrapped: %s", got)
	}
	if _, err := execT(t, p, "partials/d.html", doc); err == nil {
		t.Error("an unknown option passed")
	}
	v := &View{p: p}
	if got := v.RichText(doc, RichTextOptions{Wrap: "article", Classes: map[string]string{"paragraph": "lead"}}); !strings.HasPrefix(string(got), `<article><p class="lead">one `) {
		t.Errorf("View.RichText = %s", got)
	}
}

func TestRichTextOverrides(t *testing.T) {
	p := richPage(t, map[string]string{
		"richtext/paragraph.html": `<div class="para {{.Class}}" data-t="{{.Type}}">{{.Content}}</div>`,
		"richtext/callout.html":   `<section data-tone="{{.Attrs.tone}}" data-size="{{.Attrs.size}}" data-raw="{{index .Node "type"}}">{{.Content}}</section>`,
		"richtext/highlight.html": `<em>{{.Content}}</em>`,
		"partials/x.html":         `{{richtext . (dict "classes" (dict "paragraph" "lead"))}}`,
	})
	if len(p.eng.rich) != 3 || !p.eng.rich["callout"] {
		t.Fatalf("override set = %v", p.eng.rich)
	}
	got, err := execT(t, p, "partials/x.html", richDoc(t, richSample))
	want := `<div class="para lead" data-t="paragraph">one <strong>two</strong></div>` +
		`<section data-tone="warn" data-size="2" data-raw="callout"><div class="para lead" data-t="paragraph">in</div></section>` +
		`<div class="para lead" data-t="paragraph"><em>hl</em></div>`
	if err != nil || got != want {
		t.Errorf("got  %s %v\nwant %s", got, err, want)
	}
	if got, _ := execT(t, p, "partials/x.html", richDoc(t, `{"type":"doc","content":[{"type":"callout","content":[]}]}`)); !strings.Contains(got, `data-tone="info" data-size="2"`) {
		t.Errorf("defaults not applied: %s", got)
	}
}

func TestRichTextOverrideError(t *testing.T) {
	p := richPage(t, map[string]string{
		"richtext/paragraph.html": `{{.Missing.x}}`,
		"partials/x.html":         `{{richtext .}}`,
	})
	if _, err := execT(t, p, "partials/x.html", richDoc(t, richSample)); err == nil {
		t.Error("a failing override rendered")
	}
}

func TestRichTextTightAndLinks(t *testing.T) {
	doc := richDoc(t, `{"type":"doc","content":[{"type":"paragraph","content":[
	 {"type":"text","text":"a","marks":[{"type":"link","attrs":{"href":"item:Abc#s","target":"_blank"}}]}]}]}`)
	got := renderRichText(doc, func(id string) string {
		if id == "Abc" {
			return "/x"
		}
		return ""
	})
	if want := `<p><a href="/x#s" target="_blank" rel="noopener noreferrer">a</a></p>`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func benchPage(b *testing.B, over bool) (*page, any) {
	dir := b.TempDir()
	if over {
		writeFile(b, dir, "templates/richtext/callout.html", `<aside>{{.Content}}</aside>`)
	}
	chain, _ := newExtensions().chain("starter")
	e, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil)
	if err != nil {
		b.Fatal(err)
	}
	p := &page{eng: e, ch: &channel.Channel{Name: "t", Languages: []string{"en"}}, view: &View{}, links: map[string]string{}}
	var content []any
	for i := 0; i < 200; i++ {
		content = append(content, map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Lorem ipsum dolor sit amet "},
			map[string]any{"type": "text", "text": "bold & <b>", "marks": []any{map[string]any{"type": "bold"}}},
			map[string]any{"type": "text", "text": " link", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.com/a?b=1"}}}},
		}})
	}
	return p, map[string]any{"type": "doc", "content": content}
}

func BenchmarkRichText200(b *testing.B) {
	for _, over := range []bool{false, true} {
		name := "plain"
		if over {
			name = "overrides"
		}
		b.Run(name, func(b *testing.B) {
			p, doc := benchPage(b, over)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := p.richText(doc, RichTextOptions{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
