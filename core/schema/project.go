package schema

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Refs lists item references in document order: ref fields and rich text item: links.
func (s *Schema) Refs(t *Type, doc map[string]any) []RefValue {
	var out []RefValue
	for _, f := range t.Fields {
		if v, ok := doc[f.Name]; ok {
			out = collectRefs(f, v, f.Name, out)
		}
	}
	return out
}

func collectRefs(f *Field, v any, path string, out []RefValue) []RefValue {
	switch f.Shape.Base {
	case BaseRef:
		if id, ok := v.(string); ok && id != "" {
			out = append(out, RefValue{Path: path, To: id, Kind: "field", OnDelete: onDelete(f), Types: f.Shape.To})
		}
	case BaseBlocks:
		out = f.Config.(*blocksConfig).refs(v, path, out)
	case BaseRichText:
		out = richLinks(v, path, out)
	case BaseList:
		if list, ok := v.([]any); ok && f.Of != nil {
			for i, e := range list {
				out = collectRefs(f.Of, e, path+"["+strconv.Itoa(i)+"]", out)
			}
		}
	case BaseGroup:
		if m, ok := v.(map[string]any); ok {
			for _, sub := range f.Fields {
				if sv, ok := m[sub.Name]; ok {
					out = collectRefs(sub, sv, path+"."+sub.Name, out)
				}
			}
		}
	}
	return out
}

// OnDelete is the delete policy of a reference field (a ref or asset field, or the element field of a list of
// them): "block", "nullify" or "warn".
func (s *Schema) OnDelete(f *Field) string { return onDelete(f) }

func onDelete(f *Field) string {
	if c, ok := f.Config.(*refConfig); ok {
		return c.onDelete
	}
	return "block"
}

func richLinks(v any, path string, out []RefValue) []RefValue {
	n, ok := v.(map[string]any)
	if !ok {
		return out
	}
	marks, _ := n["marks"].([]any)
	for _, m := range marks {
		mm, _ := m.(map[string]any)
		attrs, _ := mm["attrs"].(map[string]any)
		if href, _ := attrs["href"].(string); mm["type"] == "link" && strings.HasPrefix(href, "item:") {
			out = append(out, RefValue{Path: path, To: strings.TrimPrefix(href, "item:"), Kind: "link", OnDelete: "warn"})
		}
	}
	children, _ := n["content"].([]any)
	for _, c := range children {
		out = richLinks(c, path, out)
	}
	return out
}

// IndexValues projects fields with index: true into storage columns; lists give one value per element.
func (s *Schema) IndexValues(t *Type, doc map[string]any) []IndexValue {
	var out []IndexValue
	for _, f := range t.Fields {
		if !f.Index {
			continue
		}
		v, ok := doc[f.Name]
		if !ok {
			continue
		}
		if f.Shape.Base == BaseList {
			list, _ := v.([]any)
			for _, e := range list {
				out = appendIndex(out, f.Name, f.Of, e)
			}
			continue
		}
		out = appendIndex(out, f.Name, f, v)
	}
	return out
}

func appendIndex(out []IndexValue, name string, f *Field, v any) []IndexValue {
	iv := IndexValue{Field: name}
	switch f.Shape.Base {
	case BaseString:
		str, ok := v.(string)
		if !ok || str == "" {
			return out
		}
		switch f.Shape.Format {
		case "date", "datetime":
			t, err := parseTime(str, f.Shape.Format == "datetime")
			if err != nil {
				return out
			}
			ms := t.UnixMilli()
			iv.Time = &ms
		default:
			iv.Text = &str
		}
	case BaseNumber:
		n, _, ok := numberOf(v)
		if !ok {
			return out
		}
		iv.Num = &n
	case BaseBoolean:
		b, ok := v.(bool)
		if !ok {
			return out
		}
		iv.Bool = &b
	case BaseRef:
		id, ok := v.(string)
		if !ok || id == "" {
			return out
		}
		iv.Ref = id
	case BaseFile:
		m, _ := v.(map[string]any)
		id, ok := m["id"].(string)
		if !ok || id == "" {
			return out
		}
		iv.Text = &id
	default:
		return out
	}
	return append(out, iv)
}

// SearchText collects the searchable text of doc in schema order: string, text and slug fields and the text
// of rich text fields, including inside groups and lists. Enum, url, email, json, date, number and boolean
// fields are left out, and so is any field (or group member) with `ai: false`.
func (s *Schema) SearchText(t *Type, doc map[string]any) string {
	var sb strings.Builder
	for _, f := range t.Fields {
		if v, ok := doc[f.Name]; ok {
			collectText(f, v, &sb)
		}
	}
	return sb.String()
}

func addText(sb *strings.Builder, s string) {
	if s == "" {
		return
	}
	if sb.Len() > 0 {
		sb.WriteByte(' ')
	}
	sb.WriteString(s)
}

func collectText(f *Field, v any, sb *strings.Builder) {
	if f.AI.Exclude {
		return
	}
	switch f.Shape.Base {
	case BaseString:
		switch f.Shape.Format {
		case "", "text", "slug":
			if str, ok := v.(string); ok {
				addText(sb, str)
			}
		}
	case BaseRichText:
		richText(v, sb)
	case BaseBlocks:
		f.Config.(*blocksConfig).text(v, sb)
	case BaseList:
		if list, ok := v.([]any); ok && f.Of != nil {
			for _, e := range list {
				collectText(f.Of, e, sb)
			}
		}
	case BaseGroup:
		if m, ok := v.(map[string]any); ok {
			for _, sub := range f.Fields {
				if sv, ok := m[sub.Name]; ok {
					collectText(sub, sv, sb)
				}
			}
		}
	}
}

