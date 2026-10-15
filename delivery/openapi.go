package delivery

import (
	"maps"
	"strings"
	"unicode"

	"github.com/hydrogencms/hydrogen/openapi"
	"github.com/hydrogencms/hydrogen/schema"
)

type obj = openapi.Obj

// openAPI builds the OpenAPI 3.1 document of the delivery API with per-type payload schemas. Marshal it with
// encoding/json for a deterministic result.
func openAPI(s *schema.Schema, extra []Route) obj {
	schemas := obj{
		"Error": obj{"type": "object", "required": []any{"error"}, "properties": obj{"error": obj{
			"type": "object", "required": []any{"code", "message"},
			"properties": obj{"code": openapi.Str(""), "message": openapi.Str("")}}}},
		"ChannelInfo": obj{"type": "object", "required": []any{"name", "languages", "collections", "options"},
			"properties": obj{"name": openapi.Str(""), "languages": obj{"type": "array", "items": openapi.Str("")},
				"collections": obj{"type": "array", "items": openapi.Str("")},
				"options":     obj{"type": "object", "additionalProperties": true, "description": "Public channel options of modules."}}},
	}
	var itemRefs []any
	paths := obj{}
	for _, t := range s.Types() {
		fields := s.JSONSchema(t)
		delete(fields, "$schema")
		schemas["Fields_"+t.Name] = fields
		schemas["Item_"+t.Name] = itemSchema(t.Name, openapi.SchemaRef("Fields_"+t.Name))
		itemRefs = append(itemRefs, openapi.SchemaRef("Item_"+t.Name))
		n := openapi.Pascal(t.Name)
		if t.Singleton {
			schemas["ItemResponse_"+t.Name] = itemResponse(openapi.SchemaRef("Item_" + t.Name))
			paths["/singletons/"+t.Name] = obj{"get": obj{"operationId": "get" + n, "summary": "Read " + t.Label,
				"parameters": openapi.Params("Channel", "Locale", "Include", "Preview"),
				"responses":  responses(openapi.OKResp("The singleton.", openapi.SchemaRef("ItemResponse_"+t.Name)), "400", "401", "404")}}
			continue
		}
		schemas["ListResponse_"+t.Name] = listResponse(openapi.SchemaRef("Item_" + t.Name))
		paths["/collections/"+t.Name+"/items"] = obj{"get": obj{"operationId": "list" + n, "summary": "List " + t.Label + " items",
			"description": filterNote(t) + " A preview token for one item also lists that item when it was never published and its draft matches " +
				"the filters (not with q): it comes first on the first page, regardless of sort.",
			"parameters": openapi.Params("Channel", "Locale", "Include", "Preview", "Q", "Sort", "Limit", "Offset", "Total", "Where"),
			"responses":  responses(openapi.OKResp("A page of items.", openapi.SchemaRef("ListResponse_"+t.Name)), "400", "401", "404")}}
	}
	if len(itemRefs) > 0 {
		schemas["Item"] = obj{"oneOf": itemRefs}
	} else {
		schemas["Item"] = itemSchema("", obj{"type": "object", "additionalProperties": true})
	}
	schemas["ItemResponse"] = itemResponse(openapi.SchemaRef("Item"))
	paths["/items/{id}"] = obj{"get": obj{"operationId": "getItem", "summary": "Read an item",
		"parameters": append([]any{openapi.Ref("parameters", "ItemId")}, openapi.Params("Channel", "Locale", "Include", "Preview")...),
		"responses":  responses(openapi.OKResp("The item.", openapi.SchemaRef("ItemResponse")), "400", "401", "404")}}
	paths["/items/by-key/{key}"] = obj{"get": obj{"operationId": "getItemByKey", "summary": "Read an item by its key",
		"parameters": append([]any{openapi.Ref("parameters", "ItemKey")}, openapi.Params("Channel", "Locale", "Include", "Preview")...),
		"responses":  responses(openapi.OKResp("The item.", openapi.SchemaRef("ItemResponse")), "400", "401", "404")}}
	paths["/channels/{name}"] = obj{"get": obj{"operationId": "getChannel", "summary": "Public information about a channel",
		"parameters": []any{obj{"name": "name", "in": "path", "required": true, "schema": openapi.Str("")}},
		"responses":  responses(openapi.OKResp("The channel.", openapi.SchemaRef("ChannelInfo")), "401", "404")}}
	paths["/openapi.json"] = obj{"get": obj{"operationId": "getOpenAPI", "summary": "This document", "security": []any{},
		"responses": obj{"200": openapi.OKResp("The OpenAPI document.", obj{"type": "object"})}}}
	for _, rt := range extra {
		addRoute(paths, rt)
	}
	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": "Hydrogen delivery API", "version": "1",
			"description": "Published content for a channel. Token channels need their bearer token; public channels are " +
				"selected with ?channel= (or are the only channel). Responses carry ETag and Cache-Control; send If-None-Match to get 304s."},
		"servers":  []any{obj{"url": Prefix}},
		"security": []any{obj{}, obj{"bearerAuth": []any{}}},
		"paths":    paths,
		"components": obj{
			"securitySchemes": obj{"bearerAuth": obj{"type": "http", "scheme": "bearer", "description": "A channel token."}},
			"schemas":         schemas,
			"parameters":      parameterDocs(s),
			"responses":       errorResponses(),
		},
	}
}

