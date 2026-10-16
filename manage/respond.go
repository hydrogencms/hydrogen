package manage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

const jsonType = "application/json; charset=utf-8"

// Error is a failure with an HTTP status and the management API's error envelope. Module handlers return
// or pass it to Fail; core code uses it too.
type Error struct {
	Status  int
	Code    string
	Message string
	Fields  []schema.FieldError
}

func (e *Error) Error() string { return e.Message }

// BadRequest is a 400 "bad_request" error.
func BadRequest(format string, args ...any) *Error {
	return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: fmt.Sprintf(format, args...)}
}

// NotFound is a 404 "not_found" error.
func NotFound(format string, args ...any) *Error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: fmt.Sprintf(format, args...)}
}

// Conflict is a 409 error with a module-chosen code, e.g. "path_taken".
func Conflict(code, format string, args ...any) *Error {
	return &Error{Status: http.StatusConflict, Code: code, Message: fmt.Sprintf(format, args...)}
}

type errorBody struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	Fields    []schema.FieldError `json:"fields,omitempty"`
	Locale    string              `json:"locale,omitempty"`
	Parts     []string            `json:"parts,omitempty"`
	Referrers []content.Referrer  `json:"referrers,omitempty"`
	Hidden    int                 `json:"hidden,omitempty"`
}

// WriteJSON writes v as a JSON response with the API's standard headers.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", jsonType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // the status is sent; a failure here is a gone client
}

func envelope(b errorBody) map[string]any { return map[string]any{"error": b} }

func writeErrorBody(w http.ResponseWriter, status int, b errorBody) {
	WriteJSON(w, status, envelope(b))
}

// Fail writes err as the error envelope, mapping it like the core routes: *Error as given, content errors by
// kind, anything else as a logged 500. It must be called from a handler served by this package.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		me *Error
		ve *content.ValidationError
		ce *content.ConflictError
		re *content.ReferencedError
		te *access.ThrottledError
		ie *access.InvalidError
		sm *content.StateMismatchError
	)
	switch {
	case errors.As(err, &sm):
		w.Header().Set("ETag", `"`+sm.State+`"`)
		WriteJSON(w, http.StatusPreconditionFailed, map[string]any{
			"error":   errorBody{Code: "precondition_failed", Message: "the item changed since you loaded it"},
			"current": map[string]any{"updatedAt": sm.UpdatedAt, "updatedBy": sm.UpdatedBy},
		})
	case errors.Is(err, access.ErrUnauthenticated):
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeErrorBody(w, http.StatusUnauthorized, errorBody{Code: "unauthorized", Message: "missing or invalid credentials"})
	case errors.Is(err, access.ErrCSRF):
		writeErrorBody(w, http.StatusForbidden, errorBody{Code: "csrf", Message: "missing or wrong X-CSRF-Token"})
	case errors.Is(err, access.ErrForbidden):
		writeErrorBody(w, http.StatusForbidden, errorBody{Code: "forbidden", Message: forbiddenPolicy(err)})
	case errors.As(err, &te):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(te.RetryAfter.Seconds()))))
		writeErrorBody(w, http.StatusTooManyRequests, errorBody{Code: "throttled", Message: "too many attempts; retry later"})
	case errors.As(err, &ie):
		writeErrorBody(w, http.StatusBadRequest, errorBody{Code: "bad_request", Message: "invalid input", Fields: invalidFields(ie)})
	case errors.As(err, &me):
		if me.Status >= http.StatusInternalServerError {
			requestLog(r).Error("manage: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		writeErrorBody(w, me.Status, errorBody{Code: me.Code, Message: me.Message, Fields: me.Fields})
	case errors.As(err, &ve):
		fields := ve.Errors
		if fields == nil {
			fields = []schema.FieldError{}
		}
		writeErrorBody(w, http.StatusUnprocessableEntity,
			errorBody{Code: "invalid", Message: "validation failed", Fields: fields, Locale: ve.Locale})
	case errors.As(err, &ce):
		writeErrorBody(w, http.StatusConflict, errorBody{Code: "conflict", Message: ce.Error(), Parts: ce.Parts})
	case errors.As(err, &re):
		writeErrorBody(w, http.StatusConflict,
			errorBody{Code: "referenced", Message: "item is referenced by other items", Referrers: re.Referrers, Hidden: re.Hidden})
	case errors.Is(err, content.ErrUnknownType), errors.Is(err, content.ErrNotFound), errors.Is(err, store.ErrNotFound):
		writeErrorBody(w, http.StatusNotFound, errorBody{Code: "not_found", Message: err.Error()})
	case errors.Is(err, content.ErrBadRequest):
		writeErrorBody(w, http.StatusBadRequest, errorBody{Code: "bad_request", Message: err.Error()})
	case errors.Is(err, content.ErrInvalid):
		writeErrorBody(w, http.StatusUnprocessableEntity, errorBody{Code: "invalid", Message: err.Error()})
	case errors.Is(err, content.ErrConflict):
		code := "conflict"
		var coded interface{ APICode() string }
		if errors.As(err, &coded) {
			code = coded.APICode()
		}
		writeErrorBody(w, http.StatusConflict, errorBody{Code: code, Message: err.Error()})
	case errors.Is(err, content.ErrReferenced):
		writeErrorBody(w, http.StatusConflict, errorBody{Code: "referenced", Message: err.Error()})
	case errors.Is(err, content.ErrDuplicateKey), errors.Is(err, store.ErrDuplicateKey):
		writeErrorBody(w, http.StatusConflict, errorBody{Code: "duplicate_key", Message: "key is already in use"})
	default:
		requestLog(r).Error("manage: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErrorBody(w, http.StatusInternalServerError, errorBody{Code: "internal", Message: "internal error"})
	}
}

// forbiddenPolicy is the policy an ErrForbidden error names, and nothing else of its context.
func forbiddenPolicy(err error) string {
	const marker = "access: forbidden: "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return "forbidden"
	}
	msg = msg[i+len(marker):]
	if j := strings.IndexAny(msg, ":\n"); j >= 0 {
		msg = msg[:j]
	}
	return strings.TrimSpace(msg)
}

