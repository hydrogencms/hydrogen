package graphql

import (
	"slices"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// maxProjected is the delivery API's cap on ?fields= paths; a selection that needs more is not projected.
const maxProjected = 64

// itemKeys are the Item interface fields: the delivery envelope, which a projection keeps.
var itemKeys = map[string]bool{"id": true, "type": true, "name": true, "key": true, "locale": true, "url": true, "publishedAt": true}

// cheapField is the first small top-level field of a type (a number, boolean or enum). A selection that needs no
// content field sends it, because an empty ?fields= would mean no projection.
func cheapField(t *schema.Type) string {
	for _, f := range t.Fields {
		switch f.Shape.Base {
		case schema.BaseNumber, schema.BaseBoolean:
			return f.Name
		case schema.BaseString:
			if f.Shape.Format == "enum" {
				return f.Name
			}
		}
	}
	return ""
}

// projector collects the top-level content fields a selection reads. Anything it cannot map to a field makes the
// whole selection unprojectable.
type projector struct {
	cs    *chanSchema
	frags map[string]ast.Definition
	names map[string]bool
	bad   bool
}

// neededFields returns the schema fields the selections of a field of GraphQL type typ read: nil when the
// selection cannot be analysed (every field is needed), else a non-nil, possibly empty, sorted list.
func (cs *chanSchema) neededFields(asts []*ast.Field, typ string, frags map[string]ast.Definition) []string {
	p := &projector{cs: cs, frags: frags, names: map[string]bool{}}
	for _, f := range asts {
		p.walk(f.SelectionSet, typ, 0)
	}
	if p.bad || len(p.names) > maxProjected {
		return nil
	}
	out := make([]string, 0, len(p.names))
	for n := range p.names {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func (p *projector) known(typ string) bool {
	return typ == "" || typ == "Item" || p.cs.srcs[typ] != nil || p.cs.wrappers[typ]
}

func (p *projector) walk(sel *ast.SelectionSet, typ string, guard int) {
	if sel == nil || p.bad {
		return
	}
	if guard > 64 {
		p.bad = true
		return
	}
	for _, s := range sel.Selections {
		switch x := s.(type) {
		case *ast.Field:
			if x.Name == nil {
				p.bad = true
				return
			}
			p.field(x, typ, guard)
		case *ast.InlineFragment:
			t := typ
			if x.TypeCondition != nil && x.TypeCondition.Name != nil {
				t = x.TypeCondition.Name.Value
			}
			if !p.known(t) {
				p.bad = true
				return
			}
			p.walk(x.SelectionSet, t, guard+1)
		case *ast.FragmentSpread:
			def, ok := p.frags[x.Name.Value].(*ast.FragmentDefinition)
			if !ok || def.TypeCondition == nil || def.TypeCondition.Name == nil || !p.known(def.TypeCondition.Name.Value) {
				p.bad = true
				return
			}
			p.walk(def.SelectionSet, def.TypeCondition.Name.Value, guard+1)
		default:
			p.bad = true
			return
		}
	}
}

func (p *projector) field(x *ast.Field, typ string, guard int) {
	name := x.Name.Value
	if name == "__typename" {
		return
	}
	switch {
	case p.cs.wrappers[typ]:
		if name == "items" {
			p.walk(x.SelectionSet, p.cs.meta[typ]["items"].target, guard+1)
		}
	case itemKeys[name] && (typ == "" || typ == "Item" || p.cs.srcs[typ] != nil):
	case p.cs.srcs[typ] != nil:
		src, ok := p.cs.srcs[typ][name]
		if !ok {
			p.bad = true
			return
		}
		p.names[src] = true
	default:
		p.bad = true
	}
}

// projection is the ?fields= value for the items a request returns: nil asks for whole items. need is what
// neededFields found; typ is the schema type of the route, "" when the route serves any type.
func (cs *chanSchema) projection(need []string, typ string) []string {
	if need == nil {
		return nil
	}
	if len(need) > 0 {
		return need
	}
	c := cs.anyCheap
	if typ != "" {
		c = cs.cheap[typ]
	}
	if c == "" {
		return nil
	}
	return []string{c}
}

// selectionOf returns the content fields the field being resolved reads (cached per field).
func (rc *reqCtx) selectionOf(info gql.ResolveInfo) (need []string) {
	if len(info.FieldASTs) == 0 {
		return nil
	}
	key := info.FieldASTs[0]
	if v, ok := rc.needs.Load(key); ok {
		return v.([]string)
	}
	need = rc.cs.neededFields(info.FieldASTs, namedType(info.ReturnType), info.Fragments)
	rc.needs.Store(key, need)
	return need
}

// fieldsFor is the projection to send for the field being resolved.
func (rc *reqCtx) fieldsFor(info gql.ResolveInfo, typ string) []string {
	return rc.cs.projection(rc.selectionOf(info), typ)
}

func namedType(t gql.Type) string {
	for {
		switch x := t.(type) {
		case *gql.List:
			t = x.OfType
		case *gql.NonNull:
			t = x.OfType
		default:
			return t.Name()
		}
	}
}
