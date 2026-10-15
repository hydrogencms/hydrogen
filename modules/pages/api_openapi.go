package pages

import "github.com/hydrogencms/hydrogen/openapi"

type obj = openapi.Obj

func strS(desc string) obj {
	o := obj{"type": "string"}
	if desc != "" {
		o["description"] = desc
	}
	return o
}

func uuidS(desc string) obj {
	o := obj{"type": "string", "format": "uuid"}
	if desc != "" {
		o["description"] = desc
	}
	return o
}

func nullableUUID(desc string) obj {
	return obj{"oneOf": []any{obj{"type": "string", "format": "uuid"}, obj{"type": "null"}}, "description": desc}
}

func labelS() obj {
	return obj{"type": "object", "additionalProperties": obj{"type": "string"}, "description": "Label by locale."}
}

func mountSpecS() obj {
	return obj{"type": "object", "required": []any{"segment", "collection", "route"}, "additionalProperties": false,
		"properties": obj{
			"segment":    strS("The mount's own path segment under its parent."),
			"collection": strS("Content type mounted."),
			"filter": obj{"type": "object", "description": "Top-level field equals value, or any of an array of values.",
				"additionalProperties": true},
			"route":     strS("Pattern relative to the mount, e.g. {publishDate:yyyy}/{slug}."),
			"canonical": obj{"type": "boolean"},
			"label":     labelS(),
		}}
}

func linkSpecS() obj {
	return obj{"type": "object", "required": []any{"label"}, "additionalProperties": false,
		"description": "Exactly one of item, node or url.",
		"properties": obj{
			"label": labelS(),
			"item":  uuidS(""),
			"node":  uuidS(""),
			"url":   strS("Absolute URL or site path."),
		}}
}

func nodeProps() obj {
	return obj{
		"id":        uuidS(""),
		"site":      strS(""),
		"parent":    uuidS(""),
		"kind":      obj{"type": "string", "enum": []any{KindPage, KindMount, KindLink}},
		"item":      uuidS(""),
		"hidden":    obj{"type": "boolean"},
		"depth":     obj{"type": "integer"},
		"mount":     mountSpecS(),
		"link":      linkSpecS(),
		"createdAt": obj{"type": "string", "format": "date-time"},
		"updatedAt": obj{"type": "string", "format": "date-time"},
	}
}

func nodeS() obj {
	return obj{"type": "object", "required": []any{"id", "site", "kind", "hidden", "depth", "createdAt", "updatedAt"},
		"properties": nodeProps()}
}

func treeNodeS() obj {
	p := nodeProps()
	p["type"] = strS("Content type of a page node.")
	p["name"] = strS("Page name in the locale (main locale fallback); label for mounts and links.")
	p["path"] = strS("Current path in the locale; absent when the node has none.")
	p["status"] = obj{"type": "string", "enum": []any{"draft", "published", "changed"}}
	p["children"] = obj{"type": "integer"}
	p["target"] = strS("Links: resolved path or URL.")
	p["broken"] = obj{"type": "boolean", "description": "Links: the item or node they pointed at was deleted; the link has no target."}
	p["nodes"] = obj{"type": "array", "description": "Children (same shape), when depth allows.", "items": obj{"type": "object"}}
	return obj{"type": "object", "required": []any{"id", "site", "kind", "hidden", "depth", "name", "children", "createdAt", "updatedAt"},
		"properties": p}
}

func resolutionS() obj {
	return obj{"type": "object", "required": []any{"kind", "status"}, "properties": obj{
		"kind":     obj{"type": "string", "enum": []any{ResolvedPage, ResolvedItem, ResolvedRedirect, ResolvedNotFound}},
		"item":     uuidS(""),
		"node":     uuidS(""),
		"path":     strS("Canonical current path of the resolved item."),
		"status":   obj{"type": "integer", "description": "What a site should answer: 200, 301, 302, 307, 308 or 404."},
		"location": strS("Redirects: target path or URL."),
		"breadcrumbs": openapi.ArrayOf(obj{"type": "object", "required": []any{"node", "name", "path"}, "properties": obj{
			"node": uuidS(""), "item": uuidS(""), "name": strS(""), "path": strS(""),
		}}),
	}}
}

