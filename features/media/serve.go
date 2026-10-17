package media

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/store"
)

// AssetPattern is the route of the file handler; the kernel mounts Handler under it.
const AssetPattern = "GET " + AssetPath + "/{sha}/{name}"

// sandboxCSP is sent with types a browser could render as a document (SVG, HTML, XML): the file may style
// itself but run nothing and load nothing.
const sandboxCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// Handler serves GET /assets/{sha}/{name}: public, immutable and content-addressed, with range requests. The
// name is only for the browser's download name; the file is found by its SHA-256.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sha, name := r.PathValue("sha"), r.PathValue("name")
		u, err := s.record(r.Context(), sha)
		if err == nil {
			err = s.serve(w, r, u, name)
		}
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			s.o.Log.Error("media: serve failed", "sha", sha, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, u store.Upload, name string) error {
	etag := `"` + u.SHA + `"`
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", etag)
	// A revalidation the validators settle needs no bytes: answer without asking the storage.
	if notModified(r, etag, u.CreatedAt) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	// A missing object must become a 404 before any header of the file is sent. A plain GET opens the object
	// right away (no separate existence check); a HEAD or a range request checks it with Stat.
	obj := &object{ctx: r.Context(), st: s.o.Storage, key: u.SHA, size: u.Size}
	defer func() { _ = obj.Close() }()
	var err error
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" && r.Header.Get("If-Range") == "" {
		err = obj.prime()
	} else {
		_, err = s.o.Storage.Stat(r.Context(), u.SHA)
	}
	if err != nil {
		h.Del("Cache-Control")
		h.Del("ETag")
		return err
	}
	h.Set("Content-Type", contentType(u.MIME))
	h.Set("X-Content-Type-Options", "nosniff")
	disp := "attachment"
	if inline(u.MIME) {
		disp = "inline"
	}
	params := map[string]string{}
	if n := cleanName(name); n != "" {
		params["filename"] = n
	}
	if v := mime.FormatMediaType(disp, params); v != "" {
		h.Set("Content-Disposition", v)
	} else {
		h.Set("Content-Disposition", disp)
	}
	if sandboxed(u.MIME) {
		h.Set("Content-Security-Policy", sandboxCSP)
	}
	http.ServeContent(w, r, "", u.CreatedAt, obj)
	return nil
}

// notModified answers If-None-Match (or, without it, If-Modified-Since) the way http.ServeContent would, for
// the cases that need no body. Other conditional headers go through ServeContent.
func notModified(r *http.Request, etag string, modTime time.Time) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("If-Match") != "" || r.Header.Get("If-Unmodified-Since") != "" || r.Header.Get("Range") != "" {
		return false
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, c := range strings.Split(inm, ",") {
			c = strings.TrimPrefix(strings.TrimSpace(c), "W/")
			if c == "*" || c == etag {
				return true
			}
		}
		return false
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !modTime.IsZero() {
		if t, err := http.ParseTime(ims); err == nil {
			return !modTime.Truncate(time.Second).After(t)
		}
	}
	return false
}

// storedDisposition is the Content-Disposition stored with an object whose bytes are served by something other
// than this server: inline for images (SVG excepted), video, audio and PDF, else attachment.
func storedDisposition(typ string) string {
	if inline(typ) && !sandboxed(typ) {
		return "inline"
	}
	return "attachment"
}

func contentType(typ string) string {
	if strings.HasPrefix(typ, "text/") {
		return typ + "; charset=utf-8"
	}
	return typ
}

func inline(typ string) bool {
	return strings.HasPrefix(typ, "image/") || strings.HasPrefix(typ, "video/") || strings.HasPrefix(typ, "audio/") ||
		typ == "application/pdf"
}

func sandboxed(typ string) bool {
	return typ == SVG || strings.Contains(typ, "html") || strings.Contains(typ, "xml")
}

// object adapts a Storage object to the io.ReadSeeker http.ServeContent wants: seeking only moves an offset,
// and a read opens a ranged request from there.
type object struct {
	ctx   context.Context
	st    Storage
	key   string
	size  int64
	off   int64
	cur   io.ReadCloser
	first io.ReadCloser // opened by prime at offset 0 and not read yet
}

// prime opens the whole object ahead of the first read: a missing object fails here, before the response
// has headers, and the stream is the one the first read uses.
func (o *object) prime() error {
	rc, _, err := o.st.GetRange(o.ctx, o.key, 0, o.size)
	if err != nil {
		return err
	}
	o.first = rc
	return nil
}

func (o *object) Read(p []byte) (int, error) {
	if o.off >= o.size {
		return 0, io.EOF
	}
	if o.cur == nil && o.first != nil && o.off == 0 {
		o.cur, o.first = o.first, nil
	}
	if o.cur == nil {
		rc, _, err := o.st.GetRange(o.ctx, o.key, o.off, o.size-o.off)
		if err != nil {
			return 0, err
		}
		o.cur = rc
	}
	n, err := o.cur.Read(p)
	o.off += int64(n)
	return n, err
}

func (o *object) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = o.off + offset
	case io.SeekEnd:
		abs = o.size + offset
	default:
		return 0, errors.New("media: bad seek whence")
	}
	if abs < 0 {
		return 0, errors.New("media: negative seek")
	}
	if abs != o.off && o.cur != nil {
		_ = o.cur.Close()
		o.cur = nil
	}
	o.off = abs
	return abs, nil
}

func (o *object) Close() error {
	var err error
	if o.first != nil {
		err = o.first.Close()
		o.first = nil
	}
	if o.cur != nil {
		if cerr := o.cur.Close(); err == nil {
			err = cerr
		}
		o.cur = nil
	}
	return err
}
