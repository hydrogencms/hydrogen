package manage

import (
	"maps"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/schema"
)

type obj = map[string]any

func ref(kind, name string) obj { return obj{"$ref": "#/components/" + kind + "/" + name} }

func schemaRef(name string) obj { return ref("schemas", name) }

func arrayOf(items any) obj { return obj{"type": "array", "items": items} }

func str(format string) obj {
	if format == "" {
		return obj{"type": "string"}
	}
	return obj{"type": "string", "format": format}
}

func pascal(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// OpenAPI builds the OpenAPI 3.1 document for the management API with per-type schemas. The result is
// deterministic: marshal it with encoding/json (sorted keys). Module routes are listed under their paths.
func OpenAPI(s *schema.Schema, extra ...Route) obj {
	var collections, singletons []*schema.Type
	for _, t := range s.Types() {
		if t.Singleton {
			singletons = append(singletons, t)
		} else {
			collections = append(collections, t)
		}
	}
	schemas := baseSchemas()
	itemViews := make([]any, 0, len(s.Types()))
	for _, t := range s.Types() {
		fields := s.JSONSchema(t)
		delete(fields, "$schema")
		schemas["Fields_"+t.Name] = fields
		schemas["Patch_"+t.Name] = patchSchema(fields)
		schemas["ItemView_"+t.Name] = itemView(schemaRef("Fields_"+t.Name), t.Name)
		itemViews = append(itemViews, schemaRef("ItemView_"+t.Name))
		schemas["Create_"+t.Name] = createRequest(schemaRef("Fields_" + t.Name))
		schemas["Update_"+t.Name] = updateRequest(schemaRef("Patch_" + t.Name))
	}
	if len(itemViews) > 0 {
		schemas["ItemView"] = obj{"oneOf": itemViews}
	} else {
		schemas["ItemView"] = itemView(obj{"type": "object", "additionalProperties": true}, "")
	}

	paths := obj{}
	genericPaths(paths, collections, singletons)
	for _, t := range collections {
		collectionPaths(paths, t)
	}
	for _, t := range singletons {
		singletonPaths(paths, t)
	}

	modulePaths(paths, extra)

	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": "Hydrogen management API", "version": "1",
			"description": "Content management for a Hydrogen project. Every route except this document needs a bearer token."},
		"servers":  []any{obj{"url": "/api/manage/v1"}},
		"security": []any{obj{"bearerAuth": []any{}}},
		"paths":    paths,
		"components": obj{
			"securitySchemes": obj{"bearerAuth": obj{"type": "http", "scheme": "bearer"}},
			"schemas":         schemas,
			"parameters":      parameters(collections, singletons),
			"responses":       errorResponses(),
		},
	}
}

