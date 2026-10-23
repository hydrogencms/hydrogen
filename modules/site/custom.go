package site

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
)

// ErrNotFound, returned by a custom route's function, answers the channel's 404 page.
var ErrNotFound = errors.New("not found")

// Request is one visit to a custom route.
type Request struct {
	// Client reads published content in process, as a public client of the channel (drafts when previewing).
	// Everything it reads tags the page, so the page is purged and re-exported when that content changes.
	*Client
	// R is the HTTP request: R.Method, R.Header, R.Body, R.ParseForm. Exports carry no body and no cookies.
	R *http.Request
	// Channel is the channel's name and Locale its first language.
	Channel string
	Locale  string
	// Path is the channel-relative path (without the channel's prefix).
	Path string
	// Params are the pattern's wildcards, also available as R.PathValue.
	Params map[string]string
	// Query is R.URL.Query().
	Query   url.Values
	Preview bool

	srv *server
	ch  *channel.Channel
}

// URL is the URL path of a site path on this channel: the channel's prefix plus the path ("/search" →
// "/en/search").
func (r *Request) URL(path string) string {
	rel, _ := r.srv.pages.SiteURL(r.ch, path)
	return rel
}

// LinkTo is the URL path of an item this site shows (its page, or its collection route); "" when it has none.
func (r *Request) LinkTo(it *Item) string { return r.srv.linkPath(r.ch, it) }

// linkPath is the URL path of an item this site shows (its page, or its collection route); "" when it has none.
func (s *server) linkPath(ch *channel.Channel, it *Item) string {
	if it == nil {
		return ""
	}
	if it.URL != "" {
		if u, err := url.Parse(it.URL); err == nil && u.EscapedPath() != "" {
			return u.EscapedPath()
		}
	}
	if rt := s.detail(it.Type); rt != nil && ch.AllowsCollection(it.Type) {
		if p, ok := rt.build(it.ID, it.Fields); ok {
			rel, _ := s.pages.SiteURL(ch, p)
			return rel
		}
	}
	return ""
}

// Response is what a custom route answers: exactly one of Template, Partial, Body, JSON, XML or Redirect.
// Template (routes/<name>.html, pages/…, collections/… or a registered component) renders with the full page
// context: layout, helpers, meta, navigation, translations, settings and preview; Data is .Data in it.
type Response struct {
	Template string
	// Partial renders a template or component like Template, but without the layout and the preview additions
	// (a fragment, or an XML template with ContentType).
	Partial string
	// Body is sent as it is; it needs ContentType. JSON and XML encode a value (XML with the xml.Header prolog).
	Body []byte
	JSON any
	XML  any
	// ContentType overrides the type of the answer (default text/html for Partial, application/json,
	// application/xml; charset=utf-8).
	ContentType string
	Data        any
	// Title is .Title (the head title and the 404-style headings use it).
	Title string
	// Status defaults to 200, or 302 (303 after a non-GET request) with Redirect.
	Status   int
	Headers  http.Header
	Redirect string
	// Tags are extra cache tags of the page, besides those of the content the route read.
	Tags []string
	// Cacheable lets shared caches keep a successful GET answer (Cache-Control like pages, Surrogate-Key tags);
	// the default is no-store. The site's own page cache never stores custom routes.
	Cacheable bool
	// MaxAge, with Cacheable, is the shared-cache lifetime instead of the channel's delivery.cache_ttl.
	MaxAge time.Duration
}

// kinds names the answers a response sets; exactly one is valid.
func (r *Response) kinds() []string {
	var k []string
	for _, c := range []struct {
		name string
		set  bool
	}{{"Template", r.Template != ""}, {"Partial", r.Partial != ""}, {"Body", r.Body != nil}, {"JSON", r.JSON != nil},
		{"XML", r.XML != nil}, {"Redirect", r.Redirect != ""}} {
		if c.set {
			k = append(k, c.name)
		}
	}
	return k
}

