package schema

import (
	"bytes"
	"encoding/json"
)

const schemaDialect = "https://json-schema.org/draft/2020-12/schema"

// MarshalJSON is the canonical encoding (sorted keys, no positions) stored as the schema revision and hashed
// into Hash.
func (s *Schema) MarshalJSON() ([]byte, error) {
	types := make([]any, len(s.types))
	for i, t := range s.types {
		types[i] = s.typeJSON(t)
	}
	groups := make([]any, len(s.groups))
	for i, g := range s.groups {
		m := map[string]any{"name": g.Name, "fields": s.fieldsJSON(g.Fields)}
		put(m, "description", g.Description)
		groups[i] = m
	}
	return canonical(map[string]any{"version": 1, "types": types, "groups": groups})
}

func canonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func put[T comparable](m map[string]any, key string, v T) {
	var zero T
	if v != zero {
		m[key] = v
	}
}

func (s *Schema) typeJSON(t *Type) map[string]any {
	m := map[string]any{"name": t.Name, "label": t.Label, "fields": s.fieldsJSON(t.Fields)}
	put(m, "description", t.Description)
	put(m, "singleton", t.Singleton)
	put(m, "page", t.Page)
	put(m, "namePattern", t.NamePattern)
	if len(t.Options) > 0 {
		m["options"] = t.Options
	}
	admin := map[string]any{}
	put(admin, "group", t.Admin.Group)
	put(admin, "icon", t.Admin.Icon)
	if len(t.Admin.List) > 0 {
		admin["list"] = t.Admin.List
	}
	if len(admin) > 0 {
		m["admin"] = admin
	}
	ai := map[string]any{}
	put(ai, "instructions", t.AI.Instructions)
	if len(t.AI.Generate) > 0 {
		ai["generate"] = t.AI.Generate
	}
	if len(ai) > 0 {
		m["ai"] = ai
	}
	return m
}

func (s *Schema) fieldsJSON(fields []*Field) []any {
	out := make([]any, len(fields))
	for i, f := range fields {
		out[i] = s.fieldJSON(f)
	}
	return out
}

func (s *Schema) fieldJSON(f *Field) map[string]any {
	m := map[string]any{"name": f.Name, "label": f.Label, "type": f.Type, "shape": shapeJSON(f.Shape)}
	put(m, "description", f.Description)
	put(m, "required", f.Required)
	put(m, "translatable", f.Translatable)
	put(m, "index", f.Index)
	if f.Default != nil {
		m["default"] = f.Default
	}
	if f.When != nil {
		m["when"] = map[string]any{"field": f.When.Field, "op": f.When.Op, "value": f.When.Value}
	}
	ai := map[string]any{}
	put(ai, "exclude", f.AI.Exclude)
	put(ai, "describe", f.AI.Describe)
	if len(ai) > 0 {
		m["ai"] = ai
	}
	admin := map[string]any{}
	put(admin, "widget", f.Admin.Widget)
	put(admin, "placeholder", f.Admin.Placeholder)
	put(admin, "hidden", f.Admin.Hidden)
	put(admin, "readonly", f.Admin.ReadOnly)
	if len(admin) > 0 {
		m["admin"] = admin
	}
	if opts := s.opts[f]; len(opts) > 0 {
		m["options"] = opts
	}
	if f.Of != nil {
		m["of"] = s.fieldJSON(f.Of)
	}
	if f.Group != "" {
		m["group"] = f.Group
	} else if f.Fields != nil {
		m["fields"] = s.fieldsJSON(f.Fields)
	}
	return m
}

func shapeJSON(sh Shape) map[string]any {
	m := map[string]any{"base": string(sh.Base)}
	put(m, "format", sh.Format)
	put(m, "integer", sh.Integer)
	if len(sh.Enum) > 0 {
		m["enum"] = sh.Enum
	}
	if len(sh.To) > 0 {
		m["to"] = sh.To
	}
	return m
}

// JSONSchema describes a type's full document (draft 2020-12).
func (s *Schema) JSONSchema(t *Type) map[string]any {
	m := objectSchema(t.Fields)
	m["$schema"] = schemaDialect
	m["title"] = t.Label
	put(m, "description", t.Description)
	return m
}

func objectSchema(fields []*Field) map[string]any {
	props := make(map[string]any, len(fields))
	var required []string
	for _, f := range fields {
		props[f.Name] = fieldSchema(f)
		if f.Required && f.When == nil {
			required = append(required, f.Name)
		}
	}
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func fieldSchema(f *Field) map[string]any {
	m := f.Config.JSONSchema()
	put(m, "title", f.Label)
	put(m, "description", f.Description)
	if f.Default != nil {
		m["default"] = f.Default
	}
	return m
}
