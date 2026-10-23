package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type compiler struct {
	b      *Builder
	reg    *Registry
	errs   errList
	groups map[string]*groupState
	opts   map[*Field]map[string]any
	lk     *blockLookup
	rt     *RichText
}

type groupState struct {
	fields   []*Field
	ok       bool
	visiting bool
	done     bool
}

// scope is one level of sibling fields.
type scope struct {
	c      *compiler
	top    bool   // direct fields of a type
	item   bool   // element of a list
	block  bool   // props of a block type
	prefix string // path prefix of the fields, for messages
	defs   []*fieldDef
}

// Compile validates everything and returns the immutable schema. All problems are returned together.
func (b *Builder) Compile() (*Schema, error) {
	c := &compiler{b: b, reg: b.reg, groups: map[string]*groupState{}, opts: map[*Field]map[string]any{}}
	c.errs.list = slices.Clone(b.errs.list)
	c.lk = &blockLookup{byName: map[string]*BlockType{}}
	s := &Schema{byName: map[string]*Type{}, opts: c.opts, lk: c.lk}
	c.rt = c.compileRichText()
	s.rt = c.rt
	c.compileTokens(s)
	c.compileBlocks(s)
	for _, name := range sortedKeys(b.groups) {
		fields, _ := c.groupFields(name)
		def := b.groups[name]
		s.groups = append(s.groups, &Group{Name: name, Description: def.description, Fields: fields, Pos: def.pos})
	}
	for _, name := range sortedKeys(b.types) {
		t := c.compileType(b.types[name])
		if t.Blockset {
			c.checkBlockset(b.types[name], t)
		}
		s.types = append(s.types, t)
		s.byName[name] = t
	}
	c.checkTypeOptions()
	if err := c.errs.err(); err != nil {
		return nil, err
	}
	data, err := s.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	sum := sha256.Sum256(data)
	s.hash = hex.EncodeToString(sum[:])
	return s, nil
}

// checkTypeOptions runs the module checks of type options once every type is declared.
func (c *compiler) checkTypeOptions() {
	for _, name := range sortedKeys(c.b.types) {
		def := c.b.types[name]
		for _, key := range sortedKeys(def.options) {
			check, ok := c.reg.opts[key]
			if !ok {
				continue
			}
			o := def.options[key]
			if err := check(&TypeBuilder{b: c.b, def: def}, o.value); err != nil {
				c.errs.add(o.pos, "type %q: option %q: %v", name, key, err)
			}
		}
	}
}

func (c *compiler) groupFields(name string) ([]*Field, bool) {
	def, ok := c.b.groups[name]
	if !ok {
		return nil, false
	}
	st := c.groups[name]
	if st == nil {
		st = &groupState{}
		c.groups[name] = st
	}
	if st.done {
		return st.fields, st.ok
	}
	if st.visiting {
		return nil, false
	}
	st.visiting = true
	before := c.errs.len()
	st.fields = c.compileFields(&scope{c: c, defs: def.fields})
	st.ok = c.errs.len() == before
	st.visiting, st.done = false, true
	return st.fields, st.ok
}

func (c *compiler) compileType(def *typeDef) *Type {
	t := &Type{
		Name: def.name, Label: def.label, Description: def.description, Singleton: def.singleton, Page: def.page, Upload: def.upload, Blockset: def.blockset, Review: def.review,
		NamePattern: def.namePattern.s, Pos: def.pos,
		Admin: TypeAdmin{Group: def.adminGroup, Icon: def.adminIcon},
		AI:    TypeAI{Instructions: def.aiInstr},
	}
	if len(def.options) > 0 {
		t.Options = make(map[string]any, len(def.options))
		for k, o := range def.options {
			t.Options[k] = o.value
		}
	}
	if t.Label == "" {
		t.Label = SplitName(def.name)
	}
	t.Fields = c.compileFields(&scope{c: c, top: true, defs: def.fields})
	for _, l := range def.adminList {
		t.Admin.List = append(t.Admin.List, l.s)
		field, cell, _ := strings.Cut(l.s, ":") // "field:cell" names a list cell renderer of the admin extensions
		if strings.Contains(cell, ":") {
			c.errs.add(l.pos, "type %q: admin.list entry %q has more than one \":\" (want field or field:cell)", def.name, l.s)
		}
		c.requireField(def, t, located{s: field, pos: l.pos}, "admin.list")
	}
	for _, l := range def.aiGenerate {
		t.AI.Generate = append(t.AI.Generate, l.s)
		c.requireField(def, t, l, "ai.generate")
	}
	if def.namePattern.s != "" {
		c.checkNamePattern(def, t)
	}
	return t
}

func declared(def *typeDef, name string) bool {
	return slices.ContainsFunc(def.fields, func(f *fieldDef) bool { return f.name == name })
}

func (c *compiler) requireField(def *typeDef, t *Type, l located, what string) {
	if _, ok := t.Field(l.s); !ok && !declared(def, l.s) {
		c.errs.add(l.pos, "type %q: %s names %q, which is not a field of the type", def.name, what, l.s)
	}
}

