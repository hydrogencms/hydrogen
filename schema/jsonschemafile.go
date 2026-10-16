package schema

// JSONSchemaFile is the JSON Schema (draft 2020-12) of the YAML schema-file format, for editor completion.
// The field `type` enum lists the registry's field types (core and module), sorted; a nil registry means
// the core types.
func JSONSchemaFile(reg *Registry) map[string]any {
	if reg == nil {
		reg = NewRegistry()
	}
	str := map[string]any{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	strings := map[string]any{"type": "array", "items": str}
	name := "^[a-z][a-zA-Z0-9]*$"
	fieldMap := map[string]any{
		"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
		"additionalProperties": map[string]any{"$ref": "#/$defs/field"},
	}
	field := map[string]any{
		"type":     "object",
		"required": []string{"type"},
		"properties": map[string]any{
			"type":  map[string]any{"type": "string", "enum": reg.names()},
			"label": str, "description": str, "required": boolean, "translatable": boolean, "index": boolean,
			"default": map[string]any{},
			"when": map[string]any{
				"type": "object", "required": []string{"field"}, "additionalProperties": false,
				"properties": map[string]any{
					"field": str, "eq": map[string]any{}, "ne": map[string]any{},
					"in": map[string]any{"type": "array"}, "empty": boolean,
				},
			},
			"ai": map[string]any{"oneOf": []any{boolean, map[string]any{
				"type": "object", "additionalProperties": false, "properties": map[string]any{"describe": str},
			}}},
			"admin": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"widget": str, "placeholder": str, "hidden": boolean, "readonly": boolean},
			},
			"min": map[string]any{}, "max": map[string]any{}, "pattern": str, "integer": boolean,
			"values": strings, "from": str, "to": strings,
			"on_delete": map[string]any{"enum": []string{"block", "nullify", "warn"}},
			"of":        map[string]any{"$ref": "#/$defs/field"},
			"fields":    fieldMap,
			"use":       str, "marks": strings, "nodes": strings, "allow": strings, "set": str,
		},
	}
	blockDef := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"label": str, "description": str, "icon": str, "props": fieldMap,
			"slots": map[string]any{
				"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
				"additionalProperties": map[string]any{
					"type": "object", "required": []string{"allow"}, "additionalProperties": false,
					"properties": map[string]any{
						"label": str, "description": str, "allow": strings,
						"min": map[string]any{"type": "integer", "minimum": 0}, "max": map[string]any{"type": "integer", "minimum": 1},
					},
				},
			},
		},
	}
	return map[string]any{
		"$schema": schemaDialect,
		"title":   "Hydrogen schema file",
		"type":    "object", "additionalProperties": false,
		"properties": map[string]any{
			"types": map[string]any{
				"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
				"additionalProperties": map[string]any{"$ref": "#/$defs/type"},
			},
			"groups": map[string]any{
				"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
				"additionalProperties": map[string]any{"$ref": "#/$defs/group"},
			},
			"blocks": map[string]any{
				"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
				"additionalProperties": map[string]any{"$ref": "#/$defs/block"},
			},
			"tokens": map[string]any{
				"type": "object", "propertyNames": map[string]any{"pattern": name, "maxLength": maxName},
				"additionalProperties": map[string]any{"type": "array", "items": str, "minItems": 1},
			},
		},
		"$defs": map[string]any{
			"field": field,
			"block": blockDef,
			"type": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"label": str, "description": str, "singleton": boolean, "page": boolean, "blockset": boolean, "review": boolean, "name": str,
					"admin": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{"list": strings, "group": str, "icon": str},
					},
					"ai": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{"instructions": str, "generate": strings},
					},
					"fields": fieldMap,
				},
			},
			"group": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"description": str, "fields": fieldMap},
			},
		},
	}
}
