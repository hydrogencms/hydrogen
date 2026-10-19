package siteproxy

import (
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

// staticHandler serves a static export from a file system.
type staticHandler struct {
	fsys      fs.FS
	spa       bool
	immutable []string
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if hidden(p) {
		h.notFound(w, r, p)
		return
	}
	if name, ok := h.resolve(p); ok {
		h.serve(w, r, name, p, http.StatusOK, h.isImmutable(p))
		return
	}
	h.notFound(w, r, p)
}

// resolve finds the file for a request path: the file itself, a directory's index.html, or <path>.html.
func (h *staticHandler) resolve(p string) (string, bool) {
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		name = "."
	}
	if fi, err := fs.Stat(h.fsys, name); err == nil {
		if !fi.IsDir() {
			return name, true
		}
		idx := path.Join(name, "index.html")
		if fi, err := fs.Stat(h.fsys, idx); err == nil && fi.Mode().IsRegular() {
			return idx, true
		}
	}
	if name != "." {
		if fi, err := fs.Stat(h.fsys, name+".html"); err == nil && fi.Mode().IsRegular() {
			return name + ".html", true
		}
	}
	return "", false
}

func (h *staticHandler) notFound(w http.ResponseWriter, r *http.Request, p string) {
	if h.spa && !hasExt(p) && !hidden(p) {
		if fi, err := fs.Stat(h.fsys, "index.html"); err == nil && fi.Mode().IsRegular() {
			h.serve(w, r, "index.html", p, http.StatusOK, false)
			return
		}
	}
	if fi, err := fs.Stat(h.fsys, "404.html"); err == nil && fi.Mode().IsRegular() {
		h.serve(w, r, "404.html", p, http.StatusNotFound, false)
		return
	}
	http.NotFound(w, r)
}

func (h *staticHandler) serve(w http.ResponseWriter, r *http.Request, name, reqPath string, status int, immutable bool) {
	f, err := h.fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hd := w.Header()
	if immutable {
		hd.Set("Cache-Control", cacheImmutable)
	} else {
		hd.Set("Cache-Control", cacheRevalidate)
	}
	mod := fi.ModTime()
	if !mod.IsZero() {
		hd.Set("ETag", fmt.Sprintf(`W/"%s-%s"`, strconv.FormatInt(fi.Size(), 16), strconv.FormatInt(mod.UnixNano(), 16)))
	}
	rs, seekable := f.(io.ReadSeeker)
	if status == http.StatusOK && seekable {
		http.ServeContent(w, r, name, mod, rs)
		return
	}
	// Error pages (and files that cannot seek) are written whole, with the status they stand for.
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	hd.Set("Content-Type", ct)
	hd.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (h *staticHandler) isImmutable(p string) bool {
	for _, pat := range h.immutable {
		if matchPath(pat, p) {
			return true
		}
	}
	return false
}

// matchPath matches a request path against an immutable pattern.
func matchPath(pattern, p string) bool {
	if base, ok := strings.CutSuffix(pattern, "/**"); ok {
		return strings.HasPrefix(p, base+"/")
	}
	ok, _ := path.Match(pattern, p)
	return ok
}

// hidden reports a dotfile segment (.git, .env), except /.well-known.
func hidden(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != ".well-known" {
			return true
		}
	}
	return false
}

func hasExt(p string) bool { return path.Ext(path.Base(p)) != "" }
