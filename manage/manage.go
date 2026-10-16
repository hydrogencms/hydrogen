// Package manage is the management REST API over the content service, and its OpenAPI document.
//
// Contract: planning/design/content-core.md ("Management API").
package manage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// Content is what the API needs from the content service; *content.Service satisfies it.
type Content interface {
	Schema() *schema.Schema
	Create(ctx context.Context, in content.CreateInput) (*content.View, error)
	Get(ctx context.Context, id ids.UUID, o content.GetOptions) (*content.View, error)
	Update(ctx context.Context, id ids.UUID, in content.UpdateInput) (*content.View, error)
	Publish(ctx context.Context, id ids.UUID, o content.PublishOptions) (*content.View, error)
	Discard(ctx context.Context, id ids.UUID, parts []string) error
	Delete(ctx context.Context, id ids.UUID) error // moves to the trash
	Unpublish(ctx context.Context, id ids.UUID) (*content.View, error)
	Restore(ctx context.Context, id ids.UUID) (*content.View, error)
	Purge(ctx context.Context, id ids.UUID) error
	ListTrash(ctx context.Context, o content.TrashOptions) (*content.TrashResult, error)
	List(ctx context.Context, o content.ListOptions) (*content.ListResult, error)
	Versions(ctx context.Context, id ids.UUID) ([]store.VersionMeta, error)
	VersionContent(ctx context.Context, id ids.UUID, version int) (*content.VersionView, error)
	RestoreVersion(ctx context.Context, id ids.UUID, version int, locales []string) (*content.View, error)
	ReferencesVisible(ctx context.Context, id ids.UUID) ([]content.Referrer, int, error)
	Singleton(ctx context.Context, typ string, o content.GetOptions) (*content.View, error)
	UpdateSingleton(ctx context.Context, typ string, in content.UpdateInput) (*content.View, error)
	PublishSingleton(ctx context.Context, typ string, o content.PublishOptions) (*content.View, error)
}

var _ Content = (*content.Service)(nil)

const defaultMaxBody = 4 << 20

// Authenticator turns requests into principals and runs login and logout; *access.Authenticator satisfies it.
type Authenticator interface {
	Authenticate(r *http.Request) (*access.Authn, error)
	CheckCSRF(r *http.Request, an *access.Authn) error
	Login(w http.ResponseWriter, r *http.Request, login, password string) (*access.LoginResult, error)
	Logout(w http.ResponseWriter, r *http.Request) error
}

// Gate answers the coarse route question: does the principal in ctx hold the policy under any grant (nil: yes)?
// Grants feeds GET /auth/session. *access.Checker satisfies it.
type Gate interface {
	Holds(ctx context.Context, policy string) error
	Grants(ctx context.Context, p *access.Principal) ([]access.Grant, error)
}

// Options configures New.
type Options struct {
	Content Content
	// Channels are listed by GET /channels; nil lists none.
	Channels *channel.Set
	// Auth authenticates requests. Without it, Token alone authenticates (a super principal), which is enough
	// for tools and tests without a database.
	Auth  Authenticator
	Token string // the config token when Auth is nil
	// Gate is the coarse policy check of routes; without one only super principals pass policy routes.
	Gate Gate
	// Admin serves the user, group, role, token and audit routes; without it they answer 404.
	Admin *access.Admin
	// Catalog is listed by GET /policies and checks the policy names of module routes.
	Catalog *access.Catalog
	Log     *slog.Logger
	MaxBody int64 // default 4 MiB
	// Prefix is where the handler is mounted (e.g. "/api/manage/v1"); Location headers are Prefix plus the
	// route path. Empty keeps them relative to the mount point.
	Prefix string
	// Routes are module endpoints served next to the core ones, behind the same authentication, body limit,
	// panic recovery and logging, and listed in /openapi.json. A route that conflicts with a core route or
	// another module route makes New fail.
	Routes []Route
}

// Access levels for Route.Policy besides a policy name.
const (
	// Authenticated routes need any authenticated principal (a session, an API token or the config token).
	Authenticated = "@authenticated"
	// Public routes need no credentials.
	Public = "@public"
)

