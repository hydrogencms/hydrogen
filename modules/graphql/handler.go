package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/location"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/api/openapi"
	"github.com/hydrogencms/hydrogen/core/cachetag"
)

type request struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

type response struct {
	Data   any                        `json:"data,omitempty"`
	Errors []gqlerrors.FormattedError `json:"errors,omitempty"`
}

// writeErrors answers a request that failed before execution: errors and no data.
func writeErrors(w http.ResponseWriter, status int, errs ...gqlerrors.FormattedError) {
	body, _ := json.Marshal(response{Errors: errs})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func requestError(e *reqError) gqlerrors.FormattedError {
	return gqlerrors.FormattedError{Message: e.msg, Locations: []location.SourceLocation{},
		Extensions: map[string]any{"code": e.code, "status": e.status}}
}

func badRequest(code, msg string) *reqError {
	return &reqError{status: http.StatusBadRequest, code: code, msg: msg}
}

// parseRequest reads the GraphQL request of a GET (query, variables and operationName parameters) or a POST
// (a JSON body).
func (m *module) parseRequest(r *http.Request) (*request, *reqError) {
	var req request
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		for k, v := range q {
			switch k {
			case "channel", "locale", "preview":
			case "query", "variables", "operationName":
				if len(v) > 1 {
					return nil, badRequest("BAD_REQUEST", "query parameter "+k+" may appear only once")
				}
			default:
				return nil, badRequest("BAD_REQUEST", "unknown query parameter "+k)
			}
		}
		req.Query, req.OperationName = q.Get("query"), q.Get("operationName")
		if v := q.Get("variables"); v != "" {
			if err := json.Unmarshal([]byte(v), &req.Variables); err != nil {
				return nil, badRequest("BAD_REQUEST", "variables must be a JSON object")
			}
		}
		return &req, nil
	}
	body, err := delivery.ReadBody(r)
	if err != nil {
		var me *manage.Error
		if errors.As(err, &me) {
			return nil, &reqError{status: me.Status, code: strings.ToUpper(me.Code), msg: me.Message}
		}
		return nil, badRequest("BAD_REQUEST", "cannot read the request body")
	}
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "application/graphql":
		req.Query = string(body)
	case "application/json", "":
		d := json.NewDecoder(bytes.NewReader(body))
		var raw struct {
			request
			Extensions json.RawMessage `json:"extensions"`
		}
		d.DisallowUnknownFields()
		if err := d.Decode(&raw); err != nil {
			return nil, badRequest("BAD_REQUEST", "the body must be a JSON object {query, variables, operationName}")
		}
		if d.More() {
			return nil, badRequest("BAD_REQUEST", "the body must hold one JSON object")
		}
		req = raw.request
	default:
		return nil, &reqError{status: http.StatusUnsupportedMediaType, code: "UNSUPPORTED_MEDIA_TYPE", msg: "send application/json"}
	}
	return &req, nil
}

