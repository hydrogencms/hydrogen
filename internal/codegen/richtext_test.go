package codegen

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/core/schema"
)

const richSchema = `
richtext:
  nodes:
    callout:
      tag: aside
      content: blocks
      attrs:
        variant: {type: enum, values: [info, warning], default: info}
    embed:
      tag: figure
      content: none
      attrs:
        url: {type: url, required: true}
        size: {type: int, default: 2}
    footnoteRef:
      inline: true
      content: none
      attrs:
        n: {type: int, required: true}
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

func richFixture(t testing.TB, src string) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(src)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRichTextSchemaGolden(t *testing.T) {
	src, err := RichTextSchema(richFixture(t, richSchema).RichText())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "richtext.ts.golden", src)
}

func TestRichTextSchemaUndeclared(t *testing.T) {
	s := richFixture(t, "types:\n  a:\n    fields:\n      t: {type: string}\n")
	if src, err := RichTextSchema(s.RichText()); err != nil || src != nil {
		t.Fatalf("%q %v", src, err)
	}
}

func TestWriteRichText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gen", "richtext.ts")
	s := richFixture(t, richSchema)
	if err := WriteRichText(path, s.RichText()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	none := richFixture(t, "types:\n  a:\n    fields:\n      t: {type: string}\n")
	if err := WriteRichText(path, none.RichText()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale file kept: %v", err)
	}
	if err := os.WriteFile(path, []byte("export const mine = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteRichText(path, none.RichText()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a hand-written file was removed: %v", err)
	}
}
