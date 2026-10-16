package manage

import (
	"maps"
	"net/http"
	"strings"
	"unicode"

	"github.com/hydrogencms/hydrogen/openapi"
	"github.com/hydrogencms/hydrogen/schema"
)

type obj = openapi.Obj

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
		openapi.HoistDefs(schemas, fields)
		schemas["Fields_"+t.Name] = fields
		schemas["Patch_"+t.Name] = patchSchema(fields)
		schemas["ItemView_"+t.Name] = itemView(openapi.SchemaRef("Fields_"+t.Name), t.Name)
		itemViews = append(itemViews, openapi.SchemaRef("ItemView_"+t.Name))
		schemas["Create_"+t.Name] = createRequest(openapi.SchemaRef("Fields_" + t.Name))
		schemas["Update_"+t.Name] = updateRequest(openapi.SchemaRef("Patch_" + t.Name))
	}
	blockSchemas(s, schemas)
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

	authPaths(paths)
	modulePaths(paths, extra)

	return obj{
		"openapi": "3.1.0",
		"info": obj{"title": "Hydrogen management API", "version": "1",
			"description": "Content management for a Hydrogen project. Every route except login needs credentials: " +
				"a bearer API token, or the session cookie from login with X-CSRF-Token on unsafe methods. " +
				"What a caller may do depends on its roles; 403 means no grant allows it."},
		"servers":  []any{obj{"url": "/api/manage/v1"}},
		"security": []any{obj{"bearerAuth": []any{}}, obj{"cookieAuth": []any{}}},
		"paths":    paths,
		"components": obj{
			"securitySchemes": obj{
				"bearerAuth": obj{"type": "http", "scheme": "bearer"},
				"cookieAuth": obj{"type": "apiKey", "in": "cookie", "name": "hydrogen_session"},
			},
			"schemas":    schemas,
			"parameters": parameters(collections, singletons),
			"responses":  errorResponses(),
		},
	}
}

