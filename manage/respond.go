package manage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

const jsonType = "application/json; charset=utf-8"

type httpError struct {
	status        int
	code, message string
}

func (e *httpError) Error() string { return e.message }

func badRequest(format string, args ...any) *httpError {
	return &httpError{http.StatusBadRequest, "bad_request", fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) *httpError {
	return &httpError{http.StatusNotFound, "not_found", fmt.Sprintf(format, args...)}
}

type errorBody struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	Fields    []schema.FieldError `json:"fields,omitempty"`
	Locale    string              `json:"locale,omitempty"`
	Parts     []string            `json:"parts,omitempty"`
	Referrers []content.Referrer  `json:"referrers,omitempty"`
}

func (a *api) writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", jsonType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		a.log.Error("manage: encode response", "err", err)
	}
}

func (a *api) writeError(w http.ResponseWriter, e *httpError) {
	a.writeJSON(w, e.status, map[string]any{"error": errorBody{Code: e.code, Message: e.message}})
}

func (a *api) writeErrorBody(w http.ResponseWriter, status int, b errorBody) {
	a.writeJSON(w, status, map[string]any{"error": b})
}

// fail maps a service or request error to the error envelope.
func (a *api) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		he *httpError
		ve *content.ValidationError
		ce *content.ConflictError
		re *content.ReferencedError
	)
	switch {
	case errors.As(err, &he):
		a.writeError(w, he)
	case errors.As(err, &ve):
		fields := ve.Errors
		if fields == nil {
			fields = []schema.FieldError{}
		}
		a.writeErrorBody(w, http.StatusUnprocessableEntity,
			errorBody{Code: "invalid", Message: "validation failed", Fields: fields, Locale: ve.Locale})
	case errors.As(err, &ce):
		a.writeErrorBody(w, http.StatusConflict, errorBody{Code: "conflict", Message: ce.Error(), Parts: ce.Parts})
	case errors.As(err, &re):
		a.writeErrorBody(w, http.StatusConflict,
			errorBody{Code: "referenced", Message: "item is referenced by other items", Referrers: re.Referrers})
	case errors.Is(err, content.ErrUnknownType), errors.Is(err, content.ErrNotFound), errors.Is(err, store.ErrNotFound):
		a.writeErrorBody(w, http.StatusNotFound, errorBody{Code: "not_found", Message: err.Error()})
	case errors.Is(err, content.ErrBadRequest):
		a.writeErrorBody(w, http.StatusBadRequest, errorBody{Code: "bad_request", Message: err.Error()})
	case errors.Is(err, content.ErrInvalid):
		a.writeErrorBody(w, http.StatusUnprocessableEntity, errorBody{Code: "invalid", Message: err.Error()})
	case errors.Is(err, content.ErrConflict):
		a.writeErrorBody(w, http.StatusConflict, errorBody{Code: "conflict", Message: err.Error()})
	case errors.Is(err, content.ErrReferenced):
		a.writeErrorBody(w, http.StatusConflict, errorBody{Code: "referenced", Message: err.Error()})
	case errors.Is(err, content.ErrDuplicateKey), errors.Is(err, store.ErrDuplicateKey):
		a.writeErrorBody(w, http.StatusConflict, errorBody{Code: "duplicate_key", Message: "key is already in use"})
	default:
		a.log.Error("manage: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		a.writeError(w, &httpError{http.StatusInternalServerError, "internal", "internal error"})
	}
}

// decode reads one JSON object into dst: size-limited, JSON content type (required only when the body has
// bytes), numbers kept exact, unknown top-level keys and trailing data rejected. With optional, an empty
// body, chunked or not, leaves dst untouched.
func (a *api) decode(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	body := bufio.NewReader(http.MaxBytesReader(w, r.Body, a.maxBody))
	if _, err := body.Peek(1); err != nil {
		if errors.Is(err, io.EOF) {
			if optional {
				return nil
			}
			return badRequest("request body is empty")
		}
		return bodyError(err)
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		return &httpError{http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json"}
	}
	dec := json.NewDecoder(body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			if optional {
				return nil
			}
			return badRequest("request body is empty")
		}
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return bodyError(err)
		}
		return badRequest("unexpected data after the JSON body")
	}
	return nil
}

func bodyError(err error) *httpError {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return &httpError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("body exceeds %d bytes", mb.Limit)}
	}
	return badRequest("invalid JSON body: %v", err)
}

func isJSON(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

func pathID(r *http.Request) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		return ids.Nil, notFound("item not found")
	}
	return id, nil
}
