package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
)

const defaultTTL = 60

// reqCtx is one request being resolved.
type reqCtx struct {
	ctx   context.Context
	r     *http.Request
	ch    *channel.Channel
	rest  string // the site path, after the channel's prefix
	raw   string // absolute request URL for the routing lookup
	f     *fetcher
	token string // a verified preview token, else ""
	eng   *engine
}

func (rc *reqCtx) preview() bool { return rc.token != "" }

// result is what a request resolved to.
type result struct {
	status   int
	body     []byte
	location string
	tags     []string
	store    bool
	// custom routes: the route's headers, and whether shared caches may keep the answer
	header    http.Header
	custom    bool
	cacheable bool
}

func (s *server) serveSite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !s.serveMutation(w, r) {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	eng := s.engine()
	p := r.URL.Path
	if eng == nil && s.loadErr != nil {
		http.Error(w, "site templates do not parse:\n"+s.loadErr.Error(), http.StatusInternalServerError)
		return
	}
	if eng == nil || s.owned(p) {
		http.NotFound(w, r)
		return
	}
	if rest, ok := strings.CutPrefix(p, "/preview/"); ok {
		if tok, fromCookie := previewTokenOf(r, ""); tok != "" {
			s.servePreviewItem(w, r, eng, rest, tok, fromCookie)
			return
		}
	}
	raw := s.schemeOf(r) + "://" + r.Host + r.URL.EscapedPath()
	ch, rest, ok := s.pages.Match(raw, r.Header)
	if !ok || !renders(ch) {
		if p == "/" && s.cfg.Root != "" {
			if root, ok := s.svc.Channels.Get(s.cfg.Root); ok {
				home, _ := s.pages.SiteURL(root, "/")
				http.Redirect(w, r, home, http.StatusFound)
				return
			}
		}
		s.unmatched(w, r, eng)
		return
	}
	if sub, ok := strings.CutPrefix(rest, "/preview/"); ok {
		if tok, fromCookie := previewTokenOf(r, ch.Name); tok != "" {
			s.servePreviewItem(w, r, eng, sub, tok, fromCookie)
			return
		}
	}
	token := ""
	if tok, fromCookie := previewTokenOf(r, ch.Name); tok != "" {
		var done bool
		if token, done = s.acceptToken(w, r, tok, fromCookie, ch.Name); done {
			return
		}
	}
	if rest == notFoundPath {
		rc := &reqCtx{ctx: r.Context(), r: r, ch: ch, rest: rest, raw: raw, f: s.fetcher(r, ch.Name, token), token: token, eng: eng}
		s.respond(w, r, ch, s.notFound(rc), token != "", "", 0)
		return
	}
	if s.serveCustom(w, r, eng, ch, rest, raw, token) {
		return
	}
	key := ch.Name + "\x00" + r.URL.EscapedPath()
	var gen uint64
	if token == "" && s.cache != nil && !noCache(r) {
		if e := s.cache.get(key); e != nil {
			s.writeCached(w, r, e)
			return
		}
		gen = s.cache.generation()
	}
	rc := &reqCtx{ctx: r.Context(), r: r, ch: ch, rest: rest, raw: raw, f: s.fetcher(r, ch.Name, token), token: token, eng: eng}
	res := s.resolve(rc)
	s.respond(w, r, ch, res, token != "", key, gen)
}

// notFoundPath is a channel-relative path that always answers the channel's 404 page: static exports render it
// as the site's 404 file.
const notFoundPath = "/__hydrogen/404"

type noCacheKey struct{}

// noCache reports a request that must not be answered from the page cache (static exports).
func noCache(r *http.Request) bool { v, _ := r.Context().Value(noCacheKey{}).(bool); return v }

// serveCustom answers a request a custom route claims; false when none does.
func (s *server) serveCustom(w http.ResponseWriter, r *http.Request, eng *engine, ch *channel.Channel, rest, raw, token string) bool {
	hit, allow := s.custom.match(r.Context(), ch, r.Method, rest)
	if hit == nil && allow == "" {
		return false
	}
	rc := &reqCtx{ctx: r.Context(), r: r, ch: ch, rest: rest, raw: raw, f: s.fetcher(r, ch.Name, token), token: token, eng: eng}
	if hit == nil {
		w.Header().Set("Allow", allow)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !hit.route.crossOK && crossSite(r, strings.ToLower(strings.TrimRight(ch.Frontend, "/"))) {
			s.respond(w, r, ch, &result{status: http.StatusForbidden, body: []byte("cross-site request refused"), custom: true}, false, "", 0)
			return true
		}
		limit := hit.route.maxBody
		if limit <= 0 {
			limit = defaultMaxBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	s.respond(w, r, ch, s.runCustom(rc, hit), token != "", "", 0)
	return true
}

// serveMutation answers a non-GET request when a custom route of a rendered channel claims it.
func (s *server) serveMutation(w http.ResponseWriter, r *http.Request) bool {
	eng := s.engine()
	if eng == nil || s.owned(r.URL.Path) || len(s.custom.byChannel) == 0 {
		return false
	}
	raw := s.schemeOf(r) + "://" + r.Host + r.URL.EscapedPath()
	ch, rest, ok := s.pages.Match(raw, r.Header)
	if !ok || !renders(ch) {
		return false
	}
	return s.serveCustom(w, r, eng, ch, rest, raw, "")
}

// owned are the paths other parts of the binary serve; a "/" catch-all must not answer them.
func (s *server) owned(p string) bool {
	if p == "/api" || strings.HasPrefix(p, "/api/") || p == "/healthz" || p == "/readyz" {
		return true
	}
	if a := s.svc.Core.Admin; a.Enabled && (p == a.Prefix || strings.HasPrefix(p, a.Prefix+"/")) {
		return true
	}
	return false
}

// schemeOf is https for TLS requests and for requests a trusted proxy (http.trusted_proxies) says were https;
// X-Forwarded-Proto from any other peer is ignored.
func (s *server) schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" && s.fromProxy(r) {
		return "https"
	}
	return "http"
}

