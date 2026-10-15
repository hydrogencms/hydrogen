package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Spec is a field, type or group spec in the same structure as a YAML schema file, for programmatic additions.
// Maps are walked in sorted key order, so `fields` given as a map are declared alphabetically; call
// TypeBuilder.AddField for a specific order.
type Spec map[string]any

var (
	nameRE     = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	yamlLineRE = regexp.MustCompile(`^yaml: (?:unmarshal errors:\n\s*)?line (\d+): (.*)`)
)

// isReserved reports whether a top-level field may not use name: it is part of every item's envelope.
func isReserved(name string) bool {
	switch name {
	case "id", "type", "key", "locale", "status", "version", "name", "createdAt", "updatedAt", "publishedAt":
		return true
	}
	return false
}

const maxName = 64

// Builder collects schema definitions from YAML files and programmatic additions, then compiles them.
type Builder struct {
	reg    *Registry
	origin string
	types  map[string]*typeDef
	groups map[string]*groupDef
	blocks map[string]*blockDef
	tokens map[string]*tokenDef
	errs   errList
}

func NewBuilder(reg *Registry) *Builder {
	return &Builder{reg: reg, origin: "(code)", types: map[string]*typeDef{}, groups: map[string]*groupDef{},
		blocks: map[string]*blockDef{}, tokens: map[string]*tokenDef{}}
}

// SetOrigin names the source of subsequent programmatic additions, e.g. "module seo"; it is the Pos.File of
// what they define.
func (b *Builder) SetOrigin(name string) { b.origin = name }

// LoadDir loads every *.yaml and *.yml file under dir, recursively, sorted by path. A missing dir is an empty
// schema.
func (b *Builder) LoadDir(dir string) error {
	st, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("schema: %s is not a directory", dir)
	}
	return b.load(os.DirFS(dir), ".", func(p string) string { return filepath.Join(dir, filepath.FromSlash(p)) })
}

// LoadFS loads every *.yaml and *.yml file under root in fsys; positions show paths relative to fsys.
func (b *Builder) LoadFS(fsys fs.FS, root string) error {
	return b.load(fsys, root, func(p string) string { return p })
}