func routeS() obj {
	return obj{"type": "object", "required": []any{"site", "locale", "path", "state"}, "properties": obj{
		"site": strS(""), "locale": strS(""), "path": strS(""),
		"state":  obj{"type": "string", "enum": []any{StateCurrent, StateAlias, StateRedirect, StateManual}},
		"item":   uuidS(""),
		"node":   uuidS(""),
		"target": strS(""),
		"status": obj{"type": "integer"},
	}}
}

func pathParam(name, desc string, uuid bool) obj {
	s := strS("")
	if uuid {
		s = uuidS("")
	}
	return obj{"name": name, "in": "path", "required": true, "description": desc, "schema": s}
}

func queryParam(name, desc string, schema obj, required bool) obj {
	return obj{"name": name, "in": "query", "required": required, "description": desc, "schema": schema}
}

func errResp(desc string) obj {
	return obj{"description": desc, "content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/Error"}}}}
}

func jsonResp(desc string, s obj) obj {
	return obj{"description": desc, "content": obj{"application/json": obj{"schema": s}}}
}

func respond(ok obj, okCode string, errs ...string) obj {
	out := obj{okCode: ok}
	names := map[string]string{"400": "Bad request.", "401": "Missing or invalid token.", "404": "Not found.",
		"409": "Conflict.", "413": "Body too large.", "415": "Unsupported media type.", "422": "Validation failed."}
	for _, c := range append([]string{"401"}, errs...) {
		out[c] = errResp(names[c])
	}
	return out
}

func jsonBody(s obj) obj {
	return obj{"required": true, "content": obj{"application/json": obj{"schema": s}}}
}

func opSites() obj {
	return obj{"operationId": "listPageSites", "tags": []any{"pages"},
		"responses": respond(jsonResp("The configured sites.", obj{"type": "object", "required": []any{"items"},
			"properties": obj{"items": openapi.ArrayOf(strS(""))}}), "200")}
}

func opTree() obj {
	return obj{"operationId": "getPageTree", "tags": []any{"pages"},
		"parameters": []any{
			pathParam("site", "Site name.", false),
			queryParam("locale", "Locale for names and paths; default the main locale.", strS(""), false),
			queryParam("parent", "Node whose children to list; default the site root.", uuidS(""), false),
			queryParam("depth", "Levels to return, 1 to 10.", obj{"type": "integer", "minimum": 1, "maximum": 10, "default": 1}, false),
		},
		"responses": respond(jsonResp("Children ordered by position.", obj{"type": "object", "required": []any{"items"},
			"properties": obj{"items": openapi.ArrayOf(treeNodeS())}}), "200", "400", "404")}
}

func opCreate() obj {
	after := uuidS("Sibling to follow; absent = first.")
	page := obj{"type": "object", "title": "page", "required": []any{"kind", "item"}, "additionalProperties": false, "properties": obj{
		"kind": obj{"const": KindPage}, "item": uuidS("Page item to place."),
		"parent": uuidS("Parent page node; absent only for the site's root page."), "after": after}}
	mount := obj{"type": "object", "title": "mount", "required": []any{"kind", "parent", "segment", "collection", "route"},
		"additionalProperties": false, "properties": obj{
			"kind": obj{"const": KindMount}, "parent": uuidS("Parent page node."), "after": after,
			"segment": strS(""), "collection": strS(""), "filter": obj{"type": "object", "additionalProperties": true},
			"route": strS(""), "canonical": obj{"type": "boolean"}, "label": labelS()}}
	link := obj{"type": "object", "title": "link", "required": []any{"kind", "parent", "label"},
		"description": "Exactly one of item, node or url.", "additionalProperties": false, "properties": obj{
			"kind": obj{"const": KindLink}, "parent": uuidS("Parent page node."), "after": after, "label": labelS(),
			"item": uuidS(""), "node": uuidS(""), "url": strS("Absolute URL or site path.")}}
	resp := respond(jsonResp("The new node.", nodeS()), "201", "400", "404", "409", "413", "415", "422")
	resp["201"].(obj)["headers"] = obj{"Location": obj{"schema": strS("")}}
	return obj{"operationId": "addPageNode", "tags": []any{"pages"},
		"parameters": []any{pathParam("site", "Site name.", false)},
		"requestBody": jsonBody(obj{"oneOf": []any{page, mount, link},
			"discriminator": obj{"propertyName": "kind"}}),
		"responses": resp}
}

