package review

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

func idParam() m {
	return m{"name": "id", "in": "path", "required": true, "schema": str("uuid")}
}

var states = []any{"none", "in_review", "approved", "changes_requested"}

var eventSchema = m{"type": "object", "required": []any{"id", "kind", "comment", "actor", "actorName", "at"}, "properties": m{
	"id": str("uuid"), "actor": str(""), "actorName": str(""), "at": str("date-time"), "comment": str(""),
	"kind": m{"enum": []any{"submitted", "resubmitted", "approved", "changes_requested", "withdrawn", "reopened", "edited", "comment", "published", "discarded"}},
	"data": m{"type": "object", "description": "published and discarded: {reason: published|discarded|trashed}."}}}

var viewSchema = m{"type": "object",
	"required": []any{"state", "required", "submittedBy", "submittedByName", "submittedAt", "reviewers", "reviewerNames", "decidedBy",
		"decidedByName", "decidedAt", "canSubmit", "canApprove", "canWithdraw", "draftHash", "events"},
	"properties": m{"state": m{"enum": states}, "required": m{"type": "boolean", "description": "The item's type uses reviews."},
		"submittedBy": str(""), "submittedByName": str(""), "submittedAt": m{"type": []any{"string", "null"}, "format": "date-time"},
		"reviewers":     m{"type": "array", "items": str(""), "description": "user:<id> and group:<id> refs; informational."},
		"reviewerNames": m{"type": "object", "additionalProperties": str("")},
		"decidedBy":     str(""), "decidedByName": str(""), "decidedAt": m{"type": []any{"string", "null"}, "format": "date-time"},
		"canSubmit":   m{"type": "boolean", "description": "The caller may submit the draft now."},
		"canApprove":  m{"type": "boolean", "description": "The caller may approve or request changes now (four eyes applied)."},
		"canWithdraw": m{"type": "boolean"},
		"draftHash":   m{"type": "string", "description": "Identifies the draft as it is now (parts, key, availability); send it with approve to approve what you reviewed. Empty without a draft."},
		"events":      m{"type": "array", "items": eventSchema, "description": "The thread, oldest first (the latest 200)."}}}

var rowSchema = m{"type": "object", "required": []any{"item", "type", "name", "state", "submittedBy", "submittedByName", "submittedAt", "reviewers", "reviewerNames"},
	"properties": m{"item": str("uuid"), "type": str(""), "name": str(""), "state": m{"enum": states},
		"submittedBy": str(""), "submittedByName": str(""), "submittedAt": m{"type": []any{"string", "null"}, "format": "date-time"},
		"reviewers": m{"type": "array", "items": str("")}, "reviewerNames": m{"type": "object", "additionalProperties": str("")},
		"decidedBy": str(""), "decidedByName": str(""),
		"decidedAt": m{"type": []any{"string", "null"}, "format": "date-time"}, "updatedAt": str("date-time")}}

func viewResp(desc string) m {
	return m{"description": desc, "content": m{"application/json": m{"schema": viewSchema}}}
}