func (s *server) fromProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap().WithZone("")
	return slices.ContainsFunc(s.proxies, func(p netip.Prefix) bool { return p.Contains(ip) })
}

// previewCookie is the cookie of a channel: one per channel, so previews on a shared host do not clear each other.
func previewCookie(channel string) string { return cookieName + "-" + channel }

// previewTokenOf reads ?preview=, else the channel's preview cookie (channel "": the first preview cookie).
func previewTokenOf(r *http.Request, channel string) (tok string, fromCookie bool) {
	if t := r.URL.Query().Get("preview"); t != "" {
		return t, false
	}
	if channel != "" {
		if c, err := r.Cookie(previewCookie(channel)); err == nil && c.Value != "" {
			return c.Value, true
		}
		return "", false
	}
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, cookieName+"-") && c.Value != "" {
			return c.Value, true
		}
	}
	return "", false
}

// acceptToken verifies a preview token for a channel (chName "" accepts any). A good token from the URL is
// stored in an HttpOnly cookie so navigation keeps previewing; a bad one from the URL is an error, and a bad
// cookie is dropped.
func (s *server) acceptToken(w http.ResponseWriter, r *http.Request, tok string, fromCookie bool, chName string) (token string, done bool) {
	name, _, exp, err := s.svc.Delivery.PreviewTokenInfo(tok)
	if err == nil && (chName == "" || name == chName) {
		if !fromCookie {
			c := &http.Cookie{Name: previewCookie(name), Value: tok, Path: "/", MaxAge: cookieMaxAge(exp), HttpOnly: true, SameSite: http.SameSiteLaxMode}
			if s.schemeOf(r) == "https" {
				// The admin embeds the page in a frame, which is cross-site when the hosts differ.
				c.SameSite, c.Secure, c.Partitioned = http.SameSiteNoneMode, true, true
			}
			http.SetCookie(w, c)
		}
		return tok, false
	}
	if fromCookie {
		if chName != "" {
			http.SetCookie(w, &http.Cookie{Name: previewCookie(chName), Path: "/", MaxAge: -1, HttpOnly: true})
		}
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "invalid or expired preview token", http.StatusUnauthorized)
	return "", true
}

// cookieMaxAge keeps the preview cookie as long as its token lives (share links last days); a token that
// reports no expiry gets an hour.
func cookieMaxAge(exp time.Time) int {
	if exp.IsZero() {
		return 3600
	}
	return max(1, int(time.Until(exp).Seconds()))
}

func (s *server) cacheControl(ch *channel.Channel) string {
	ttl := defaultTTL
	if v, ok := ch.Setting("delivery.cache_ttl"); ok {
		switch x := v.(type) {
		case int:
			ttl = x
		case int64:
			ttl = int(x)
		case uint64:
			ttl = int(x)
		case float64:
			ttl = int(x)
		}
	}
	ttl = max(ttl, 0)
	n := strconv.Itoa(ttl)
	return "public, max-age=0, s-maxage=" + n + ", stale-while-revalidate=" + n
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func matchesETag(r *http.Request, etag string) bool {
	for _, v := range r.Header.Values("If-None-Match") {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == etag || p == "W/"+etag || p == "*" {
				return true
			}
		}
	}
	return false
}

func (s *server) respond(w http.ResponseWriter, r *http.Request, ch *channel.Channel, res *result, preview bool, key string, gen uint64) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	cc := "no-store"
	if preview {
		h.Set("Referrer-Policy", "no-referrer")
		if s.cfg.PreviewCSP {
			h.Set("Content-Security-Policy", "script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors "+s.adminOrigin(r))
		}
	}
	if !preview && res.status < 500 {
		cc = s.cacheControl(ch)
	}
	if res.custom && (!res.cacheable || preview || res.status != http.StatusOK || (r.Method != http.MethodGet && r.Method != http.MethodHead)) {
		cc = "no-store"
	}
	h.Set("Cache-Control", cc)
	if !preview && len(res.tags) > 0 {
		h.Set("Surrogate-Key", strings.Join(res.tags, " "))
		h.Set("Cache-Tag", strings.Join(res.tags, ","))
	}
	for k, v := range res.header {
		h[http.CanonicalHeaderKey(k)] = slices.Clone(v)
	}
	if res.location != "" {
		h.Set("Location", res.location)
		w.WriteHeader(res.status)
		return
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/html; charset=utf-8")
	}
	if res.status == http.StatusOK && !preview && !res.custom {
		etag := etagOf(res.body)
		h.Set("ETag", etag)
		if s.cache != nil && res.store {
			h.Set("X-Hydrogen-Cache", "miss")
			s.cache.put(&cached{key: key, body: res.body, etag: etag, cc: cc, tags: res.tags}, gen)
		}
		if matchesETag(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(res.body)))
	w.WriteHeader(res.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(res.body)
	}
}

