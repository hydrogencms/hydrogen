package content

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/schema"
)

// ErrFileNotFound is what Files.Lookup returns for a file that was never uploaded.
var ErrFileNotFound = errors.New("content: file not found")

// FileInfo is what the server knows about a stored file. Clients never provide it.
type FileInfo struct {
	ID            string // SHA-256 of the content, lower-case hex
	Mime          string // sniffed from the content
	Size          int64
	Width, Height int // 0 when the file is not an image
}

// Files is the file registry behind `file` fields (implemented by the media module; the content service does
// not import it). Lookup returns ErrFileNotFound for an unknown sha. It runs inside the write transaction: ctx
// carries it (store.TxFrom), and an implementation reads through it rather than opening another connection.
type Files interface {
	Lookup(ctx context.Context, sha string) (FileInfo, error)
	// Touch marks the file as used now, in the same transaction, so a garbage collection that is running
	// cannot remove it before the write commits. It returns ErrFileNotFound when the file is gone.
	Touch(ctx context.Context, sha string) error
}

// File is the value of a `file` field, as the generated Go types use it.
type File struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Mime   string      `json:"mime,omitempty"`
	Size   int64       `json:"size,omitempty"`
	Width  int         `json:"width,omitempty"`
	Height int         `json:"height,omitempty"`
	Focal  *FocalPoint `json:"focal,omitempty"`
}

// FocalPoint is the part of an image that must stay visible when it is cropped, as fractions (0–1) of its
// width and height.
type FocalPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Asset is an `asset` field as the delivery API expands it. The field is null when the asset is not published.
type Asset struct {
	ID     string         `json:"id"`
	URL    string         `json:"url"`
	Name   string         `json:"name"`
	Mime   string         `json:"mime"`
	Size   int64          `json:"size"`
	Width  int            `json:"width,omitempty"`
	Height int            `json:"height,omitempty"`
	Focal  *FocalPoint    `json:"focal,omitempty"`
	Fields map[string]any `json:"fields"`
}

// SetFiles installs the file registry after construction (Options.Files does the same up front). Call it before
// serving.
func (s *Service) SetFiles(f Files) {
	if f == nil {
		s.registry.Store(nil)
		return
	}
	s.registry.Store(&f)
}

func (s *Service) fileRegistry() Files {
	if p := s.registry.Load(); p != nil {
		return *p
	}
	return nil
}

func isFileField(f *schema.Field) bool { return f.Shape.Base == schema.BaseFile }

// fileTypes lists the types whose documents can hold file values.
func fileTypes(sc *schema.Schema) map[string]bool {
	out := map[string]bool{}
	for _, t := range sc.Types() {
		if sc.HasFields(t, isFileField) {
			out[t.Name] = true
		}
	}
	return out
}

// resolveFiles makes every file value of doc trustworthy: the id decides, and the server's record supplies
// mime, size, width and height; the client only keeps name and focal. Upload types also check the file against
// their accept and max_size. It returns the problems it found as field errors.
func (s *Service) resolveFiles(ctx context.Context, t *schema.Type, doc map[string]any) ([]schema.FieldError, error) {
	if !s.files[t.Name] {
		return nil, nil
	}
	var errs []schema.FieldError
	var fatal error
	s.o.Schema.MapValues(t, doc, isFileField, func(f *schema.Field, path string, v any) (any, bool) {
		in, ok := v.(map[string]any)
		if !ok || fatal != nil {
			return v, false
		}
		reg := s.fileRegistry()
		if reg == nil {
			fatal = errors.New("content: a file value was written but no file registry is configured")
			return v, false
		}
		id, _ := in["id"].(string)
		info, err := reg.Lookup(ctx, id)
		switch {
		case errors.Is(err, ErrFileNotFound):
			errs = append(errs, schema.FieldError{Path: path + ".id", Code: "file_not_found", Message: "no uploaded file has this id"})
			return v, false
		case err != nil:
			fatal = fmt.Errorf("content: look up file %s: %w", id, err)
			return v, false
		}
		if err := reg.Touch(ctx, info.ID); errors.Is(err, ErrFileNotFound) {
			errs = append(errs, schema.FieldError{Path: path + ".id", Code: "file_not_found", Message: "no uploaded file has this id"})
			return v, false
		} else if err != nil {
			fatal = fmt.Errorf("content: touch file %s: %w", id, err)
			return v, false
		}
		if path == schema.UploadFile && t.Upload != nil {
			errs = append(errs, checkUpload(t.Upload, path, info)...)
		}
		out := map[string]any{"id": info.ID, "name": strings.TrimSpace(fmt.Sprint(in["name"])), "mime": info.Mime, "size": info.Size}
		if info.Width > 0 && info.Height > 0 {
			out["width"], out["height"] = info.Width, info.Height
		}
		if focal, ok := in["focal"].(map[string]any); ok {
			out["focal"] = map[string]any{"x": focal["x"], "y": focal["y"]}
		}
		return out, true
	})
	return errs, fatal
}

func checkUpload(u *schema.Upload, path string, info FileInfo) []schema.FieldError {
	var errs []schema.FieldError
	if !u.Matches(info.Mime) {
		errs = append(errs, schema.FieldError{Path: path, Code: "file_type",
			Message: fmt.Sprintf("files of type %s are not accepted (accepted: %s)", info.Mime, strings.Join(u.Accept, ", "))})
	}
	if u.MaxSize > 0 && info.Size > u.MaxSize {
		errs = append(errs, schema.FieldError{Path: path, Code: "file_size",
			Message: fmt.Sprintf("the file is %d bytes; the limit is %d", info.Size, u.MaxSize)})
	}
	return errs
}