func (c *compiler) checkNamePattern(def *typeDef, t *Type) {
	err := scanPattern(t.NamePattern, func(_, field string) error {
		if field == "" {
			return nil
		}
		f, ok := t.Field(field)
		switch {
		case !ok && declared(def, field):
		case !ok:
			return fmt.Errorf("name pattern uses %q, which is not a field of the type", field)
		case f.Shape.Base != BaseString && f.Shape.Base != BaseNumber && f.Shape.Base != BaseBoolean:
			return fmt.Errorf("name pattern uses %q, which is not a scalar field", field)
		}
		return nil
	})
	if err != nil {
		c.errs.add(def.namePattern.pos, "type %q: %v", def.name, err)
	}
}

func (c *compiler) compileFields(sc *scope) []*Field {
	fields := make([]*Field, 0, len(sc.defs))
	for i, d := range sc.defs {
		if msg := fieldNameProblem(d.name, sc.top); msg != "" {
			c.errs.add(Pos{File: d.file, Line: d.line}, "%s", msg)
			continue
		}
		if f := c.compileField(sc, i, d); f != nil {
			fields = append(fields, f)
		}
	}
	return fields
}

func (c *compiler) compileField(sc *scope, index int, d *fieldDef) *Field {
	o := &Options{c: c, sc: sc, def: d, index: index}
	n := deref(d.node)
	ents, ok := mappingEntries(n, func(at *yaml.Node, msg string) { o.errAt(at, "%s", msg) })
	if !ok {
		return nil
	}
	o.ents, o.used = ents, make([]bool, len(ents))
	f := &Field{Name: d.name, Pos: Pos{File: d.file, Line: d.line}}
	ft := o.fieldType()
	o.parseCommon(f)
	if ft == nil || o.failed {
		return nil
	}
	o.track = true
	shape, cfg, err := ft.Compile(o)
	o.track = false
	if err != nil {
		o.errAt(n, "%v", err)
	}
	if cfg == nil && !o.failed {
		o.errAt(n, "field type %q returned no behaviour", ft.Name())
	}
	o.reportUnknown(ft)
	if o.failed {
		return nil
	}
	f.Type = ft.Name()
	f.Shape, f.Config, f.Of, f.Fields, f.Group = shape, cfg, o.of, o.fields, o.group
	o.checkPlacement(f)
	o.checkDefault(f)
	if o.failed {
		return nil
	}
	if len(o.opts) > 0 {
		c.opts[f] = o.opts
	}
	return f
}

func (o *Options) fieldType() FieldType {
	i, ok := o.find("type")
	if !ok {
		o.Errorf("", "type is required")
		return nil
	}
	name, ok := asString(o.ents[i].v)
	if !ok {
		o.errAt(o.ents[i].k, "type must be a string")
		return nil
	}
	ft, ok := o.c.reg.types[name]
	if !ok {
		o.errAt(o.ents[i].k, "unknown field type %q (known: %s)", name, strings.Join(o.c.reg.names(), ", "))
		return nil
	}
	return ft
}

func (o *Options) reportUnknown(ft FieldType) {
	known := strings.Join(slices.DeleteFunc(uniqueSorted(o.known), isCommon), ", ")
	for i, e := range o.ents {
		if !o.used[i] {
			o.errAt(e.k, "unknown option %q for type %q (options: %s)", e.key, ft.Name(), known)
		}
	}
}

func (o *Options) parseCommon(f *Field) {
	if v, ok := o.String("label"); ok {
		f.Label = v
	} else {
		f.Label = SplitName(f.Name)
	}
	f.Description, _ = o.String("description")
	f.Required, _ = o.Bool("required")
	f.Translatable, _ = o.Bool("translatable")
	f.Index, _ = o.Bool("index")
	f.Unique, _ = o.Bool("unique")
	if v, ok := o.Raw("default"); ok {
		if v == nil {
			o.Errorf("default", "default must not be null")
		}
		f.Default = v
	}
	f.When = o.condition()
	o.parseAI(f)
	o.parseAdmin(f)
	if o.sc.item {
		for _, k := range []string{"required", "translatable", "index", "unique", "when", "default"} {
			if o.Has(k) {
				o.Errorf(k, "%s is not allowed on list items", k)
			}
		}
	}
}

func (o *Options) parseAI(f *Field) {
	i, ok := o.find("ai")
	if !ok {
		return
	}
	n := deref(o.ents[i].v)
	if b, ok := asBool(n); ok {
		f.AI.Exclude = !b
		return
	}
	ents, ok := mappingEntries(n, func(at *yaml.Node, msg string) { o.errAt(at, "ai: %s", msg) })
	if !ok {
		return
	}
	for _, e := range ents {
		if e.key != "describe" {
			o.errAt(e.k, "unknown key %q in ai (known: describe)", e.key)
			continue
		}
		if s, ok := asString(e.v); ok {
			f.AI.Describe = s
		} else {
			o.errAt(e.k, "ai.describe must be a string")
		}
	}
}