// Route is an endpoint a module adds to the management API.
type Route struct {
	Method  string // GET, POST, PATCH, PUT or DELETE
	Path    string // relative to the API prefix, ServeMux syntax, e.g. "/pages/sites/{site}/tree"
	Handler http.HandlerFunc
	Summary string
	// OpenAPI is the operation object for the document (parameters, requestBody, responses); optional. Without
	// it the document gets a generic operation: path parameters and the standard error responses.
	OpenAPI map[string]any
	// Module names the module that registered the route, for error messages.
	Module string
	// Policy is required: a policy name ("pages.edit"), Authenticated or Public. The middleware refuses callers
	// that hold no grant of the policy at all; it is a coarse gate, and the module's service must still check
	// the policy against what the request acts on (a node, a site, an item).
	Policy string
}

func (rt Route) describe() string {
	if rt.Module != "" {
		return fmt.Sprintf("module %s: route %s %s", rt.Module, rt.Method, rt.Path)
	}
	return fmt.Sprintf("route %s %s", rt.Method, rt.Path)
}

func (rt Route) validate() error {
	switch rt.Method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
	default:
		return fmt.Errorf("manage: %s: method must be GET, POST, PATCH, PUT or DELETE", rt.describe())
	}
	if !strings.HasPrefix(rt.Path, "/") || strings.ContainsAny(rt.Path, " \t") {
		return fmt.Errorf("manage: %s: path must start with / and contain no spaces", rt.describe())
	}
	if rt.Handler == nil {
		return fmt.Errorf("manage: %s: Handler is required", rt.describe())
	}
	if rt.Policy == "" {
		return fmt.Errorf("manage: %s: Policy is required (a policy name, manage.Authenticated or manage.Public)", rt.describe())
	}
	return nil
}

type api struct {
	c       Content
	sc      *schema.Schema
	log     *slog.Logger
	maxBody int64
	prefix  string
	auth    Authenticator
	gate    Gate
	admin   *access.Admin
	catalog *access.Catalog

	channels     *channel.Set
	channelsJSON []byte
	typesJSON    []byte
	typeJSON     map[string][]byte
	blocksJSON   []byte
	openapi      []byte
	etag         string
}

type route struct {
	method, path string
	policy       string
	h            func(*api, http.ResponseWriter, *http.Request) error
}

// routes is the single list of endpoints; the mux and the tests' OpenAPI coverage check both read it.
var routes = slices.Concat(coreRoutes, authRoutes)

var coreRoutes = []route{
	{"GET", "/types", Authenticated, (*api).listTypes},
	{"GET", "/types/{type}", Authenticated, (*api).getType},
	{"GET", "/blocks", Authenticated, (*api).listBlocks},
	{"GET", "/collections/{type}/items", access.PolicyContentRead, (*api).listItems},
	{"POST", "/collections/{type}/items", access.PolicyContentCreate, (*api).createItem},
	{"GET", "/items/{id}", access.PolicyContentRead, (*api).getItem},
	{"PATCH", "/items/{id}", access.PolicyContentEdit, (*api).updateItem},
	{"POST", "/items/{id}/publish", access.PolicyContentPublish, (*api).publishItem},
	{"DELETE", "/items/{id}/draft", access.PolicyContentEdit, (*api).discardDraft},
	{"POST", "/items/{id}/unpublish", access.PolicyContentPublish, (*api).unpublishItem},
	{"DELETE", "/items/{id}", access.PolicyContentDelete, (*api).deleteItem},
	{"POST", "/items/{id}/restore", access.PolicyContentDelete, (*api).restoreItem},
	{"GET", "/trash", access.PolicyContentRead, (*api).listTrash},
	{"GET", "/items/{id}/versions", access.PolicyContentRead, (*api).versions},
	{"GET", "/items/{id}/versions/{n}", access.PolicyContentRead, (*api).version},
	{"POST", "/items/{id}/versions/{n}/restore", access.PolicyContentEdit, (*api).restoreVersion},
	{"GET", "/items/{id}/references", access.PolicyContentRead, (*api).references},
	{"GET", "/singletons/{type}", access.PolicyContentRead, (*api).getSingleton},
	{"PATCH", "/singletons/{type}", access.PolicyContentEdit, (*api).updateSingleton},
	{"POST", "/singletons/{type}/publish", access.PolicyContentPublish, (*api).publishSingleton},
	{"GET", "/channels", Authenticated, (*api).listChannels},
	{"GET", "/openapi.json", Authenticated, (*api).serveOpenAPI},
}