// richText adds one token per block, so inline marks do not split words.
func richText(v any, sb *strings.Builder) {
	doc, ok := v.(map[string]any)
	if !ok {
		return
	}
	list, _ := doc["content"].([]any)
	for _, n := range list {
		richBlock(n, sb)
	}
}

func richBlock(v any, sb *strings.Builder) {
	n, ok := v.(map[string]any)
	if !ok {
		return
	}
	children, _ := n["content"].([]any)
	switch n["type"] {
	case "paragraph", "heading", "codeBlock":
		var line strings.Builder
		richInline(children, &line)
		addText(sb, strings.TrimSpace(line.String()))
	default:
		for _, c := range children {
			richBlock(c, sb)
		}
	}
}

func richInline(children []any, sb *strings.Builder) {
	for _, c := range children {
		n, _ := c.(map[string]any)
		switch n["type"] {
		case "text":
			s, _ := n["text"].(string)
			sb.WriteString(s)
		case "hardBreak":
			sb.WriteByte(' ')
		}
	}
}

// Name computes the display name: the type's pattern, or the first non-empty top-level string/text field.
func (s *Schema) Name(t *Type, doc map[string]any) string {
	if t.NamePattern == "" && t.Upload != nil {
		file, _ := doc[UploadFile].(map[string]any)
		name, _ := file["name"].(string)
		return strings.TrimSpace(name)
	}
	if t.NamePattern == "" {
		for _, f := range t.Fields {
			if f.Shape.Base == BaseString && (f.Shape.Format == "" || f.Shape.Format == "text") {
				if str, _ := doc[f.Name].(string); strings.TrimSpace(str) != "" {
					return strings.TrimSpace(str)
				}
			}
		}
		return ""
	}
	return patternName(t.NamePattern, doc)
}

const nameSeparators = " \t-–—:|,/"

// patternName fills the placeholders of a name pattern. Nothing is trimmed from field values (only outer
// whitespace). Separators in the pattern's literal text next to an empty placeholder are dropped with it:
// the literal after it, or the literal before it when nothing non-empty follows.
func patternName(pattern string, doc map[string]any) string {
	type part struct {
		text  string
		lit   bool
		empty bool // placeholder with no value
	}
	var parts []part
	_ = scanPattern(pattern, func(lit, field string) error {
		if field == "" {
			parts = append(parts, part{text: lit, lit: true})
			return nil
		}
		v := scalarText(doc[field])
		parts = append(parts, part{text: v, empty: strings.TrimSpace(v) == ""})
		return nil
	})
	laterValue := false
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		switch {
		case p.lit:
		case p.empty:
			if i+1 < len(parts) {
				parts[i+1].text = strings.TrimLeft(parts[i+1].text, nameSeparators)
			}
			if !laterValue && i > 0 {
				parts[i-1].text = strings.TrimRight(parts[i-1].text, nameSeparators)
			}
			parts[i].text = ""
		default:
			laterValue = true
		}
	}
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.text)
	}
	return strings.TrimSpace(sb.String())
}

func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	}
	if f, _, ok := numberOf(v); ok {
		if n, isNum := v.(json.Number); isNum {
			return string(n)
		}
		return fmtNum(f)
	}
	return ""
}

// RemoveRef removes ref field values equal to target (a single ref key is deleted, list elements are
// dropped) and reports whether doc changed. Rich text links are left alone. Ids compare case-insensitively.
func (s *Schema) RemoveRef(t *Type, doc map[string]any, target string) bool {
	return s.RemoveRefIn(t, doc, target, func(*Field) bool { return true })
}

// RemoveRefIn is RemoveRef limited to the ref fields for which match returns true. match gets the ref field
// itself; for a list of refs that is the list's element field (Field.Of).
func (s *Schema) RemoveRefIn(t *Type, doc map[string]any, target string, match func(f *Field) bool) bool {
	return removeRefs(t.Fields, doc, target, match)
}

func removeRefs(fields []*Field, doc map[string]any, target string, match func(*Field) bool) bool {
	changed := false
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok {
			continue
		}
		nv, drop, ch := removeRefValue(f, v, target, match)
		switch {
		case drop:
			delete(doc, f.Name)
			changed = true
		case ch:
			doc[f.Name] = nv
			changed = true
		}
	}
	return changed
}

// removeRefValue returns the new value, whether the value itself should be removed, and whether it changed.
func removeRefValue(f *Field, v any, target string, match func(*Field) bool) (nv any, drop, changed bool) {
	switch f.Shape.Base {
	case BaseRef:
		str, ok := v.(string)
		return v, ok && strings.EqualFold(str, target) && match(f), false
	case BaseGroup:
		if m, ok := v.(map[string]any); ok {
			return v, false, removeRefs(f.Fields, m, target, match)
		}
	case BaseBlocks:
		return v, false, f.Config.(*blocksConfig).removeRef(v, target, match)
	case BaseList:
		list, ok := v.([]any)
		if !ok || f.Of == nil {
			return v, false, false
		}
		out := list[:0:0]
		for _, e := range list {
			enew, edrop, ech := removeRefValue(f.Of, e, target, match)
			if edrop {
				changed = true
				continue
			}
			changed = changed || ech
			out = append(out, enew)
		}
		return out, false, changed
	}
	return v, false, false
}

// Option returns the type-specific option of a compiled field as recorded in the canonical JSON: only
// options the schema spells out (a ref's on_delete is absent when left at its default).
func (s *Schema) Option(f *Field, key string) (any, bool) {
	v, ok := s.opts[f][key]
	return v, ok
}
