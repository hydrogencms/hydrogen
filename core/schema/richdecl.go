package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	maxRichNodes  = 50
	maxRichMarks  = 20
	maxRichAttrs  = 20
	maxRichEnum   = 100
	richIntLimit  = 1_000_000_000
	richStringMax = 500
	richStringCap = 10000
	richURLMax    = 2000
	richNodeParts = "parts"
)

var (
	richNameRE = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,39}$`)
	richEnumRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	richBlockTags  = []string{"aside", "details", "dd", "div", "dl", "dt", "figcaption", "figure", "footer", "header", "section", "summary"}
	richInlineTags = []string{"abbr", "cite", "del", "dfn", "ins", "kbd", "mark", "q", "samp", "small", "span", "time", "var"}
)

// RichText is the project's rich text vocabulary beyond the built-in nodes and marks, declared by the top-level
// `richtext:` schema key.
type RichText struct {
	Nodes []*RichNode // sorted by name
	Marks []*RichMark // sorted by name

	nodes map[string]*RichNode
	marks map[string]*RichMark
}

// RichNode is a custom node. Content is "blocks", "inline", "none" or "parts" (Parts lists the part nodes in
// order). Part says another node names it as a part.
type RichNode struct {
	Name, Label, Tag string
	Inline           bool
	Content          string
	Parts            []string
	Part             bool
	Attrs            []*RichAttr
}

// RichMark is a custom mark.
type RichMark struct {
	Name, Label, Tag string
	Attrs            []*RichAttr
}

// RichAttr is a typed attribute of a custom node or mark. Type is string, enum, int, bool or url. MaxLen is the
// longest string; Min and Max bound an int. Default is a string, int64 or bool, nil when none.
type RichAttr struct {
	Name, Type string
	Values     []string
	Min, Max   *int64
	MaxLen     int
	Required   bool
	Default    any
}

// RichText is the compiled custom vocabulary; never nil, and empty when the schema declares none.
func (s *Schema) RichText() *RichText { return s.rt }

func (r *RichText) Node(name string) (*RichNode, bool) {
	n, ok := r.nodes[name]
	return n, ok
}

func (r *RichText) Mark(name string) (*RichMark, bool) {
	m, ok := r.marks[name]
	return m, ok
}

// problem says why v is not a valid value of the attribute, or "".
func (a *RichAttr) problem(v any) string {
	switch a.Type {
	case "string":
		s, ok := v.(string)
		switch {
		case !ok:
			return "must be a string"
		case strings.IndexByte(s, 0) >= 0:
			return "must not contain NUL characters"
		case utf8.RuneCountInString(s) > a.MaxLen:
			return fmt.Sprintf("must be at most %d characters", a.MaxLen)
		}
	case "enum":
		s, ok := v.(string)
		if !ok || !slices.Contains(a.Values, s) {
			return "must be one of " + strings.Join(a.Values, ", ")
		}
	case "int":
		f, exact, ok := numberOf(v)
		if !ok || !exact {
			return "must be an integer"
		}
		if n := int64(f); n < *a.Min || n > *a.Max {
			return fmt.Sprintf("must be from %d to %d", *a.Min, *a.Max)
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return "must be true or false"
		}
	case "url":
		s, ok := v.(string)
		if !ok || len(s) > richURLMax || !validImageSrc(s) {
			return "must be an http(s) URL or a root-relative path"
		}
	}
	return ""
}

// ---- definitions

type richDefs struct {
	nodes map[string]*richNodeDef
	marks map[string]*richMarkDef
}

type richNodeDef struct {
	name         string
	pos          Pos
	label, tag   string
	tagPos       Pos
	inline       bool
	content      string
	contentPos   Pos
	parts        []located
	attrs        []*richAttrDef
	hasContent   bool
	inlinePos    Pos
	contentIsSeq bool
}

type richMarkDef struct {
	name       string
	pos        Pos
	label, tag string
	tagPos     Pos
	attrs      []*richAttrDef
}

type richAttrDef struct {
	name        string
	pos         Pos
	typ         string
	values      []located
	min, max    *int
	required    bool
	def         any
	hasDef      bool
	defPos      Pos
	badType     bool
	typePresent bool
}

func (b *Builder) addRichText(s src, n *yaml.Node) {
	if isNull(deref(n)) {
		return
	}
	ents, _ := s.mapping(n, "richtext")
	for _, e := range ents {
		switch e.key {
		case "nodes":
			b.addNamed(s, e.v, "richtext nodes", func(k, v *yaml.Node) { b.registerRichNode(s, k, v) })
		case "marks":
			b.addNamed(s, e.v, "richtext marks", func(k, v *yaml.Node) { b.registerRichMark(s, k, v) })
		default:
			s.errorf(e.k, "unknown key %q in richtext (known: marks, nodes)", e.key)
		}
	}
}

func (b *Builder) registerRichNode(s src, k, v *yaml.Node) {
	def := parseRichNode(s, k, v)
	if def == nil {
		return
	}
	if old, dup := b.rich.nodes[def.name]; dup {
		s.errorf(k, "richtext node %q is already defined at %s", def.name, old.pos)
		return
	}
	b.rich.nodes[def.name] = def
}

func (b *Builder) registerRichMark(s src, k, v *yaml.Node) {
	def := parseRichMark(s, k, v)
	if def == nil {
		return
	}
	if old, dup := b.rich.marks[def.name]; dup {
		s.errorf(k, "richtext mark %q is already defined at %s", def.name, old.pos)
		return
	}
	b.rich.marks[def.name] = def
}

func richName(s src, k *yaml.Node, what string) bool {
	if !richNameRE.MatchString(k.Value) {
		s.errorf(k, "richtext %s name %q must match %s", what, k.Value, richNameRE)
		return false
	}
	return true
}

func parseRichNode(s src, k, v *yaml.Node) *richNodeDef {
	if !richName(s, k, "node") {
		return nil
	}
	def := &richNodeDef{name: k.Value, pos: s.pos(k)}
	ents, _ := s.mapping(v, fmt.Sprintf("richtext node %q", def.name))
	for _, e := range ents {
		switch e.key {
		case "label":
			s.setString(&def.label, e)
		case "tag":
			if s.setStringOK(&def.tag, e) {
				def.tagPos = s.pos(e.v)
			}
		case "inline":
			s.setBool(&def.inline, e)
			def.inlinePos = s.pos(e.k)
		case "content":
			def.hasContent = true
			def.contentPos = s.pos(e.k)
			n := deref(e.v)
			switch n.Kind {
			case yaml.ScalarNode:
				if str, ok := asString(n); ok {
					def.content = str
				} else {
					s.errorf(e.k, "content must be blocks, inline, none or a list of part nodes")
				}
			case yaml.SequenceNode:
				def.contentIsSeq = true
				def.content = richNodeParts
				def.parts = s.locatedList(e)
			default:
				s.errorf(e.k, "content must be blocks, inline, none or a list of part nodes")
			}
		case "attrs":
			def.attrs = s.richAttrs(e.v)
		default:
			s.errorf(e.k, "unknown key %q in richtext node %q (known: attrs, content, inline, label, tag)", e.key, def.name)
		}
	}
	return def
}

func parseRichMark(s src, k, v *yaml.Node) *richMarkDef {
	if !richName(s, k, "mark") {
		return nil
	}
	def := &richMarkDef{name: k.Value, pos: s.pos(k)}
	ents, _ := s.mapping(v, fmt.Sprintf("richtext mark %q", def.name))
	for _, e := range ents {
		switch e.key {
		case "label":
			s.setString(&def.label, e)
		case "tag":
			if s.setStringOK(&def.tag, e) {
				def.tagPos = s.pos(e.v)
			}
		case "attrs":
			def.attrs = s.richAttrs(e.v)
		default:
			s.errorf(e.k, "unknown key %q in richtext mark %q (known: attrs, label, tag)", e.key, def.name)
		}
	}
	return def
}

func (s src) setStringOK(dst *string, e entry) bool {
	v, ok := s.str(e)
	if ok {
		*dst = v
	}
	return ok
}

func (s src) richAttrs(n *yaml.Node) []*richAttrDef {
	if isNull(deref(n)) {
		return nil
	}
	ents, _ := s.mapping(n, "attrs")
	var out []*richAttrDef
	for _, e := range ents {
		if !richNameRE.MatchString(e.key) {
			s.errorf(e.k, "attribute name %q must match %s", e.key, richNameRE)
			continue
		}
		def := &richAttrDef{name: e.key, pos: s.pos(e.k)}
		sub, _ := s.mapping(e.v, fmt.Sprintf("attribute %q", e.key))
		var defNode *yaml.Node
		for _, a := range sub {
			switch a.key {
			case "type":
				def.typePresent = true
				if !s.setStringOK(&def.typ, a) {
					def.badType = true
				}
			case "values":
				def.values = s.locatedList(a)
			case "min", "max":
				v, ok := asInt(a.v)
				if !ok {
					s.errorf(a.k, "attribute %q: %s must be a whole number", e.key, a.key)
					continue
				}
				if a.key == "min" {
					def.min = &v
				} else {
					def.max = &v
				}
			case "required":
				s.setBool(&def.required, a)
			case "default":
				defNode = a.v
				def.defPos = s.pos(a.k)
			default:
				s.errorf(a.k, "unknown key %q in attribute %q (known: default, max, min, required, type, values)", a.key, e.key)
			}
		}
		if defNode != nil {
			def.def, def.hasDef = deref(defNode), true
		}
		out = append(out, def)
	}
	return out
}

// ---- compile

func (c *compiler) compileRichText() *RichText {
	rt := &RichText{nodes: map[string]*RichNode{}, marks: map[string]*RichMark{}}
	nodeDefs, markDefs := c.b.rich.nodes, c.b.rich.marks
	if len(nodeDefs) > maxRichNodes {
		c.errs.add(nodeDefs[sortedKeys(nodeDefs)[maxRichNodes]].pos, "richtext declares %d nodes; the most is %d", len(nodeDefs), maxRichNodes)
	}
	if len(markDefs) > maxRichMarks {
		c.errs.add(markDefs[sortedKeys(markDefs)[maxRichMarks]].pos, "richtext declares %d marks; the most is %d", len(markDefs), maxRichMarks)
	}
	partOf := map[string]string{}
	for _, name := range sortedKeys(nodeDefs) {
		def := nodeDefs[name]
		if slices.Contains(richNodeNames(), name) || slices.Contains([]string{"doc", "text", "listItem", "tableRow", "tableCell", "tableHeader"}, name) {
			c.errs.add(def.pos, "richtext node %q is a built-in node", name)
			continue
		}
		n := c.compileRichNode(def)
		rt.Nodes = append(rt.Nodes, n)
		rt.nodes[name] = n
	}
	for _, name := range sortedKeys(nodeDefs) {
		def, n := nodeDefs[name], rt.nodes[name]
		if n == nil || n.Content != richNodeParts {
			continue
		}
		seen := map[string]bool{}
		for _, p := range def.parts {
			pn := rt.nodes[p.s]
			switch {
			case seen[p.s]:
				c.errs.add(p.pos, "richtext node %q lists part %q twice", name, p.s)
			case p.s == name:
				c.errs.add(p.pos, "richtext node %q cannot be its own part", name)
			case pn == nil:
				c.errs.add(p.pos, "richtext node %q: part %q is not a declared node", name, p.s)
			case pn.Inline:
				c.errs.add(p.pos, "richtext node %q: part %q is inline and cannot be a part", name, p.s)
			case partOf[p.s] != "":
				c.errs.add(p.pos, "richtext node %q is already a part of %q", p.s, partOf[p.s])
			default:
				partOf[p.s] = name
				pn.Part = true
				n.Parts = append(n.Parts, p.s)
			}
			seen[p.s] = true
		}
	}
	for _, name := range sortedKeys(nodeDefs) {
		if n := rt.nodes[name]; n != nil && n.Content == richNodeParts && richPartCycle(rt, name, name, 0) {
			c.errs.add(nodeDefs[name].pos, "richtext node %q contains itself through its parts", name)
		}
	}
	for _, name := range sortedKeys(markDefs) {
		def := markDefs[name]
		if slices.Contains(richMarkNames(), name) {
			c.errs.add(def.pos, "richtext mark %q is a built-in mark", name)
			continue
		}
		m := &RichMark{Name: name, Label: def.label, Tag: def.tag}
		if m.Label == "" {
			m.Label = SplitName(name)
		}
		m.Tag = c.richTag(m.Tag, def.tagPos, richInlineTags, "span", "mark", name)
		m.Attrs = c.compileRichAttrs(def.attrs, "mark "+name)
		rt.Marks = append(rt.Marks, m)
		rt.marks[name] = m
	}
	return rt
}

func richPartCycle(rt *RichText, root, at string, depth int) bool {
	if depth > maxRichNodes {
		return true
	}
	for _, p := range rt.nodes[at].Parts {
		if p == root || (rt.nodes[p] != nil && rt.nodes[p].Content == richNodeParts && richPartCycle(rt, root, p, depth+1)) {
			return true
		}
	}
	return false
}

func (c *compiler) richTag(tag string, pos Pos, allowed []string, def, what, name string) string {
	if tag == "" {
		return def
	}
	if !slices.Contains(allowed, tag) {
		c.errs.add(pos, "richtext %s %q: tag %q must be one of %s", what, name, tag, strings.Join(allowed, ", "))
		return def
	}
	return tag
}

func (c *compiler) compileRichNode(def *richNodeDef) *RichNode {
	n := &RichNode{Name: def.name, Label: def.label, Inline: def.inline, Content: def.content}
	if n.Label == "" {
		n.Label = SplitName(def.name)
	}
	switch {
	case !def.hasContent:
		c.errs.add(def.pos, "richtext node %q: content is required (blocks, inline, none or a list of part nodes)", def.name)
	case def.content != "blocks" && def.content != "inline" && def.content != "none" && !def.contentIsSeq:
		c.errs.add(def.contentPos, "richtext node %q: content %q must be blocks, inline, none or a list of part nodes", def.name, def.content)
	case def.contentIsSeq && len(def.parts) == 0:
		c.errs.add(def.contentPos, "richtext node %q: a list of parts needs at least one part", def.name)
	}
	if def.inline && def.hasContent && def.content != "none" {
		c.errs.add(def.inlinePos, "richtext node %q: an inline node needs content: none", def.name)
	}
	if def.inline {
		n.Tag = c.richTag(def.tag, def.tagPos, richInlineTags, "span", "node", def.name)
	} else {
		n.Tag = c.richTag(def.tag, def.tagPos, richBlockTags, "div", "node", def.name)
	}
	n.Attrs = c.compileRichAttrs(def.attrs, "node "+def.name)
	return n
}

func (c *compiler) compileRichAttrs(defs []*richAttrDef, where string) []*RichAttr {
	if len(defs) > maxRichAttrs {
		c.errs.add(defs[maxRichAttrs].pos, "richtext %s has %d attributes; the most is %d", where, len(defs), maxRichAttrs)
	}
	var out []*RichAttr
	for _, d := range defs {
		a := c.compileRichAttr(d, where)
		if a != nil {
			out = append(out, a)
		}
	}
	return out
}

func (c *compiler) compileRichAttr(d *richAttrDef, where string) *RichAttr {
	at := func(format string, args ...any) {
		c.errs.add(d.pos, "richtext %s attribute %q: "+format, append([]any{where, d.name}, args...)...)
	}
	known := []string{"bool", "enum", "int", "string", "url"}
	if !d.typePresent {
		at("type is required (%s)", strings.Join(known, ", "))
		return nil
	}
	if d.badType {
		return nil
	}
	if !slices.Contains(known, d.typ) {
		at("unknown type %q (known: %s)", d.typ, strings.Join(known, ", "))
		return nil
	}
	a := &RichAttr{Name: d.name, Type: d.typ, Required: d.required}
	bad := false
	if d.typ != "enum" && len(d.values) > 0 {
		at("values only apply to enum attributes")
		bad = true
	}
	if d.typ != "int" && d.typ != "string" && (d.min != nil || d.max != nil) {
		at("min and max only apply to int attributes (max also to string)")
		bad = true
	}
	if d.typ == "string" && d.min != nil {
		at("min does not apply to string attributes")
		bad = true
	}
	switch d.typ {
	case "enum":
		if len(d.values) == 0 || len(d.values) > maxRichEnum {
			at("an enum needs 1 to %d values", maxRichEnum)
			bad = true
		}
		seen := map[string]bool{}
		for _, v := range d.values {
			switch {
			case !richEnumRE.MatchString(v.s):
				c.errs.add(v.pos, "richtext %s attribute %q: value %q must match %s", where, d.name, v.s, richEnumRE)
				bad = true
			case seen[v.s]:
				c.errs.add(v.pos, "richtext %s attribute %q: value %q is listed twice", where, d.name, v.s)
				bad = true
			default:
				seen[v.s] = true
				a.Values = append(a.Values, v.s)
			}
		}
	case "string":
		a.MaxLen = richStringMax
		if d.max != nil {
			if *d.max < 1 || *d.max > richStringCap {
				at("max must be from 1 to %d", richStringCap)
				bad = true
			} else {
				a.MaxLen = *d.max
			}
		}
	case "int":
		lo, hi := int64(-richIntLimit), int64(richIntLimit)
		if d.min != nil {
			lo = int64(*d.min)
		}
		if d.max != nil {
			hi = int64(*d.max)
		}
		switch {
		case lo < -richIntLimit || hi > richIntLimit:
			at("min and max must lie within +-%d", richIntLimit)
			bad = true
		case lo > hi:
			at("min must not exceed max")
			bad = true
		}
		a.Min, a.Max = &lo, &hi
	}
	if d.hasDef && !bad {
		a.Default = c.richDefault(d, a, where)
	}
	if bad {
		return nil
	}
	return a
}

func (c *compiler) richDefault(d *richAttrDef, a *RichAttr, where string) any {
	var v any
	n := d.def.(*yaml.Node)
	switch a.Type {
	case "int":
		if i, ok := asInt(n); ok {
			v = int64(i)
		}
	case "bool":
		if b, ok := asBool(n); ok {
			v = b
		}
	default:
		if s, ok := asString(n); ok {
			v = s
		}
	}
	if v == nil {
		c.errs.add(d.defPos, "richtext %s attribute %q: default must be a %s value", where, d.name, a.Type)
		return nil
	}
	if p := a.problem(v); p != "" {
		c.errs.add(d.defPos, "richtext %s attribute %q: default %s", where, d.name, p)
		return nil
	}
	return v
}

// richDecl is the canonical JSON of the declaration, for the schema hash.
func (s *Schema) richDecl() map[string]any {
	if len(s.rt.Nodes) == 0 && len(s.rt.Marks) == 0 {
		return nil
	}
	attrs := func(list []*RichAttr) []any {
		out := make([]any, len(list))
		for i, a := range list {
			m := map[string]any{"name": a.Name, "type": a.Type}
			if len(a.Values) > 0 {
				m["values"] = a.Values
			}
			if a.Min != nil {
				m["min"], m["max"] = *a.Min, *a.Max
			}
			if a.Type == "string" {
				m["maxLength"] = a.MaxLen
			}
			if a.Required {
				m["required"] = true
			}
			if a.Default != nil {
				m["default"] = a.Default
			}
			out[i] = m
		}
		return out
	}
	nodes := make([]any, len(s.rt.Nodes))
	for i, n := range s.rt.Nodes {
		m := map[string]any{"name": n.Name, "label": n.Label, "tag": n.Tag, "content": n.Content, "attrs": attrs(n.Attrs)}
		if n.Inline {
			m["inline"] = true
		}
		if len(n.Parts) > 0 {
			m["parts"] = n.Parts
		}
		nodes[i] = m
	}
	marks := make([]any, len(s.rt.Marks))
	for i, mk := range s.rt.Marks {
		marks[i] = map[string]any{"name": mk.Name, "label": mk.Label, "tag": mk.Tag, "attrs": attrs(mk.Attrs)}
	}
	return map[string]any{"nodes": nodes, "marks": marks}
}
