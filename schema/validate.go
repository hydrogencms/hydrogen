package schema

import (
	"strings"
	"time"
)

// Validate checks doc against t. Errors come in schema order; unknown fields follow, sorted. A report holds
// at most 100 errors; if there were more, the last one has code too_many_errors.
func (s *Schema) Validate(t *Type, doc map[string]any, mode Mode) []FieldError {
	var r Report
	validateFields(t.Fields, doc, mode, &r)
	return r.errs
}

func validateFields(fields []*Field, doc map[string]any, mode Mode, r *Report) {
	present := 0
	for _, f := range fields {
		if r.full() {
			return
		}
		v, ok := doc[f.Name]
		if !ok {
			if mode == ModePublish && f.Required && visible(f, fields, doc) {
				r.Enter(f.Name)
				r.Fail("required", "is required")
				r.Leave()
			}
			continue
		}
		present++
		r.Enter(f.Name)
		if v == nil {
			r.Fail("type", "must not be null")
			r.Leave()
			continue
		}
		before := r.adds
		f.Config.Validate(v, mode, r)
		if mode == ModePublish && f.Required && r.adds == before && f.Config.IsEmpty(v) && visible(f, fields, doc) {
			r.Fail("required", "is required")
		}
		r.Leave()
	}
	if len(doc) > present {
		unknownFields(fields, doc, r)
	}
}

func unknownFields(fields []*Field, doc map[string]any, r *Report) {
	for _, k := range sortedKeys(doc) {
		if r.full() {
			return
		}
		if !hasField(fields, k) {
			r.Enter(k)
			r.Fail("unknown_field", "unknown field %q", k)
			r.Leave()
		}
	}
}

func hasField(fields []*Field, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func visible(f *Field, siblings []*Field, doc map[string]any) bool {
	return f.When == nil || condMet(f.When, siblings, doc)
}

func condMet(c *Condition, siblings []*Field, doc map[string]any) bool {
	v, present := doc[c.Field]
	switch c.Op {
	case "eq":
		return present && jsonEqual(v, c.Value)
	case "ne":
		return !present || !jsonEqual(v, c.Value)
	case "in":
		list, _ := c.Value.([]any)
		if !present {
			return false
		}
		for _, x := range list {
			if jsonEqual(v, x) {
				return true
			}
		}
		return false
	case "empty":
		want, _ := c.Value.(bool)
		return siblingEmpty(c.Field, siblings, v, present) == want
	}
	return false
}

func siblingEmpty(name string, siblings []*Field, v any, present bool) bool {
	if !present || v == nil {
		return true
	}
	for _, f := range siblings {
		if f.Name == name {
			return f.Config.IsEmpty(v)
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case nil:
		return b == nil
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, found := y[k]
			if !found || !jsonEqual(xv, yv) {
				return false
			}
		}
		return true
	}
	fa, _, okA := numberOf(a)
	fb, _, okB := numberOf(b)
	return okA && okB && fa == fb
}

func cloneJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneJSON(e)
		}
		return out
	}
	return v
}

// ApplyDefaults fills missing fields that have a default, in schema order, skipping hidden fields.
func (s *Schema) ApplyDefaults(t *Type, doc map[string]any) { defaultFields(t.Fields, doc) }

func defaultFields(fields []*Field, doc map[string]any) {
	for _, f := range fields {
		if !visible(f, fields, doc) {
			continue
		}
		v, ok := doc[f.Name]
		if !ok {
			if f.Default != nil {
				doc[f.Name] = cloneJSON(f.Default)
			}
			continue
		}
		walkNested(f, v, defaultFields)
	}
}

// walkNested applies fn to the object(s) a group or list-of-group value holds.
func walkNested(f *Field, v any, fn func([]*Field, map[string]any)) {
	switch f.Shape.Base {
	case BaseGroup:
		if m, ok := v.(map[string]any); ok {
			fn(f.Fields, m)
		}
	case BaseList:
		if list, ok := v.([]any); ok && f.Of != nil {
			for _, e := range list {
				walkNested(f.Of, e, fn)
			}
		}
	}
}

// Prune removes fields hidden by their `when` condition, recursively.
func (s *Schema) Prune(t *Type, doc map[string]any) { pruneFields(t.Fields, doc) }

func pruneFields(fields []*Field, doc map[string]any) {
	for _, f := range fields {
		if !visible(f, fields, doc) {
			delete(doc, f.Name)
			continue
		}
		if v, ok := doc[f.Name]; ok {
			walkNested(f, v, pruneFields)
		}
	}
}

// Normalize puts values in their canonical form: datetimes in UTC with milliseconds
// (2006-01-02T15:04:05.000Z), item ids (ref fields and item: links in rich text) in lower case, and integral
// numbers of integer fields without fraction or exponent ("5.0" and "1e3" become 5 and 1000). Invalid values
// stay as they are for Validate to report.
func (s *Schema) Normalize(t *Type, doc map[string]any) { normalizeFields(t.Fields, doc) }

func normalizeFields(fields []*Field, doc map[string]any) {
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok {
			continue
		}
		if nv, changed := normalizeValue(f, v); changed {
			doc[f.Name] = nv
		}
	}
}

func normalizeValue(f *Field, v any) (any, bool) {
	switch {
	case f.Shape.Base == BaseString && f.Shape.Format == "datetime":
		str, ok := v.(string)
		if !ok {
			return v, false
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
			return v, false
		}
		n := t.UTC().Format(datetimeLayout)
		return n, n != str
	case f.Shape.Base == BaseRef:
		str, ok := v.(string)
		if !ok {
			return v, false
		}
		n := strings.ToLower(str)
		return n, n != str
	case f.Shape.Base == BaseNumber && f.Shape.Integer:
		return normalizeInteger(v)
	case f.Shape.Base == BaseRichText:
		lowerItemLinks(v)
	case f.Shape.Base == BaseGroup:
		walkNested(f, v, normalizeFields)
	case f.Shape.Base == BaseList && f.Of != nil:
		if list, ok := v.([]any); ok {
			for i, e := range list {
				if nv, changed := normalizeValue(f.Of, e); changed {
					list[i] = nv
				}
			}
		}
	}
	return v, false
}

// lowerItemLinks lower-cases the id of item:<id> link hrefs in a rich text document, in place.
func lowerItemLinks(v any) {
	n, ok := v.(map[string]any)
	if !ok {
		return
	}
	marks, _ := n["marks"].([]any)
	for _, m := range marks {
		mm, _ := m.(map[string]any)
		if mm["type"] != "link" {
			continue
		}
		attrs, _ := mm["attrs"].(map[string]any)
		if href, _ := attrs["href"].(string); strings.HasPrefix(href, "item:") {
			if low := "item:" + strings.ToLower(href[len("item:"):]); low != href {
				attrs["href"] = low
			}
		}
	}
	children, _ := n["content"].([]any)
	for _, c := range children {
		lowerItemLinks(c)
	}
}
