package kernel

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/api/manage"
)

// Auth says who may call a custom endpoint: Public, Authenticated or Policy("module.action").
type Auth string

const (
	// Public endpoints need no credentials.
	Public Auth = "public"
	// Authenticated endpoints need any authenticated principal: a session, an API token or the config token.
	Authenticated Auth = "authenticated"
)

const policyPrefix = "policy:"

// Policy endpoints need a principal that holds the policy (a registered policy name, e.g. "content.edit"). The
// check is coarse, as for management routes: the handler must check the policy against what the request acts
// on, with Services.Access.Can.
func Policy(name string) Auth { return Auth(policyPrefix + name) }

func (a Auth) policy() (string, bool) { return strings.CutPrefix(string(a), policyPrefix) }

func (a Auth) String() string { return string(a) }

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
// The handler gets the services from ServicesFrom(r.Context()); a non-public endpoint's context also carries the
// principal (access.PrincipalFrom) and the audit actor. Panics are recovered as a 500, and manage.WriteJSON, DecodeJSON and Fail work as in
// management routes. Registering the same method and path twice is an error naming both modules.
func (r *Registry) Endpoint(method, path string, auth Auth, h http.HandlerFunc) error {
	switch {
	case !slices.Contains(endpointMethods, method):
		return fmt.Errorf("endpoint %s %s: method must be one of %s", method, path, strings.Join(endpointMethods, ", "))
	case !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \t\r\n?#") || strings.Contains(path, "//"):
		return fmt.Errorf("endpoint %s %s: path must start with / and have no spaces, query or empty segments", method, path)
	case !auth.valid():
		return fmt.Errorf("endpoint %s %s: unknown auth %q (use Public, Authenticated or Policy(name))", method, path, auth)
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

func (a Auth) valid() bool {
	name, ok := a.policy()
	return a == Public || a == Authenticated || ok && name != ""
}

func (a *App) mountEndpoints(mux *http.ServeMux) error {
	s := a.services
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
			if ep.auth != Public {
				var err error
				if ctx, err = a.authorizeEndpoint(r.WithContext(ctx), ep.auth); err != nil {
					manage.Fail(w, r, err)
					return
				}
			}
			ep.handler(w, r.WithContext(ctx))
		})
		if err := handle(mux, ep.method+" "+EndpointPrefix+ep.path, h); err != nil {
			return fmt.Errorf("module %s: endpoint %s %s: %w", e.module, ep.method, ep.path, err)
		}
	}
	return nil
}

// authorizeEndpoint authenticates the request like the management API (bearer token or session with CSRF) and
// returns a context carrying the principal and audit actor.
func (a *App) authorizeEndpoint(r *http.Request, auth Auth) (context.Context, error) {
	s := a.services
	an, err := s.Auth.Authenticate(r)
	if err == nil {
		err = s.Auth.CheckCSRF(r, an)
	}
	if err != nil {
		return nil, err
	}
	ctx := access.WithPrincipal(r.Context(), an.Principal)
	if name, ok := auth.policy(); ok {
		if err := s.Access.Holds(ctx, name); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}
