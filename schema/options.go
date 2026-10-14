package schema

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Options gives a FieldType access to its field spec. Getters return (value, true) when the key is present
// and valid; a present but invalid key is reported (with its position) and returns false. Every key a
// getter is asked about counts as known; keys never asked about fail as "unknown option".
type Options struct {
	c      *compiler
	sc     *scope
	def    *fieldDef
	index  int
	ents   []entry
	used   []bool
	known  []string
	track  bool
	failed bool
	opts   map[string]any

	of     *Field
	fields []*Field
	group  string
}

func (o *Options) find(key string) (int, bool) {
	if o.track {
		o.known = append(o.known, key)
	}
	for i := range o.ents {
		if o.ents[i].key == key {
			o.used[i] = true
			return i, true
		}
	}
	return 0, false
}

func (o *Options) fieldPath() string { return o.sc.prefix + o.def.name }

func (o *Options) errAt(n *yaml.Node, format string, args ...any) {
	o.failed = true
	o.c.errs.add(Pos{File: o.def.file, Line: n.Line}, "field %q: %s", o.fieldPath(), fmt.Sprintf(format, args...))
}

func (o *Options) record(key string, v any) {
	if isCommon(key) {
		return
	}
	if o.opts == nil {
		o.opts = map[string]any{}
	}
	o.opts[key] = v
}

func option[T any](o *Options, key, want string, conv func(*yaml.Node) (T, bool)) (T, bool) {
	var zero T
	i, ok := o.find(key)
	if !ok {
		return zero, false
	}
	v, ok := conv(o.ents[i].v)
	if !ok {
		o.errAt(o.ents[i].k, "%s must be %s", key, want)
		return zero, false
	}
	o.record(key, v)
	return v, true
}

// Has reports whether the spec has key.
func (o *Options) Has(key string) bool {
	for _, e := range o.ents {
		if e.key == key {
			return true
		}
	}
	return false
}

func (o *Options) String(key string) (string, bool) { return option(o, key, "a string", asString) }
func (o *Options) Int(key string) (int, bool)       { return option(o, key, "a whole number", asInt) }
func (o *Options) Float(key string) (float64, bool) { return option(o, key, "a number", asFloat) }
func (o *Options) Bool(key string) (bool, bool)     { return option(o, key, "true or false", asBool) }
func (o *Options) Strings(key string) ([]string, bool) {
	return option(o, key, "a list of strings", asStrings)
}

// Raw returns the value of key as JSON-compatible data (numbers are json.Number).
func (o *Options) Raw(key string) (any, bool) {
	i, ok := o.find(key)
	if !ok {
		return nil, false
	}
	v, err := toValue(o.ents[i].v)
	if err != nil {
		o.errAt(o.ents[i].k, "%s: %v", key, err)
		return nil, false
	}
	o.record(key, v)
	return v, true
}

// Errorf reports a problem at key's position (at the field when key is empty or absent).
func (o *Options) Errorf(key, format string, args ...any) {
	n := &yaml.Node{Line: o.def.line}
	for _, e := range o.ents {
		if e.key == key {
			n = e.k
		}
	}
	o.errAt(n, format, args...)
}

// SiblingType returns the declared `type` of a sibling field.
func (o *Options) SiblingType(name string) (string, bool) {
	for _, d := range o.sc.defs {
		if d.name != name {
			continue
		}
		ents, _ := mappingEntries(d.node, func(*yaml.Node, string) {})
		for _, e := range ents {
			if e.key == "type" {
				return asString(e.v)
			}
		}
	}
	return "", false
}

// TypeExists reports whether the schema defines a type called name.
func (o *Options) TypeExists(name string) bool {
	_, ok := o.c.b.types[name]
	return ok
}

// SubField compiles the field spec at key (e.g. a list's `of`). It returns nil when key is absent or its
// problems were already reported.
func (o *Options) SubField(key string) *Field {
	i, ok := o.find(key)
	if !ok {
		return nil
	}
	e := o.ents[i]
	def := &fieldDef{node: e.v, file: o.def.file, line: e.k.Line}
	child := &scope{c: o.c, item: true, prefix: o.fieldPath() + "[]", defs: []*fieldDef{def}}
	f := o.c.compileField(child, 0, def)
	if f == nil {
		o.failed = true
		return nil
	}
	o.of = f
	return f
}

// SubFields compiles the mapping of field specs at key (e.g. a group's `fields`).
func (o *Options) SubFields(key string) []*Field {
	i, ok := o.find(key)
	if !ok {
		return nil
	}
	e := o.ents[i]
	s := src{file: o.def.file, errs: &o.c.errs}
	defs := s.fieldDefs(e.v)
	child := &scope{c: o.c, prefix: o.fieldPath() + ".", defs: defs}
	o.fields = o.c.compileFields(child)
	if o.fields == nil {
		o.fields = []*Field{}
	}
	return o.fields
}

// UseGroup resolves the named group at key and returns its fields.
func (o *Options) UseGroup(key string) []*Field {
	i, ok := o.find(key)
	if !ok {
		return nil
	}
	name, ok := asString(o.ents[i].v)
	if !ok {
		o.errAt(o.ents[i].k, "%s must be a group name", key)
		return nil
	}
	fields, ok := o.c.groupFields(name)
	if !ok {
		if _, defined := o.c.b.groups[name]; !defined {
			o.errAt(o.ents[i].k, "unknown group %q (defined: %s)", name, strings.Join(sortedKeys(o.c.b.groups), ", "))
		} else {
			o.errAt(o.ents[i].k, "group %q is invalid or uses itself", name)
		}
		return nil
	}
	o.group = name
	o.fields = fields
	return fields
}
