package collab

type m = map[string]any

func ref(code string) m { return m{"$ref": "#/components/responses/" + code} }

func str(format string) m {
	if format == "" {
		return m{"type": "string"}
	}
	return m{"type": "string", "format": format}
}

func errResp(codes ...string) m {
	names := map[string]string{"400": "BadRequest", "401": "Unauthorized", "403": "Forbidden", "404": "NotFound",
		"409": "Conflict", "413": "TooLarge", "415": "UnsupportedMedia", "429": "Throttled"}
	out := m{}
	for _, c := range codes {
		out[c] = ref(names[c])
	}
	return out
}

func idParam() m {
	return m{"name": "id", "in": "path", "required": true, "schema": str("uuid")}
}

func body(props m, required ...any) m {
	sch := m{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	return m{"required": true, "content": m{"application/json": m{"schema": sch}}}
}

func with(base, extra m) m {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

var updateSchema = m{"type": "object", "required": []any{"seq", "data", "client"}, "properties": m{
	"seq": m{"type": "integer", "format": "int64"}, "data": str("byte"), "client": str(""),
	"init": m{"type": "array", "items": str(""), "description": "Parts this update seeded."}}}

var participantSchema = m{"type": "object",
	"required": []any{"client", "principal", "name", "color", "locale", "focus", "idle", "edit"},
	"properties": m{"client": str(""), "principal": str(""), "name": str(""), "color": str(""), "locale": str(""),
		"focus": str(""), "idle": m{"type": "boolean"}, "edit": m{"type": "boolean"}}}

var joinedSchema = m{"type": "object", "required": []any{"client", "session", "seq", "updates", "more", "init", "pending", "presence", "etag"},
	"properties": m{"client": str(""), "session": str(""), "seq": m{"type": "integer", "format": "int64"},
		"updates":  m{"type": "array", "items": updateSchema},
		"init":     m{"type": "array", "items": str(""), "description": "Parts this client must seed and post with init."},
		"pending":  m{"type": "array", "items": str(""), "description": "Parts another client is seeding."},
		"presence": m{"type": "array", "items": participantSchema},
		"etag":     str("")}}

func opJoin() m {
	return m{"operationId": "joinItemCollab", "summary": "Join the co-editing session of an item",
		"description": "Needs read access to the item; edit true needs content.edit on the item and locale (403). The answer carries the log " +
			"to apply, the parts to seed (claimed for this client alone, for 15 s) and those another client seeds, the presence list " +
			"and the item's etag now. client may name a client that re-joins. A long log is paged: with more true, fetch the rest with " +
			"GET updates. A session seeded from an older draft is dropped first (its clients get reset external). 429 session_full " +
			"beyond 20 clients.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"locale": str(""), "edit": m{"type": "boolean"}, "client": str("")}, "locale"),
		"responses": with(m{"200": m{"description": "The session.", "content": m{"application/json": m{"schema": joinedSchema}}}},
			errResp("400", "401", "403", "404", "409", "413", "415", "429")),
	}
}

func opEvents() m {
	return m{"operationId": "streamItemCollab", "summary": "The event stream of a joined client",
		"description": "Server-Sent Events: update (an update object; id is its seq), presence {clients}, saved {etag, client, name, seq}, " +
			"reset {reason: external|expired|trashed} (the stream ends: reload the draft and join again), compact {upto} and a ping " +
			"comment every 25 seconds. Updates after the Last-Event-ID header, else since, else the log's head are replayed first. " +
			"One stream per client (a new one closes the older). 429 too_many_streams beyond 20 streams per item, 6 per principal and " +
			"item, or 2000 per server.",
		"parameters": []any{idParam(),
			m{"name": "client", "in": "query", "required": true, "schema": str("")},
			m{"name": "since", "in": "query", "schema": m{"type": "integer", "format": "int64"}}},
		"responses": with(m{"200": m{"description": "The stream.", "content": m{"text/event-stream": m{"schema": str("")}}},
			"429": m{"description": "Too many open streams."}}, errResp("400", "401", "403", "404")),
	}
}

func opUpdates() m {
	return m{"operationId": "postItemCollabUpdate", "summary": "Post an update",
		"description": "At most 512 KB. Editing clients only (403 read_only; a client that lost edit access becomes a viewer). init lists the parts " +
			"this update seeds: 409 claim_lost when the claim expired or went to another client (join again). 409 compact_required " +
			"when the log is at its hard cap (2000 updates or 32 MB): a compact event was sent, retry after it. 429 rate_limited " +
			"beyond 50 updates at once / 20 a second per client (see Retry-After). 404 unknown_client when the client was dropped, " +
			"also when it lost read access to the item.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"client": str(""), "data": str("byte"), "init": m{"type": "array", "items": str("")}}, "client", "data"),
		"responses": with(m{"200": m{"description": "Stored.", "content": m{"application/json": m{"schema": m{
			"type": "object", "required": []any{"seq"}, "properties": m{"seq": m{"type": "integer", "format": "int64"}}}}}}},
			errResp("400", "401", "403", "404", "409", "413", "415", "429")),
	}
}

func opPage() m {
	return m{"operationId": "pageItemCollabUpdates", "summary": "Read a page of the log",
		"description": "The updates after since (at most 200 and about 2 MB), the seq to continue from and whether more follow. For a client " +
			"that joined with more true.",
		"parameters": []any{idParam(),
			m{"name": "client", "in": "query", "required": true, "schema": str("")},
			m{"name": "since", "in": "query", "required": true, "schema": m{"type": "integer", "format": "int64"}}},
		"responses": with(m{"200": m{"description": "A page.", "content": m{"application/json": m{"schema": m{
			"type": "object", "required": []any{"updates", "seq", "more"}, "properties": m{
				"updates": m{"type": "array", "items": updateSchema}, "seq": m{"type": "integer", "format": "int64"},
				"more": m{"type": "boolean"}}}}}}}, errResp("400", "401", "403", "404")),
	}
}

func opReset() m {
	return m{"operationId": "resetItemCollab", "summary": "Drop the session",
		"description": "For an editing client that cannot apply the log: drops the log, the claims and every client, and sends reset " +
			"external to all streams. Join again afterwards.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"client": str("")}, "client"),
		"responses":   with(m{"204": m{"description": "Dropped."}}, errResp("400", "401", "403", "404", "413", "415")),
	}
}

func opCompact() m {
	return m{"operationId": "compactItemCollab", "summary": "Replace the log up to a seq by one state",
		"description": "Sent in answer to a compact event, by the client it named, with upto at most the event's. data must be a state that includes every update up to upto. At most 2.5 MB. 409 compact_not_requested otherwise.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"client": str(""), "data": str("byte"), "upto": m{"type": "integer", "format": "int64"}}, "client", "data", "upto"),
		"responses":   with(m{"204": m{"description": "Compacted."}}, errResp("400", "401", "403", "404", "413", "415")),
	}
}

func opPresence() m {
	return m{"operationId": "presenceItemCollab", "summary": "Heartbeat and presence",
		"description": "Every 15 s and on focus change; a client without a heartbeat for 45 s is dropped (404 unknown_client afterwards: join again). " +
			"A new locale must be one the client may read (and edit, for an editing client): 403 otherwise. The call re-checks the client's access to the item.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"client": str(""), "locale": str(""), "focus": str(""), "idle": m{"type": "boolean"}}, "client", "locale"),
		"responses":   with(m{"204": m{"description": "Recorded."}}, errResp("400", "401", "403", "404", "413", "415")),
	}
}

func opLeave() m {
	return m{"operationId": "leaveItemCollab", "summary": "Leave the session",
		"description": "Idempotent.",
		"parameters":  []any{idParam()},
		"requestBody": body(m{"client": str("")}, "client"),
		"responses":   with(m{"204": m{"description": "Left."}}, errResp("400", "401", "403", "413", "415")),
	}
}