func baseSchemas() obj {
	uuid := openapi.Str("uuid")
	dt := openapi.Str("date-time")
	strs := openapi.ArrayOf(openapi.Str(""))
	return mergeObj(mergeObj(channelSchemas(), authSchemas()), obj{
		"FieldError": obj{"type": "object", "required": []any{"path", "code", "message"},
			"properties": obj{"path": openapi.Str(""), "code": openapi.Str(""), "message": openapi.Str("")}},
		"Referrer": obj{"type": "object", "required": []any{"id", "type", "name", "field", "kind", "onDelete"},
			"properties": obj{"id": uuid, "type": openapi.Str(""), "name": openapi.Str(""), "field": openapi.Str(""), "kind": openapi.Str(""),
				"onDelete": obj{"type": "string", "enum": []any{"block", "nullify", "warn"}}}},
		"Error": obj{"type": "object", "required": []any{"error"}, "properties": obj{"error": obj{
			"type": "object", "required": []any{"code", "message"},
			"properties": obj{
				"code": obj{"type": "string", "description": "Machine-readable code; modules may add their own.",
					"examples": []any{"bad_request", "unauthorized", "not_found", "unsupported_media_type",
						"too_large", "invalid", "conflict", "has_children", "referenced", "duplicate_key", "forbidden", "csrf", "throttled", "internal"}},
				"message":   openapi.Str(""),
				"fields":    openapi.ArrayOf(openapi.SchemaRef("FieldError")),
				"locale":    openapi.Str(""),
				"parts":     strs,
				"referrers": openapi.ArrayOf(openapi.SchemaRef("Referrer")),
				"hidden":    obj{"type": "integer", "description": "Referrers the caller may not read."},
			}}}},
		"Summary": obj{"type": "object",
			"required": []any{"id", "type", "name", "status", "version", "locales", "draftParts", "createdAt", "updatedAt"},
			"properties": obj{"id": uuid, "type": openapi.Str(""), "key": openapi.Str(""), "name": openapi.Str(""), "status": statusSchema(),
				"version": obj{"type": "integer"}, "locales": strs, "draftParts": strs, "createdAt": dt, "updatedAt": dt,
				"fields": obj{"type": "object", "description": "The fields named by the fields parameter (working document, requested locale with main-locale fallback); absent fields are left out."}}},
		"ListResult": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef("Summary")), "total": obj{"type": "integer"}}},
		"TrashSummary": obj{"type": "object",
			"required":    []any{"id", "type", "name", "status", "version", "locales", "draftParts", "createdAt", "updatedAt", "deletedAt", "deletedBy", "deletedByName"},
			"description": "A trashed item: the summary fields plus who trashed it and when.",
			"properties": obj{"id": uuid, "type": openapi.Str(""), "key": openapi.Str(""), "name": openapi.Str(""), "status": statusSchema(),
				"version": obj{"type": "integer"}, "locales": strs, "draftParts": strs, "createdAt": dt, "updatedAt": dt,
				"deletedAt": dt, "deletedBy": openapi.Str(""), "deletedByName": openapi.Str("")}},
		"TrashList": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef("TrashSummary")), "total": obj{"type": "integer"}}},
		"VersionMeta": obj{"type": "object", "required": []any{"version", "schemaRev", "locales", "createdAt", "createdBy"},
			"properties": obj{"version": obj{"type": "integer"}, "schemaRev": obj{"type": "integer"}, "locales": strs,
				"createdAt": dt, "createdBy": openapi.Str("")}},
		"VersionDoc": obj{"type": "object", "required": []any{"fields"},
			"properties": obj{"name": openapi.Str(""), "fields": obj{"type": "object", "additionalProperties": true}}},
		"VersionView": obj{"type": "object", "required": []any{"version", "schemaRev", "createdAt", "createdBy", "shared", "locales"},
			"description": "The stored documents of a version: shared holds the non-translatable fields, locales the translatable ones per locale.",
			"properties": obj{"version": obj{"type": "integer"}, "schemaRev": obj{"type": "integer"}, "createdAt": dt, "createdBy": openapi.Str(""),
				"shared": openapi.SchemaRef("VersionDoc"), "locales": obj{"type": "object", "additionalProperties": openapi.SchemaRef("VersionDoc")}}},
		"VersionList": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef("VersionMeta")), "total": obj{"type": "integer"}}},
		"ReferrerList": obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef("Referrer")), "total": obj{"type": "integer"},
				"hidden": obj{"type": "integer", "description": "Referrers the caller may not read."}}},
		"CreateRequest": createRequest(obj{"type": "object", "additionalProperties": true}),
		"UpdateRequest": updateRequest(obj{"type": "object", "additionalProperties": true,
			"description": "JSON merge patch: null removes a field."}),
		"PublishRequest": obj{"type": "object", "additionalProperties": false, "properties": obj{
			"locales": obj{"type": "array", "items": openapi.Str(""), "description": "Locale parts to publish; default every draft locale part."},
			"force":   obj{"type": "boolean", "description": "Publish over conflicts."}}},
	})
}

func mergeObj(a, b obj) obj {
	maps.Copy(a, b)
	return a
}

func statusSchema() obj {
	return obj{"type": "string", "enum": []any{"draft", "published", "changed", "unpublished"},
		"description": "unpublished: was published, is not now (single-item views only; lists show such items as draft)."}
}

func itemView(fields obj, typeName string) obj {
	typ := openapi.Str("")
	if typeName != "" {
		typ = obj{"const": typeName}
	}
	strs := openapi.ArrayOf(openapi.Str(""))
	return obj{"type": "object",
		"required": []any{"id", "type", "locale", "mainLocale", "alwaysAvailable", "status", "version", "draft", "name",
			"fields", "locales", "draftParts", "createdAt", "updatedAt"},
		"properties": obj{"id": openapi.Str("uuid"), "type": typ, "key": openapi.Str(""), "locale": openapi.Str(""), "mainLocale": openapi.Str(""),
			"alwaysAvailable": obj{"type": "boolean"}, "status": statusSchema(), "version": obj{"type": "integer"},
			"draft": obj{"type": "boolean"}, "name": openapi.Str(""), "fields": fields, "locales": strs, "draftParts": strs,
			"createdAt": openapi.Str("date-time"), "updatedAt": openapi.Str("date-time"), "publishedAt": openapi.Str("date-time"),
			"deletedAt": openapi.Str("date-time"), "deletedBy": openapi.Str(""), "deletedByName": openapi.Str(""),
			"actions": obj{"type": "object", "required": []any{"edit", "publish", "delete"},
				"description": "What the calling principal may do with the item.",
				"properties":  obj{"edit": obj{"type": "boolean"}, "publish": obj{"type": "boolean"}, "delete": obj{"type": "boolean"}}}}}
}

