// Package manage is the management REST API over the content service, and its OpenAPI document.
//
// Contract: planning/design/content-core.md ("Management API").
package manage

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/hydrogencms/hydrogen/audit"
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
	Delete(ctx context.Context, id ids.UUID) error
	List(ctx context.Context, o content.ListOptions) (*content.ListResult, error)
	Versions(ctx context.Context, id ids.UUID) ([]store.VersionMeta, error)
	References(ctx context.Context, id ids.UUID) ([]content.Referrer, error)
	Singleton(ctx context.Context, typ string, o content.GetOptions) (*content.View, error)
	UpdateSingleton(ctx context.Context, typ string, in content.UpdateInput) (*content.View, error)
	PublishSingleton(ctx context.Context, typ string, o content.PublishOptions) (*content.View, error)
}

var _ Content = (*content.Service)(nil)

const defaultMaxBody = 4 << 20

// Options configures New.
type Options struct {
	Content Content
	// Channels are listed by GET /channels; nil lists none.
	Channels *channel.Set
	Token    string // required bearer token
	Log      *slog.Logger
	MaxBody  int64 // default 4 MiB
	// Prefix is where the handler is mounted (e.g. "/api/manage/v1"); Location headers are Prefix plus the
	// route path. Empty keeps them relative to the mount point.
	Prefix string
	// Routes are module endpoints served next to the core ones, behind the same bearer token, body limit,
	// panic recovery and logging, and listed in /openapi.json. A route that conflicts with a core route or
	// another module route makes New fail.
	Routes []Route
}

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
	return nil
}

type api struct {
	c        Content
	sc       *schema.Schema
	log      *slog.Logger
	maxBody  int64
	prefix   string
	tokenSum [sha256.Size]byte

	channels     *channel.Set
	channelsJSON []byte
	typesJSON    []byte
	typeJSON     map[string][]byte
	openapi      []byte
	etag         string
}

type route struct {
	method, path string
	h            func(*api, http.ResponseWriter, *http.Request) error
}

// routes is the single list of endpoints; the mux and the tests' OpenAPI coverage check both read it.
var routes = []route{
	{"GET", "/types", (*api).listTypes},
	{"GET", "/types/{type}", (*api).getType},
	{"GET", "/collections/{type}/items", (*api).listItems},
	{"POST", "/collections/{type}/items", (*api).createItem},
	{"GET", "/items/{id}", (*api).getItem},
	{"PATCH", "/items/{id}", (*api).updateItem},
	{"POST", "/items/{id}/publish", (*api).publishItem},
	{"DELETE", "/items/{id}/draft", (*api).discardDraft},
	{"DELETE", "/items/{id}", (*api).deleteItem},
	{"GET", "/items/{id}/versions", (*api).versions},
	{"GET", "/items/{id}/references", (*api).references},
	{"GET", "/singletons/{type}", (*api).getSingleton},
	{"PATCH", "/singletons/{type}", (*api).updateSingleton},
	{"POST", "/singletons/{type}/publish", (*api).publishSingleton},
	{"GET", "/channels", (*api).listChannels},
	{"GET", "/openapi.json", (*api).serveOpenAPI},
}

// New returns the API handler. Every route, /openapi.json included, needs the bearer token. Routes are relative: mount it with http.StripPrefix("/api/manage/v1", h).
func New(o Options) (http.Handler, error) {
	if o.Content == nil {
		return nil, errors.New("manage: Content is required")
	}
	if o.Token == "" {
		return nil, errors.New("manage: Token is required")
	}
	sc := o.Content.Schema()
	if sc == nil {
		return nil, errors.New("manage: content service has no schema")
	}
	a := &api{c: o.Content, sc: sc, log: o.Log, maxBody: o.MaxBody, prefix: strings.TrimRight(o.Prefix, "/"), tokenSum: sha256.Sum256([]byte(o.Token)), channels: o.Channels}
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
	}
	if err := a.buildCaches(o.Routes); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.method+" "+rt.path, a.auth(a.handle(rt.h)))
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
	mux.Handle(rt.Method+" "+rt.Path, a.auth(rt.Handler))
	return nil
}

func (a *api) handle(fn func(*api, http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(a, w, r); err != nil {
			Fail(w, r, err)
		}
	})
}

func (a *api) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		sum := sha256.Sum256([]byte(strings.TrimSpace(tok)))
		if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(sum[:], a.tokenSum[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			Fail(w, r, &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "missing or invalid bearer token"})
			return
		}
		ctx := audit.WithActor(r.Context(), audit.Actor{Kind: "token", ID: "manage"})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
