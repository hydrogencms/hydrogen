package schema

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

type located struct {
	s   string
	pos Pos
}

type fieldDef struct {
	name string
	node *yaml.Node
	file string
	line int
}

type typeDef struct {
	name        string
	pos         Pos
	label       string
	description string
	singleton   bool
	page        bool
	blockset    bool
	namePattern located
	adminList   []located
	adminGroup  string
	adminIcon   string
	aiInstr     string
	aiGenerate  []located
	fields      []*fieldDef
	options     map[string]optionDef
}

// optionDef is a module type option as written in the schema.
type optionDef struct {
	value any
	pos   Pos
}

type groupDef struct {
	name        string
	pos         Pos
	description string
	fields      []*fieldDef
}

func (s src) defName(k *yaml.Node, what string) (string, bool) {
	name := k.Value
	if len(name) > maxName || !nameRE.MatchString(name) {
		s.errorf(k, "%s name %q must match %s and be at most %d characters", what, name, nameRE, maxName)
		return "", false
	}
	return name, true
}

func parseType(s src, reg *Registry, k, v *yaml.Node) *typeDef {
	name, ok := s.defName(k, "type")
	if !ok {
		return nil
	}
	def := &typeDef{name: name, pos: s.pos(k)}
	ents, _ := s.mapping(v, fmt.Sprintf("type %q", name))
	for _, e := range ents {
		switch e.key {
		case "label":
			s.setString(&def.label, e)
		case "description":
			s.setString(&def.description, e)
		case "singleton":
			s.setBool(&def.singleton, e)
		case "page":
			s.setBool(&def.page, e)
		case "blockset":
			s.setBool(&def.blockset, e)
		case "name":
			if p, ok := s.str(e); ok {
				def.namePattern = located{p, s.pos(e.k)}
			}
		case "admin":
			s.typeAdmin(def, e)
		case "ai":
			s.typeAI(def, e)
		case "fields":
			def.fields = s.fieldDefs(e.v)
		default:
			if _, ok := reg.opts[e.key]; !ok {
				known := uniqueSorted(append(coreTypeKeys(), sortedKeys(reg.opts)...))
				s.errorf(e.k, "unknown key %q in type %q (known: %s)", e.key, name, strings.Join(known, ", "))
				break
			}
			v, err := toValue(e.v)
			if err != nil {
				s.errorf(e.k, "type %q: %s: %v", name, e.key, err)
				break
			}
			if def.options == nil {
				def.options = map[string]optionDef{}
			}
			def.options[e.key] = optionDef{value: v, pos: s.pos(e.k)}
		}
	}
	return def
}

func parseGroup(s src, k, v *yaml.Node) *groupDef {
	name, ok := s.defName(k, "group")
	if !ok {
		return nil
	}
	def := &groupDef{name: name, pos: s.pos(k)}
	ents, _ := s.mapping(v, fmt.Sprintf("group %q", name))
	for _, e := range ents {
		switch e.key {
		case "description":
			s.setString(&def.description, e)
		case "fields":
			def.fields = s.fieldDefs(e.v)
		default:
			s.errorf(e.k, "unknown key %q in group %q (known: description, fields)", e.key, name)
		}
	}
	return def
}

func (s src) str(e entry) (string, bool) {
	v, ok := asString(e.v)
	if !ok {
		s.errorf(e.k, "%s must be a string", e.key)
	}
	return v, ok
}

func (s src) setString(dst *string, e entry) {
	if v, ok := s.str(e); ok {
		*dst = v
	}
}

func (s src) setBool(dst *bool, e entry) {
	v, ok := asBool(e.v)
	if !ok {
		s.errorf(e.k, "%s must be true or false", e.key)
		return
	}
	*dst = v
}

func (s src) locatedList(e entry) []located {
	n := deref(e.v)
	if n.Kind != yaml.SequenceNode {
		s.errorf(e.k, "%s must be a list of field names", e.key)
		return nil
	}
	out := make([]located, 0, len(n.Content))
	for _, c := range n.Content {
		v, ok := asString(c)
		if !ok {
			s.errorf(c, "%s must be a list of field names", e.key)
			continue
		}
		out = append(out, located{v, s.pos(c)})
	}
	return out
}

func (s src) typeAdmin(def *typeDef, e entry) {
	ents, _ := s.mapping(e.v, "admin")
	for _, a := range ents {
		switch a.key {
		case "list":
			def.adminList = s.locatedList(a)
		case "group":
			s.setString(&def.adminGroup, a)
		case "icon":
			s.setString(&def.adminIcon, a)
		default:
			s.errorf(a.k, "unknown key %q in admin (known: group, icon, list)", a.key)
		}
	}
}

func (s src) typeAI(def *typeDef, e entry) {
	ents, _ := s.mapping(e.v, "ai")
	for _, a := range ents {
		switch a.key {
		case "instructions":
			s.setString(&def.aiInstr, a)
		case "generate":
			def.aiGenerate = s.locatedList(a)
		default:
			s.errorf(a.k, "unknown key %q in ai (known: generate, instructions)", a.key)
		}
	}
}

func (s src) fieldDefs(n *yaml.Node) []*fieldDef {
	if isNull(deref(n)) {
		return nil
	}
	ents, _ := s.mapping(n, "fields")
	out := make([]*fieldDef, len(ents))
	for i, e := range ents {
		out[i] = &fieldDef{name: e.key, node: e.v, file: s.file, line: e.k.Line}
	}
	return out
}