func opNode() obj {
	return obj{"operationId": "getPageNode", "tags": []any{"pages"},
		"parameters": []any{pathParam("id", "Node id.", true)},
		"responses":  respond(jsonResp("The node.", nodeS()), "200", "400", "404")}
}

func opPatch() obj {
	return obj{"operationId": "updatePageNode", "tags": []any{"pages"},
		"description": "parent moves the node (after positions it among the new siblings; absent = first). after alone reorders among the current siblings; null = first.",
		"parameters":  []any{pathParam("id", "Node id.", true)},
		"requestBody": jsonBody(obj{"type": "object", "additionalProperties": false, "minProperties": 1, "properties": obj{
			"parent": uuidS("New parent page node."),
			"after":  nullableUUID("Sibling to follow; null = first."),
			"hidden": obj{"type": "boolean"},
			"mount":  mountSpecS(),
			"link":   linkSpecS(),
		}}),
		"responses": respond(jsonResp("The updated node.", nodeS()), "200", "400", "404", "409", "413", "415", "422")}
}

func opDelete() obj {
	return obj{"operationId": "deletePageNode", "tags": []any{"pages"},
		"description": "Mount and link nodes only; page nodes go away with their item.",
		"parameters":  []any{pathParam("id", "Node id.", true)},
		"responses":   respond(obj{"description": "Deleted."}, "204", "400", "404", "409")}
}

func opResolve() obj {
	return obj{"operationId": "resolvePagePath", "tags": []any{"pages"},
		"description": "A lookup: always 200; the resolution's status field says what a site should answer.",
		"parameters": []any{
			pathParam("site", "Site name.", false),
			queryParam("locale", "Locale; default the main locale.", strS(""), false),
			queryParam("path", "Path to resolve, e.g. /about/team.", strS(""), true),
		},
		"responses": respond(jsonResp("The resolution.", resolutionS()), "200", "400", "404")}
}

func opPaths() obj {
	return obj{"operationId": "listItemPaths", "tags": []any{"pages"},
		"parameters": []any{pathParam("id", "Item id.", true)},
		"responses": respond(jsonResp("Every route row of the item.", obj{"type": "object", "required": []any{"items"},
			"properties": obj{"items": openapi.ArrayOf(routeS())}}), "200", "400")}
}

func locatedS() obj {
	return obj{"type": "object", "required": []any{"kind"}, "properties": obj{
		"channel": strS("The web channel that matched the URL."),
		"kind": obj{"type": "string", "enum": []any{ResolvedPage, ResolvedItem, ResolvedRedirect, ResolvedNotFound,
			LocatedReserved, LocatedNoChannel}},
		"locale":     strS("The channel language whose route matched."),
		"resolution": resolutionS(),
		"canonical":  strS("Absolute canonical URL of a page or item."),
		"alternates": openapi.ArrayOf(obj{"type": "object", "required": []any{"channel", "locale", "url"}, "properties": obj{
			"channel": strS(""), "locale": strS(""), "url": strS("Absolute URL."),
		}}),
	}}
}

func opLocate() obj {
	return obj{"operationId": "locatePageURL", "tags": []any{"pages"},
		"description": "A lookup: always 200. Picks the most specific web channel for the URL, resolves the path in the channel's languages in order and adds the canonical URL and the hreflang alternates. Request headers are not used; header.<Name> query parameters simulate header matchers.",
		"parameters": []any{
			queryParam("url", "URL to locate: absolute, or a path to match channels without a host.", strS(""), true),
			obj{"name": "header.{Name}", "in": "query", "required": false, "description": "Request header to simulate, e.g. header.X-Market=sg.",
				"schema": strS(""), "style": "form"},
		},
		"responses": respond(jsonResp("Where the URL leads.", locatedS()), "200", "400")}
}
