package graphql

import (
	"fmt"
	"net/http"

	"github.com/graphql-go/graphql/language/ast"
)

// fragments indexes the document's fragment definitions.
func fragments(doc *ast.Document) map[string]*ast.FragmentDefinition {
	out := map[string]*ast.FragmentDefinition{}
	for _, d := range doc.Definitions {
		if f, ok := d.(*ast.FragmentDefinition); ok && f.Name != nil {
			out[f.Name.Value] = f
		}
	}
	return out
}

// operation picks the operation to run: the named one, else the only one.
func operation(doc *ast.Document, name string) *ast.OperationDefinition {
	var found *ast.OperationDefinition
	for _, d := range doc.Definitions {
		op, ok := d.(*ast.OperationDefinition)
		if !ok {
			continue
		}
		if name != "" {
			if op.Name != nil && op.Name.Value == name {
				return op
			}
			continue
		}
		if found != nil {
			return nil
		}
		found = op
	}
	return found
}

func isIntrospection(name string) bool { return name == "__schema" || name == "__type" }

// size is the depth and the number of fields of a selection set, fragments expanded. Introspection fields count
// for nothing: their depth is the tooling's, not the content's.
type size struct {
	depth  int
	fields int64
}

type measurer struct {
	frags    map[string]*ast.FragmentDefinition
	memo     map[string]size
	limit    int64
	hasIntro bool
	introErr *reqError
}

func (m *measurer) selection(sel *ast.SelectionSet) size {
	var out size
	if sel == nil {
		return out
	}
	add := func(s size) {
		out.depth = max(out.depth, s.depth)
		out.fields = satAdd(out.fields, s.fields, m.limit)
	}
	for _, s := range sel.Selections {
		switch x := s.(type) {
		case *ast.Field:
			if x.Name != nil && isIntrospection(x.Name.Value) {
				m.hasIntro = true
				if m.introErr == nil {
					m.introErr = m.checkIntrospection(x)
				}
				continue
			}
			inner := m.selection(x.SelectionSet)
			add(size{depth: inner.depth + 1, fields: satAdd(1, inner.fields, m.limit)})
		case *ast.InlineFragment:
			add(m.selection(x.SelectionSet))
		case *ast.FragmentSpread:
			if x.Name == nil {
				continue
			}
			name := x.Name.Value
			if sz, ok := m.memo[name]; ok {
				add(sz)
				continue
			}
			def := m.frags[name]
			if def == nil {
				continue
			}
			m.memo[name] = size{}
			sz := m.selection(def.SelectionSet)
			m.memo[name] = sz
			add(sz)
		}
	}
	return out
}

func satAdd(a, b, limit int64) int64 {
	if a > limit || b > limit || a+b > limit {
		return limit + 1
	}
	return a + b
}

// checkLimits applies the configured limits to the operation that will run. The error is a request error.
func checkLimits(doc *ast.Document, opName string, cfg *Config) error {
	op := operation(doc, opName)
	m := &measurer{frags: fragments(doc), memo: map[string]size{}, limit: int64(cfg.MaxFields)}
	var sz size
	if op != nil {
		sz = m.selection(op.SelectionSet)
	}
	if m.hasIntro && !cfg.Introspection {
		return &reqError{status: http.StatusBadRequest, code: "INTROSPECTION_DISABLED", msg: "introspection is disabled"}
	}
	if m.introErr != nil {
		return m.introErr
	}
	if sz.depth > cfg.MaxDepth {
		return &reqError{status: http.StatusBadRequest, code: "QUERY_TOO_DEEP",
			msg: fmt.Sprintf("the query is nested %d levels deep; the limit is %d", sz.depth, cfg.MaxDepth)}
	}
	if sz.fields > int64(cfg.MaxFields) {
		return &reqError{status: http.StatusBadRequest, code: "QUERY_TOO_LARGE",
			msg: fmt.Sprintf("the query selects more than %d fields once fragments are expanded", cfg.MaxFields)}
	}
	return nil
}

// reqError is an error of the request as a whole: no data, a status.
type reqError struct {
	status int
	code   string
	msg    string
}

func (e *reqError) Error() string { return e.msg }

// includeDepth is how many reference hops below the items of a selection the query reads, so that one
// include=N request carries them. typ is the GraphQL type the selection is on; meta tells which fields hop.
func includeDepth(sel *ast.SelectionSet, typ string, frags map[string]ast.Definition, meta map[string]map[string]fieldMeta) int {
	return walkDepth(sel, typ, frags, meta, 0)
}