func (b *Builder) load(fsys fs.FS, root string, display func(string) string) error {
	var files []string
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ext := path.Ext(p); !d.IsDir() && (ext == ".yaml" || ext == ".yml") {
			files = append(files, p)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	slices.Sort(files)
	var errs []error
	for _, p := range files {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("schema: %w", err)
		}
		if err := b.addFile(display(p), data); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addFile parses one file; syntax errors are returned and also reported by Compile.
func (b *Builder) addFile(file string, data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		e := &Error{Pos: Pos{File: file}, Message: err.Error()}
		if m := yamlLineRE.FindStringSubmatch(err.Error()); m != nil {
			e.Pos.Line = atoiOr0(m[1])
			e.Message = m[2]
		}
		b.errs.list = append(b.errs.list, e)
		return e
	}
	if doc.Kind == 0 || len(doc.Content) == 0 || isNull(doc.Content[0]) {
		return nil
	}
	s := src{file: file, errs: &b.errs}
	ents, ok := s.mapping(doc.Content[0], "schema file")
	if !ok {
		return nil
	}
	for _, e := range ents {
		switch e.key {
		case "types":
			b.addNamed(s, e.v, "types", func(k, v *yaml.Node) { b.registerType(s, k, v) })
		case "groups":
			b.addNamed(s, e.v, "groups", func(k, v *yaml.Node) { b.registerGroup(s, k, v) })
		case "blocks":
			b.addNamed(s, e.v, "blocks", func(k, v *yaml.Node) { b.registerBlock(s, k, v) })
		case "tokens":
			b.addNamed(s, e.v, "tokens", func(k, v *yaml.Node) { b.registerTokens(s, k, v) })
		default:
			s.errorf(e.k, "unknown key %q (known: blocks, groups, tokens, types)", e.key)
		}
	}
	return nil
}

func (b *Builder) addNamed(s src, n *yaml.Node, what string, fn func(k, v *yaml.Node)) {
	if isNull(deref(n)) {
		return
	}
	ents, _ := s.mapping(n, what)
	for _, e := range ents {
		fn(e.k, e.v)
	}
}

func (b *Builder) registerType(s src, k, v *yaml.Node) {
	def := parseType(s, b.reg, k, v)
	if def == nil {
		return
	}
	if old, dup := b.types[def.name]; dup {
		s.errorf(k, "type %q is already defined at %s", def.name, old.pos)
		return
	}
	b.types[def.name] = def
}

func (b *Builder) registerGroup(s src, k, v *yaml.Node) {
	def := parseGroup(s, k, v)
	if def == nil {
		return
	}
	if old, dup := b.groups[def.name]; dup {
		s.errorf(k, "group %q is already defined at %s", def.name, old.pos)
		return
	}
	b.groups[def.name] = def
}

func (b *Builder) registerBlock(s src, k, v *yaml.Node) {
	def := parseBlock(s, k, v)
	if def == nil {
		return
	}
	if old, dup := b.blocks[def.name]; dup {
		s.errorf(k, "block %q is already defined at %s", def.name, old.pos)
		return
	}
	b.blocks[def.name] = def
}

func (b *Builder) registerTokens(s src, k, v *yaml.Node) {
	def := parseTokens(s, k, v)
	if def == nil {
		return
	}
	if old, dup := b.tokens[def.name]; dup {
		s.errorf(k, "token set %q is already defined at %s", def.name, old.pos)
		return
	}
	b.tokens[def.name] = def
}

// AddBlock adds a block type defined in code, with the same spec as in a schema file.
func (b *Builder) AddBlock(name string, spec Spec) error {
	_, existed := b.blocks[name]
	return b.addSpec(name, spec, "block", b.registerBlock, func() {
		if !existed {
			delete(b.blocks, name)
		}
	})
}

// AddTokens adds a design token set defined in code.
func (b *Builder) AddTokens(name string, values []string) error {
	v, err := toNode(values)
	if err != nil {
		return fmt.Errorf("schema: token set %q: %w", name, err)
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
	var local errList
	b.registerTokens(src{file: b.origin, errs: &local}, k, v)
	return local.err()
}

// AddType adds a type defined in code. Nothing is added when the spec has problems.
func (b *Builder) AddType(name string, spec Spec) error {
	_, existed := b.types[name]
	return b.addSpec(name, spec, "type", b.registerType, func() {
		if !existed {
			delete(b.types, name)
		}
	})
}

// AddGroup adds a reusable field group defined in code.
func (b *Builder) AddGroup(name string, spec Spec) error {
	_, existed := b.groups[name]
	return b.addSpec(name, spec, "group", b.registerGroup, func() {
		if !existed {
			delete(b.groups, name)
		}
	})
}

func (b *Builder) addSpec(name string, spec Spec, what string, register func(src, *yaml.Node, *yaml.Node), undo func()) error {
	v, err := toNode(spec)
	if err != nil {
		return fmt.Errorf("schema: %s %q: %w", what, name, err)
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
	var local errList
	register(src{file: b.origin, errs: &local}, k, v)
	if err := local.err(); err != nil {
		undo()
		return err
	}
	return nil
}

// Types lists the types collected so far, sorted by name.
func (b *Builder) Types() []*TypeBuilder {
	out := make([]*TypeBuilder, 0, len(b.types))
	for _, name := range sortedKeys(b.types) {
		out = append(out, &TypeBuilder{b: b, def: b.types[name]})
	}
	return out
}

func (b *Builder) Type(name string) (*TypeBuilder, bool) {
	def, ok := b.types[name]
	if !ok {
		return nil, false
	}
	return &TypeBuilder{b: b, def: def}, true
}

// TypeBuilder is a type that can still change before Compile; module transforms use it.
type TypeBuilder struct {
	b   *Builder
	def *typeDef
}

func (t *TypeBuilder) Name() string    { return t.def.name }
func (t *TypeBuilder) Page() bool      { return t.def.page }
func (t *TypeBuilder) Singleton() bool { return t.def.singleton }

// Type returns another type of the same builder, for checks that relate types to each other.
func (t *TypeBuilder) Type(name string) (*TypeBuilder, bool) { return t.b.Type(name) }

// Option returns the value of a module type option set on this type (JSON-compatible; numbers are json.Number).
func (t *TypeBuilder) Option(name string) (any, bool) {
	o, ok := t.def.options[name]
	return o.value, ok
}

// SetOption sets a module type option registered with Registry.TypeOption, replacing an earlier value. The
// value is checked at Compile, like one from YAML.
func (t *TypeBuilder) SetOption(name string, v any) error {
	if _, ok := t.b.reg.opts[name]; !ok {
		return fmt.Errorf("%s: type %q: unknown type option %q (registered: %s)", t.b.origin, t.def.name, name,
			strings.Join(sortedKeys(t.b.reg.opts), ", "))
	}
	n, err := toNode(v)
	if err != nil {
		return fmt.Errorf("%s: type %q option %q: %w", t.b.origin, t.def.name, name, err)
	}
	val, err := toValue(n)
	if err != nil {
		return fmt.Errorf("%s: type %q option %q: %w", t.b.origin, t.def.name, name, err)
	}
	if t.def.options == nil {
		t.def.options = map[string]optionDef{}
	}
	t.def.options[name] = optionDef{value: val, pos: Pos{File: t.b.origin}}
	return nil
}

func (t *TypeBuilder) HasField(name string) bool {
	return slices.ContainsFunc(t.def.fields, func(f *fieldDef) bool { return f.name == name })
}

// AddField appends a field after the existing ones.
func (t *TypeBuilder) AddField(name string, spec Spec) error {
	if msg := fieldNameProblem(name, true); msg != "" {
		return fmt.Errorf("%s: type %q: %s", t.b.origin, t.def.name, msg)
	}
	if t.HasField(name) {
		return fmt.Errorf("%s: type %q already has field %q", t.b.origin, t.def.name, name)
	}
	n, err := toNode(spec)
	if err != nil {
		return fmt.Errorf("%s: type %q field %q: %w", t.b.origin, t.def.name, name, err)
	}
	t.def.fields = append(t.def.fields, &fieldDef{name: name, node: n, file: t.b.origin})
	return nil
}

// fieldNameProblem returns why name is not a valid field name, or "".
func fieldNameProblem(name string, top bool) string {
	switch {
	case len(name) > maxName || !nameRE.MatchString(name):
		return fmt.Sprintf("field name %q must match %s and be at most %d characters", name, nameRE, maxName)
	case top && isReserved(name):
		return fmt.Sprintf("field name %q is reserved", name)
	}
	return ""
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

func atoiOr0(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