// encode turns a raw answer (Body, JSON, XML) into bytes and its default content type; ok is false for the others.
func (r *Response) encode() (body []byte, ct string, ok bool, err error) {
	var buf bytes.Buffer
	switch {
	case r.Body != nil:
		if r.ContentType == "" && r.Headers.Get("Content-Type") == "" {
			return nil, "", true, errors.New("a Body needs a ContentType")
		}
		return r.Body, "", true, nil
	case r.JSON != nil:
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		err = enc.Encode(r.JSON)
		return buf.Bytes(), "application/json", true, err
	case r.XML != nil:
		buf.WriteString(xml.Header)
		err = xml.NewEncoder(&buf).Encode(r.XML)
		return buf.Bytes(), "application/xml; charset=utf-8", true, err
	}
	return nil, "", false, nil
}

// CustomRoute is a route added with Handle. Its builder methods run before the application starts.
type CustomRoute struct {
	pattern string // as given
	method  string
	path    string
	names   []string
	fn      func(*Request) (*Response, error)
	sample  func() *Response
	static  func(ctx context.Context) ([]map[string]string, error)
	// channels restricts the route to these channels (default: every rendered channel).
	channels []string
	maxBody  int64
	crossOK  bool
	err      error
}

// defaultMaxBody bounds the request body of a custom route.
const defaultMaxBody = 1 << 20

// MaxBody sets the largest request body the route reads, multipart included (default 1 MiB); a larger body
// answers 413 once the function reads it.
func (r *CustomRoute) MaxBody(n int64) *CustomRoute { r.maxBody = n; return r }

// AllowCrossSite lets a non-GET request that browsers mark as cross-site reach the route (an API called from
// other origins). By default such requests are refused with 403: the route is CSRF-protected by the Origin and
// Sec-Fetch-Site headers, not by tokens.
func (r *CustomRoute) AllowCrossSite() *CustomRoute { r.crossOK = true; return r }

// crossSite reports a state-changing request a browser sent from another site.
func crossSite(r *http.Request, own ...string) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "":
	case "same-origin", "none":
		return false
	default:
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return true
	}
	return u.Host != r.Host && !slices.Contains(own, strings.ToLower(o))
}

var wildcardRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)(\.\.\.)?\}`)

var routeMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

func newCustomRoute(pattern string, fn func(*Request) (*Response, error)) *CustomRoute {
	r := &CustomRoute{pattern: pattern, fn: fn}
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = "", pattern
	}
	path = strings.TrimSpace(path)
	switch {
	case fn == nil:
		r.err = fmt.Errorf("site: custom route %q has no function", pattern)
	case method != "" && !slices.Contains(routeMethods, method):
		r.err = fmt.Errorf("site: custom route %q: method must be one of %s", pattern, strings.Join(routeMethods, ", "))
	case !strings.HasPrefix(path, "/"):
		r.err = fmt.Errorf("site: custom route %q: the path must start with / (no host)", pattern)
	}
	r.method, r.path = method, path
	for _, m := range wildcardRE.FindAllStringSubmatch(path, -1) {
		r.names = append(r.names, m[1])
	}
	if r.err == nil {
		r.err = validatePattern(r)
	}
	return r
}

// validatePattern lets http.ServeMux judge the syntax.
func validatePattern(r *CustomRoute) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("site: custom route %q: %v", r.pattern, p)
		}
	}()
	http.NewServeMux().Handle(r.pattern, http.NotFoundHandler())
	return nil
}

// Sample declares the response site:check renders the route with (the route's function is not called by the
// check, which has no visitor and may have no database).
func (r *CustomRoute) Sample(fn func() *Response) *CustomRoute { r.sample = fn; return r }

// Static declares the parameter sets the route has pages for in a static export: one map per URL, keyed by the
// pattern's wildcard names. ClientFrom(ctx) reads content. A route without wildcards returns one empty map
// (or use Export).
func (r *CustomRoute) Static(fn func(ctx context.Context) ([]map[string]string, error)) *CustomRoute {
	r.static = fn
	return r
}

// Export marks a route without wildcards as one page of a static export.
func (r *CustomRoute) Export() *CustomRoute {
	return r.Static(func(context.Context) ([]map[string]string, error) { return []map[string]string{{}}, nil })
}

// Channels restricts the route to the named channels.
func (r *CustomRoute) Channels(names ...string) *CustomRoute { r.channels = names; return r }

func (r *CustomRoute) applies(ch *channel.Channel) bool {
	return len(r.channels) == 0 || slices.Contains(r.channels, ch.Name)
}

// exportable reports whether a GET of the route is a page of a static export.
func (r *CustomRoute) exportable() bool {
	return r.static != nil && (r.method == "" || r.method == "GET")
}

// samplePath is a concrete path the pattern matches, for the reserved-path check.
func (r *CustomRoute) samplePath() string {
	p := wildcardRE.ReplaceAllStringFunc(r.path, func(m string) string {
		if strings.HasSuffix(m, "...}") {
			return "x/y"
		}
		return "x"
	})
	return strings.ReplaceAll(p, "{$}", "")
}

// fill builds the path for one parameter set.
func (r *CustomRoute) fill(params map[string]string) (string, error) {
	var missing string
	p := wildcardRE.ReplaceAllStringFunc(r.path, func(m string) string {
		sub := wildcardRE.FindStringSubmatch(m)
		v, ok := params[sub[1]]
		if !ok || v == "" {
			missing = sub[1]
			return ""
		}
		if sub[2] != "" {
			parts := strings.Split(v, "/")
			for i := range parts {
				parts[i] = url.PathEscape(parts[i])
			}
			return strings.Join(parts, "/")
		}
		return url.PathEscape(v)
	})
	if missing != "" {
		return "", fmt.Errorf("custom route %s: static params lack %q", r.pattern, missing)
	}
	p = strings.ReplaceAll(p, "{$}", "")
	if p == "" {
		p = "/"
	}
	return p, nil
}

// reservedBy reports whether every path a route pattern can match is reserved: its leading literal segments
// must start with a reserved path's segments, and a pattern longer than the reserved path needs the /* form.
// A wildcard segment never counts as part of the reserved prefix.
func reservedBy(reserved []string, pattern string) bool {
	pattern = strings.TrimSuffix(pattern, "{$}")
	segs := strings.Split(strings.Trim(pattern, "/"), "/")
	for _, pat := range reserved {
		base, wild := strings.CutSuffix(pat, "/*")
		bsegs := strings.Split(strings.Trim(base, "/"), "/")
		if len(segs) < len(bsegs) || slices.Compare(segs[:len(bsegs)], bsegs) != 0 {
			continue
		}
		if len(segs) == len(bsegs) || wild {
			return true
		}
	}
	return false
}

func reservedOf(ch *channel.Channel) []string {
	l, _ := ch.Options["reserved"].([]string)
	if roots, ok := ch.Options["root_reserved"].([]string); ok {
		l = append(slices.Clone(l), roots...)
	}
	return l
}

// checkCustomRoutes verifies the routes against the rendered channels: names, and that every route lies on a
// reserved path.
func checkCustomRoutes(routes []*CustomRoute, chans []*channel.Channel, known func(string) bool) error {
	var errs []error
	for _, r := range routes {
		for _, n := range r.channels {
			if !known(n) {
				errs = append(errs, fmt.Errorf("site: custom route %s: channel %q does not render (render: go)", r.pattern, n))
			}
		}
		for _, ch := range chans {
			if !r.applies(ch) {
				continue
			}
			if !reservedBy(reservedOf(ch), r.path) {
				errs = append(errs, fmt.Errorf("site: custom route %s is not on a reserved path of channel %s: add its path to the channel's reserved list "+
					"(for example reserved: [%s]) so pages never route it", r.pattern, ch.Name, reserveHint(r)))
			}
		}
	}
	return errors.Join(errs...)
}

func reserveHint(r *CustomRoute) string {
	p := r.path
	if i := strings.Index(p, "{"); i >= 0 {
		p = strings.TrimRight(p[:i], "/") + "/*"
	}
	return p
}

// customSet is the compiled custom routes of the rendered channels.
type customSet struct {
	byChannel map[string]*http.ServeMux
}

type hitKey struct{}

type customHit struct {
	route  *CustomRoute
	params map[string]string
}

func buildCustom(routes []*CustomRoute, chans []*channel.Channel) (*customSet, error) {
	cs := &customSet{byChannel: map[string]*http.ServeMux{}}
	for _, ch := range chans {
		mux := http.NewServeMux()
		n := 0
		for _, r := range routes {
			if !r.applies(ch) {
				continue
			}
			if err := func() (err error) {
				defer func() {
					if p := recover(); p != nil {
						err = fmt.Errorf("site: custom route %s on channel %s: %v", r.pattern, ch.Name, p)
					}
				}()
				mux.HandleFunc(r.pattern, func(_ http.ResponseWriter, req *http.Request) {
					h, _ := req.Context().Value(hitKey{}).(*customHit)
					if h == nil {
						return
					}
					h.route, h.params = r, map[string]string{}
					for _, name := range r.names {
						h.params[name] = req.PathValue(name)
					}
				})
				return nil
			}(); err != nil {
				return nil, err
			}
			n++
		}
		if n > 0 {
			cs.byChannel[ch.Name] = mux
		}
	}
	return cs, nil
}

type nullWriter struct{ h http.Header }

func (w *nullWriter) Header() http.Header       { return w.h }
func (*nullWriter) Write(b []byte) (int, error) { return len(b), nil }
func (*nullWriter) WriteHeader(int)             {}

// match finds the custom route for a channel-relative (escaped) path. allow lists the methods that would
// match the path when only the method differs.
func (cs *customSet) match(ctx context.Context, ch *channel.Channel, method, rest string) (hit *customHit, allow string) {
	if cs == nil {
		return nil, ""
	}
	mux := cs.byChannel[ch.Name]
	if mux == nil || rest == "" {
		return nil, ""
	}
	h := &customHit{}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, hitKey{}, h), method, "http://site.invalid"+rest, nil)
	if err != nil {
		return nil, ""
	}
	w := &statusWriter{nullWriter: nullWriter{h: http.Header{}}}
	mux.ServeHTTP(w, req)
	switch {
	case h.route != nil:
		return h, ""
	case w.code == http.StatusMethodNotAllowed:
		return nil, w.h.Get("Allow")
	}
	return nil, ""
}

type statusWriter struct {
	nullWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) {
	if w.code == 0 {
		w.code = c
	}
}

// Client reads published content from the delivery API in process, as a public client of one channel.
type Client struct {
	ctx context.Context
	f   *fetcher
}

type clientKey struct{}

// ClientFrom returns the content client of a Static function's context (nil elsewhere).
func ClientFrom(ctx context.Context) *Client {
	c, _ := ctx.Value(clientKey{}).(*Client)
	return c
}

// Get requests a delivery API path (such as "/search" or "/collections/post/items") with the channel
// selected, decodes a 200 answer into out (when not nil) and returns the status.
func (c *Client) Get(path string, q url.Values, out any) (int, error) {
	rep := c.f.get(c.ctx, path, q, c.f.preview != "")
	if rep.status == http.StatusOK && out != nil {
		return rep.status, rep.decode(out)
	}
	if rep.status >= 500 {
		return rep.status, fmt.Errorf("delivery answered %d: %s", rep.status, truncate(string(rep.body), 200))
	}
	return rep.status, nil
}

// Items lists a collection with the delivery API's query parameters (limit, offset, sort, where[…][…]).
func (c *Client) Items(typ string, q url.Values) (items []*Item, hasMore bool, err error) {
	l, err := c.f.list(c.ctx, typ, q, c.f.preview != "")
	if err != nil {
		return nil, false, err
	}
	if l == nil {
		return nil, false, nil
	}
	return l.Items, l.HasMore, nil
}

// Item returns a published item (a draft when previewing); nil when it is not available.
func (c *Client) Item(id string) (*Item, error) { return c.f.item(c.ctx, id, c.f.preview != "", "") }

// runCustom calls a custom route and turns its answer into a result.
func (s *server) runCustom(rc *reqCtx, hit *customHit) *result {
	req := &Request{Client: &Client{ctx: rc.ctx, f: rc.f}, R: rc.r, Channel: rc.ch.Name, Locale: rc.ch.Languages[0],
		Path: rc.rest, Params: hit.params, Query: rc.r.URL.Query(), Preview: rc.preview(), srv: s, ch: rc.ch}
	resp, err := hit.route.fn(req)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return &result{status: http.StatusRequestEntityTooLarge, body: []byte("request body too large"), custom: true}
	case errors.Is(err, ErrNotFound), err == nil && resp == nil:
		return s.notFound(rc)
	case err != nil:
		return s.internal(rc, fmt.Errorf("custom route %s: %w", hit.route.pattern, err))
	}
	return s.customResult(rc, hit, resp)
}

func (s *server) customResult(rc *reqCtx, hit *customHit, resp *Response) *result {
	res := &result{header: resp.Headers, custom: true}
	// the template's own reads (items, query…) tag the page too, so the tags are final after rendering
	tags := func() []string {
		tags := rc.f.tagList()
		if len(resp.Tags) > 0 {
			tags = append(tags, resp.Tags...)
			slices.Sort(tags)
			tags = slices.Compact(tags)
		}
		return tags
	}
	res.tags = tags()
	res.cacheable, res.maxAge = resp.Cacheable, resp.MaxAge
	if k := resp.kinds(); len(k) != 1 {
		if len(k) == 0 {
			return s.internal(rc, fmt.Errorf("custom route %s: the response answers nothing (set one of Template, Partial, Body, JSON, XML, Redirect)", hit.route.pattern))
		}
		return s.internal(rc, fmt.Errorf("custom route %s: the response sets %s, but only one is allowed", hit.route.pattern, strings.Join(k, " and ")))
	}
	status := resp.Status
	if resp.Redirect != "" {
		if status == 0 {
			status = http.StatusFound
			if rc.r.Method != http.MethodGet && rc.r.Method != http.MethodHead {
				status = http.StatusSeeOther
			}
		}
		if status < 300 || status > 399 {
			return s.internal(rc, fmt.Errorf("custom route %s: redirect with status %d", hit.route.pattern, status))
		}
		res.status, res.location = status, resp.Redirect
		return res
	}
	if status == 0 {
		status = http.StatusOK
	}
	if status < 200 || status > 599 || (status >= 300 && status < 400) {
		return s.internal(rc, fmt.Errorf("custom route %s: status %d needs a Redirect or is invalid", hit.route.pattern, status))
	}
	if body, ct, ok, err := resp.encode(); ok {
		if err != nil {
			return s.internal(rc, fmt.Errorf("custom route %s: %w", hit.route.pattern, err))
		}
		res.status, res.body, res.tags = status, body, tags()
		res.contentType = resp.ContentType
		if res.contentType == "" && resp.Headers.Get("Content-Type") == "" {
			res.contentType = ct
		}
		return res
	}
	name, partial := resp.Template, false
	if resp.Partial != "" {
		name, partial = resp.Partial, true
	}
	if !rc.eng.has(name) {
		return s.internal(rc, fmt.Errorf("custom route %s: template %q does not exist (add site/templates/routes/<name>.html)", hit.route.pattern, name))
	}
	p, v := s.newPage(rc, rc.ch.Languages[0], nil)
	p.loadNav = s.navLoader(rc, p)
	v.Data, v.Status, v.Title = resp.Data, status, resp.Title
	for k, val := range hit.params {
		v.Params[k] = val
	}
	var abs string
	v.Path, abs = s.pages.SiteURL(rc.ch, strings.TrimSuffix(rc.rest, "/"))
	if rc.r.Method == http.MethodGet || rc.r.Method == http.MethodHead {
		p.canon = abs
	}
	p.crumbs = []Crumb{{Name: p.tFn("home"), URL: v.Channel.Home}}
	if resp.Title != "" {
		p.crumbs = append(p.crumbs, Crumb{Name: resp.Title, URL: v.Path})
	}
	if partial {
		body, err := rc.eng.renderPartial(name, p, v)
		if err != nil {
			return s.internal(rc, err)
		}
		res.status, res.body, res.tags, res.contentType = status, body, tags(), resp.ContentType
		return res
	}
	body, err := rc.eng.render(name, p, v)
	if err != nil {
		return s.internal(rc, err)
	}
	res.status, res.body, res.tags, res.contentType = status, s.finish(rc, body), tags(), resp.ContentType
	return res
}