func baseSchemas() obj {
	uuid := str("uuid")
	dt := str("date-time")
	strs := arrayOf(str(""))
	return mergeObj(channelSchemas(), obj{
		"FieldError": obj{"type": "object", "required": []any{"path", "code", "message"},
			"properties": obj{"path": str(""), "code": str(""), "message": str("")}},
		"Referrer": obj{"type": "object", "required": []any{"id", "type", "name", "field", "kind", "onDelete"},
			"properties": obj{"id": uuid, "type": str(""), "name": str(""), "field": str(""), "kind": str(""),
				"onDelete": obj{"type": "string", "enum": []any{"block", "nullify", "warn"}}}},
		"Error": obj{"type": "object", "required": []any{"error"}, "properties": obj{"error": obj{
			"type": "object", "required": []any{"code", "message"},
			"properties": obj{
				"code": obj{"type": "string", "description": "Machine-readable code; modules may add their own.",
					"examples": []any{"bad_request", "unauthorized", "not_found", "unsupported_media_type",
						"too_large", "invalid", "conflict", "referenced", "duplicate_key", "internal"}},
				"message":   str(""),
				"fields":    arrayOf(schemaRef("FieldError")),
				"locale":    str(""),
				"parts":     strs,
				"referrers": arrayOf(schemaRef("Referrer")),
			}}}},
		"Summary": obj{"type": "object",
			"required": []any{"id", "type", "name", "status", "version", "locales", "draftParts", "createdAt", "updatedAt"},
			"properties": obj{"id": uuid, "type": str(""), "key": str(""), "name": str(""), "status": statusSchema(),
				"version": obj{"type": "integer"}, "locales": strs, "draftParts": strs, "createdAt": dt, "updatedAt": dt}},
		"ListResult": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": arrayOf(schemaRef("Summary")), "total": obj{"type": "integer"}}},
		"VersionMeta": obj{"type": "object", "required": []any{"version", "schemaRev", "locales", "createdAt", "createdBy"},
			"properties": obj{"version": obj{"type": "integer"}, "schemaRev": obj{"type": "integer"}, "locales": strs,
				"createdAt": dt, "createdBy": str("")}},
		"VersionList": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": arrayOf(schemaRef("VersionMeta")), "total": obj{"type": "integer"}}},
		"ReferrerList": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": arrayOf(schemaRef("Referrer")), "total": obj{"type": "integer"}}},
		"CreateRequest": createRequest(obj{"type": "object", "additionalProperties": true}),
		"UpdateRequest": updateRequest(obj{"type": "object", "additionalProperties": true,
			"description": "JSON merge patch: null removes a field."}),
		"PublishRequest": obj{"type": "object", "additionalProperties": false, "properties": obj{
			"locales": obj{"type": "array", "items": str(""), "description": "Locale parts to publish; default every draft locale part."},
			"force":   obj{"type": "boolean", "description": "Publish over conflicts."}}},
	})
}

func mergeObj(a, b obj) obj {
	maps.Copy(a, b)
	return a
}

func statusSchema() obj {
	return obj{"type": "string", "enum": []any{"draft", "published", "changed"}}
}

func itemView(fields obj, typeName string) obj {
	typ := str("")
	if typeName != "" {
		typ = obj{"const": typeName}
	}
	strs := arrayOf(str(""))
	return obj{"type": "object",
		"required": []any{"id", "type", "locale", "mainLocale", "alwaysAvailable", "status", "version", "draft", "name",
			"fields", "locales", "draftParts", "createdAt", "updatedAt"},
		"properties": obj{"id": str("uuid"), "type": typ, "key": str(""), "locale": str(""), "mainLocale": str(""),
			"alwaysAvailable": obj{"type": "boolean"}, "status": statusSchema(), "version": obj{"type": "integer"},
			"draft": obj{"type": "boolean"}, "name": str(""), "fields": fields, "locales": strs, "draftParts": strs,
			"createdAt": str("date-time"), "updatedAt": str("date-time"), "publishedAt": str("date-time")}}
}

func createRequest(fields obj) obj {
	return obj{"type": "object", "additionalProperties": false, "properties": obj{
		"locale": str(""), "key": str(""), "alwaysAvailable": obj{"type": "boolean"}, "fields": fields}}
}

func updateRequest(fields obj) obj {
	return obj{"type": "object", "additionalProperties": false, "required": []any{"fields"}, "properties": obj{
		"locale": str(""), "key": str(""), "alwaysAvailable": obj{"type": "boolean"}, "fields": fields}}
}

// patchSchema is the type's fields as a merge patch: all optional, null removes.
func patchSchema(fields obj) obj {
	props, _ := fields["properties"].(obj)
	out := make(obj, len(props))
	for k, v := range props {
		out[k] = obj{"anyOf": []any{v, obj{"type": "null"}}}
	}
	return obj{"type": "object", "properties": out, "additionalProperties": false,
		"description": "JSON merge patch: null removes a field."}
}

