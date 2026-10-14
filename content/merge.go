package content

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"

	"github.com/hydrogencms/hydrogen/schema"
)

// cloneValue deep-copies decoded JSON (objects, arrays and scalars).
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneDoc(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

func cloneDoc(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

// applyPatch applies an RFC 7396 JSON merge patch to dst in place: nil removes a key, objects merge
// recursively, everything else (arrays included) replaces. Patch values are copied, never aliased.
func applyPatch(dst, patch map[string]any) {
	for k, pv := range patch {
		switch p := pv.(type) {
		case nil:
			delete(dst, k)
		case map[string]any:
			target, ok := dst[k].(map[string]any)
			if !ok {
				target = make(map[string]any, len(p))
				dst[k] = target
			}
			applyPatch(target, p)
		default:
			dst[k] = cloneValue(pv)
		}
	}
}

// mergeParts returns shared and locale fields as one document (a new top-level map).
func mergeParts(shared, locale map[string]any) map[string]any {
	out := make(map[string]any, len(shared)+len(locale))
	for k, v := range shared {
		out[k] = v
	}
	for k, v := range locale {
		out[k] = v
	}
	return out
}

// splitDoc divides doc by the type's fields: non-translatable fields into shared, translatable ones into
// locale. Keys the type does not declare are dropped.
func splitDoc(t *schema.Type, doc map[string]any) (shared, locale map[string]any) {
	shared, locale = map[string]any{}, map[string]any{}
	for _, f := range t.Fields {
		v, ok := doc[f.Name]
		switch {
		case !ok:
		case f.Translatable:
			locale[f.Name] = v
		default:
			shared[f.Name] = v
		}
	}
	return shared, locale
}

// equalJSON compares decoded JSON semantically: numbers by value, objects ignoring key order.
func equalJSON(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && equalDocs(x, y)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return equalNumbers(a, b)
}

func equalDocs(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !equalJSON(av, bv) {
			return false
		}
	}
	return true
}

func equalNumbers(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		if y, ok := b.(json.Number); ok && x == y {
			return true
		}
	}
	fa, okA := numberValue(a)
	fb, okB := numberValue(b)
	return okA && okB && fa == fb
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
		return f, err == nil
	case float64:
		return x, !math.IsNaN(x)
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

// encodeDoc encodes a document with sorted keys and without HTML escaping.
func encodeDoc(m map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// fillSlugs sets empty slug fields that name a source field to the slug of the source's text.
func (s *Service) fillSlugs(t *schema.Type, doc map[string]any) {
	sources := s.slugs[t.Name]
	if len(sources) == 0 {
		return
	}
	for _, f := range t.Fields {
		src := sources[f.Name]
		if cur, _ := doc[f.Name].(string); src == "" || cur != "" {
			continue
		}
		text, _ := doc[src].(string)
		if slug := schema.Slugify(text); slug != "" {
			doc[f.Name] = slug
		}
	}
}
