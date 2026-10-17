package media

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/manage"
)

// ManageRoutes is POST /uploads of the management API.
func (s *Service) ManageRoutes() []manage.Route {
	return []manage.Route{{
		Method: http.MethodPost, Path: "/uploads", Handler: s.postUpload, Policy: manage.Authenticated,
		Module: "media", Summary: "Upload a file", OpenAPI: opUpload(),
	}}
}

// postUpload reads a multipart body with one `file` part (and optionally a `type` field before it), stores the
// file and answers with the `file` value for an item of the upload type: 201 for a new file, 200 when the same
// bytes were already stored.
func (s *Service) postUpload(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "5")
		manage.Fail(w, r, &manage.Error{Status: http.StatusTooManyRequests, Code: "busy", Message: ErrBusy.Error()})
		return
	}
	// A slow sender must not hold a slot (and a temporary file) for ever; the server's own read timeout does
	// not know how big an upload is.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(s.o.UploadTimeout))
	res, err := s.upload(w, r)
	if err != nil {
		manage.Fail(w, r, uploadAPIError(err))
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	w.Header().Set("Location", s.URL(res.SHA, res.Name))
	manage.WriteJSON(w, status, FileValue(res))
}

// FileValue is the `file` value of an item for an upload result: what the client sends back when it creates or
// updates the item.
func FileValue(r Result) map[string]any {
	v := map[string]any{"id": r.SHA, "name": r.Name, "mime": r.MIME, "size": r.Size}
	if r.Width > 0 && r.Height > 0 {
		v["width"], v["height"] = r.Width, r.Height
	}
	return v
}

func (s *Service) upload(w http.ResponseWriter, r *http.Request) (Result, error) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		return Result{}, manage.BadRequest("Content-Type must be multipart/form-data")
	}
	// Room for the form's own framing on top of the largest file.
	r.Body = http.MaxBytesReader(w, r.Body, s.o.MaxUpload+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		return Result{}, manage.BadRequest("invalid multipart body: %v", err)
	}
	var typ string
	var res *Result
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, bodyError(err)
		}
		switch part.FormName() {
		case "type":
			if res != nil {
				return Result{}, manage.BadRequest("send the type field before the file")
			}
			b, err := io.ReadAll(io.LimitReader(part, 257))
			if err != nil {
				return Result{}, bodyError(err)
			}
			if typ = strings.TrimSpace(string(b)); len(typ) > 256 {
				return Result{}, manage.BadRequest("type is too long")
			}
		case "file":
			if res != nil {
				return Result{}, manage.BadRequest("send one file part")
			}
			out, err := s.Upload(r.Context(), UploadInput{Name: part.FileName(), Body: part, Type: typ})
			if err != nil {
				return Result{}, bodyError(err)
			}
			res = &out
		default:
			_, _ = io.Copy(io.Discard, io.LimitReader(part, 1<<20))
		}
	}
	if res == nil {
		return Result{}, manage.BadRequest("the file part is missing")
	}
	return *res, nil
}

// bodyError maps a request body that exceeded its limit to the upload's own too-large error.
func bodyError(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return uploadErr(ErrTooLarge, "request body exceeds %d bytes", mb.Limit)
	}
	return err
}

func uploadAPIError(err error) error {
	switch {
	case errors.Is(err, ErrTooLarge):
		return &manage.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: err.Error()}
	case errors.Is(err, ErrUnsupportedType):
		return &manage.Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: err.Error()}
	case errors.Is(err, ErrEmptyFile):
		return manage.BadRequest("%s", err.Error())
	case errors.Is(err, os.ErrDeadlineExceeded):
		return &manage.Error{Status: http.StatusRequestTimeout, Code: "request_timeout", Message: "the upload took too long to arrive"}
	case errors.Is(err, ErrUnknownType):
		return &manage.Error{Status: http.StatusBadRequest, Code: "unknown_type", Message: err.Error()}
	}
	return err
}

type m = map[string]any

func opUpload() m {
	str := func(format string) m {
		if format == "" {
			return m{"type": "string"}
		}
		return m{"type": "string", "format": format}
	}
	fileValue := m{"type": "object", "required": []any{"id", "name", "mime", "size"}, "properties": m{
		"id": str(""), "name": str(""), "mime": str(""), "size": m{"type": "integer", "format": "int64"},
		"width": m{"type": "integer"}, "height": m{"type": "integer"}},
		"description": "The `file` value of an item of the upload type."}
	ref := func(code string) m { return m{"$ref": "#/components/responses/" + code} }
	return m{"operationId": "uploadFile", "summary": "Upload a file",
		"description": "Stores one file (multipart/form-data) and answers with the `file` value to put in an item of an upload type; " +
			"create or update the item with it. The file type is detected from its content, never from the client. Optional field type " +
			"(before the file) names the upload type, whose accept patterns and max_size are then checked early. The same bytes are stored " +
			"once: 201 for a new file, 200 when it was already known. Location is the file's URL. Needs content.create or content.edit " +
			"on the upload type (any upload type when type is omitted); 413 too_large beyond media.max_upload or the type's max_size, 415 " +
			"unsupported_media_type when the detected type is not accepted, 429 busy while the server handles its limit of concurrent uploads. " +
			"The detected type is recorded with the first upload of the bytes (the file name only refines it then). Unreferenced uploads are removed after 24 hours.",
		"requestBody": m{"required": true, "content": m{"multipart/form-data": m{"schema": m{"type": "object",
			"required": []any{"file"}, "properties": m{"type": str(""), "file": str("binary")}}}}},
		"responses": m{
			"200": m{"description": "The file was already stored.", "content": m{"application/json": m{"schema": fileValue}}},
			"201": m{"description": "The file was stored.", "content": m{"application/json": m{"schema": fileValue}},
				"headers": m{"Location": m{"schema": str(""), "description": "URL of the file."}}},
			"400": ref("BadRequest"), "401": ref("Unauthorized"), "403": ref("Forbidden"),
			"413": ref("TooLarge"), "415": ref("UnsupportedMedia"), "429": ref("Throttled")},
	}
}