func itemSchema(typeName string, fields obj) obj {
	typ := openapi.Str("")
	if typeName != "" {
		typ = obj{"const": typeName}
	}
	return obj{"type": "object", "additionalProperties": true,
		"required": []any{"id", "type", "locale", "version", "publishedAt", "name", "fields"},
		"description": "The stored payload; modules may add more keys (pages: url, links). fallback is true when the shown " +
			"locale is not the one asked for.",
		"properties": obj{"id": openapi.Str("uuid"), "type": typ, "key": openapi.Str(""), "locale": openapi.Str(""), "version": obj{"type": "integer"},
			"publishedAt": openapi.Str("date-time"), "name": openapi.Str(""), "fields": fields, "fallback": obj{"type": "boolean"}}}
}

func itemResponse(item obj) obj {
	return obj{"type": "object", "required": []any{"item"}, "properties": obj{"item": item,
		"included": obj{"type": "object", "additionalProperties": openapi.SchemaRef("Item"),
			"description": "Referenced items by id, when include was given."}}}
}

func listResponse(item obj) obj {
	return obj{"type": "object", "required": []any{"items", "hasMore"}, "properties": obj{
		"items": obj{"type": "array", "items": item}, "hasMore": obj{"type": "boolean",
			"description": "Whether another page follows."},
		"total":    obj{"type": "integer", "description": "Matches in all; only with total=true, which costs a count."},
		"included": obj{"type": "object", "additionalProperties": openapi.SchemaRef("Item")}}}
}