func parameters(collections, singletons []*schema.Type) obj {
	p := obj{
		"ItemId": obj{"name": "id", "in": "path", "required": true, "schema": str("uuid")},
		"Locale": obj{"name": "locale", "in": "query", "schema": str(""),
			"description": "Locale; default the item's main locale (reads) or the service default (writes)."},
		"Draft": obj{"name": "draft", "in": "query", "schema": obj{"type": "boolean"},
			"description": "Overlay draft parts on the published content."},
		"Status": obj{"name": "status", "in": "query",
			"schema": obj{"type": "string", "enum": []any{"any", "published", "draft"}, "default": "any"}},
		"Q": obj{"name": "q", "in": "query", "schema": str(""), "description": "Full-text search."},
		"Sort": obj{"name": "sort", "in": "query", "schema": str(""),
			"description": "Comma-separated fields; a - prefix sorts descending, e.g. -publishDate,name."},
		"Limit":  obj{"name": "limit", "in": "query", "schema": obj{"type": "integer", "minimum": 1, "maximum": maxLimit, "default": defaultLimit}},
		"Offset": obj{"name": "offset", "in": "query", "schema": obj{"type": "integer", "minimum": 0, "maximum": maxOffset, "default": 0}},
		"Where": obj{"name": "where", "in": "query", "style": "deepObject", "explode": true,
			"schema": obj{"type": "object", "additionalProperties": obj{"type": "object", "additionalProperties": str("")}},
			"description": "Filters as where[field][op]=value, ops eq ne lt lte gt gte in contains exists prefix; " +
				"repeated filters are ANDed; in takes comma-separated values."},
		"Parts": obj{"name": "parts", "in": "query", "schema": str(""),
			"description": "Comma-separated draft parts (shared or locale codes); default all."},
	}
	p["CollectionType"] = typeParam(collections)
	p["SingletonType"] = typeParam(singletons)
	return p
}

func typeParam(ts []*schema.Type) obj {
	sch := str("")
	if len(ts) > 0 {
		names := make([]any, len(ts))
		for i, t := range ts {
			names[i] = t.Name
		}
		sch["enum"] = names
	}
	return obj{"name": "type", "in": "path", "required": true, "schema": sch}
}

func errorResponses() obj {
	mk := func(desc string) obj {
		return obj{"description": desc, "content": obj{"application/json": obj{"schema": schemaRef("Error")}}}
	}
	return obj{
		"BadRequest": mk("Malformed request."),
		"Unauthorized": obj{"description": "Missing or invalid bearer token.", "headers": obj{"WWW-Authenticate": obj{"schema": str("")}},
			"content": obj{"application/json": obj{"schema": schemaRef("Error")}}},
		"NotFound":         mk("Unknown type or item."),
		"Conflict":         mk("The published version changed (conflict), the item is referenced (referenced) or the key is taken (duplicate_key)."),
		"Invalid":          mk("Validation failed; error.fields lists the problems."),
		"TooLarge":         mk("Request body too large."),
		"UnsupportedMedia": mk("Content-Type must be application/json."),
	}
}

func jsonContent(schema any) obj { return obj{"application/json": obj{"schema": schema}} }

func okResp(desc string, sch any) obj { return obj{"description": desc, "content": jsonContent(sch)} }

func body(sch any) obj { return obj{"required": true, "content": jsonContent(sch)} }

func params(names ...string) []any {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = ref("parameters", n)
	}
	return out
}

func errs(codes ...string) obj {
	m := map[string]string{"400": "BadRequest", "401": "Unauthorized", "404": "NotFound", "409": "Conflict",
		"413": "TooLarge", "415": "UnsupportedMedia", "422": "Invalid"}
	out := obj{}
	for _, c := range codes {
		out[c] = ref("responses", m[c])
	}
	return out
}

func responses(ok obj, codes ...string) obj {
	out := errs(codes...)
	for k, v := range ok {
		out[k] = v
	}
	return out
}

func operation(id, summary string, ps []any, reqBody obj, resp obj) obj {
	o := obj{"operationId": id, "summary": summary, "responses": resp}
	if len(ps) > 0 {
		o["parameters"] = ps
	}
	if reqBody != nil {
		o["requestBody"] = reqBody
	}
	return o
}

