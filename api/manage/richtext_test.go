package manage

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/core/schema"
)

const richTextTestSchema = `
richtext:
  nodes:
    callout:
      label: Callout
      tag: aside
      content: blocks
      attrs:
        variant: {type: enum, values: [info, warning], default: info}
    details: {tag: details, content: [summary, body]}
    summary: {tag: summary, content: inline}
    body: {content: blocks}
    embed:
      content: none
      attrs:
        url: {type: url, required: true}
        caption: {type: string, max: 300}
        size: {type: int, min: 1, max: 5, default: 2}
  marks:
    highlight:
      tag: mark
      attrs:
        wide: {type: bool, default: false}
types:
  page:
    fields:
      body: {type: richtext}
`

func richTextFake(t testing.TB, src string) *fake {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(src)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	f.sc = s
	return f
}

func TestRichTextEndpoint(t *testing.T) {
	h := newAPI(t, richTextFake(t, richTextTestSchema))
	w := do(h, "GET", "/richtext", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got, want any
	mustJSON(t, w, &got)
	if err := json.Unmarshal([]byte(`{
	"nodes": [
	  {"name":"body","label":"Body","tag":"div","inline":false,"content":"blocks","parts":[],"part":true,"attrs":[]},
	  {"name":"callout","label":"Callout","tag":"aside","inline":false,"content":"blocks","parts":[],"part":false,
	   "attrs":[{"name":"variant","type":"enum","values":["info","warning"],"required":false,"default":"info"}]},
	  {"name":"details","label":"Details","tag":"details","inline":false,"content":"parts","parts":["summary","body"],"part":false,"attrs":[]},
	  {"name":"embed","label":"Embed","tag":"div","inline":false,"content":"none","parts":[],"part":false,"attrs":[
	    {"name":"url","type":"url","required":true},
	    {"name":"caption","type":"string","maxLength":300,"required":false},
	    {"name":"size","type":"int","min":1,"max":5,"required":false,"default":2}]},
	  {"name":"summary","label":"Summary","tag":"summary","inline":false,"content":"inline","parts":[],"part":true,"attrs":[]}
	],
	"marks": [
	  {"name":"highlight","label":"Highlight","tag":"mark","attrs":[{"name":"wide","type":"bool","required":false,"default":false}]}
	]}`), &want); err != nil {
		t.Fatal(err)
	}
	gb, _ := json.Marshal(got)
	wb, _ := json.Marshal(want)
	if string(gb) != string(wb) {
		t.Fatalf("got  %s\nwant %s", gb, wb)
	}
}

func TestRichTextEndpointEmpty(t *testing.T) {
	h := newAPI(t, richTextFake(t, "types:\n  a:\n    fields:\n      t: {type: string}\n"))
	w := do(h, "GET", "/richtext", "")
	if w.Code != 200 || w.Body.String() != "{\"marks\":[],\"nodes\":[]}\n" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}
