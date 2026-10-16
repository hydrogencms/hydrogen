package release

type m = map[string]any

func ref(code string) m { return m{"$ref": "#/components/responses/" + code} }

func uuidParam(name string) m {
	return m{"name": name, "in": "path", "required": true, "schema": m{"type": "string", "format": "uuid"}}
}

func str(format string) m {
	if format == "" {
		return m{"type": "string"}
	}
	return m{"type": "string", "format": format}
}

func errResp(codes ...string) m {
	names := map[string]string{"400": "BadRequest", "401": "Unauthorized", "403": "Forbidden", "404": "NotFound",
		"409": "Conflict", "413": "TooLarge", "415": "UnsupportedMedia"}
	out := m{}
	for _, c := range codes {
		out[c] = ref(names[c])
	}
	return out
}

func with(base m, extra m) m {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

var itemErrorSchema = m{"type": "object", "required": []any{"code", "message"}, "properties": m{
	"item": str("uuid"), "code": str(""), "message": str(""), "path": str("")}}

var itemViewSchema = m{"type": "object", "required": []any{"item", "type", "name", "action", "locales", "state", "addedBy", "addedAt"},
	"properties": m{"item": str("uuid"), "type": str(""), "name": str(""), "action": m{"enum": []any{"publish", "unpublish"}},
		"locales": m{"type": "array", "items": str("")},
		"state": m{"enum": []any{"changes", "current", "trashed", "missing", "hidden"},
			"description": "changes: the action has something to do; current: nothing to do; trashed or missing: the item is gone; " +
				"hidden: the caller may not read it."},
		"addedBy": str(""), "addedAt": str("date-time")}}

var releaseSchema = m{"type": "object",
	"required": []any{"id", "name", "description", "kind", "status", "errors", "createdBy", "createdByName", "updatedBy", "createdAt", "updatedAt", "itemCount"},
	"properties": m{"id": str("uuid"), "name": str(""), "description": str(""), "kind": m{"enum": []any{"release", "item"}},
		"status":      m{"enum": []any{"open", "scheduled", "publishing", "published", "failed", "cancelled"}},
		"scheduledAt": str("date-time"), "publishedAt": str("date-time"),
		"errors":    m{"type": "array", "items": itemErrorSchema, "description": "Why the last publish failed."},
		"createdBy": str(""), "createdByName": str(""), "updatedBy": str(""), "scheduledBy": str(""),
		"createdAt": str("date-time"), "updatedAt": str("date-time"), "itemCount": m{"type": "integer"},
		"items":   m{"type": "array", "items": itemViewSchema, "description": "In the detail views only."},
		"action":  m{"enum": []any{"publish", "unpublish"}, "description": "GET /items/{id}/releases only: the release's action for that item."},
		"locales": m{"type": "array", "items": str(""), "description": "GET /items/{id}/releases only."}}}

func relResp(desc string) m {
	return m{"description": desc, "content": m{"application/json": m{"schema": releaseSchema}}}
}

func listResp(desc string) m {
	return m{"description": desc, "content": m{"application/json": m{"schema": m{"type": "object",
		"required":   []any{"items", "total", "hasMore"},
		"properties": m{"items": m{"type": "array", "items": releaseSchema}, "total": m{"type": "integer"}, "hasMore": m{"type": "boolean"}}}}}}
}

func jsonBody(props m, required ...any) m {
	sch := m{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	return m{"required": true, "content": m{"application/json": m{"schema": sch}}}
}

var itemProps = m{
	"action":  m{"enum": []any{"publish", "unpublish"}, "default": "publish"},
	"locales": m{"type": "array", "items": str(""), "description": "Publish only these locales (default: every locale with a draft)."},
}

func opList() m {
	return m{"operationId": "listReleases", "summary": "List releases",
		"description": "Newest first. Needs content.read.",
		"parameters": []any{
			m{"name": "status", "in": "query", "schema": str(""), "description": "Comma-separated statuses: open, scheduled, publishing, published, failed, cancelled."},
			m{"name": "kind", "in": "query", "schema": m{"enum": []any{"release", "item"}}},
			m{"name": "item", "in": "query", "schema": str("uuid"), "description": "Only releases that contain this item."},
			m{"name": "limit", "in": "query", "schema": m{"type": "integer", "maximum": maxPage}},
			m{"name": "offset", "in": "query", "schema": m{"type": "integer"}}},
		"responses": with(m{"200": listResp("A page of releases, without their items.")}, errResp("400", "401", "403"))}
}

func opCreate() m {
	return m{"operationId": "createRelease", "summary": "Create a release",
		"description": "Creates an open release; with scheduledAt it is scheduled at once (it needs items then). Needs release.manage and content.read on the items.",
		"requestBody": jsonBody(m{"name": str(""), "description": str(""), "kind": m{"enum": []any{"release", "item"}, "default": "release"},
			"scheduledAt": str("date-time"),
			"items": m{"type": "array", "items": m{"type": "object", "required": []any{"item"}, "additionalProperties": false,
				"properties": with(m{"item": str("uuid")}, itemProps)}}}, "name"),
		"responses": with(m{"201": relResp("The release.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opGet() m {
	return m{"operationId": "getRelease", "summary": "Read a release", "description": "With its items. Needs content.read.",
		"parameters": []any{uuidParam("id")},
		"responses":  with(m{"200": relResp("The release.")}, errResp("401", "403", "404"))}
}

func opPatch() m {
	return m{"operationId": "updateRelease", "summary": "Rename a release",
		"description": "Changes the name or description. A failed release becomes open again; published and cancelled releases cannot change (409 release_state).",
		"parameters":  []any{uuidParam("id")},
		"requestBody": jsonBody(m{"name": str(""), "description": str("")}),
		"responses":   with(m{"200": relResp("The release.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opDelete() m {
	return m{"operationId": "deleteRelease", "summary": "Delete a release",
		"description": "Only open and cancelled releases (409 release_state otherwise).",
		"parameters":  []any{uuidParam("id")},
		"responses":   with(m{"204": m{"description": "Deleted."}}, errResp("401", "403", "404", "409"))}
}

func opPutItem() m {
	return m{"operationId": "putReleaseItem", "summary": "Add an item to a release",
		"description": "Adds the item or replaces its action and locales. Only open and failed releases change items (a failed one becomes open); " +
			"scheduled releases must be unscheduled first. A trashed item is refused (409 trashed), and so is unpublishing a singleton.",
		"parameters":  []any{uuidParam("id"), uuidParam("item")},
		"requestBody": jsonBody(itemProps),
		"responses":   with(m{"200": relResp("The release.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opDeleteItem() m {
	return m{"operationId": "deleteReleaseItem", "summary": "Remove an item from a release",
		"description": "Idempotent. Same status rules as adding.",
		"parameters":  []any{uuidParam("id"), uuidParam("item")},
		"responses":   with(m{"204": m{"description": "Removed."}}, errResp("401", "403", "404", "409"))}
}

func opSchedule() m {
	return m{"operationId": "scheduleRelease", "summary": "Schedule a release",
		"description": "Publishes the release at a future time (at most a year ahead) as the caller: the job re-loads the principal then, and a " +
			"user who is gone or lost rights makes the release fail with forbidden. Open, failed and scheduled releases with items.",
		"parameters":  []any{uuidParam("id")},
		"requestBody": jsonBody(m{"at": str("date-time")}, "at"),
		"responses":   with(m{"200": relResp("The release.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opState(id, summary, desc string) m {
	return m{"operationId": id, "summary": summary, "description": desc, "parameters": []any{uuidParam("id")},
		"responses": with(m{"200": relResp("The release.")}, errResp("401", "403", "404", "409"))}
}

func opPublish() m {
	return m{"operationId": "publishRelease", "summary": "Publish a release now",
		"description": "Publishes (or unpublishes) every item in one transaction, as the caller. If any item cannot be, nothing is: the answer is 422 " +
			"release_failed with the release (status failed, errors listing item, code, message and path) and the release can be fixed and published " +
			"again. force publishes over draft conflicts. 409 release_state when the release is not open, scheduled or failed, or is being published.",
		"parameters": []any{uuidParam("id")},
		"requestBody": m{"required": false, "content": m{"application/json": m{"schema": m{"type": "object", "additionalProperties": false,
			"properties": m{"force": m{"type": "boolean"}}}}}},
		"responses": with(m{"200": relResp("Published."),
			"422": m{"description": "Nothing was published.", "content": m{"application/json": m{"schema": m{"type": "object",
				"required": []any{"error", "release"}, "properties": m{
					"error":   m{"type": "object", "properties": m{"code": m{"const": "release_failed"}, "message": str("")}},
					"release": releaseSchema}}}}}},
			errResp("401", "403", "404", "409", "413", "415"))}
}

func opOfItem() m {
	return m{"operationId": "listItemReleases", "summary": "Releases that contain an item",
		"description": "Open and scheduled releases, each with the item's action and locales. Needs content.read.",
		"parameters":  []any{uuidParam("id")},
		"responses":   with(m{"200": listResp("The releases.")}, errResp("401", "403", "404"))}
}
