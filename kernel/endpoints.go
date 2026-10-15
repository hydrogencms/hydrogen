package kernel

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/manage"
)

// Auth says who may call a custom endpoint.
type Auth int

const (
	// Public endpoints need no credentials.
	Public Auth = iota + 1
	// ManageToken endpoints need the management API token (manage.token) as a bearer token.
	ManageToken
)

func (a Auth) String() string {
	switch a {
	case Public:
		return "Public"
	case ManageToken:
		return "ManageToken"
	}
	return fmt.Sprintf("Auth(%d)", int(a))
}

// EndpointPrefix is where custom endpoints are mounted.
const EndpointPrefix = "/api/x"

type endpoint struct {
	method, path string
	auth         Auth
	handler      http.HandlerFunc
}

type servicesKey struct{}

// ServicesFrom returns the kernel services for a custom endpoint's request, or nil outside one.
func ServicesFrom(ctx context.Context) *Services {
	s, _ := ctx.Value(servicesKey{}).(*Services)
	return s
}

var endpointMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

// Endpoint adds an HTTP endpoint under /api/x (path is relative, net/http pattern syntax, e.g. "/sync/{id}").
// The handler gets the services from ServicesFrom(r.Context()); a ManageToken endpoint's context also carries
// the audit actor. Panics are recovered as a 500, and manage.WriteJSON, DecodeJSON and Fail work as in
// management routes. Registering the same method and path twice is an error naming both modules.
func (r *Registry) Endpoint(method, path string, auth Auth, h http.HandlerFunc) error {
	switch {
	case !slices.Contains(endpointMethods, method):
		return fmt.Errorf("endpoint %s %s: method must be one of %s", method, path, strings.Join(endpointMethods, ", "))
	case !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \t\r\n?#") || strings.Contains(path, "//"):
		return fmt.Errorf("endpoint %s %s: path must start with / and have no spaces, query or empty segments", method, path)
	case auth != Public && auth != ManageToken:
		return fmt.Errorf("endpoint %s %s: unknown auth %v", method, path, auth)
	case h == nil:
		return fmt.Errorf("endpoint %s %s: no handler", method, path)
	}
	for _, e := range r.endpoints {
		if e.value.method == method && e.value.path == path {
			return fmt.Errorf("endpoint %s %s registered by both %s and %s", method, path, e.module, r.current)
		}
	}
	r.endpoints = append(r.endpoints, registered[endpoint]{r.current, endpoint{method, path, auth, h}})
	return nil
}

func (a *App) mountEndpoints(mux *http.ServeMux) error {
	s := a.services
	tokenSum := sha256.Sum256([]byte(s.Core.Manage.Token))
	for _, e := range a.reg.endpoints {
		ep := e.value
		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					s.Log.Error("endpoint: panic", "method", r.Method, "path", r.URL.Path, "module", e.module, "panic", v)
					manage.WriteJSON(w, http.StatusInternalServerError,
						map[string]any{"error": map[string]string{"code": "internal", "message": "internal error"}})
				}
			}()
			ctx := context.WithValue(r.Context(), servicesKey{}, s)
			if ep.auth == ManageToken {
				scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
				sum := sha256.Sum256([]byte(strings.TrimSpace(tok)))
				if s.Core.Manage.Token == "" || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(sum[:], tokenSum[:]) != 1 {
					w.Header().Set("WWW-Authenticate", "Bearer")
					manage.Fail(w, r, &manage.Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "missing or invalid bearer token"})
					return
				}
				ctx = audit.WithActor(ctx, audit.Actor{Kind: "token", ID: "manage"})
			}
			ep.handler(w, r.WithContext(ctx))
		})
		if err := handle(mux, ep.method+" "+EndpointPrefix+ep.path, h); err != nil {
			return fmt.Errorf("module %s: endpoint %s %s: %w", e.module, ep.method, ep.path, err)
		}
	}
	return nil
}