func (o *Options) parseAdmin(f *Field) {
	i, ok := o.find("admin")
	if !ok {
		return
	}
	ents, ok := mappingEntries(o.ents[i].v, func(at *yaml.Node, msg string) { o.errAt(at, "admin: %s", msg) })
	if !ok {
		return
	}
	for _, e := range ents {
		var ok bool
		switch e.key {
		case "widget":
			f.Admin.Widget, ok = asString(e.v)
		case "placeholder":
			f.Admin.Placeholder, ok = asString(e.v)
		case "hidden":
			f.Admin.Hidden, ok = asBool(e.v)
		case "readonly":
			f.Admin.ReadOnly, ok = asBool(e.v)
		default:
			o.errAt(e.k, "unknown key %q in admin (known: hidden, placeholder, readonly, widget)", e.key)
			continue
		}
		if !ok {
			o.errAt(e.k, "admin.%s has the wrong type", e.key)
		}
	}
}

func (o *Options) condition() *Condition {
	i, ok := o.find("when")
	if !ok {
		return nil
	}
	fail := func(at *yaml.Node, msg string) { o.errAt(at, "when: %s", msg) }
	ents, ok := mappingEntries(o.ents[i].v, fail)
	if !ok {
		return nil
	}
	cond := &Condition{}
	ops := 0
	for _, e := range ents {
		switch e.key {
		case "field":
			if s, ok := asString(e.v); ok {
				cond.Field = s
			} else {
				fail(e.k, "field must be a string")
			}
		case "eq", "ne":
			cond.Op, cond.Value = e.key, o.condValue(e)
			ops++
		case "in":
			cond.Op = "in"
			if seq, ok := o.condValue(e).([]any); ok {
				cond.Value = seq
			} else {
				fail(e.k, "in must be a list")
			}
			ops++
		case "empty":
			cond.Op = "empty"
			b, ok := asBool(e.v)
			if !ok {
				fail(e.k, "empty must be true or false")
			}
			cond.Value = b
			ops++
		default:
			fail(e.k, fmt.Sprintf("unknown key %q (known: empty, eq, field, in, ne)", e.key))
		}
	}
	if ops != 1 {
		fail(o.ents[i].k, "needs exactly one of eq, ne, in, empty")
	}
	if !o.siblingBefore(cond.Field) {
		fail(o.ents[i].k, fmt.Sprintf("field %q must be a sibling declared before this field", cond.Field))
	}
	return cond
}

func (o *Options) condValue(e entry) any {
	v, err := toValue(e.v)
	if err != nil {
		o.errAt(e.k, "when.%s: %v", e.key, err)
	}
	return v
}

func (o *Options) siblingBefore(name string) bool {
	for _, d := range o.sc.defs[:o.index] {
		if d.name == name {
			return true
		}
	}
	return false
}

func (o *Options) checkPlacement(f *Field) {
	if f.Unique {
		switch {
		case o.sc.block:
			o.Errorf("unique", "block props cannot be unique")
		case !o.sc.top || f.Shape.Base != BaseString || f.Shape.Format != "" && f.Shape.Format != "slug" && f.Shape.Format != "email":
			o.Errorf("unique", "unique is only allowed on top-level string, slug and email fields")
		}
		f.Index = true
	}
	if f.Translatable && o.sc.block {
		o.Errorf("translatable", "block props are not translatable on their own; set translatable on the blocks field")
	} else if f.Translatable && !o.sc.top {
		o.Errorf("translatable", "translatable is only allowed on top-level fields")
	}
	if f.Index && o.sc.block {
		o.Errorf("index", "block props cannot be indexed")
	} else if f.Index && (!o.sc.top || !indexable(f)) {
		o.Errorf("index", "index is only allowed on top-level scalar, ref and list-of-scalar fields")
	}
}

func scalarIndexable(f *Field) bool {
	switch f.Shape.Base {
	case BaseString, BaseNumber, BaseBoolean, BaseRef, BaseFile:
		return true
	}
	return false
}

func indexable(f *Field) bool {
	if f.Shape.Base == BaseList {
		return f.Of != nil && scalarIndexable(f.Of)
	}
	return scalarIndexable(f)
}

func (o *Options) checkDefault(f *Field) {
	if f.Default == nil {
		return
	}
	var r Report
	f.Config.Validate(f.Default, ModeDraft, &r)
	for _, e := range r.Errors() {
		o.Errorf("default", "default is invalid: %s", e.Message)
	}
}

// scanPattern splits a name pattern into literals and {field} placeholders; fn gets each literal (field "")
// and each placeholder (lit "").
func scanPattern(p string, fn func(lit, field string) error) error {
	for p != "" {
		i := strings.IndexByte(p, '{')
		if i < 0 {
			return fn(p, "")
		}
		j := strings.IndexByte(p[i:], '}')
		if j < 0 {
			return fmt.Errorf("name pattern %q has an unclosed {", p)
		}
		if i > 0 {
			if err := fn(p[:i], ""); err != nil {
				return err
			}
		}
		if err := fn("", p[i+1:i+j]); err != nil {
			return err
		}
		p = p[i+j+1:]
	}
	return nil
}
