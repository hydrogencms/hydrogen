package schema

import "strconv"

// MapValues calls fn for every present value, anywhere in doc (top-level fields, groups, list elements and
// block props), of a field for which match returns true, with the value's JSON path. For a list, match and fn
// get the element field (Field.Of). fn returns the replacement and whether the value changed; replacements are
// stored in place.
func (s *Schema) MapValues(t *Type, doc map[string]any, match func(*Field) bool,
	fn func(f *Field, path string, v any) (any, bool)) {
	mapFields(t.Fields, doc, "", match, fn)
}

func mapFields(fields []*Field, doc map[string]any, prefix string, match func(*Field) bool,
	fn func(*Field, string, any) (any, bool)) {
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok || v == nil {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		if nv, changed := mapValue(f, v, path, match, fn); changed {
			doc[f.Name] = nv
		}
	}
}

func mapValue(f *Field, v any, path string, match func(*Field) bool,
	fn func(*Field, string, any) (any, bool)) (any, bool) {
	if match(f) {
		return fn(f, path, v)
	}
	switch f.Shape.Base {
	case BaseGroup:
		if m, ok := v.(map[string]any); ok {
			mapFields(f.Fields, m, path, match, fn)
		}
	case BaseList:
		list, ok := v.([]any)
		if !ok || f.Of == nil {
			return v, false
		}
		for i, e := range list {
			if e == nil {
				continue
			}
			if nv, changed := mapValue(f.Of, e, path+"["+strconv.Itoa(i)+"]", match, fn); changed {
				list[i] = nv
			}
		}
	case BaseBlocks:
		f.Config.(*blocksConfig).each(v, path, func(p string, m map[string]any, bt *BlockType) {
			if props, ok := m["props"].(map[string]any); ok {
				mapFields(bt.Props, props, p+".props", match, fn)
			}
		})
	}
	return v, false
}

// HasFields reports whether documents of t can hold a value of a field for which match returns true: in a
// top-level field, a group, a list, or (when the type has a blocks field) any block type's props.
func (s *Schema) HasFields(t *Type, match func(*Field) bool) bool {
	return fieldsHave(t.Fields, match, s, map[*Field]bool{})
}

func fieldsHave(fields []*Field, match func(*Field) bool, s *Schema, seen map[*Field]bool) bool {
	for _, f := range fields {
		if match(f) || fieldHas(f, match, s, seen) {
			return true
		}
	}
	return false
}

func fieldHas(f *Field, match func(*Field) bool, s *Schema, seen map[*Field]bool) bool {
	switch f.Shape.Base {
	case BaseGroup:
		return fieldsHave(f.Fields, match, s, seen)
	case BaseList:
		return f.Of != nil && (match(f.Of) || fieldHas(f.Of, match, s, seen))
	case BaseBlocks:
		for _, bt := range s.blocks {
			if fieldsHave(bt.Props, match, s, seen) {
				return true
			}
		}
	}
	return false
}