// serve answers GET and POST /graphql.
func (m *module) serve(w http.ResponseWriter, r *http.Request) {
	ch := delivery.Channel(r)
	cs, err := m.schemaFor(ch)
	if err != nil {
		m.svc.Log.Error("graphql: schema", "err", err)
		writeErrors(w, http.StatusInternalServerError, requestError(&reqError{status: 500, code: "INTERNAL", msg: "internal error"}))
		return
	}
	req, rerr := m.parseRequest(r)
	if rerr != nil {
		writeErrors(w, rerr.status, requestError(rerr))
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeErrors(w, http.StatusBadRequest, requestError(badRequest("BAD_REQUEST", "query is required")))
		return
	}
	if len(req.Query) > m.cfg.MaxQueryBytes {
		writeErrors(w, http.StatusBadRequest, requestError(badRequest("QUERY_TOO_LARGE",
			"the query is longer than "+strconv.Itoa(m.cfg.MaxQueryBytes)+" bytes")))
		return
	}
	doc, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(req.Query), Name: "GraphQL request"})})
	if err != nil {
		writeErrors(w, http.StatusBadRequest, gqlerrors.FormatErrors(err)...)
		return
	}
	if vr := gql.ValidateDocument(&cs.schema, doc, nil); !vr.IsValid {
		writeErrors(w, http.StatusBadRequest, vr.Errors...)
		return
	}
	if lerr := checkLimits(doc, req.OperationName, &m.cfg); lerr != nil {
		var re *reqError
		errors.As(lerr, &re)
		writeErrors(w, re.status, requestError(re))
		return
	}

	f := newFetcher(m.svc.Delivery.Handler(), r, ch.Name, m.cfg.MaxCalls, m.cfg.MaxItems)
	f.trace = m.trace
	rc := &reqCtx{ctx: r.Context(), f: f, cs: cs, maxNodes: int64(m.cfg.MaxNodes)}
	res := gql.Execute(gql.ExecuteParams{
		Schema:        cs.schema,
		AST:           doc,
		OperationName: req.OperationName,
		Args:          req.Variables,
		Context:       withReq(r.Context(), rc),
	})
	if res.Data == nil && len(res.Errors) > 0 {
		writeErrors(w, http.StatusBadRequest, res.Errors...)
		return
	}
	out, merr := json.Marshal(response{Data: res.Data, Errors: res.Errors})
	if merr != nil {
		m.svc.Log.Error("graphql: encode", "err", merr)
		writeErrors(w, http.StatusInternalServerError, requestError(&reqError{status: 500, code: "INTERNAL", msg: "internal error"}))
		return
	}
	if m.observe != nil {
		m.observe(f.callCount())
	}
	delivery.Tag(r, f.tagList(cs.allowed, func(typ string) bool {
		t, ok := m.svc.Schema.Type(typ)
		return ok && t.Singleton
	})...)
	if len(res.Errors) > 0 || f.wasMissing() {
		delivery.NoStore(w)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// serveSDL answers GET /graphql/schema.graphql: the channel's schema as SDL.
func (m *module) serveSDL(w http.ResponseWriter, r *http.Request) {
	if !m.cfg.Introspection {
		delivery.Fail(w, r, manage.NotFound("not available"))
		return
	}
	ch := delivery.Channel(r)
	cs, err := m.schemaFor(ch)
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	if len(r.URL.Query()) > 0 {
		for k := range r.URL.Query() {
			if k != "channel" && k != "locale" && k != "preview" {
				delivery.Fail(w, r, manage.BadRequest("unknown query parameter %q", k))
				return
			}
		}
	}
	delivery.Tag(r, cachetag.Channel(ch.Name))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(cs.sdl))
}

func withReq(ctx context.Context, rc *reqCtx) context.Context {
	return context.WithValue(ctx, rcKey{}, rc)
}

func openAPIOperation() map[string]any {
	param := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "in": "query", "description": desc, "schema": map[string]any{"type": "string"}}
	}
	errResp := func(name string) map[string]any { return openapi.Ref("responses", name) }
	return map[string]any{
		"description": "GraphQL over HTTP. GET takes query, variables (JSON) and operationName; POST takes a JSON body " +
			"{query, variables, operationName}. The schema is generated per channel; GET /graphql/schema.graphql serves it as SDL. " +
			"GET responses are cached like the rest of the delivery API; POST responses are never cached.",
		"parameters": append(openapi.Params("Channel", "Locale", "Preview"),
			param("query", "The GraphQL document (GET)."), param("variables", "A JSON object (GET)."), param("operationName", "Which operation to run (GET).")),
		"responses": map[string]any{
			"200": openapi.OKResp("A GraphQL response: data and, when something failed, errors.", map[string]any{"type": "object"}),
			"400": errResp("BadRequest"), "401": errResp("Unauthorized"),
		},
	}
}
