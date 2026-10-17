package manage

import "github.com/hydrogencms/hydrogen/api/openapi"

// authSchemas are the schemas of the sign-in and access-management routes.
func authSchemas() obj {
	uuid, dt, strs := openapi.Str("uuid"), openapi.Str("date-time"), openapi.ArrayOf(openapi.Str(""))
	list := func(item string) obj {
		return obj{"type": "object", "required": []any{"items", "total"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef(item)), "total": obj{"type": "integer"}}}
	}
	limits := obj{"type": "object", "additionalProperties": strs,
		"description": "Limitation type to its values, e.g. {\"channel\": [\"careers_en\"]}."}
	return obj{
		"User": obj{"type": "object", "required": []any{"id", "login", "email", "name", "disabled", "hasPassword", "createdAt", "updatedAt"},
			"properties": obj{"id": uuid, "login": openapi.Str(""), "email": openapi.Str(""), "name": openapi.Str(""), "disabled": obj{"type": "boolean"},
				"hasPassword": obj{"type": "boolean"}, "createdAt": dt, "updatedAt": dt, "lastLoginAt": dt}},
		"UserList": list("User"),
		"Group": obj{"type": "object", "required": []any{"id", "name", "description", "createdAt", "updatedAt"},
			"properties": obj{"id": uuid, "name": openapi.Str(""), "description": openapi.Str(""), "createdAt": dt, "updatedAt": dt}},
		"GroupList": list("Group"),
		"RolePolicy": obj{"type": "object", "required": []any{"policy"}, "additionalProperties": false,
			"properties": obj{"policy": openapi.Str(""), "limitations": limits}},
		"Role": obj{"type": "object", "required": []any{"id", "name", "description", "policies", "createdAt", "updatedAt"},
			"properties": obj{"id": uuid, "name": openapi.Str(""), "description": openapi.Str(""), "policies": openapi.ArrayOf(openapi.SchemaRef("RolePolicy")),
				"template": openapi.Str(""), "createdAt": dt, "updatedAt": dt}},
		"RoleList": list("Role"),
		"Assignment": obj{"type": "object", "required": []any{"id", "roleId", "principalKind", "principalId", "limitations", "createdAt"},
			"properties": obj{"id": uuid, "roleId": uuid, "role": openapi.Str(""),
				"principalKind": obj{"type": "string", "enum": []any{"user", "group", "token"}}, "principalId": uuid,
				"limitations": limits, "createdAt": dt}},
		"AssignmentList": list("Assignment"),
		"Token": obj{"type": "object", "required": []any{"id", "name", "kind", "prefix", "createdBy", "createdAt"},
			"description": "An API token as listed: never its secret.",
			"properties": obj{"id": uuid, "name": openapi.Str(""), "kind": obj{"type": "string", "enum": []any{"token", "agent"}},
				"prefix": openapi.Str(""), "createdBy": openapi.Str(""), "createdAt": dt, "expiresAt": dt, "lastUsedAt": dt, "revokedAt": dt}},
		"TokenList": list("Token"),
		"TokenCreated": obj{"type": "object", "required": []any{"token", "secret"},
			"properties": obj{"token": openapi.SchemaRef("Token"),
				"secret": obj{"type": "string", "description": "The bearer secret. Shown once; only its hash is stored."}}},
		"Grant": obj{"type": "object", "required": []any{"policy", "role"},
			"properties": obj{"policy": openapi.Str(""), "role": openapi.Str(""), "limits": openapi.ArrayOf(obj{"type": "object",
				"required": []any{"name", "values"}, "properties": obj{"name": openapi.Str(""), "values": strs}})}},
		"Session": obj{"type": "object", "required": []any{"principal", "grants"},
			"properties": obj{
				"principal": obj{"type": "object", "required": []any{"kind", "name"},
					"properties": obj{"kind": obj{"type": "string", "enum": []any{"user", "token", "agent"}}, "id": uuid, "name": openapi.Str("")}},
				"grants":    openapi.ArrayOf(openapi.SchemaRef("Grant")),
				"groups":    obj{"type": "array", "items": uuid, "description": "Users only: ids of the groups the user belongs to."},
				"csrf":      obj{"type": "string", "description": "Session requests send this as X-CSRF-Token on unsafe methods."},
				"expiresAt": dt}},
		"LoginResult": obj{"type": "object", "required": []any{"user", "csrf", "expiresAt"},
			"properties": obj{"user": openapi.SchemaRef("User"), "csrf": openapi.Str(""), "expiresAt": dt}},
		"Catalog": obj{"type": "object", "required": []any{"policies", "limitations"},
			"properties": obj{
				"policies": openapi.ArrayOf(obj{"type": "object", "required": []any{"name", "description", "limitations"},
					"properties": obj{"name": openapi.Str(""), "description": openapi.Str(""), "limitations": strs}}),
				"limitations": openapi.ArrayOf(obj{"type": "object", "required": []any{"name", "choices", "policies"},
					"properties": obj{"name": openapi.Str(""), "policies": strs,
						"choices": obj{"type": []any{"array", "null"}, "description": "Values to offer; null means free text.",
							"items": obj{"type": "object", "required": []any{"value", "label"},
								"properties": obj{"value": openapi.Str(""), "label": openapi.Str("")}}}}})}},
		"AuditEntry": obj{"type": "object", "required": []any{"id", "at", "actorKind", "action"},
			"properties": obj{"id": obj{"type": "integer"}, "at": dt, "actorKind": openapi.Str(""), "actorId": openapi.Str(""), "action": openapi.Str(""),
				"subject": openapi.Str(""), "data": obj{"description": "Action data; an agent's model and task are under actor."}}},
		"AuditList": obj{"type": "object", "required": []any{"items", "hasMore"},
			"properties": obj{"items": openapi.ArrayOf(openapi.SchemaRef("AuditEntry")), "hasMore": obj{"type": "boolean"},
				"next": obj{"type": "integer", "description": "With hasMore: the last id of this page, for before."}}},
	}
}