func (s *server) writeCached(w http.ResponseWriter, r *http.Request, e *cached) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", e.cc)
	h.Set("X-Hydrogen-Cache", "hit")
	h.Set("ETag", e.etag)
	if len(e.tags) > 0 {
		h.Set("Surrogate-Key", strings.Join(e.tags, " "))
		h.Set("Cache-Tag", strings.Join(e.tags, ","))
	}
	if matchesETag(r, e.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(e.body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(e.body)
	}
}

func (s *server) serveAsset(w http.ResponseWriter, r *http.Request) {
	eng := s.engine()
	if eng == nil {
		http.NotFound(w, r)
		return
	}
	eng.assets.serve(w, r, strings.TrimPrefix(r.URL.Path, assetsURL))
}

// injectPreview adds what the admin's preview pane needs: the preview marker the bridge looks for, a
// data-h-root element to swap on reload (unless the layout marks one), and the bridge script. The marker holds
// the SHA-256 of the preview token, which proves to the admin that this server knows the token.
func injectPreview(page []byte, adminOrigin, tokenHash string) []byte {
	marker := `<meta name="robots" content="noindex"><meta name="hydrogen-preview" content="` + tokenHash + `">`
	script := `<script src="` + bridgeURL + `" data-auto data-mode="reload" data-admin-origin="` + adminOrigin + `"></script>`
	out := page
	if !rootAttr.Match(out) {
		if at := afterTag(out, "<body"); at >= 0 {
			out = splice(out, at, "<div data-h-root>")
			if k := lastIndexFold(out, "</body>"); k >= 0 {
				out = splice(out, k, "</div>")
			} else {
				out = append(out, "</div>"...)
			}
		}
	}
	switch i := lastIndexFold(out, "</head>"); {
	case i >= 0:
		out = splice(out, i, marker+"\n")
	default:
		// no </head>: after <head …>, else at the start of <body …>, else first
		if at := afterTag(out, "<head"); at >= 0 {
			out = splice(out, at, marker)
		} else if at := afterTag(out, "<body"); at >= 0 {
			out = splice(out, at, marker)
		} else {
			out = append([]byte(marker), out...)
		}
	}
	if i := lastIndexFold(out, "</body>"); i >= 0 {
		return splice(out, i, script+"\n")
	}
	return append(out, script...)
}

// rootAttr matches data-h-root as an attribute of an element, not as text.
var rootAttr = regexp.MustCompile(`(?i)<[a-z][^>]*\sdata-h-root(\s|=|>|/)`)

func lastIndexFold(b []byte, s string) int { return bytes.LastIndex(bytes.ToLower(b), []byte(s)) }

// afterTag is the offset after the end of the first <name …> tag (name given as "<head"), or -1.
func afterTag(b []byte, open string) int {
	low := bytes.ToLower(b)
	for from := 0; ; {
		i := bytes.Index(low[from:], []byte(open))
		if i < 0 {
			return -1
		}
		i += from
		if n := i + len(open); n < len(low) && (low[n] == '>' || low[n] == ' ' || low[n] == '\n' || low[n] == '\t') {
			if j := bytes.IndexByte(low[i:], '>'); j >= 0 {
				return i + j + 1
			}
			return -1
		}
		from = i + len(open)
	}
}

func splice(b []byte, at int, s string) []byte {
	out := make([]byte, 0, len(b)+len(s))
	out = append(out, b[:at]...)
	out = append(out, s...)
	return append(out, b[at:]...)
}

func isUUID(s string) bool {
	_, err := ids.Parse(s)
	return err == nil
}

// unmatched answers a URL no rendered channel serves with the 404 page of the root channel (else the first
// rendered one), so visitors of unknown prefixes see the site rather than a bare error.
func (s *server) unmatched(w http.ResponseWriter, r *http.Request, eng *engine) {
	var ch *channel.Channel
	if root, ok := s.svc.Channels.Get(s.cfg.Root); ok && renders(root) {
		ch = root
	}
	for _, c := range s.svc.Channels.All() {
		if ch == nil && renders(c) {
			ch = c
		}
	}
	if ch == nil || !eng.has("404.html") {
		http.NotFound(w, r)
		return
	}
	rc := &reqCtx{ctx: r.Context(), r: r, ch: ch, f: s.fetcher(r, ch.Name, ""), eng: eng}
	s.respond(w, r, ch, s.notFound(rc), false, "", 0)
}
