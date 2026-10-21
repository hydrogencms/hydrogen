package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// Field projection (?fields=title,seo.title) shows only some fields of the requested items: the envelope (id,
// type, key, locale, version, publishedAt, name and the decorator keys) stays, fields holds the selected ones,
// and a dot path keeps one member of a group. The references an item counts (include, links) are those of the
// selected fields, so nothing is inlined, expanded or looked up for fields that are left out. Items in included
// are not projected. Snapshots are still read whole; the projection saves encoding, transfer, decoding and the
// response cache's memory.

const (
	maxFieldPaths = 64
	maxFieldPath  = 256
)

// Fields is a parsed field projection. A nil *Fields keeps every field.
type Fields struct {
	sel  selection
	refs map[string]bool // types whose selected fields can hold references
}

// selection maps a field name to the members to keep of a group; nil keeps the whole value.
type selection map[string]selection

func (s selection) add(path []string) {
	sub, ok := s[path[0]]
	switch {
	case len(path) == 1:
		s[path[0]] = nil
	case ok && sub == nil:
	default:
		if sub == nil {
			sub = selection{}
			s[path[0]] = sub
		}
		sub.add(path[1:])
	}
}

// parseFields reads a fields value against the types the items can have: every path must name a field of one
// of them. Empty is no projection.
func parseFields(raw string, types []*schema.Type) (*Fields, error) {
	if raw == "" {
		return nil, nil
	}
	f := &Fields{sel: selection{}, refs: map[string]bool{}}
	paths := strings.Split(raw, ",")
	if len(paths) > maxFieldPaths {
		return nil, manage.BadRequest("fields: at most %d paths", maxFieldPaths)
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > maxFieldPath {
			return nil, manage.BadRequest("fields must be a comma-separated list of field names (dot paths into groups)")
		}
		parts := strings.Split(p, ".")
		var why string
		found := false
		for _, t := range types {
			if msg := resolvePath(t, parts); msg == "" {
				found = true
			} else if why == "" || len(types) == 1 {
				why = msg
			}
		}
		if !found {
			if len(types) != 1 {
				why = fmt.Sprintf("no collection of the channel has a field %s", p)
			}
			return nil, manage.BadRequest("fields: %s", why)
		}
		f.sel.add(parts)
	}
	for _, t := range types {
		f.refs[t.Name] = selectsRefs(t.Fields, f.sel)
	}
	return f, nil
}

// resolvePath returns why parts is not a field path of t, "" when it is.
func resolvePath(t *schema.Type, parts []string) string {
	fields := t.Fields
	for i, name := range parts {
		f := fieldNamed(fields, name)
		if f == nil {
			return fmt.Sprintf("type %s has no field %s", t.Name, strings.Join(parts[:i+1], "."))
		}
		if i < len(parts)-1 && f.Shape.Base != schema.BaseGroup {
			return fmt.Sprintf("%s of type %s is not a group", strings.Join(parts[:i+1], "."), t.Name)
		}
		fields = f.Fields
	}
	return ""
}

