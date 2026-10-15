// Package openapi has helpers for building OpenAPI documents as maps.
package openapi

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Obj = map[string]any

func Ref(kind, name string) Obj { return Obj{"$ref": "#/components/" + kind + "/" + name} }

func SchemaRef(name string) Obj { return Ref("schemas", name) }

func ArrayOf(items any) Obj { return Obj{"type": "array", "items": items} }

func Str(format string) Obj {
	if format == "" {
		return Obj{"type": "string"}
	}
	return Obj{"type": "string", "format": format}
}

func Pascal(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func JSONContent(sch any) Obj { return Obj{"application/json": Obj{"schema": sch}} }

func OKResp(desc string, sch any) Obj {
	return Obj{"description": desc, "content": JSONContent(sch)}
}

// Params references the named components/parameters.
func Params(names ...string) []any {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = Ref("parameters", n)
	}
	return out
}

// HoistDefs moves the $defs of a JSON Schema into the components' schemas and points its $ref values there.
func HoistDefs(schemas, sch Obj) {
	defs, _ := sch["$defs"].(map[string]any)
	delete(sch, "$defs")
	for name, d := range defs {
		schemas[name] = d
		repoint(d)
	}
	repoint(sch)
}

func repoint(v any) {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			if name, found := strings.CutPrefix(ref, "#/$defs/"); found {
				x["$ref"] = "#/components/schemas/" + name
			}
		}
		for _, c := range x {
			repoint(c)
		}
	case []any:
		for _, c := range x {
			repoint(c)
		}
	}
}
