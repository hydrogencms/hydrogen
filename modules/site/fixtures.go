package site

import (
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// gen builds sample content from the schema: every field and prop is present, every allowed block type
// appears, so a template that reads something the schema does not have fails in site:check.
type gen struct {
	s *schema.Schema
	n int
}

func (g *gen) id() string {
	g.n++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", g.n)
}

func (g *gen) fields(list []*schema.Field) map[string]any {
	out := map[string]any{}
	for _, f := range list {
		out[f.Name] = g.value(f, 0)
	}
	return out
}

func (g *gen) value(f *schema.Field, depth int) any {
	sh := f.Shape
	switch sh.Base {
	case schema.BaseString:
		switch sh.Format {
		case "text":
			return "Sample text for " + f.Name + ".\nA second line."
		case "slug":
			return "sample-" + f.Name
		case "email":
			return "sample@example.com"
		case "url":
			return "https://example.com/sample"
		case "date":
			return "2026-01-01"
		case "datetime":
			return "2026-01-01T09:00:00Z"
		case "enum", "token":
			if len(sh.Enum) > 0 {
				return sh.Enum[0]
			}
		}
		return "Sample " + f.Name
	case schema.BaseNumber:
		return float64(1)
	case schema.BaseBoolean:
		return true
	case schema.BaseRef:
		return g.id()
	case schema.BaseRichText:
		return sampleDoc(g.id())
	case schema.BaseList:
		if f.Of == nil {
			return []any{}
		}
		return []any{g.value(f.Of, depth+1), g.value(f.Of, depth+1)}
	case schema.BaseGroup:
		return g.fields(f.Fields)
	case schema.BaseBlocks:
		return g.blocks(sh.Allow, 0)
	}
	return map[string]any{}
}

func sampleDoc(ref string) map[string]any {
	text := func(s string, marks ...any) map[string]any {
		n := map[string]any{"type": "text", "text": s}
		if len(marks) > 0 {
			n["marks"] = marks
		}
		return n
	}
	link := func(href string) map[string]any {
		return map[string]any{"type": "link", "attrs": map[string]any{"href": href}}
	}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": float64(2)}, "content": []any{text("Sample heading")}},
		map[string]any{"type": "paragraph", "content": []any{
			text("Sample "), text("bold", map[string]any{"type": "bold"}), text(" and "), text("italic", map[string]any{"type": "italic"}),
			text(" text with an "), text("internal link", link("item:"+ref)), text(" and an "), text("external one", link("https://example.com")),
		}},
		map[string]any{"type": "bulletList", "content": []any{
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{text("First")}}}},
			map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{text("Second")}}}},
		}},
		map[string]any{"type": "blockquote", "content": []any{map[string]any{"type": "paragraph", "content": []any{text("Quote")}}}},
	}}
}

const maxSampleDepth = 3

// blocks makes one block of each allowed type.
func (g *gen) blocks(allow []string, depth int) []any {
	out := []any{}
	for _, name := range allow {
		if b := g.block(name, depth); b != nil {
			out = append(out, b)
		}
	}
	return out
}

func (g *gen) block(name string, depth int) map[string]any {
	b := map[string]any{"id": g.id(), "type": name}
	if name == "shared" {
		b["props"] = map[string]any{"item": g.id()}
		var inner []any
		for _, t := range g.s.Types() {
			if f, ok := t.BlocksField(); ok && t.Blockset {
				inner = g.blocks(f.Shape.Allow, depth+1)
				break
			}
		}
		b["blocks"] = inner
		return b
	}
	bt, ok := g.s.Block(name)
	if !ok {
		return nil
	}
	b["props"] = g.fields(bt.Props)
	if len(bt.Slots) > 0 {
		slots := map[string]any{}
		for _, sl := range bt.Slots {
			if depth >= maxSampleDepth {
				continue
			}
			allow := sl.Allow
			if sl.Max > 0 && len(allow) > sl.Max {
				allow = allow[:sl.Max]
			}
			list := g.blocks(allow, depth+1)
			if sl.Min > len(list) && len(allow) > 0 {
				for len(list) < sl.Min {
					list = append(list, g.block(allow[0], depth+1))
				}
			}
			slots[sl.Name] = list
		}
		b["slots"] = slots
	}
	return b
}

// sampleItem is an item of a type with generated fields.
func (g *gen) sampleItem(t *schema.Type, locale string) *Item {
	it := &Item{ID: g.id(), Type: t.Name, Locale: locale, Name: "Sample " + t.Name, Fields: g.fields(t.Fields), Links: map[string]string{}}
	if title, ok := it.Fields["title"].(string); ok {
		it.Name = title
	}
	return it
}

// blockTypesOf lists the block types in a block tree, slots and shared content included.
func blockTypesOf(list []any, out *[]string) {
	for _, e := range list {
		m, _ := e.(map[string]any)
		if t, _ := m["type"].(string); t != "" && !slices.Contains(*out, t) {
			*out = append(*out, t)
		}
		slots, _ := m["slots"].(map[string]any)
		for _, k := range sortedKeys(slots) {
			l, _ := slots[k].([]any)
			blockTypesOf(l, out)
		}
		inner, _ := m["blocks"].([]any)
		blockTypesOf(inner, out)
	}
}