func fieldNamed(fields []*schema.Field, name string) *schema.Field {
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// selectsRefs reports whether the selected fields can hold item references.
func selectsRefs(fields []*schema.Field, sel selection) bool {
	for _, f := range fields {
		sub, ok := sel[f.Name]
		if !ok {
			continue
		}
		if sub != nil && f.Shape.Base == schema.BaseGroup {
			if selectsRefs(f.Fields, sub) {
				return true
			}
			continue
		}
		if mayRef(f) {
			return true
		}
	}
	return false
}

func mayRef(f *schema.Field) bool {
	switch f.Shape.Base {
	case schema.BaseRef, schema.BaseBlocks, schema.BaseRichText:
		return true
	case schema.BaseString:
		return f.Shape.Format == "url"
	case schema.BaseList:
		return f.Of != nil && mayRef(f.Of)
	case schema.BaseGroup:
		return slices.ContainsFunc(f.Fields, mayRef)
	}
	return false
}

// fields reads ?fields= for items of the given types; none means any type the channel shows.
func (st *state) fields(types ...string) (*Fields, error) {
	raw := st.q.Get("fields")
	if raw == "" {
		return nil, nil
	}
	sc := st.a.o.Schema
	var list []*schema.Type
	if len(types) == 0 {
		for _, t := range sc.Types() {
			if st.m.ch.AllowsCollection(t.Name) {
				list = append(list, t)
			}
		}
	}
	for _, name := range types {
		if t, ok := sc.Type(name); ok {
			list = append(list, t)
		}
	}
	return parseFields(raw, list)
}

// project replaces the payloads of the items with their projection, and their references with those of the
// selected fields. The whole payload stays in full for what reads unselected fields (link patterns).
func (st *state) project(list []*resolved, f *Fields) error {
	if f == nil {
		return nil
	}
	for _, r := range list {
		payload, fields, err := projectPayload(r.payload, f.sel)
		if err != nil {
			return fmt.Errorf("delivery: project payload of %s: %w", r.id, err)
		}
		r.full, r.payload, r.etag, r.refs = r.payload, payload, "", nil
		t, ok := st.a.o.Schema.Type(r.typ)
		if !ok || !f.refs[r.typ] {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(fields, &doc); err != nil {
			return fmt.Errorf("delivery: decode fields of %s: %w", r.id, err)
		}
		for _, ref := range st.a.o.Schema.Refs(t, doc) {
			if id, err := ids.Parse(ref.To); err == nil {
				r.refs = append(r.refs, id)
			}
		}
		r.refs = slices.Compact(slices.SortedFunc(slices.Values(r.refs), cmpUUID))
	}
	return nil
}

// projectPayload keeps the selected members of the payload's fields and returns the new payload and its fields
// object.
func projectPayload(payload []byte, sel selection) (out, fields []byte, err error) {
	out = make([]byte, 0, 256)
	err = eachMember(payload, func(key, val []byte, first bool) error {
		if !first {
			out = append(out, ',')
		}
		out = appendKey(out, key)
		if string(key) != `"fields"` {
			out = append(out, val...)
			return nil
		}
		start := len(out)
		var err error
		out, err = projectObject(out, val, sel)
		fields = out[start:]
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return append(out, '}'), fields, nil
}

func appendKey(dst, key []byte) []byte {
	if len(dst) == 0 {
		dst = append(dst, '{')
	}
	dst = append(dst, key...)
	return append(dst, ':')
}

// projectObject appends the members of obj that sel selects; a group selected by member is projected again.
func projectObject(dst, obj []byte, sel selection) ([]byte, error) {
	dst = append(dst, '{')
	n := 0
	err := eachMember(obj, func(key, val []byte, _ bool) error {
		name, err := keyName(key)
		if err != nil {
			return err
		}
		sub, ok := sel[name]
		if !ok {
			return nil
		}
		if n > 0 {
			dst = append(dst, ',')
		}
		n++
		dst = append(dst, key...)
		dst = append(dst, ':')
		if sub == nil || len(val) == 0 || val[0] != '{' {
			dst = append(dst, val...)
			return nil
		}
		dst, err = projectObject(dst, val, sub)
		return err
	})
	return append(dst, '}'), err
}

// keyName is the text of a quoted member name; stored names never need unescaping, but are unescaped if they do.
func keyName(key []byte) (string, error) {
	if bytes.IndexByte(key, '\\') < 0 {
		return string(key[1 : len(key)-1]), nil
	}
	var s string
	err := json.Unmarshal(key, &s)
	return s, err
}

var errBadJSON = errors.New("malformed JSON")

// eachMember calls fn with the raw key (quoted) and raw value of every member of a JSON object. It scans the
// bytes without decoding values, so a large value that is left out costs one pass and no allocation.
func eachMember(obj []byte, fn func(key, val []byte, first bool) error) error {
	i := skipSpace(obj, 0)
	if i >= len(obj) || obj[i] != '{' {
		return errBadJSON
	}
	i = skipSpace(obj, i+1)
	if i < len(obj) && obj[i] == '}' {
		return nil
	}
	for first := true; ; first = false {
		if i >= len(obj) || obj[i] != '"' {
			return errBadJSON
		}
		end, err := skipString(obj, i)
		if err != nil {
			return err
		}
		key := obj[i:end]
		i = skipSpace(obj, end)
		if i >= len(obj) || obj[i] != ':' {
			return errBadJSON
		}
		i = skipSpace(obj, i+1)
		end, err = skipValue(obj, i)
		if err != nil {
			return err
		}
		if err := fn(key, obj[i:end], first); err != nil {
			return err
		}
		i = skipSpace(obj, end)
		if i >= len(obj) {
			return errBadJSON
		}
		switch obj[i] {
		case ',':
			i = skipSpace(obj, i+1)
		case '}':
			return nil
		default:
			return errBadJSON
		}
	}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	return i
}

// skipString returns the index after the string starting at b[i] (a quote).
func skipString(b []byte, i int) (int, error) {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		}
	}
	return 0, errBadJSON
}

// skipValue returns the index after the value starting at b[i].
func skipValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errBadJSON
	}
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '"':
				end, err := skipString(b, i)
				if err != nil {
					return 0, err
				}
				i = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, errBadJSON
	}
	start := i
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && b[i] != ' ' && b[i] != '\n' && b[i] != '\t' && b[i] != '\r' {
		i++
	}
	if i == start {
		return 0, errBadJSON
	}
	return i, nil
}

// FieldsParam reads ?fields= of a module route's request, for items of any type the channel shows: every path
// must name a field of one of them (400 otherwise). It returns nil when the request has none. A route that
// takes it must accept the parameter in its own checks.
func FieldsParam(r *http.Request) (*Fields, error) {
	st := stateOf(r)
	if st == nil {
		return nil, errors.New("delivery: FieldsParam called outside a delivery route")
	}
	return st.fields()
}

// ItemsFields is Items showing only the fields f selects (every field when f is nil); the links of each item are
// those of its selected fields.
func ItemsFields(r *http.Request, locale string, f *Fields, list ...ids.UUID) ([]json.RawMessage, error) {
	return items(r, locale, f, list)
}
