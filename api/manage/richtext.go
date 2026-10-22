package manage

import (
	"net/http"

	"github.com/hydrogencms/hydrogen/api/openapi"
	"github.com/hydrogencms/hydrogen/core/schema"
)

type richAttrJSON struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Values    []string `json:"values,omitempty"`
	Min       *int64   `json:"min,omitempty"`
	Max       *int64   `json:"max,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Required  bool     `json:"required"`
	Default   any      `json:"default,omitempty"`
}

type richNodeJSON struct {
	Name    string         `json:"name"`
	Label   string         `json:"label"`
	Tag     string         `json:"tag"`
	Inline  bool           `json:"inline"`
	Content string         `json:"content"`
	Parts   []string       `json:"parts"`
	Part    bool           `json:"part"`
	Attrs   []richAttrJSON `json:"attrs"`
}

type richMarkJSON struct {
	Name  string         `json:"name"`
	Label string         `json:"label"`
	Tag   string         `json:"tag"`
	Attrs []richAttrJSON `json:"attrs"`
}

func richAttrs(list []*schema.RichAttr) []richAttrJSON {
	out := make([]richAttrJSON, len(list))
	for i, a := range list {
		out[i] = richAttrJSON{Name: a.Name, Type: a.Type, Values: a.Values, Min: a.Min, Max: a.Max, MaxLength: a.MaxLen,
			Required: a.Required, Default: a.Default}
	}
	return out
}

func richTextResponse(rt *schema.RichText) map[string]any {
	nodes := make([]richNodeJSON, len(rt.Nodes))
	for i, n := range rt.Nodes {
		parts := n.Parts
		if parts == nil {
			parts = []string{}
		}
		nodes[i] = richNodeJSON{Name: n.Name, Label: n.Label, Tag: n.Tag, Inline: n.Inline, Content: n.Content,
			Parts: parts, Part: n.Part, Attrs: richAttrs(n.Attrs)}
	}
	marks := make([]richMarkJSON, len(rt.Marks))
	for i, m := range rt.Marks {
		marks[i] = richMarkJSON{Name: m.Name, Label: m.Label, Tag: m.Tag, Attrs: richAttrs(m.Attrs)}
	}
	return map[string]any{"nodes": nodes, "marks": marks}
}

// listRichText answers the project's custom rich text nodes and marks (the schema's richtext: key).
func (a *api) listRichText(w http.ResponseWriter, _ *http.Request) error {
	a.writeRaw(w, a.richTextJSON)
	return nil
}

func richTextSchemas(schemas obj) {
	attr := obj{"type": "object", "required": []any{"name", "type", "required"}, "properties": obj{
		"name": openapi.Str(""), "type": obj{"type": "string", "enum": []any{"string", "enum", "int", "bool", "url"}},
		"values": openapi.ArrayOf(openapi.Str("")), "min": obj{"type": "integer"}, "max": obj{"type": "integer"},
		"maxLength": obj{"type": "integer"}, "required": obj{"type": "boolean"},
		"default": obj{"description": "A string, integer or boolean matching the type."}}}
	attrs := obj{"type": "array", "items": attr}
	schemas["RichTextCatalog"] = obj{"type": "object", "required": []any{"nodes", "marks"}, "properties": obj{
		"nodes": obj{"type": "array", "items": obj{"type": "object",
			"required": []any{"name", "label", "tag", "inline", "content", "parts", "part", "attrs"}, "properties": obj{
				"name": openapi.Str(""), "label": openapi.Str(""), "tag": openapi.Str(""), "inline": obj{"type": "boolean"},
				"content": obj{"type": "string", "enum": []any{"blocks", "inline", "none", "parts"}},
				"parts":   openapi.ArrayOf(openapi.Str("")), "part": obj{"type": "boolean", "description": "Another node names this one as a part."},
				"attrs": attrs}}},
		"marks": obj{"type": "array", "items": obj{"type": "object", "required": []any{"name", "label", "tag", "attrs"},
			"properties": obj{"name": openapi.Str(""), "label": openapi.Str(""), "tag": openapi.Str(""), "attrs": attrs}}},
	}}
}
