package kernel

import (
	"net/http"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
)

// systemRoutes is GET /system: the system report (policy system.read).
func (a *App) systemRoutes() []manage.Route {
	type m = map[string]any
	str := m{"type": "string"}
	return []manage.Route{{
		Method: http.MethodGet, Path: "/system", Module: "core", Summary: "Read the system report",
		Policy: access.PolicySystemRead,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			manage.WriteJSON(w, http.StatusOK, a.System())
		},
		OpenAPI: m{
			"operationId": "getSystem",
			"summary":     "Read the system report",
			"description": "The core version and Go toolchain, the registered modules in registration order with the Go " +
				"module and version each was built from (\"devel\" for a local build), and what the build uses.",
			"responses": m{
				"200": m{"description": "The system report.", "content": m{"application/json": m{"schema": m{
					"type": "object", "required": []any{"core", "modules", "database", "search", "media"},
					"properties": m{
						"core": m{"type": "object", "required": []any{"version", "go"},
							"properties": m{"version": m{"type": "string", "description": "The core version, \"devel\" for a local build."},
								"go": m{"type": "string", "description": "The Go toolchain version."}}},
						"modules": m{"type": "array", "items": m{"type": "object",
							"required": []any{"name", "package", "module", "version", "requires"},
							"properties": m{"name": str, "package": str,
								"module":   m{"type": "string", "description": "The Go module path; empty when unknown."},
								"version":  str,
								"requires": m{"type": "array", "items": str}}}},
						"database": m{"type": "string", "description": "The database adapter: sqlite or postgres."},
						"search":   m{"type": "string", "description": "The search engine's name."},
						"media":    m{"type": "boolean", "description": "Whether uploads are enabled."},
					}}}}},
				"401": m{"$ref": "#/components/responses/Unauthorized"},
				"403": m{"$ref": "#/components/responses/Forbidden"},
			},
		},
	}}
}
