package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	sharedBlock   = "shared"
	maxBlockDepth = 8    // slot levels below a top-level block
	maxDocBlocks  = 1000 // blocks in one document
	walkDepth     = maxBlockDepth + 2
)

var tokenValueRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// BlockType is a compiled block type: typed props and named slots that hold child blocks.
type BlockType struct {
	Name        string
	Label       string
	Description string
	Icon        string
	Props       []*Field
	Slots       []Slot
	Pos         Pos
}

func (b *BlockType) Prop(name string) (*Field, bool) {
	for _, f := range b.Props {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

func (b *BlockType) Slot(name string) (*Slot, bool) {
	for i := range b.Slots {
		if b.Slots[i].Name == name {
			return &b.Slots[i], true
		}
	}
	return nil, false
}

// Slot is a named place for child blocks. Max 0 means no limit.
type Slot struct {
	Name        string
	Label       string
	Description string
	Allow       []string
	Min, Max    int
}

// TokenSet is a design token set: names the frontend maps to styles.
type TokenSet struct {
	Name   string
	Values []string
	Pos    Pos
}

type blockLookup struct{ byName map[string]*BlockType }

// Blocks lists the block types sorted by name. The built-in shared block is listed when the schema has a
// blockset type.
func (s *Schema) Blocks() []*BlockType { return s.blocks }

func (s *Schema) Block(name string) (*BlockType, bool) {
	b, ok := s.lk.byName[name]
	return b, ok
}

func (s *Schema) Tokens() []*TokenSet { return s.tokens }

func (s *Schema) Token(name string) (*TokenSet, bool) {
	for _, t := range s.tokens {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// BlocksField returns the single blocks field of a blockset type.
func (t *Type) BlocksField() (*Field, bool) {
	for _, f := range t.Fields {
		if f.Shape.Base == BaseBlocks {
			return f, true
		}
	}
	return nil, false
}

// MaySharedBlocks reports whether a blocks field can hold shared blocks, directly or inside slots.
func (s *Schema) MaySharedBlocks(f *Field) bool {
	if f.Shape.Base != BaseBlocks {
		return false
	}
	seen := map[string]bool{}
	queue := slices.Clone(f.Shape.Allow)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n == sharedBlock {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		if bt, ok := s.lk.byName[n]; ok {
			for _, sl := range bt.Slots {
				queue = append(queue, sl.Allow...)
			}
		}
	}
	return false
}

// ---- definitions

type blockDef struct {
	name        string
	pos         Pos
	label       string
	description string
	icon        string
	props       []*fieldDef
	slots       []*slotDef
}

type slotDef struct {
	name        string
	pos         Pos
	label       string
	description string
	allow       []located
	min, max    int
}

type tokenDef struct {
	name   string
	pos    Pos
	values []located
}

func parseBlock(s src, k, v *yaml.Node) *blockDef {
	name, ok := s.defName(k, "block")
	if !ok {
		return nil
	}
	if name == sharedBlock {
		s.errorf(k, "block name %q is reserved for the built-in shared block", name)
		return nil
	}
	def := &blockDef{name: name, pos: s.pos(k)}
	ents, _ := s.mapping(v, fmt.Sprintf("block %q", name))
	for _, e := range ents {
		switch e.key {
		case "label":
			s.setString(&def.label, e)
		case "description":
			s.setString(&def.description, e)
		case "icon":
			s.setString(&def.icon, e)
		case "props":
			def.props = s.fieldDefs(e.v)
		case "slots":
			def.slots = s.slotDefs(e.v)
		default:
			s.errorf(e.k, "unknown key %q in block %q (known: description, icon, label, props, slots)", e.key, name)
		}
	}
	return def
}

func (s src) slotDefs(n *yaml.Node) []*slotDef {
	if isNull(deref(n)) {
		return nil
	}
	ents, _ := s.mapping(n, "slots")
	var out []*slotDef
	for _, e := range ents {
		if len(e.key) > maxName || !nameRE.MatchString(e.key) {
			s.errorf(e.k, "slot name %q must match %s and be at most %d characters", e.key, nameRE, maxName)
			continue
		}
		def := &slotDef{name: e.key, pos: s.pos(e.k)}
		sub, _ := s.mapping(e.v, fmt.Sprintf("slot %q", e.key))
		hasAllow := false
		for _, a := range sub {
			switch a.key {
			case "label":
				s.setString(&def.label, a)
			case "description":
				s.setString(&def.description, a)
			case "allow":
				hasAllow = true
				def.allow = s.locatedList(a)
			case "min", "max":
				v, ok := asInt(a.v)
				switch {
				case !ok || v < 0:
					s.errorf(a.k, "%s must be a whole number, zero or more", a.key)
				case a.key == "min":
					def.min = v
				case v == 0:
					s.errorf(a.k, "max must be at least 1")
				default:
					def.max = v
				}
			default:
				s.errorf(a.k, "unknown key %q in slot %q (known: allow, description, label, max, min)", a.key, e.key)
			}
		}
		switch {
		case !hasAllow:
			s.errorf(e.k, "slot %q: allow is required", e.key)
		case len(def.allow) == 0:
			s.errorf(e.k, "slot %q: allow must name at least one block type", e.key)
		}
		if def.max > 0 && def.min > def.max {
			s.errorf(e.k, "slot %q: min must not exceed max", e.key)
		}
		out = append(out, def)
	}
	return out
}

func parseTokens(s src, k, v *yaml.Node) *tokenDef {
	name, ok := s.defName(k, "token set")
	if !ok {
		return nil
	}
	def := &tokenDef{name: name, pos: s.pos(k)}
	n := deref(v)
	if n.Kind != yaml.SequenceNode {
		s.errorf(k, "token set %q must be a list of names", name)
		return nil
	}
	seen := map[string]bool{}
	for _, c := range n.Content {
		c = deref(c)
		if c.Kind != yaml.ScalarNode || isNull(c) {
			s.errorf(c, "token set %q: values must be plain names", name)
			continue
		}
		switch {
		case len(c.Value) > maxName || !tokenValueRE.MatchString(c.Value):
			s.errorf(c, "token set %q: value %q must match %s and be at most %d characters", name, c.Value, tokenValueRE, maxName)
		case seen[c.Value]:
			s.errorf(c, "token set %q: value %q is listed twice", name, c.Value)
		default:
			seen[c.Value] = true
			def.values = append(def.values, located{c.Value, s.pos(c)})
		}
	}
	if len(def.values) == 0 && len(n.Content) == 0 {
		s.errorf(k, "token set %q needs at least one value", name)
	}
	return def
}

// ---- compile

func (c *compiler) hasShared() bool {
	for _, d := range c.b.types {
		if d.blockset {
			return true
		}
	}
	return false
}

func (c *compiler) blockExists(name string) bool {
	if name == sharedBlock {
		return c.hasShared()
	}
	_, ok := c.b.blocks[name]
	return ok
}

func (c *compiler) compileTokens(s *Schema) {
	for _, name := range sortedKeys(c.b.tokens) {
		def := c.b.tokens[name]
		ts := &TokenSet{Name: name, Pos: def.pos}
		for _, v := range def.values {
			ts.Values = append(ts.Values, v.s)
		}
		s.tokens = append(s.tokens, ts)
	}
}

func (c *compiler) compileBlocks(s *Schema) {
	for _, name := range sortedKeys(c.b.blocks) {
		bt := c.compileBlock(c.b.blocks[name])
		s.blocks = append(s.blocks, bt)
		s.lk.byName[name] = bt
	}
	if c.hasShared() {
		var to []string
		for _, name := range sortedKeys(c.b.types) {
			if c.b.types[name].blockset {
				to = append(to, name)
			}
		}
		item := &Field{Name: "item", Label: "Item", Description: "The blockset item whose blocks this block shows.", Type: "ref",
			Required: true, Shape: Shape{Base: BaseRef, To: to}, Config: &refConfig{to: to, onDelete: "block"}}
		bt := &BlockType{Name: sharedBlock, Label: "Shared block", Icon: "link", Props: []*Field{item},
			Description: "Blocks kept in one place and used by many documents."}
		s.blocks = append(s.blocks, bt)
		s.lk.byName[sharedBlock] = bt
		slices.SortFunc(s.blocks, func(a, b *BlockType) int { return strings.Compare(a.Name, b.Name) })
	}
}

func (c *compiler) compileBlock(def *blockDef) *BlockType {
	bt := &BlockType{Name: def.name, Label: def.label, Description: def.description, Icon: def.icon, Pos: def.pos}
	if bt.Label == "" {
		bt.Label = SplitName(def.name)
	}
	bt.Props = c.compileFields(&scope{c: c, block: true, prefix: def.name + ".props.", defs: def.props})
	for _, sd := range def.slots {
		sl := Slot{Name: sd.name, Label: sd.label, Description: sd.description, Min: sd.min, Max: sd.max}
		if sl.Label == "" {
			sl.Label = SplitName(sd.name)
		}
		seen := map[string]bool{}
		for _, a := range sd.allow {
			switch {
			case !c.blockExists(a.s):
				c.errs.add(a.pos, "block %q: slot %q allows unknown block type %q", def.name, sd.name, a.s)
			case seen[a.s]:
				c.errs.add(a.pos, "block %q: slot %q lists %q twice", def.name, sd.name, a.s)
			default:
				seen[a.s] = true
				sl.Allow = append(sl.Allow, a.s)
			}
		}
		bt.Slots = append(bt.Slots, sl)
	}
	return bt
}

// checkBlockset verifies a blockset type: one blocks field that cannot reach a shared block.
func (c *compiler) checkBlockset(def *typeDef, t *Type) {
	var fields []*Field
	for _, f := range t.Fields {
		if f.Shape.Base == BaseBlocks {
			fields = append(fields, f)
		}
	}
	if len(fields) != 1 {
		c.errs.add(def.pos, "type %q: a blockset type needs exactly one blocks field (found %d)", def.name, len(fields))
		return
	}
	if c.mayReachShared(fields[0].Shape.Allow) {
		c.errs.add(fields[0].Pos, "type %q: blocks of a blockset type must not contain shared blocks, directly or through slots", def.name)
	}
}

func (c *compiler) mayReachShared(allow []string) bool {
	seen := map[string]bool{}
	queue := slices.Clone(allow)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n == sharedBlock {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		if d, ok := c.b.blocks[n]; ok {
			for _, sd := range d.slots {
				for _, a := range sd.allow {
					queue = append(queue, a.s)
				}
			}
		}
	}
	return false
}

// ---- field types

type blocksType struct{}

func (blocksType) Name() string { return "blocks" }

func (blocksType) Compile(o *Options) (Shape, Config, error) {
	switch {
	case o.sc.block:
		o.Errorf("", "blocks cannot be block props; nest blocks through slots")
	case !o.sc.top:
		o.Errorf("", "blocks fields must be top-level fields of a type, not inside lists or groups")
	}
	allow, ok := o.Strings("allow")
	switch {
	case !o.Has("allow"):
		o.Errorf("", "allow is required")
	case ok && len(allow) == 0:
		o.Errorf("allow", "allow must name at least one block type")
	}
	seen := map[string]bool{}
	for _, name := range allow {
		switch {
		case !o.c.blockExists(name) && name == sharedBlock:
			o.Errorf("allow", "allow names shared, but no type has blockset: true")
		case !o.c.blockExists(name):
			o.Errorf("allow", "allow names unknown block type %q", name)
		case seen[name]:
			o.Errorf("allow", "allow lists %q twice", name)
		}
		seen[name] = true
	}
	cfg := &blocksConfig{allow: allow, lim: readLimits(o, 0), lk: o.c.lk}
	return Shape{Base: BaseBlocks, Allow: allow}, cfg, nil
}

type blocksConfig struct {
	allow []string
	lim   limits
	lk    *blockLookup
}

func (c *blocksConfig) Validate(v any, mode Mode, r *Report) {
	list, ok := v.([]any)
	if !ok {
		typeErr(r, "a list of blocks")
		return
	}
	if c.lim.hasMax && len(list) > c.lim.max {
		r.Fail("max_items", "must have at most %d blocks", c.lim.max)
	}
	if mode == ModePublish && len(list) < c.lim.min {
		r.Fail("min_items", "must have at least %d blocks", c.lim.min)
	}
	c.validateList(list, c.allow, 0, mode, r)
}

func (c *blocksConfig) validateList(list []any, allow []string, depth int, mode Mode, r *Report) {
	for i, e := range list {
		if r.full() {
			return
		}
		r.EnterIndex(i)
		c.validateBlock(e, allow, depth, mode, r)
		r.Leave()
	}
}

func (c *blocksConfig) validateBlock(v any, allow []string, depth int, mode Mode, r *Report) {
	m, ok := v.(map[string]any)
	if !ok {
		typeErr(r, "a block object")
		return
	}
	if r.blocks++; r.blocks > maxDocBlocks {
		if r.blocks == maxDocBlocks+1 {
			r.Fail("max_blocks", "a document holds at most %d blocks", maxDocBlocks)
		}
		return
	}
	for _, k := range sortedKeys(m) {
		switch k {
		case "id", "type", "props", "slots":
		default:
			r.Enter(k)
			r.Fail("unknown_field", "unknown field %q", k)
			r.Leave()
		}
	}
	c.validateID(m, r)
	r.Enter("type")
	typ, ok := m["type"].(string)
	bt := c.lk.byName[typ]
	switch {
	case m["type"] == nil:
		r.Fail("required", "is required")
	case !ok:
		typeErr(r, "a string")
	case bt == nil || !slices.Contains(allow, typ):
		r.Fail("block_type", "block type %q is not allowed here (allowed: %s)", typ, strings.Join(allow, ", "))
		bt = nil
	}
	r.Leave()
	if bt == nil {
		return
	}
	c.validateProps(bt, m, mode, r)
	c.validateSlots(bt, m, depth, mode, r)
}

func (c *blocksConfig) validateID(m map[string]any, r *Report) {
	r.Enter("id")
	defer r.Leave()
	raw, present := m["id"]
	id, ok := raw.(string)
	switch {
	case !present || raw == nil:
		r.Fail("required", "is required")
	case !ok:
		typeErr(r, "a string")
	case !isUUIDv7(id):
		r.Fail("format", "must be a UUIDv7")
	case r.blockIDs[id]:
		r.Fail("duplicate_id", "block id %s is used more than once", id)
	default:
		if r.blockIDs == nil {
			r.blockIDs = map[string]bool{}
		}
		r.blockIDs[id] = true
	}
}

func isUUIDv7(s string) bool {
	return isUUID(s) && s[14] == '7' && strings.IndexByte("89abAB", s[19]) >= 0
}

func (c *blocksConfig) validateProps(bt *BlockType, m map[string]any, mode Mode, r *Report) {
	r.Enter("props")
	defer r.Leave()
	raw, present := m["props"]
	props := map[string]any{}
	if present {
		pm, ok := raw.(map[string]any)
		if !ok {
			typeErr(r, "an object")
			return
		}
		props = pm
	}
	validateFields(bt.Props, props, mode, r)
}

func (c *blocksConfig) validateSlots(bt *BlockType, m map[string]any, depth int, mode Mode, r *Report) {
	raw, present := m["slots"]
	slots := map[string]any{}
	if present {
		sm, ok := raw.(map[string]any)
		if !ok {
			r.Enter("slots")
			typeErr(r, "an object")
			r.Leave()
			return
		}
		slots = sm
	}
	r.Enter("slots")
	defer r.Leave()
	for _, k := range sortedKeys(slots) {
		if _, ok := bt.Slot(k); !ok {
			r.Enter(k)
			r.Fail("unknown_field", "unknown slot %q", k)
			r.Leave()
		}
	}
	for _, sl := range bt.Slots {
		if r.full() {
			return
		}
		r.Enter(sl.Name)
		c.validateSlot(sl, slots[sl.Name], slots, depth, mode, r)
		r.Leave()
	}
}

func (c *blocksConfig) validateSlot(sl Slot, v any, slots map[string]any, depth int, mode Mode, r *Report) {
	if _, present := slots[sl.Name]; !present {
		if mode == ModePublish && sl.Min > 0 {
			r.Fail("min_items", "must have at least %d blocks", sl.Min)
		}
		return
	}
	list, ok := v.([]any)
	if !ok {
		if v == nil {
			r.Fail("type", "must not be null")
		} else {
			typeErr(r, "a list of blocks")
		}
		return
	}
	if sl.Max > 0 && len(list) > sl.Max {
		r.Fail("max_items", "must have at most %d blocks", sl.Max)
	}
	if mode == ModePublish && len(list) < sl.Min {
		r.Fail("min_items", "must have at least %d blocks", sl.Min)
	}
	if depth+1 > maxBlockDepth {
		if len(list) > 0 {
			r.Fail("max_depth", "blocks nest at most %d levels deep", maxBlockDepth)
		}
		return
	}
	c.validateList(list, sl.Allow, depth+1, mode, r)
}

func (c *blocksConfig) IsEmpty(v any) bool {
	list, ok := v.([]any)
	return ok && len(list) == 0
}

func blockRef(name string) map[string]any { return map[string]any{"$ref": "#/$defs/Block_" + name} }

func blockChoice(allow []string) map[string]any {
	of := make([]any, len(allow))
	for i, n := range allow {
		of[i] = blockRef(n)
	}
	return map[string]any{"oneOf": of}
}

func (c *blocksConfig) JSONSchema() map[string]any {
	m := map[string]any{"type": "array", "items": blockChoice(c.allow)}
	c.lim.schema(m, "minItems", "maxItems")
	return m
}

// blockSchema describes one block type; its nested blocks refer to the other block definitions by $ref.
func blockSchema(bt *BlockType) map[string]any {
	props := objectSchema(bt.Props)
	props["title"] = bt.Label
	slots := map[string]any{}
	for _, sl := range bt.Slots {
		m := map[string]any{"type": "array", "items": blockChoice(sl.Allow), "title": sl.Label}
		if sl.Min > 0 {
			m["minItems"] = sl.Min
		}
		if sl.Max > 0 {
			m["maxItems"] = sl.Max
		}
		slots[sl.Name] = m
	}
	properties := map[string]any{
		"id":    map[string]any{"type": "string", "format": "uuid", "description": "Assigned by the server when missing."},
		"type":  map[string]any{"const": bt.Name},
		"props": props,
		"slots": map[string]any{"type": "object", "properties": slots, "additionalProperties": false},
	}
	if bt.Name == sharedBlock {
		properties["blocks"] = map[string]any{"type": "array", "items": map[string]any{"type": "object"},
			"description": "Delivery only: the blocks of the shared item, inlined."}
	}
	m := map[string]any{"type": "object", "properties": properties, "required": []string{"type"}, "additionalProperties": false}
	put(m, "description", bt.Description)
	return m
}

// blockDefs returns the JSON Schema definitions (Block_<name>) the blocks fields of t refer to.
func (s *Schema) blockDefs(t *Type) map[string]any {
	var queue []string
	for _, f := range t.Fields {
		if f.Shape.Base == BaseBlocks {
			queue = append(queue, f.Shape.Allow...)
		}
	}
	defs := map[string]any{}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		bt, ok := s.lk.byName[n]
		if _, done := defs["Block_"+n]; done || !ok {
			continue
		}
		defs["Block_"+n] = blockSchema(bt)
		for _, sl := range bt.Slots {
			queue = append(queue, sl.Allow...)
		}
	}
	return defs
}

// BlockJSONSchemas returns the JSON Schema of every block type by definition name (Block_<name>); nested
// blocks refer to #/$defs/Block_<name>.
func (s *Schema) BlockJSONSchemas() map[string]any {
	defs := make(map[string]any, len(s.blocks))
	for _, bt := range s.blocks {
		defs["Block_"+bt.Name] = blockSchema(bt)
	}
	return defs
}

type tokenType struct{}

func (tokenType) Name() string { return "token" }

func (tokenType) Compile(o *Options) (Shape, Config, error) {
	set, ok := o.String("set")
	var values []string
	switch {
	case !o.Has("set"):
		o.Errorf("", "set is required")
	case ok:
		def, found := o.c.b.tokens[set]
		if !found {
			o.Errorf("set", "set names unknown token set %q (defined: %s)", set, strings.Join(sortedKeys(o.c.b.tokens), ", "))
			break
		}
		for _, v := range def.values {
			values = append(values, v.s)
		}
	}
	return Shape{Base: BaseString, Format: "token", Set: set, Enum: values}, &tokenConfig{set: set, values: values}, nil
}

type tokenConfig struct {
	set    string
	values []string
}

func (c *tokenConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if !slices.Contains(c.values, s) {
		r.Fail("enum", "must be one of the %s tokens: %s", c.set, strings.Join(c.values, ", "))
	}
}

func (c *tokenConfig) IsEmpty(v any) bool { return v == "" }

func (c *tokenConfig) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "enum": slices.Clone(c.values), "x-hydrogen-tokens": c.set}
}

// ---- walking documents

// each calls fn for every block of a blocks value in document order (a block before its children) with its
// path ("body[2]", "body[1].slots.items[0]"), skipping values that are not well-formed blocks of known types.
func (c *blocksConfig) each(v any, path string, fn func(path string, b map[string]any, bt *BlockType)) {
	c.eachDepth(v, path, 0, fn)
}

func (c *blocksConfig) eachDepth(v any, path string, depth int, fn func(string, map[string]any, *BlockType)) {
	list, _ := v.([]any)
	if depth > walkDepth {
		return
	}
	for i, e := range list {
		m, _ := e.(map[string]any)
		typ, _ := m["type"].(string)
		bt := c.lk.byName[typ]
		if m == nil || bt == nil {
			continue
		}
		p := path + "[" + strconv.Itoa(i) + "]"
		fn(p, m, bt)
		slots, _ := m["slots"].(map[string]any)
		for _, sl := range bt.Slots {
			c.eachDepth(slots[sl.Name], p+".slots."+sl.Name, depth+1, fn)
		}
	}
}

// eachProp calls fn for every present prop of every block.
func (c *blocksConfig) eachProp(v any, path string, fn func(path string, f *Field, v any)) {
	c.each(v, path, func(p string, m map[string]any, bt *BlockType) {
		props, _ := m["props"].(map[string]any)
		for _, f := range bt.Props {
			if pv, ok := props[f.Name]; ok {
				fn(p+".props."+f.Name, f, pv)
			}
		}
	})
}

func (c *blocksConfig) normalize(v any) {
	c.each(v, "", func(_ string, m map[string]any, bt *BlockType) {
		if id, ok := m["id"].(string); ok {
			if low := strings.ToLower(id); low != id {
				m["id"] = low
			}
		}
		if props, ok := m["props"].(map[string]any); ok {
			normalizeFields(bt.Props, props)
		}
	})
}

func (c *blocksConfig) prune(v any) {
	c.each(v, "", func(_ string, m map[string]any, bt *BlockType) {
		if props, ok := m["props"].(map[string]any); ok {
			pruneFields(bt.Props, props)
		}
	})
}

func (c *blocksConfig) defaults(v any) {
	c.each(v, "", func(_ string, m map[string]any, bt *BlockType) {
		props, ok := m["props"].(map[string]any)
		if !ok {
			if _, has := m["props"]; has {
				return
			}
			props = map[string]any{}
		}
		defaultFields(bt.Props, props)
		if len(props) > 0 {
			m["props"] = props
		}
	})
}

// AssignBlockIDs gives every block without an id a new one from newID. Blocks are visited in document order.
func (s *Schema) AssignBlockIDs(t *Type, doc map[string]any, newID func() string) {
	for _, f := range t.Fields {
		if f.Shape.Base == BaseBlocks {
			assignIDs(doc[f.Name], newID, 0)
		}
	}
}

func assignIDs(v any, newID func() string, depth int) {
	list, _ := v.([]any)
	if depth > walkDepth {
		return
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if id, has := m["id"]; !has || id == "" || id == nil {
			m["id"] = newID()
		}
		if slots, ok := m["slots"].(map[string]any); ok {
			for _, k := range sortedKeys(slots) {
				assignIDs(slots[k], newID, depth+1)
			}
		}
	}
}

func (c *blocksConfig) refs(v any, path string, out []RefValue) []RefValue {
	c.eachProp(v, path, func(p string, f *Field, pv any) { out = collectRefs(f, pv, p, out) })
	return out
}

func (c *blocksConfig) text(v any, sb *strings.Builder) {
	c.eachProp(v, "", func(_ string, f *Field, pv any) { collectText(f, pv, sb) })
}

func (c *blocksConfig) removeRef(v any, target string, match func(*Field) bool) bool {
	changed := false
	c.each(v, "", func(_ string, m map[string]any, bt *BlockType) {
		if props, ok := m["props"].(map[string]any); ok && removeRefs(bt.Props, props, target, match) {
			changed = true
		}
	})
	return changed
}

// ---- canonical JSON

func (s *Schema) blockJSON(bt *BlockType) map[string]any {
	m := map[string]any{"name": bt.Name, "label": bt.Label, "props": s.fieldsJSON(bt.Props)}
	put(m, "description", bt.Description)
	put(m, "icon", bt.Icon)
	if len(bt.Slots) > 0 {
		slots := make([]any, len(bt.Slots))
		for i, sl := range bt.Slots {
			sm := map[string]any{"name": sl.Name, "label": sl.Label, "allow": sl.Allow}
			put(sm, "description", sl.Description)
			put(sm, "min", sl.Min)
			put(sm, "max", sl.Max)
			slots[i] = sm
		}
		m["slots"] = slots
	}
	return m
}