func parameterDocs(s *schema.Schema) obj {
	return obj{
		"Channel": obj{"name": "channel", "in": "query", "schema": openapi.Str(""),
			"description": "A public channel's name; not needed with a token or when there is one channel."},
		"Locale": obj{"name": "locale", "in": "query", "schema": openapi.Str(""),
			"description": "Narrow to a language of the channel; the channel's other languages follow as fallbacks."},
		"Include": obj{"name": "include", "in": "query", "schema": obj{"type": "integer", "minimum": 0, "maximum": maxInclude},
			"description": "Resolve referenced items to this depth (at most 100 items) into included."},
		"Preview": obj{"name": "preview", "in": "query", "schema": openapi.Str(""),
			"description": "A preview token (or the X-Hydrogen-Preview header): draft content, never cached."},
		"ItemId":  obj{"name": "id", "in": "path", "required": true, "schema": openapi.Str("uuid")},
		"ItemKey": obj{"name": "key", "in": "path", "required": true, "schema": openapi.Str("")},
		"Q":       obj{"name": "q", "in": "query", "schema": openapi.Str(""), "description": "Full-text search."},
		"Sort": obj{"name": "sort", "in": "query", "schema": openapi.Str(""),
			"description": "Comma-separated fields; a - prefix sorts descending."},
		"Limit":  obj{"name": "limit", "in": "query", "schema": obj{"type": "integer", "minimum": 1, "maximum": 500, "default": 50}},
		"Total":  obj{"name": "total", "in": "query", "schema": obj{"type": "boolean", "default": false}, "description": "Include total, the number of matches."},
		"Offset": obj{"name": "offset", "in": "query", "schema": obj{"type": "integer", "minimum": 0, "default": 0}},
		"Where": obj{"name": "where", "in": "query", "style": "deepObject", "explode": true,
			"schema":      obj{"type": "object", "additionalProperties": obj{"type": "object", "additionalProperties": openapi.Str("")}},
			"description": "Filters as where[field][op]=value on indexed fields; ops eq ne lt lte gt gte in contains exists prefix."},
	}
}

func errorResponses() obj {
	mk := func(desc string) obj {
		return obj{"description": desc, "content": obj{"application/json": obj{"schema": openapi.SchemaRef("Error")}}}
	}
	return obj{
		"BadRequest":   mk("Malformed request, unknown channel or locale."),
		"Unauthorized": mk("Missing or invalid channel token, or invalid preview token."),
		"NotFound":     mk("Not available on this channel (one message for missing, unpublished and disallowed items)."),
	}
}

func responses(ok obj, codes ...string) obj {
	m := map[string]string{"400": "BadRequest", "401": "Unauthorized", "404": "NotFound"}
	out := obj{"200": ok}
	for _, c := range codes {
		out[c] = openapi.Ref("responses", m[c])
	}
	return out
}

func filterNote(t *schema.Type) string {
	var idx []string
	for _, f := range t.Fields {
		if f.Index {
			idx = append(idx, f.Name)
		}
	}
	if len(idx) == 0 {
		return "No fields are indexed; filter and sort on name, key, createdAt and updatedAt."
	}
	return "Indexed fields for where and sort: " + strings.Join(idx, ", ") + "."
}

func addRoute(paths obj, rt Route) {
	path, names := openAPIPath(rt.Path)
	item, _ := paths[path].(obj)
	if item == nil {
		item = obj{}
		paths[path] = item
	}
	op := obj{}
	if rt.OpenAPI != nil {
		maps.Copy(op, rt.OpenAPI)
	}
	if _, ok := op["operationId"]; !ok {
		op["operationId"] = "get" + pathWords(rt.Path)
	}
	if _, ok := op["summary"]; !ok && rt.Summary != "" {
		op["summary"] = rt.Summary
	}
	if rt.OpenAPI == nil {
		ps := []any{}
		for _, n := range names {
			ps = append(ps, obj{"name": n, "in": "path", "required": true, "schema": openapi.Str("")})
		}
		ps = append(ps, openapi.Params("Channel", "Locale", "Preview")...)
		op["parameters"] = ps
		op["responses"] = responses(openapi.OKResp("Success.", obj{"type": "object"}), "400", "401", "404")
	}
	item["get"] = op
}

func openAPIPath(p string) (string, []string) {
	p = strings.TrimSuffix(p, "{$}")
	var names []string
	var sb strings.Builder
	for {
		i := strings.IndexByte(p, '{')
		j := strings.IndexByte(p, '}')
		if i < 0 || j < i {
			sb.WriteString(p)
			break
		}
		name := strings.TrimSuffix(p[i+1:j], "...")
		names = append(names, name)
		sb.WriteString(p[:i+1] + name + "}")
		p = p[j+1:]
	}
	return sb.String(), names
}

func pathWords(p string) string {
	var sb strings.Builder
	for _, w := range strings.FieldsFunc(p, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		sb.WriteString(openapi.Pascal(w))
	}
	return sb.String()
}