func createRequest(fields obj) obj {
	return obj{"type": "object", "additionalProperties": false, "properties": obj{
		"locale": openapi.Str(""), "key": openapi.Str(""), "alwaysAvailable": obj{"type": "boolean"}, "fields": fields}}
}

func updateRequest(fields obj) obj {
	return obj{"type": "object", "additionalProperties": false, "required": []any{"fields"}, "properties": obj{
		"locale": openapi.Str(""), "key": openapi.Str(""), "alwaysAvailable": obj{"type": "boolean"}, "fields": fields}}
}

// patchSchema is the type's fields as a merge patch: all optional, null removes.
// blockSchemas adds the block definitions (Block_<name>, Block) and the catalog GET /blocks answers with.
func blockSchemas(s *schema.Schema, schemas obj) {
	openapi.HoistDefs(schemas, obj{"$defs": s.BlockJSONSchemas()})
	var of []any
	for _, b := range s.Blocks() {
		of = append(of, openapi.SchemaRef("Block_"+b.Name))
	}
	if len(of) > 0 {
		schemas["Block"] = obj{"oneOf": of, "description": "A block of a blocks field, discriminated by type. " +
			"Ids are UUIDv7 and assigned by the server when missing. A blocks field is replaced whole by a merge patch."}
	} else {
		schemas["Block"] = obj{"type": "object", "description": "A block of a blocks field; the schema defines no block types."}
	}
	schemas["BlockCatalog"] = obj{"type": "object", "required": []any{"blocks", "tokens"}, "properties": obj{
		"blocks": openapi.ArrayOf(obj{"type": "object", "required": []any{"name", "label", "props"}, "properties": obj{
			"name": openapi.Str(""), "label": openapi.Str(""), "description": openapi.Str(""), "icon": openapi.Str(""),
			"props": obj{"type": "array", "description": "Compiled fields, as in GET /types.", "items": obj{"type": "object"}},
			"slots": openapi.ArrayOf(obj{"type": "object", "required": []any{"name", "label", "allow"}, "properties": obj{
				"name": openapi.Str(""), "label": openapi.Str(""), "description": openapi.Str(""),
				"allow": openapi.ArrayOf(openapi.Str("")), "min": obj{"type": "integer"}, "max": obj{"type": "integer"}}}),
		}}),
		"tokens": openapi.ArrayOf(obj{"type": "object", "required": []any{"name", "values"}, "properties": obj{
			"name": openapi.Str(""), "values": openapi.ArrayOf(openapi.Str(""))}}),
	}}
}

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
		"ItemId": obj{"name": "id", "in": "path", "required": true, "schema": openapi.Str("uuid")},
		"Locale": obj{"name": "locale", "in": "query", "schema": openapi.Str(""),
			"description": "Locale; default the item's main locale (reads) or the service default (writes)."},
		"Draft": obj{"name": "draft", "in": "query", "schema": obj{"type": "boolean"},
			"description": "Overlay draft parts on the published content."},
		"Status": obj{"name": "status", "in": "query",
			"schema": obj{"type": "string", "enum": []any{"any", "published", "draft"}, "default": "any"}},
		"Q": obj{"name": "q", "in": "query", "schema": openapi.Str(""), "description": "Full-text search."},
		"Sort": obj{"name": "sort", "in": "query", "schema": openapi.Str(""),
			"description": "Comma-separated fields; a - prefix sorts descending, e.g. -publishDate,name."},
		"Limit":  obj{"name": "limit", "in": "query", "schema": obj{"type": "integer", "minimum": 1, "maximum": maxLimit, "default": defaultLimit}},
		"Offset": obj{"name": "offset", "in": "query", "schema": obj{"type": "integer", "minimum": 0, "maximum": maxOffset, "default": 0}},
		"Where": obj{"name": "where", "in": "query", "style": "deepObject", "explode": true,
			"schema": obj{"type": "object", "additionalProperties": obj{"type": "object", "additionalProperties": openapi.Str("")}},
			"description": "Filters as where[field][op]=value, ops eq ne lt lte gt gte in contains exists prefix; " +
				"repeated filters are ANDed; in takes comma-separated values."},
		"IfMatch": obj{"name": "If-Match", "in": "header", "schema": openapi.Str(""),
			"description": "The ETag of the item as read (GET returns it, writes return the new one). A mismatch answers 412; without the header nothing is checked."},
		"Trashed": obj{"name": "trashed", "in": "query", "schema": obj{"type": "boolean"},
			"description": "Also read an item in the trash (returned as its draft view, with deletedAt and deletedBy)."},
		"Permanent": obj{"name": "permanent", "in": "query", "schema": obj{"type": "boolean"},
			"description": "Delete for good instead of moving to the trash; only items already in the trash (else 409)."},
		"TrashType": obj{"name": "type", "in": "query", "schema": openapi.Str(""), "description": "Only items of this content type."},
		"Fields": obj{"name": "fields", "in": "query", "schema": openapi.Str(""),
			"description": "Comma-separated top-level fields (at most 10) to return in each item's fields."},
		"VersionNumber": obj{"name": "n", "in": "path", "required": true, "schema": obj{"type": "integer", "minimum": 1}},
		"Parts": obj{"name": "parts", "in": "query", "schema": openapi.Str(""),
			"description": "Comma-separated draft parts (shared or locale codes); default all."},
	}
	maps.Copy(p, authParameters())
	p["CollectionType"] = typeParam(collections)
	p["SingletonType"] = typeParam(singletons)
	return p
}