var listParams = []string{"Locale", "Status", "Q", "Sort", "Limit", "Offset", "Where"}

func genericPaths(paths obj, collections, singletons []*schema.Type) {
	view := okResp("The item.", schemaRef("ItemView"))
	paths["/types"] = obj{"get": operation("listTypes", "List content types", nil, nil,
		responses(obj{"200": okResp("Compiled types.", arrayOf(obj{"type": "object"}))}, "401"))}
	paths["/types/{type}"] = obj{"get": operation("getType", "A compiled type and its JSON Schema",
		[]any{obj{"name": "type", "in": "path", "required": true, "schema": str("")}}, nil,
		responses(obj{"200": okResp("The type.", obj{"type": "object", "required": []any{"type", "jsonSchema"},
			"properties": obj{"type": obj{"type": "object"}, "jsonSchema": obj{"type": "object"}}})}, "401", "404"))}
	if len(collections) > 0 {
		paths["/collections/{type}/items"] = obj{
			"get": operation("listItems", "List items of a collection type", params(append([]string{"CollectionType"}, listParams...)...), nil,
				responses(obj{"200": okResp("A page of items.", schemaRef("ListResult"))}, "400", "401", "404")),
			"post": operation("createItem", "Create a draft item", params("CollectionType"), body(schemaRef("CreateRequest")),
				responses(obj{"201": obj{"description": "The new draft.", "headers": obj{"Location": obj{"schema": str("")}},
					"content": jsonContent(schemaRef("ItemView"))}}, "400", "401", "404", "409", "413", "415", "422")),
		}
	}
	paths["/items/{id}"] = obj{
		"get": operation("getItem", "Read an item", params("ItemId", "Locale", "Draft"), nil,
			responses(obj{"200": view}, "400", "401", "404")),
		"patch": operation("updateItem", "Update the draft (merge patch)", params("ItemId"), body(schemaRef("UpdateRequest")),
			responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422")),
		"delete": operation("deleteItem", "Delete an item", params("ItemId"), nil,
			responses(obj{"204": obj{"description": "Deleted."}}, "401", "404", "409")),
	}
	paths["/items/{id}/publish"] = obj{"post": operation("publishItem", "Publish draft parts", params("ItemId"),
		optionalBody(schemaRef("PublishRequest")), responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422"))}
	paths["/items/{id}/draft"] = obj{"delete": operation("discardDraft", "Discard draft parts", params("ItemId", "Parts"), nil,
		responses(obj{"204": obj{"description": "Discarded."}}, "400", "401", "404"))}
	paths["/items/{id}/versions"] = obj{"get": operation("listVersions", "Version history", params("ItemId"), nil,
		responses(obj{"200": okResp("Versions, newest first.", schemaRef("VersionList"))}, "401", "404"))}
	paths["/items/{id}/references"] = obj{"get": operation("listReferences", "Items pointing at this one", params("ItemId"), nil,
		responses(obj{"200": okResp("Referrers.", schemaRef("ReferrerList"))}, "401", "404"))}
	if len(singletons) > 0 {
		paths["/singletons/{type}"] = obj{
			"get": operation("getSingleton", "Read a singleton", params("SingletonType", "Locale", "Draft"), nil,
				responses(obj{"200": view}, "400", "401", "404")),
			"patch": operation("updateSingleton", "Update a singleton's draft (merge patch)", params("SingletonType"),
				body(schemaRef("UpdateRequest")), responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422")),
		}
		paths["/singletons/{type}/publish"] = obj{"post": operation("publishSingleton", "Publish a singleton",
			params("SingletonType"), optionalBody(schemaRef("PublishRequest")),
			responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422"))}
	}
	paths["/channels"] = obj{"get": operation("listChannels", "List delivery channels", nil, nil,
		responses(obj{"200": okResp("The configured channels, sorted by name.", schemaRef("ChannelList"))}, "401"))}
	paths["/openapi.json"] = obj{"get": obj{"operationId": "getOpenAPI", "summary": "This document",
		"responses": obj{"200": okResp("The OpenAPI document.", obj{"type": "object"})}}}
}

func optionalBody(sch any) obj {
	return obj{"required": false, "content": jsonContent(sch)}
}

func collectionPaths(paths obj, t *schema.Type) {
	n := pascal(t.Name)
	list := operation("list"+n, "List "+t.Label+" items", params(listParams...), nil,
		responses(obj{"200": okResp("A page of items.", schemaRef("ListResult"))}, "400", "401"))
	list["description"] = filterNote(t)
	paths["/collections/"+t.Name+"/items"] = obj{
		"get": list,
		"post": operation("create"+n, "Create a draft "+t.Label, nil, body(schemaRef("Create_"+t.Name)),
			responses(obj{"201": obj{"description": "The new draft.", "headers": obj{"Location": obj{"schema": str("")}},
				"content": jsonContent(schemaRef("ItemView_" + t.Name))}}, "400", "401", "409", "413", "415", "422")),
	}
}

func singletonPaths(paths obj, t *schema.Type) {
	n := pascal(t.Name)
	view := okResp("The singleton.", schemaRef("ItemView_"+t.Name))
	paths["/singletons/"+t.Name] = obj{
		"get": operation("get"+n, "Read "+t.Label, params("Locale", "Draft"), nil,
			responses(obj{"200": view}, "400", "401", "404")),
		"patch": operation("update"+n, "Update "+t.Label+" (merge patch)", nil, body(schemaRef("Update_"+t.Name)),
			responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422")),
	}
	paths["/singletons/"+t.Name+"/publish"] = obj{"post": operation("publish"+n, "Publish "+t.Label, nil,
		optionalBody(schemaRef("PublishRequest")), responses(obj{"200": view}, "400", "401", "404", "409", "413", "415", "422"))}
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

// modulePaths adds module routes to paths, in the order given; operations of one path share a path item.
func modulePaths(paths obj, routes []Route) {
	for _, rt := range routes {
		path, names := openAPIPath(rt.Path)
		item, _ := paths[path].(obj)
		if item == nil {
			item = obj{}
			paths[path] = item
		}
		item[strings.ToLower(rt.Method)] = moduleOperation(rt, names)
	}
}

// openAPIPath converts a ServeMux pattern to an OpenAPI path and lists its parameter names. A trailing {$}
// matches the exact path and a {name...} wildcard is a parameter named name.
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

func moduleOperation(rt Route, names []string) obj {
	op := obj{}
	if rt.OpenAPI != nil {
		maps.Copy(op, rt.OpenAPI)
	}
	if _, ok := op["operationId"]; !ok {
		op["operationId"] = strings.ToLower(rt.Method) + pathWords(rt.Path)
	}
	if _, ok := op["summary"]; !ok && rt.Summary != "" {
		op["summary"] = rt.Summary
	}
	if rt.OpenAPI != nil {
		return op
	}
	if len(names) > 0 {
		ps := make([]any, len(names))
		for i, n := range names {
			ps[i] = obj{"name": n, "in": "path", "required": true, "schema": str("")}
		}
		op["parameters"] = ps
	}
	codes := []string{"400", "401", "404", "409", "422"}
	ok := okResp("Success.", obj{"type": "object"})
	switch rt.Method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		op["requestBody"] = obj{"required": false, "content": jsonContent(obj{"type": "object"})}
		codes = append(codes, "413", "415")
	}
	op["responses"] = responses(obj{"200": ok}, codes...)
	return op
}

// pathWords turns "/pages/sites/{site}/tree" into "PagesSitesSiteTree".
func pathWords(p string) string {
	var sb strings.Builder
	for _, w := range strings.FieldsFunc(p, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		sb.WriteString(pascal(w))
	}
	return sb.String()
}