// New returns the API handler. Every route, /openapi.json included, needs the bearer token. Routes are relative: mount it with http.StripPrefix("/api/manage/v1", h).
func New(o Options) (http.Handler, error) {
	if o.Content == nil {
		return nil, errors.New("manage: Content is required")
	}
	if o.Auth == nil && o.Token == "" {
		return nil, errors.New("manage: Auth or Token is required")
	}
	sc := o.Content.Schema()
	if sc == nil {
		return nil, errors.New("manage: content service has no schema")
	}
	a := &api{c: o.Content, sc: sc, log: o.Log, maxBody: o.MaxBody, prefix: strings.TrimRight(o.Prefix, "/"), auth: o.Auth,
		gate: o.Gate, admin: o.Admin, catalog: o.Catalog, channels: o.Channels}
	if a.auth == nil {
		var err error
		if a.auth, err = access.NewAuthenticator(access.AuthOptions{ConfigToken: o.Token}); err != nil {
			return nil, err
		}
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if a.maxBody <= 0 {
		a.maxBody = defaultMaxBody
	}
	for _, rt := range o.Routes {
		if err := rt.validate(); err != nil {
			return nil, err
		}
		if err := a.checkPolicy(rt); err != nil {
			return nil, err
		}
	}
	if err := a.buildCaches(o.Routes); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.method+" "+rt.path, a.guard(rt.policy, a.handle(rt.h)))
	}
	for _, rt := range o.Routes {
		if err := a.handleRoute(mux, rt); err != nil {
			return nil, err
		}
	}
	return a.recoverer(mux), nil
}

// handleRoute mounts a module route; ServeMux panics on a conflicting pattern.
func (a *api) handleRoute(mux *http.ServeMux, rt Route) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("manage: %s: %v", rt.describe(), v)
		}
	}()
	mux.Handle(rt.Method+" "+rt.Path, a.guard(rt.Policy, rt.Handler))
	return nil
}

// checkPolicy rejects a module route naming a policy nobody declared.
func (a *api) checkPolicy(rt Route) error {
	if rt.Policy == Authenticated || rt.Policy == Public || a.catalog == nil {
		return nil
	}
	if _, ok := a.catalog.Policy(rt.Policy); !ok {
		return fmt.Errorf("manage: %s: unknown policy %q (declare it with Registry.Policy)", rt.describe(), rt.Policy)
	}
	return nil
}

func (a *api) handle(fn func(*api, http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(a, w, r); err != nil {
			Fail(w, r, err)
		}
	})
}

type authnKey struct{}

// authnFrom is the authenticated request in ctx (nil on public routes).
func authnFrom(ctx context.Context) *access.Authn {
	an, _ := ctx.Value(authnKey{}).(*access.Authn)
	return an
}

// guard authenticates the request, checks CSRF on session requests, puts the principal (and so the audit
// actor) in the context and applies the route's coarse policy gate. Public routes skip all of it.
func (a *api) guard(policy string, next http.Handler) http.Handler {
	if policy == Public {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		an, err := a.auth.Authenticate(r)
		if err == nil {
			err = a.auth.CheckCSRF(r, an)
		}
		if err != nil {
			Fail(w, r, err)
			return
		}
		ctx := context.WithValue(access.WithPrincipal(r.Context(), an.Principal), authnKey{}, an)
		if policy != Authenticated {
			if err := a.holds(ctx, an.Principal, policy); err != nil {
				Fail(w, r, err)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *api) holds(ctx context.Context, p *access.Principal, policy string) error {
	if p.Super {
		return nil
	}
	if a.gate == nil {
		return fmt.Errorf("%w: %s", access.ErrForbidden, policy)
	}
	return a.gate.Holds(ctx, policy)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status, s.wrote = http.StatusOK, true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// recoverer turns panics into a 500 envelope and logs each request at debug level.
func (a *api) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		r = r.WithContext(context.WithValue(r.Context(), reqStateKey{}, reqState{log: a.log, maxBody: a.maxBody}))
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value
					panic(v)
				}
				a.log.Error("manage: panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if !sw.wrote {
					writeErrorBody(sw, http.StatusInternalServerError, errorBody{Code: "internal", Message: "internal error"})
				}
			}
			a.log.Debug("manage request", "method", r.Method, "path", r.URL.Path, "status", sw.status)
		}()
		next.ServeHTTP(sw, r)
	})
}