func typeParam(ts []*schema.Type) obj {
	sch := openapi.Str("")
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
		return obj{"description": desc, "content": obj{"application/json": obj{"schema": openapi.SchemaRef("Error")}}}
	}
	return obj{
		"BadRequest": mk("Malformed request."),
		"Unauthorized": obj{"description": "Missing or invalid credentials.", "headers": obj{"WWW-Authenticate": obj{"schema": openapi.Str("")}},
			"content": obj{"application/json": obj{"schema": openapi.SchemaRef("Error")}}},
		"NotFound":  mk("Unknown type or item."),
		"Forbidden": mk("Authenticated, but no grant allows it (code forbidden, message the policy) or the CSRF token is missing (code csrf)."),
		"Throttled": obj{"description": "Too many failed attempts.", "headers": obj{"Retry-After": obj{"schema": obj{"type": "integer"}}},
			"content": obj{"application/json": obj{"schema": openapi.SchemaRef("Error")}}},
		"PreconditionFailed": mk("If-Match did not match: the item changed since it was read. The ETag header and the body's current {updatedAt, updatedBy} describe its current state."),
		"Conflict":           mk("The published version changed (conflict), the item is referenced (referenced) or the key is taken (duplicate_key)."),
		"Invalid":            mk("Validation failed; error.fields lists the problems."),
		"TooLarge":           mk("Request body too large."),
		"UnsupportedMedia":   mk("Content-Type must be application/json."),
	}
}

func body(sch any) obj { return obj{"required": true, "content": openapi.JSONContent(sch)} }