func body(props m, required ...any) m {
	sch := m{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	return m{"required": len(required) > 0, "content": m{"application/json": m{"schema": sch}}}
}

var commentProp = m{"comment": m{"type": "string", "maxLength": 4000}}

func opGet() m {
	return m{"operationId": "getItemReview", "summary": "The review of an item",
		"description": "State, who submitted and decided, what the caller can do, and the thread. An item whose type does not use reviews answers state none with required false. Needs read access to the item.",
		"parameters":  []any{idParam()},
		"responses":   with(m{"200": viewResp("The review.")}, errResp("401", "403", "404"))}
}

func opSubmit() m {
	return m{"operationId": "submitItemReview", "summary": "Submit a draft for review",
		"description": "Moves none or changes_requested to in_review (a resubmission when changes were requested), and an approved review whose draft changed since the approval. The item needs a draft. Needs review.submit on the item. 409 review_state when the draft is already in review or approved.",
		"parameters":  []any{idParam()},
		"requestBody": body(with(m{"reviewers": m{"type": "array", "items": str(""), "maxItems": maxReviewers,
			"description": "user:<id> and group:<id> refs; they see the review in their \"assigned to me\" queue, but anyone with review.approve may decide."}}, commentProp)),
		"responses": with(m{"200": viewResp("The review.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opApprove() m {
	return m{"operationId": "approveItemReview", "summary": "Approve the draft",
		"description": "in_review to approved. Publishing the approved draft needs no further right. The principal who submitted or edited any part of the draft, or whose token did (a token is its creator), gets 403 four_eyes (unless they hold review.bypass or content.review_four_eyes is off). Needs review.approve on the item.",
		"parameters":  []any{idParam()},
		"requestBody": body(with(m{"draftHash": m{"type": "string", "description": "The review's draftHash that the reviewer saw. When the draft differs now: 409 review_stale."}}, commentProp)),
		"responses":   with(m{"200": viewResp("The review.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opRequestChanges() m {
	return m{"operationId": "requestItemReviewChanges", "summary": "Request changes on the draft",
		"description": "in_review to changes_requested; the comment is required. Needs review.approve on the item.",
		"parameters":  []any{idParam()},
		"requestBody": body(commentProp, "comment"),
		"responses":   with(m{"200": viewResp("The review.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opWithdraw() m {
	return m{"operationId": "withdrawItemReview", "summary": "Withdraw the draft from review",
		"description": "in_review, approved or changes_requested to none. The principal who submitted it can, and anyone with review.approve.",
		"parameters":  []any{idParam()},
		"requestBody": body(commentProp),
		"responses":   with(m{"200": viewResp("The review.")}, errResp("400", "401", "403", "404", "409", "413", "415"))}
}

func opComment() m {
	return m{"operationId": "commentItemReview", "summary": "Comment on the review",
		"description": "Adds a comment to the thread; the state stays. Needs review.submit or review.approve on the item.",
		"parameters":  []any{idParam()},
		"requestBody": body(commentProp, "comment"),
		"responses":   with(m{"200": viewResp("The review.")}, errResp("400", "401", "403", "404", "413", "415"))}
}

func opQueue() m {
	return m{"operationId": "listReviews", "summary": "The review queue",
		"description": "Reviews of items the caller can read, newest submission first. Without state: in_review, changes_requested and approved. The scan stops one readable row past the page: hasMore is exact, total is exact only when hasMore is false (else a lower bound).",
		"parameters": []any{
			m{"name": "state", "in": "query", "schema": str(""), "description": "Comma-separated: in_review, approved, changes_requested, none."},
			m{"name": "type", "in": "query", "schema": str("")},
			m{"name": "reviewer", "in": "query", "schema": m{"enum": []any{"me"}}, "description": "me: reviews that name the caller or one of the caller's groups."},
			m{"name": "limit", "in": "query", "schema": m{"type": "integer", "maximum": maxPage}},
			m{"name": "offset", "in": "query", "schema": m{"type": "integer"}}},
		"responses": with(m{"200": m{"description": "A page of reviews.", "content": m{"application/json": m{"schema": m{"type": "object",
			"required":   []any{"items", "total", "hasMore"},
			"properties": m{"items": m{"type": "array", "items": rowSchema}, "total": m{"type": "integer"}, "hasMore": m{"type": "boolean"}}}}}}},
			errResp("400", "401", "403"))}
}

func opApprovers() m {
	return m{"operationId": "listReviewApprovers", "summary": "Who can approve an item",
		"description": "The users and groups that hold review.approve (and read access) on the item through their roles, with the item's limitations applied, sorted by name. The caller is left out. Needs review.submit on the item.",
		"parameters":  []any{m{"name": "item", "in": "query", "required": true, "schema": str("uuid")}},
		"responses": with(m{"200": m{"description": "The approvers.", "content": m{"application/json": m{"schema": m{"type": "object",
			"required": []any{"items"},
			"properties": m{"items": m{"type": "array", "items": m{"type": "object", "required": []any{"ref", "name", "kind"},
				"properties": m{"ref": str(""), "name": str(""), "kind": m{"enum": []any{"user", "group"}}}}}}}}}}},
			errResp("400", "401", "403", "404"))}
}
