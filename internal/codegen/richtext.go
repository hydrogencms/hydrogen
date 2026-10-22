package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// RichTextSchema returns gen/richtext.ts: the project's custom rich text nodes and marks (tags, inline flags,
// attribute types and defaults) as a const object frontends pass to the renderers. It is nil when the schema
// declares none. Output is sorted and deterministic.
func RichTextSchema(rt *schema.RichText) ([]byte, error) {
	if rt == nil || len(rt.Nodes) == 0 && len(rt.Marks) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("// " + header + "\n\nexport const richTextSchema = {\n  nodes: {")
	for i, n := range rt.Nodes {
		if err := tsRichEntry(&b, i, n.Name, n.Tag, n.Inline, true, n.Attrs); err != nil {
			return nil, err
		}
	}
	b.WriteString(closeRich(len(rt.Nodes), ",") + "  marks: {")
	for i, m := range rt.Marks {
		if err := tsRichEntry(&b, i, m.Name, m.Tag, false, false, m.Attrs); err != nil {
			return nil, err
		}
	}
	b.WriteString(closeRich(len(rt.Marks), "") + "} as const;\n")
	return []byte(b.String()), nil
}

func closeRich(n int, comma string) string {
	if n == 0 {
		return "}" + comma + "\n"
	}
	return "\n  }" + comma + "\n"
}

func tsRichEntry(b *strings.Builder, i int, name, tag string, inline, withInline bool, attrs []*schema.RichAttr) error {
	if i > 0 {
		b.WriteByte(',')
	}
	fmt.Fprintf(b, "\n    %s: { tag: %s", name, jsString(tag))
	if withInline {
		fmt.Fprintf(b, ", inline: %t", inline)
	}
	b.WriteString(", attrs: {")
	for j, a := range attrs {
		if j > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(b, " %s: { type: %s", a.Name, jsString(a.Type))
		if a.Default != nil {
			def, err := json.Marshal(a.Default)
			if err != nil {
				return fmt.Errorf("codegen: attribute %s: %w", a.Name, err)
			}
			fmt.Fprintf(b, ", default: %s", def)
		}
		b.WriteString(" }")
	}
	if len(attrs) > 0 {
		b.WriteByte(' ')
	}
	b.WriteString("} }")
	return nil
}

func jsString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

// WriteRichText writes the rich text schema to path when the schema declares one, and otherwise removes a stale
// generated file there (a file without the generated header is never touched).
func WriteRichText(path string, rt *schema.RichText) error {
	src, err := RichTextSchema(rt)
	if err != nil {
		return err
	}
	if src == nil {
		old, err := os.ReadFile(path)
		if err == nil && bytes.HasPrefix(old, []byte("// "+header)) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("codegen: %w", err)
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("codegen: %w", err)
	}
	_, err = writeIfChanged(path, src)
	return err
}
