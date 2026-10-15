// Package admin serves the admin single-page app. The built assets are committed in dist/ and embedded, so a
// Go build needs no Node. The app only talks to the management API; it learns its prefixes from config.json.
package admin

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// Dist is the embedded build output, rooted at the directory that holds index.html.
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

type Options struct {
	APIPrefix      string // management API, for config.json
	DeliveryPrefix string // delivery API, for config.json
	Pages          bool   // the pages module is on
	// DevURL, when set, proxies everything except config.json to a dev server (Vite) instead of serving dist.
	DevURL string
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

type handler struct {
	prefix string
	dist   fs.FS
	index  []byte
	config []byte
	proxy  http.Handler
}

// Handler serves the app under prefix ("/admin": leading slash, no trailing one). Mount it at prefix+"/"
// without stripping the prefix. index.html is served for every path that is not a file in dist (and not under assets/), with its
// relative asset links ("./assets/…") made absolute under prefix.
func Handler(prefix string, dist fs.FS, o Options) http.Handler {
	h := &handler{prefix: prefix, dist: dist}
	h.config, _ = json.Marshal(struct {
		APIPrefix      string `json:"apiPrefix"`
		Prefix         string `json:"prefix"`
		Pages          bool   `json:"pages"`
		DeliveryPrefix string `json:"deliveryPrefix"`
	}{o.APIPrefix, prefix, o.Pages, o.DeliveryPrefix})
	if o.DevURL != "" {
		if u, err := url.Parse(o.DevURL); err == nil {
			h.proxy = &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u) }}
		}
	}
	if b, err := fs.ReadFile(dist, "index.html"); err == nil {
		h.index = bytes.ReplaceAll(b, []byte(`="./`), []byte(`="`+prefix+`/`))
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel, ok := strings.CutPrefix(r.URL.Path, h.prefix+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rel == "config.json" {
		h.secure(w)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(h.config)
		return
	}
	if h.proxy != nil {
		h.proxy.ServeHTTP(w, r)
		return
	}
	h.secure(w)
	rel = path.Clean("/" + rel)[1:]
	switch {
	case rel == "" || rel == "index.html" || !strings.HasPrefix(rel, "assets/") && !h.exists(rel):
		if h.index == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(h.index)
	default:
		h.file(w, r, rel)
	}
}

func (h *handler) exists(name string) bool {
	st, err := fs.Stat(h.dist, name)
	return err == nil && !st.IsDir()
}

func (h *handler) secure(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("Content-Security-Policy", csp)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "same-origin")
}

func (h *handler) file(w http.ResponseWriter, r *http.Request, name string) {
	f, err := h.dist.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	rs, ok := f.(io.ReadSeeker)
	if err != nil || st.IsDir() || !ok {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, rs)
}
