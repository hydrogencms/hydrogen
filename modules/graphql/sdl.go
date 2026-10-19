package graphql

import (
	"slices"
	"strconv"
	"strings"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// jsonScalar is JSON: any JSON value. Literals in queries may be objects and lists; variables inside a literal
// are not supported (pass the whole value as a variable).
func jsonScalar() *gql.Scalar {
	return gql.NewScalar(gql.ScalarConfig{
		Name:         "JSON",
		Description:  "Any JSON value.",
		Serialize:    func(v any) any { return v },
		ParseValue:   func(v any) any { return v },
		ParseLiteral: literalValue,
	})
}

// longScalar is Long: a 64-bit whole number, sent as a JSON number (JavaScript clients read it exactly up to 2^53).
func longScalar() *gql.Scalar {
	return gql.NewScalar(gql.ScalarConfig{
		Name:        "Long",
		Description: "A 64-bit integer, sent as a JSON number. JavaScript clients read it exactly only up to 2^53.",
		Serialize:   func(v any) any { return longOf(v) },
		ParseValue: func(v any) any {
			if s, ok := v.(string); ok {
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					return n
				}
				return nil
			}
			return longOf(v)
		},
		ParseLiteral: func(v ast.Value) any {
			if x, ok := v.(*ast.IntValue); ok {
				if n, err := strconv.ParseInt(x.Value, 10, 64); err == nil {
					return n
				}
			}
			return nil
		},
	})
}

func literalValue(v ast.Value) any {
	switch x := v.(type) {
	case *ast.StringValue:
		return x.Value
	case *ast.BooleanValue:
		return x.Value
	case *ast.EnumValue:
		return x.Value
	case *ast.IntValue:
		n, err := strconv.ParseInt(x.Value, 10, 64)
		if err != nil {
			return nil
		}
		return float64(n)
	case *ast.FloatValue:
		f, err := strconv.ParseFloat(x.Value, 64)
		if err != nil {
			return nil
		}
		return f
	case *ast.ListValue:
		out := make([]any, 0, len(x.Values))
		for _, e := range x.Values {
			lv := literalValue(e)
			if lv == nil {
				if _, null := e.(*ast.EnumValue); !null {
					return nil
				}
			}
			out = append(out, lv)
		}
		return out
	case *ast.ObjectValue:
		out := make(map[string]any, len(x.Fields))
		for _, f := range x.Fields {
			lv := literalValue(f.Value)
			if lv == nil {
				return nil
			}
			out[f.Name.Value] = lv
		}
		return out
	}
	return nil
}

var builtinScalars = map[string]bool{"String": true, "Int": true, "Float": true, "Boolean": true, "ID": true}

// printSDL prints the schema as SDL with every definition and field in name order, so the text is stable.
func printSDL(s *gql.Schema) string {
	tm := s.TypeMap()
	names := make([]string, 0, len(tm))
	for n := range tm {
		if !strings.HasPrefix(n, "__") && !builtinScalars[n] {
			names = append(names, n)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == "Query":
			return -1
		case b == "Query":
			return 1
		}
		return strings.Compare(a, b)
	})
	var sb strings.Builder
	for i, n := range names {
		if i > 0 {
			sb.WriteByte('\n')
		}
		printType(&sb, tm[n])
	}
	return sb.String()
}

func desc(sb *strings.Builder, indent, d string) {
	if d == "" {
		return
	}
	sb.WriteString(indent + `"""` + strings.ReplaceAll(d, `"""`, `\"""`) + `"""` + "\n")
}

func printType(sb *strings.Builder, t gql.Type) {
	switch x := t.(type) {
	case *gql.Scalar:
		desc(sb, "", x.Description())
		sb.WriteString("scalar " + x.Name() + "\n")
	case *gql.Enum:
		desc(sb, "", x.Description())
		sb.WriteString("enum " + x.Name() + " {\n")
		vals := slices.Clone(x.Values())
		slices.SortFunc(vals, func(a, b *gql.EnumValueDefinition) int { return strings.Compare(a.Name, b.Name) })
		for _, v := range vals {
			sb.WriteString("  " + v.Name + "\n")
		}
		sb.WriteString("}\n")
	case *gql.Union:
		desc(sb, "", x.Description())
		var parts []string
		for _, o := range x.Types() {
			parts = append(parts, o.Name())
		}
		slices.Sort(parts)
		sb.WriteString("union " + x.Name() + " = " + strings.Join(parts, " | ") + "\n")
	case *gql.Interface:
		desc(sb, "", x.Description())
		sb.WriteString("interface " + x.Name() + " {\n")
		printFields(sb, x.Fields())
		sb.WriteString("}\n")
	case *gql.Object:
		desc(sb, "", x.Description())
		sb.WriteString("type " + x.Name())
		var ifs []string
		for _, i := range x.Interfaces() {
			ifs = append(ifs, i.Name())
		}
		slices.Sort(ifs)
		if len(ifs) > 0 {
			sb.WriteString(" implements " + strings.Join(ifs, " & "))
		}
		sb.WriteString(" {\n")
		printFields(sb, x.Fields())
		sb.WriteString("}\n")
	}
}

func printFields(sb *strings.Builder, fs gql.FieldDefinitionMap) {
	names := make([]string, 0, len(fs))
	for n := range fs {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		f := fs[n]
		desc(sb, "  ", f.Description)
		sb.WriteString("  " + n)
		if len(f.Args) > 0 {
			args := slices.Clone(f.Args)
			slices.SortFunc(args, func(a, b *gql.Argument) int { return strings.Compare(a.Name(), b.Name()) })
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = a.Name() + ": " + a.Type.String()
			}
			sb.WriteString("(" + strings.Join(parts, ", ") + ")")
		}
		sb.WriteString(": " + f.Type.String() + "\n")
	}
}