func authParameters() obj {
	return obj{
		"UserId":    obj{"name": "id", "in": "path", "required": true, "schema": openapi.Str("uuid")},
		"GroupUser": obj{"name": "user", "in": "path", "required": true, "schema": openapi.Str("uuid")},
	}
}

// authPaths documents the routes of authRoutes.
func authPaths(paths obj) {
	pageParams := []any{openapi.Ref("parameters", "Limit"), openapi.Ref("parameters", "Offset")}
	byID := []any{openapi.Ref("parameters", "UserId")}
	with := func(a, b []any) []any { return append(append([]any{}, a...), b...) }
	resp := func(ok obj, codes ...string) obj {
		out := obj{}
		for _, c := range codes {
			switch c {
			case "403":
				out[c] = openapi.Ref("responses", "Forbidden")
			case "429":
				out[c] = openapi.Ref("responses", "Throttled")
			default:
				for k, v := range errs(c) {
					out[k] = v
				}
			}
		}
		for k, v := range ok {
			out[k] = v
		}
		return out
	}
	noContent := obj{"204": obj{"description": "Done."}}
	created := func(sch string) obj {
		return obj{"201": obj{"description": "Created.", "headers": obj{"Location": obj{"schema": openapi.Str("")}},
			"content": openapi.JSONContent(openapi.SchemaRef(sch))}}
	}
	ok := func(sch string) obj { return obj{"200": openapi.OKResp("OK.", openapi.SchemaRef(sch))} }
	jsonBody := func(props obj, required ...string) obj {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		return body(obj{"type": "object", "additionalProperties": false, "required": req, "properties": props})
	}
	strs := openapi.ArrayOf(openapi.Str(""))

	login := operation("login", "Sign in with a login and password",
		nil, jsonBody(obj{"login": openapi.Str(""), "password": openapi.Str("password")}, "login", "password"),
		resp(ok("LoginResult"), "400", "401", "413", "415", "429"))
	login["description"] = "Sets the hydrogen_session cookie (HttpOnly, SameSite=Lax) and returns the CSRF token to send as " +
		"X-CSRF-Token on unsafe requests made with the cookie. Bearer requests need no CSRF token."
	login["security"] = []any{}
	paths["/auth/login"] = obj{"post": login}
	paths["/auth/logout"] = obj{"post": operation("logout", "End the session", nil, nil, resp(noContent, "401", "403"))}
	paths["/auth/session"] = obj{"get": operation("getSession", "Who is calling, and what they may do", nil, nil,
		resp(ok("Session"), "401"))}
	paths["/auth/password"] = obj{"post": operation("changePassword", "Change your own password; ends your other sessions and keeps this one", nil,
		jsonBody(obj{"current": openapi.Str("password"), "new": openapi.Str("password")}, "current", "new"),
		resp(noContent, "400", "401", "403", "413", "415", "429"))}

	paths["/users"] = obj{
		"get": operation("listUsers", "List users; q keeps those whose login, name or email contains it (case-insensitive)",
			with(pageParams, []any{obj{"name": "q", "in": "query", "schema": openapi.Str("")}}), nil, resp(ok("UserList"), "400", "401", "403")),
		"post": operation("createUser", "Create a user", nil,
			jsonBody(obj{"login": openapi.Str(""), "name": openapi.Str(""), "email": openapi.Str(""), "password": openapi.Str("password")}, "login"),
			resp(created("User"), "400", "401", "403", "413", "415")),
	}
	paths["/users/{id}"] = obj{
		"get": operation("getUser", "Read a user", byID, nil, resp(ok("User"), "401", "403", "404")),
		"patch": operation("updateUser", "Update a user; a password resets it and ends their sessions", byID,
			jsonBody(obj{"name": openapi.Str(""), "email": openapi.Str(""), "disabled": obj{"type": "boolean"}, "password": openapi.Str("password")}),
			resp(ok("User"), "400", "401", "403", "404", "413", "415")),
		"delete": operation("deleteUser", "Delete a user", byID, nil, resp(noContent, "400", "401", "403", "404")),
	}
	paths["/users/{id}/groups"] = obj{"get": operation("listUserGroups", "List the groups a user belongs to", byID, nil,
		resp(ok("GroupList"), "401", "403", "404"))}
	paths["/groups"] = obj{
		"get": operation("listGroups", "List groups", pageParams, nil, resp(ok("GroupList"), "400", "401", "403")),
		"post": operation("createGroup", "Create a group", nil, jsonBody(obj{"name": openapi.Str(""), "description": openapi.Str("")}, "name"),
			resp(created("Group"), "400", "401", "403", "413", "415")),
	}
	paths["/groups/{id}"] = obj{
		"get": operation("getGroup", "Read a group", byID, nil, resp(ok("Group"), "401", "403", "404")),
		"patch": operation("updateGroup", "Update a group", byID, jsonBody(obj{"name": openapi.Str(""), "description": openapi.Str("")}),
			resp(ok("Group"), "400", "401", "403", "404", "413", "415")),
		"delete": operation("deleteGroup", "Delete a group", byID, nil, resp(noContent, "401", "403", "404")),
	}
	paths["/groups/{id}/members"] = obj{"get": operation("listMembers", "List a group's members", with(byID, pageParams), nil,
		resp(ok("UserList"), "400", "401", "403", "404"))}
	member := with(byID, []any{openapi.Ref("parameters", "GroupUser")})
	paths["/groups/{id}/members/{user}"] = obj{
		"put":    operation("addMember", "Add a user to a group", member, nil, resp(noContent, "400", "401", "403", "404")),
		"delete": operation("removeMember", "Remove a user from a group", member, nil, resp(noContent, "400", "401", "403", "404")),
	}
	paths["/roles"] = obj{
		"get": operation("listRoles", "List roles", pageParams, nil, resp(ok("RoleList"), "400", "401", "403")),
		"post": operation("createRole", "Create a role", nil,
			jsonBody(obj{"name": openapi.Str(""), "description": openapi.Str(""), "policies": openapi.ArrayOf(openapi.SchemaRef("RolePolicy"))}, "name"),
			resp(created("Role"), "400", "401", "403", "413", "415")),
	}
	paths["/roles/{id}"] = obj{
		"get": operation("getRole", "Read a role", byID, nil, resp(ok("Role"), "401", "403", "404")),
		"patch": operation("updateRole", "Update a role", byID,
			jsonBody(obj{"name": openapi.Str(""), "description": openapi.Str(""), "policies": openapi.ArrayOf(openapi.SchemaRef("RolePolicy"))}),
			resp(ok("Role"), "400", "401", "403", "404", "413", "415")),
		"delete": operation("deleteRole", "Delete a role and its assignments", byID, nil, resp(noContent, "401", "403", "404")),
	}
	paths["/roles/{id}/assignments"] = obj{"get": operation("listRoleAssignments", "Who holds a role", byID, nil,
		resp(ok("AssignmentList"), "401", "403", "404"))}
	paths["/assignments"] = obj{
		"get": operation("listAssignments", "The roles of one principal", []any{
			obj{"name": "principalKind", "in": "query", "required": true, "schema": obj{"type": "string", "enum": []any{"user", "group", "token"}}},
			obj{"name": "principalId", "in": "query", "required": true, "schema": openapi.Str("uuid")}}, nil,
			resp(ok("AssignmentList"), "400", "401", "403")),
		"post": operation("createAssignment", "Give a role to a user, group or token", nil,
			jsonBody(obj{"roleId": openapi.Str("uuid"), "principalKind": obj{"type": "string", "enum": []any{"user", "group", "token"}},
				"principalId": openapi.Str("uuid"), "limitations": obj{"type": "object", "additionalProperties": strs}},
				"roleId", "principalKind", "principalId"),
			resp(created("Assignment"), "400", "401", "403", "413", "415")),
	}
	paths["/assignments/{id}"] = obj{"delete": operation("deleteAssignment", "Remove an assignment", byID, nil,
		resp(noContent, "400", "401", "403", "404"))}
	paths["/policies"] = obj{"get": operation("getPolicies", "Policies and limitation types roles are built from", nil, nil,
		resp(ok("Catalog"), "401", "403", "404"))}
	paths["/tokens"] = obj{
		"get": operation("listTokens", "List API tokens", pageParams, nil, resp(ok("TokenList"), "400", "401", "403")),
		"post": operation("createToken", "Create an API token; the secret is in this response only", nil,
			jsonBody(obj{"name": openapi.Str(""), "kind": obj{"type": "string", "enum": []any{"token", "agent"}},
				"roles": obj{"type": "array", "items": openapi.Str(""), "description": "Role ids or names."}, "expiresAt": dt()},
				"name"),
			resp(created("TokenCreated"), "400", "401", "403", "404", "413", "415")),
	}
	paths["/tokens/{id}"] = obj{
		"get":    operation("getToken", "Read a token", byID, nil, resp(ok("Token"), "401", "403", "404")),
		"delete": operation("revokeToken", "Revoke a token", byID, nil, resp(noContent, "401", "403", "404")),
	}
	paths["/audit"] = obj{"get": operation("listAudit", "Read the audit log, newest first", []any{
		obj{"name": "actor", "in": "query", "schema": openapi.Str(""), "description": "A kind (agent) or kind:id."},
		obj{"name": "action", "in": "query", "schema": openapi.Str(""), "description": "An action, or a prefix ending in * (token.*)."},
		obj{"name": "subject", "in": "query", "schema": openapi.Str("")},
		obj{"name": "since", "in": "query", "schema": openapi.Str("date-time")},
		obj{"name": "before", "in": "query", "schema": obj{"type": "integer", "minimum": 1}, "description": "Cursor: only entries with a smaller id; pass the previous page's next."},
		openapi.Ref("parameters", "Limit"), openapi.Ref("parameters", "Offset")}, nil, resp(ok("AuditList"), "400", "401", "403"))}
}

func dt() obj { return openapi.Str("date-time") }
