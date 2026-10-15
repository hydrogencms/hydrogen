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
	"slices"
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
	// Extensions are the project's and modules' admin extensions (nil: none), listed in config.json and served
	// under <prefix>/x/.
	Extensions *Extensions
	// ElementsDevURL, when set, proxies <prefix>/x/dev/* to a dev server (Vite) for extension elements.
	ElementsDevURL string
	// FrameOrigins are the origins the preview pane may frame (the channels' frontends, "scheme://host[:port]").
	FrameOrigins []string
}

// Scripts are 'self' only. Styles allow inline: extension elements written with Svelte inject their component
// styles as <style> elements, which 'self' alone would drop; inline styles cannot run code.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// frameSrc lets the preview pane frame the channels' frontends, and nothing else.
func frameSrc(origins []string) string {
	src := []string{"'self'"}
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
			strings.ContainsAny(u.Host, " ;,'\"") {
			continue
		}
		if origin := u.Scheme + "://" + u.Host; !slices.Contains(src, origin) {
			src = append(src, origin)
		}
	}
	return "; frame-src " + strings.Join(src, " ")
}

type handler struct {
	csp    string
	prefix string
	dist   fs.FS
	index  []byte
	config []byte
	proxy  http.Handler
	files  map[string]*xfile
	xdev   http.Handler
}

// Handler serves the app under prefix ("/admin": leading slash, no trailing one). Mount it at prefix+"/"
// without stripping the prefix. index.html is served for every path that is not a file in dist (and not under assets/), with its
// relative asset links ("./assets/…") made absolute under prefix.
func Handler(prefix string, dist fs.FS, o Options) http.Handler {
	h := &handler{csp: csp + frameSrc(o.FrameOrigins), prefix: prefix, dist: dist}
	if o.Extensions == nil {
		o.Extensions, _ = Compose(prefix, nil, "")
	}
	h.files = o.Extensions.files
	h.config, _ = json.Marshal(struct {
		APIPrefix      string      `json:"apiPrefix"`
		Prefix         string      `json:"prefix"`
		Pages          bool        `json:"pages"`
		DeliveryPrefix string      `json:"deliveryPrefix"`
		Extensions     *Extensions `json:"extensions"`
	}{o.APIPrefix, prefix, o.Pages, o.DeliveryPrefix, o.Extensions})
	h.proxy = devProxy(o.DevURL)
	h.xdev = devProxy(o.ElementsDevURL)
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
	if rel, ok := strings.CutPrefix(rel, "x/"); ok {
		h.extension(w, r, rel)
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

func devProxy(raw string) http.Handler {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u) }}
}

// extension serves a declared extension file, or proxies the elements dev server. Anything else under x/ is 404.
func (h *handler) extension(w http.ResponseWriter, r *http.Request, rel string) {
	if strings.HasPrefix(rel, "dev/") && h.xdev != nil {
		h.xdev.ServeHTTP(w, r)
		return
	}
	f := h.files[rel]
	if f == nil {
		http.NotFound(w, r)
		return
	}
	b, etag, err := f.read()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.secure(w)
	w.Header().Set("Content-Type", f.typ)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
}

func (h *handler) exists(name string) bool {
	st, err := fs.Stat(h.dist, name)
	return err == nil && !st.IsDir()
}

func (h *handler) secure(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("Content-Security-Policy", h.csp)
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