func walkDepth(sel *ast.SelectionSet, typ string, frags map[string]ast.Definition, meta map[string]map[string]fieldMeta, guard int) int {
	if sel == nil || guard > 64 {
		return 0
	}
	best := 0
	for _, s := range sel.Selections {
		d := 0
		switch x := s.(type) {
		case *ast.Field:
			if x.Name == nil {
				continue
			}
			m, ok := meta[typ][x.Name.Value]
			if !ok {
				continue
			}
			d = walkDepth(x.SelectionSet, m.target, frags, meta, guard+1)
			if m.ref {
				d++
			}
		case *ast.InlineFragment:
			t := typ
			if x.TypeCondition != nil && x.TypeCondition.Name != nil {
				t = x.TypeCondition.Name.Value
			}
			d = walkDepth(x.SelectionSet, t, frags, meta, guard+1)
		case *ast.FragmentSpread:
			if x.Name == nil {
				continue
			}
			def, ok := frags[x.Name.Value].(*ast.FragmentDefinition)
			if !ok || def.TypeCondition == nil || def.TypeCondition.Name == nil {
				continue
			}
			d = walkDepth(def.SelectionSet, def.TypeCondition.Name.Value, frags, meta, guard+1)
		}
		best = max(best, d)
	}
	return best
}

// selects reports whether a selection set (fragments followed) selects a field by name.
func selects(sel *ast.SelectionSet, name string, frags map[string]ast.Definition) bool {
	return selectsDeep(sel, name, frags, 0)
}

func selectsDeep(sel *ast.SelectionSet, name string, frags map[string]ast.Definition, guard int) bool {
	if sel == nil || guard > 64 {
		return false
	}
	for _, s := range sel.Selections {
		switch x := s.(type) {
		case *ast.Field:
			if x.Name != nil && x.Name.Value == name {
				return true
			}
		case *ast.InlineFragment:
			if selectsDeep(x.SelectionSet, name, frags, guard+1) {
				return true
			}
		case *ast.FragmentSpread:
			if x.Name == nil {
				continue
			}
			if def, ok := frags[x.Name.Value].(*ast.FragmentDefinition); ok && selectsDeep(def.SelectionSet, name, frags, guard+1) {
				return true
			}
		}
	}
	return false
}

// introspectionDepth lets the standard introspection query through (it nests ofType seven times).
const introspectionDepth = 13

// checkIntrospection bounds an introspection field. Besides depth and field count, no field name but ofType may
// repeat on a path: nesting possibleTypes, interfaces or fields inside themselves multiplies the response by the
// size of the schema at each level, and the standard queries never do it.
func (m *measurer) checkIntrospection(f *ast.Field) *reqError {
	var count int64
	var walk func(sel *ast.SelectionSet, depth int, seen map[string]int, guard int) *reqError
	walk = func(sel *ast.SelectionSet, depth int, seen map[string]int, guard int) *reqError {
		if sel == nil {
			return nil
		}
		if guard > 64 {
			return badRequest("QUERY_TOO_DEEP", "the introspection query is too deeply nested")
		}
		for _, s := range sel.Selections {
			switch x := s.(type) {
			case *ast.Field:
				if x.Name == nil {
					continue
				}
				count++
				if count > m.limit {
					return badRequest("QUERY_TOO_LARGE", fmt.Sprintf("the introspection query selects more than %d fields once fragments are expanded", m.limit))
				}
				if depth+1 > introspectionDepth {
					return badRequest("QUERY_TOO_DEEP", fmt.Sprintf("the introspection query is nested deeper than %d levels", introspectionDepth))
				}
				name := x.Name.Value
				limit := 1
				if name == "ofType" {
					limit = 8
				}
				if x.SelectionSet == nil {
					continue
				}
				if seen[name] >= limit {
					return badRequest("INTROSPECTION_TOO_COMPLEX", "the introspection query nests "+name+" inside itself")
				}
				seen[name]++
				err := walk(x.SelectionSet, depth+1, seen, guard+1)
				seen[name]--
				if err != nil {
					return err
				}
			case *ast.InlineFragment:
				if err := walk(x.SelectionSet, depth, seen, guard+1); err != nil {
					return err
				}
			case *ast.FragmentSpread:
				if x.Name == nil {
					continue
				}
				if def := m.frags[x.Name.Value]; def != nil {
					if err := walk(def.SelectionSet, depth, seen, guard+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return walk(&ast.SelectionSet{Selections: []ast.Selection{f}}, 0, map[string]int{}, 0)
}
