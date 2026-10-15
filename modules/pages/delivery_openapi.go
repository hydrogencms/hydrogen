package pages

func dref(kind, name string) obj { return obj{"$ref": "#/components/" + kind + "/" + name} }

func deliveryErrors() obj {
	return obj{
		"400": dref("responses", "BadRequest"),
		"401": dref("responses", "Unauthorized"),
		"404": dref("responses", "NotFound"),
	}
}

func opRouteDelivery() obj {
	resp := deliveryErrors()
	resp["200"] = obj{"description": "Where the URL leads. kind is page or item (item holds the published payload with url and links), redirect (resolution.location), not_found or reserved (a path the frontend owns). A URL the channel does not serve is a 404.",
		"content": obj{"application/json": obj{"schema": obj{"type": "object", "required": []any{"located"}, "properties": obj{
			"located": obj{"type": "object", "required": []any{"kind"}, "properties": obj{
				"channel":    strS(""),
				"kind":       obj{"type": "string", "enum": []any{"page", "item", "redirect", "not_found", "reserved"}},
				"locale":     strS(""),
				"resolution": obj{"type": "object"},
				"canonical":  strS(""),
				"alternates": obj{"type": "array", "items": obj{"type": "object"}},
			}},
			"item": obj{"type": "object", "description": "The located item's payload in the located locale."},
		}}}}}
	return obj{"operationId": "getRoute", "tags": []any{"pages"},
		"summary": "Locate a URL",
		"parameters": []any{
			dref("parameters", "Channel"), dref("parameters", "Locale"), dref("parameters", "Preview"),
			obj{"name": "url", "in": "query", "required": true, "schema": strS(""),
				"description": "The frontend's URL or path; it must belong to the requesting channel."},
		},
		"responses": resp}
}

func opTreeDelivery() obj {
	node := obj{"type": "object", "required": []any{"id", "kind", "name"}, "properties": obj{
		"id": strS(""), "kind": obj{"type": "string", "enum": []any{"page", "mount", "link"}}, "item": strS(""),
		"type": strS(""), "name": strS(""), "path": strS(""), "url": strS(""),
		"nodes": obj{"type": "array", "items": obj{"type": "object"}},
	}}
	resp := deliveryErrors()
	resp["200"] = obj{"description": "Visible navigation below the site root, ordered by position.",
		"content": obj{"application/json": obj{"schema": obj{"type": "object", "required": []any{"site", "locale", "nodes"}, "properties": obj{
			"site": strS(""), "locale": strS(""), "nodes": obj{"type": "array", "items": node},
		}}}}}
	return obj{"operationId": "getTree", "tags": []any{"pages"},
		"summary": "Navigation of a site",
		"parameters": []any{
			dref("parameters", "Channel"), dref("parameters", "Locale"), dref("parameters", "Preview"),
			obj{"name": "site", "in": "query", "schema": strS(""), "description": "Default: the channel's site."},
			obj{"name": "depth", "in": "query", "schema": obj{"type": "integer", "minimum": 1, "maximum": 10, "default": 1}},
		},
		"responses": resp}
}