func invalidFields(e *access.InvalidError) []schema.FieldError {
	out := make([]schema.FieldError, 0, len(e.Fields))
	for _, path := range slices.Sorted(maps.Keys(e.Fields)) {
		out = append(out, schema.FieldError{Path: path, Code: "invalid", Message: e.Fields[path]})
	}
	return out
}

// reqState is what the middleware leaves in the request context for the exported helpers.
type reqState struct {
	log     *slog.Logger
	maxBody int64
}

type reqStateKey struct{}

func state(r *http.Request) reqState {
	if st, ok := r.Context().Value(reqStateKey{}).(reqState); ok {
		return st
	}
	return reqState{log: slog.Default(), maxBody: defaultMaxBody}
}

func requestLog(r *http.Request) *slog.Logger { return state(r).log }

// DecodeJSON reads the request body, which must be one JSON object, into dst. Rules shared with the core
// routes: size-limited, a JSON Content-Type, numbers kept exact (json.Number in any), unknown fields and
// trailing data rejected, an empty body is an error. It returns an *Error to pass to Fail.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeBody(w, r, dst, false)
}

// decodeBody reads one JSON object into dst: size-limited, JSON content type (required only when the body has
// bytes), numbers kept exact, unknown top-level keys and trailing data rejected. With optional, an empty
// body, chunked or not, leaves dst untouched.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	body := bufio.NewReader(http.MaxBytesReader(w, r.Body, state(r).maxBody))
	if _, err := body.Peek(1); err != nil {
		if errors.Is(err, io.EOF) {
			if optional {
				return nil
			}
			return BadRequest("request body is empty")
		}
		return bodyError(err)
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		return &Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: "Content-Type must be application/json"}
	}
	dec := json.NewDecoder(body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			if optional {
				return nil
			}
			return BadRequest("request body is empty")
		}
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return bodyError(err)
		}
		return BadRequest("unexpected data after the JSON body")
	}
	return nil
}

func bodyError(err error) *Error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return &Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: fmt.Sprintf("body exceeds %d bytes", mb.Limit)}
	}
	return BadRequest("invalid JSON body: %v", err)
}

func isJSON(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

func pathID(r *http.Request) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		return ids.Nil, NotFound("item not found")
	}
	return id, nil
}