func errs(codes ...string) obj {
	m := map[string]string{"400": "BadRequest", "401": "Unauthorized", "404": "NotFound", "409": "Conflict",
		"403": "Forbidden", "412": "PreconditionFailed", "413": "TooLarge", "415": "UnsupportedMedia", "422": "Invalid", "429": "Throttled"}
	out := obj{}
	for _, c := range codes {
		out[c] = openapi.Ref("responses", m[c])
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

var listParams = []string{"Locale", "Status", "Q", "Sort", "Limit", "Offset", "Where", "Fields"}

func genericPaths(paths obj, collections, singletons []*schema.Type) {
	view := withETag(openapi.OKResp("The item.", openapi.SchemaRef("ItemView")))
	paths["/types"] = obj{"get": operation("listTypes", "List content types", nil, nil,
		responses(obj{"200": openapi.OKResp("Compiled types: name, label, description, singleton, page, fields, and when set admin (group, icon, list), namePattern and options.", openapi.ArrayOf(obj{"type": "object",
			"required": []any{"name", "label", "fields"},
			"properties": obj{"name": openapi.Str(""), "label": openapi.Str(""), "description": openapi.Str(""),
				"singleton": obj{"type": "boolean"}, "page": obj{"type": "boolean"}, "namePattern": openapi.Str(""),
				"fields":  obj{"type": "array", "items": obj{"type": "object"}},
				"options": obj{"type": "object"},
				"admin": obj{"type": "object", "properties": obj{"group": openapi.Str(""), "icon": openapi.Str(""),
					"list": obj{"type": "array", "items": openapi.Str("")}}}}}))}, "401", "403"))}
	paths["/blocks"] = obj{"get": operation("listBlocks", "List block types and design tokens", nil, nil,
		responses(obj{"200": openapi.OKResp("Compiled block types and token sets.", openapi.SchemaRef("BlockCatalog"))}, "401", "403"))}
	paths["/types/{type}"] = obj{"get": operation("getType", "A compiled type and its JSON Schema",
		[]any{obj{"name": "type", "in": "path", "required": true, "schema": openapi.Str("")}}, nil,
		responses(obj{"200": openapi.OKResp("The type.", obj{"type": "object", "required": []any{"type", "jsonSchema"},
			"properties": obj{"type": obj{"type": "object"}, "jsonSchema": obj{"type": "object"}}})}, "401", "403", "404"))}
	if len(collections) > 0 {
		paths["/collections/{type}/items"] = obj{
			"get": operation("listItems", "List items of a collection type", openapi.Params(append([]string{"CollectionType"}, listParams...)...), nil,
				responses(obj{"200": openapi.OKResp("A page of items.", openapi.SchemaRef("ListResult"))}, "400", "401", "403", "404")),
			"post": operation("createItem", "Create a draft item", openapi.Params("CollectionType"), body(openapi.SchemaRef("CreateRequest")),
				responses(obj{"201": obj{"description": "The new draft.", "headers": obj{"Location": obj{"schema": openapi.Str("")}},
					"content": openapi.JSONContent(openapi.SchemaRef("ItemView"))}}, "400", "401", "403", "404", "409", "413", "415", "422")),
		}
	}
	paths["/items/{id}"] = obj{
		"get": operation("getItem", "Read an item", openapi.Params("ItemId", "Locale", "Draft", "Trashed"), nil,
			responses(obj{"200": view}, "400", "401", "403", "404")),
		"patch": operation("updateItem", "Update the draft (merge patch)", openapi.Params("ItemId", "IfMatch"), body(openapi.SchemaRef("UpdateRequest")),
			responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422")),
		"delete": operation("deleteItem", "Move an item to the trash, or delete it for good", openapi.Params("ItemId", "Permanent", "IfMatch"), nil,
			responses(obj{"204": obj{"description": "Moved to the trash (unpublished first when it was published), or deleted for good with permanent=true."}},
				"400", "401", "403", "404", "409", "412")),
	}
	paths["/items/{id}/unpublish"] = obj{"post": operation("unpublishItem", "Unpublish an item", openapi.Params("ItemId", "IfMatch"), nil,
		responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "422"))}
	paths["/items/{id}/restore"] = obj{"post": operation("restoreItem", "Restore an item from the trash (it comes back unpublished)", openapi.Params("ItemId", "IfMatch"), nil,
		responses(obj{"200": view}, "401", "403", "404", "409", "412"))}
	paths["/trash"] = obj{"get": operation("listTrash", "List trashed items, newest deletion first", openapi.Params("TrashType", "Locale", "Limit", "Offset"), nil,
		responses(obj{"200": openapi.OKResp("A page of trashed items.", openapi.SchemaRef("TrashList"))}, "400", "401", "403", "404"))}
	paths["/items/{id}/publish"] = obj{"post": operation("publishItem", "Publish draft parts", openapi.Params("ItemId", "IfMatch"),
		optionalBody(openapi.SchemaRef("PublishRequest")), responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422"))}
	paths["/items/{id}/draft"] = obj{"delete": operation("discardDraft", "Discard draft parts", openapi.Params("ItemId", "Parts", "IfMatch"), nil,
		responses(obj{"204": obj{"description": "Discarded."}}, "400", "401", "403", "404", "412"))}
	paths["/items/{id}/versions"] = obj{"get": operation("listVersions", "Version history", openapi.Params("ItemId"), nil,
		responses(obj{"200": openapi.OKResp("Versions, newest first.", openapi.SchemaRef("VersionList"))}, "401", "403", "404"))}
	paths["/items/{id}/versions/{n}"] = obj{"get": operation("getVersion", "The documents of one version (read-only)", openapi.Params("ItemId", "VersionNumber"), nil,
		responses(obj{"200": openapi.OKResp("The version.", openapi.SchemaRef("VersionView"))}, "400", "401", "403", "404"))}
	paths["/items/{id}/references"] = obj{"get": operation("listReferences", "Items pointing at this one", openapi.Params("ItemId"), nil,
		responses(obj{"200": openapi.OKResp("Referrers.", openapi.SchemaRef("ReferrerList"))}, "401", "403", "404"))}
	if len(singletons) > 0 {
		paths["/singletons/{type}"] = obj{
			"get": operation("getSingleton", "Read a singleton", openapi.Params("SingletonType", "Locale", "Draft"), nil,
				responses(obj{"200": view}, "400", "401", "403", "404")),
			"patch": operation("updateSingleton", "Update a singleton's draft (merge patch)", openapi.Params("SingletonType", "IfMatch"),
				body(openapi.SchemaRef("UpdateRequest")), responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422")),
		}
		paths["/singletons/{type}/publish"] = obj{"post": operation("publishSingleton", "Publish a singleton",
			openapi.Params("SingletonType", "IfMatch"), optionalBody(openapi.SchemaRef("PublishRequest")),
			responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422"))}
	}
	paths["/channels"] = obj{"get": operation("listChannels", "List delivery channels", nil, nil,
		responses(obj{"200": openapi.OKResp("The configured channels, sorted by name.", openapi.SchemaRef("ChannelList"))}, "401", "403"))}
	paths["/openapi.json"] = obj{"get": obj{"operationId": "getOpenAPI", "summary": "This document",
		"responses": obj{"200": openapi.OKResp("The OpenAPI document.", obj{"type": "object"})}}}
}

func optionalBody(sch any) obj {
	return obj{"required": false, "content": openapi.JSONContent(sch)}
}

func collectionPaths(paths obj, t *schema.Type) {
	n := openapi.Pascal(t.Name)
	list := operation("list"+n, "List "+t.Label+" items", openapi.Params(listParams...), nil,
		responses(obj{"200": openapi.OKResp("A page of items.", openapi.SchemaRef("ListResult"))}, "400", "401", "403"))
	list["description"] = filterNote(t)
	paths["/collections/"+t.Name+"/items"] = obj{
		"get": list,
		"post": operation("create"+n, "Create a draft "+t.Label, nil, body(openapi.SchemaRef("Create_"+t.Name)),
			responses(obj{"201": obj{"description": "The new draft.", "headers": obj{"Location": obj{"schema": openapi.Str("")}},
				"content": openapi.JSONContent(openapi.SchemaRef("ItemView_" + t.Name))}}, "400", "401", "403", "409", "413", "415", "422")),
	}
}

func singletonPaths(paths obj, t *schema.Type) {
	n := openapi.Pascal(t.Name)
	view := withETag(openapi.OKResp("The singleton.", openapi.SchemaRef("ItemView_"+t.Name)))
	paths["/singletons/"+t.Name] = obj{
		"get": operation("get"+n, "Read "+t.Label, openapi.Params("Locale", "Draft"), nil,
			responses(obj{"200": view}, "400", "401", "403", "404")),
		"patch": operation("update"+n, "Update "+t.Label+" (merge patch)", openapi.Params("IfMatch"), body(openapi.SchemaRef("Update_"+t.Name)),
			responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422")),
	}
	paths["/singletons/"+t.Name+"/publish"] = obj{"post": operation("publish"+n, "Publish "+t.Label, openapi.Params("IfMatch"),
		optionalBody(openapi.SchemaRef("PublishRequest")), responses(obj{"200": view}, "400", "401", "403", "404", "409", "412", "413", "415", "422"))}
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
			ps[i] = obj{"name": n, "in": "path", "required": true, "schema": openapi.Str("")}
		}
		op["parameters"] = ps
	}
	codes := []string{"400", "401", "403", "404", "409", "422"}
	ok := openapi.OKResp("Success.", obj{"type": "object"})
	switch rt.Method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		op["requestBody"] = obj{"required": false, "content": openapi.JSONContent(obj{"type": "object"})}
		codes = append(codes, "413", "415")
	}
	op["responses"] = responses(obj{"200": ok}, codes...)
	return op
}

// pathWords turns "/pages/sites/{site}/tree" into "PagesSitesSiteTree".
func pathWords(p string) string {
	var sb strings.Builder
	for _, w := range strings.FieldsFunc(p, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		sb.WriteString(openapi.Pascal(w))
	}
	return sb.String()
}

func withETag(r obj) obj {
	r["headers"] = obj{"ETag": obj{"schema": openapi.Str(""), "description": "Identifies the item's working state; send it as If-Match on writes."}}
	return r
}
