package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/store"
)

const previewHeader = "X-Hydrogen-Preview"

var (
	hContentType = []string{"application/json; charset=utf-8"}
	hNoSniff     = []string{"nosniff"}
	hNoStore     = []string{"no-store"}
	hNoIndex     = []string{"noindex"}
)

func errUnavailable() error { return manage.NotFound("%s", unavailable) }

// state is what a request carries through its handler.
type state struct {
	a       *API
	m       *chanMeta
	q       url.Values
	eff     *channel.Channel // the channel with Languages narrowed by ?locale=
	locale  string           // the requested locale, "" when none
	preview *claims
	tags    []string
	noStore bool
	looked  map[ids.UUID]string // every item id the response looked up, with its type ("" when unknown)
	whole   []stored            // payloads that may have been sent unchanged as the whole body
	// relays holds the relayed drafts of the items prefetchRelays looked up, by item (none: no entry).
	relays   map[ids.UUID][]store.RelayDraft
	relaysOf map[ids.UUID]bool
}

// stored is a snapshot payload with the ETag the store holds for it.
type stored struct {
	payload []byte
	etag    string // hex SHA-256 of payload
}

type stateKey struct{}

func stateOf(r *http.Request) *state {
	st, _ := r.Context().Value(stateKey{}).(*state)
	return st
}

// Channel returns the channel a delivery route is serving.
func Channel(r *http.Request) *channel.Channel {
	if st := stateOf(r); st != nil {
		return st.m.ch
	}
	return nil
}

// Locale returns the locale the request asked for with ?locale=, "" when it didn't.
func Locale(r *http.Request) string {
	if st := stateOf(r); st != nil {
		return st.locale
	}
	return ""
}

// Languages returns the languages to try, in order: the requested locale first, then the channel's own order.
func Languages(r *http.Request) []string {
	if st := stateOf(r); st != nil {
		return st.eff.Languages
	}
	return nil
}

// Tag adds cache tags to the response (Surrogate-Key, Cache-Tag and the in-process cache's purge index).
func Tag(r *http.Request, tags ...string) {
	if st := stateOf(r); st != nil {
		st.tags = append(st.tags, tags...)
	}
}

// NoStore makes the response uncacheable: Cache-Control: no-store, no ETag, nothing stored.
func NoStore(w http.ResponseWriter) {
	if c, ok := w.(*capture); ok {
		c.noStore = true
	}
}

// Preview reports whether the request carries a valid preview token.
func Preview(r *http.Request) bool {
	st := stateOf(r)
	return st != nil && st.preview != nil
}

// capture buffers a handler's response so the framework can add headers, hash and cache it.
type capture struct {
	h       http.Header
	status  int
	body    bytes.Buffer
	noStore bool
}

func (c *capture) Header() http.Header { return c.h }

func (c *capture) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
}

func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

// WriteJSON writes v as a JSON response body.
func WriteJSON(w http.ResponseWriter, _ *http.Request, status int, v any) {
	b, err := marshal(v)
	if err != nil {
		b, status = []byte(`{"error":{"code":"internal","message":"internal error"}}`), http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", hContentType[0])
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Fail writes err as the API's error envelope. A *manage.Error is sent as given, content and store not-found
// errors become the one 404 for unavailable items, bad requests become 400 and anything else a logged 500.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var me *manage.Error
	switch {
	case errors.As(err, &me):
	case errors.Is(err, content.ErrNotFound), errors.Is(err, content.ErrUnknownType), errors.Is(err, store.ErrNotFound):
		me = manage.NotFound("%s", unavailable)
	case errors.Is(err, content.ErrBadRequest):
		me = manage.BadRequest("%s", strings.TrimPrefix(err.Error(), content.ErrBadRequest.Error()+": "))
	default:
		if st := stateOf(r); st != nil {
			st.a.log.Error("delivery: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		me = &manage.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error"}
	}
	writeError(w, me, false)
}

// plainError writes an error before a request has a state: the complete response, with its own headers.
func (a *API) plainError(w http.ResponseWriter, me *manage.Error) { writeError(w, me, true) }

// writeError sends me in the envelope manage defines. Errors from handlers (full false) get their remaining
// headers from finish.
func writeError(w http.ResponseWriter, me *manage.Error, full bool) {
	body, _ := marshal(manage.ErrorEnvelope(me))
	h := w.Header()
	h["Content-Type"] = hContentType
	if full {
		h["X-Content-Type-Options"] = hNoSniff
		h["Cache-Control"] = hNoStore
	}
	if me.Status == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(me.Status)
	_, _ = w.Write(body)
}

func unauthorized(msg string) *manage.Error {
	return &manage.Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

// rawParam finds a query parameter without parsing the whole query.
func rawParam(raw, name string) string {
	for raw != "" {
		var p string
		p, raw, _ = strings.Cut(raw, "&")
		k, v, _ := strings.Cut(p, "=")
		if k != name {
			continue
		}
		if strings.ContainsAny(v, "%+") {
			if d, err := url.QueryUnescape(v); err == nil {
				return d
			}
		}
		return v
	}
	return ""
}

func bearer(r *http.Request) (tok string, present, ok bool) {
	v := r.Header["Authorization"]
	if len(v) == 0 {
		return "", false, false
	}
	scheme, t, _ := strings.Cut(v[0], " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", true, false
	}
	return strings.TrimSpace(t), true, true
}

// selectChannel applies the channel rules: a token picks its channel; else ?channel= (public channels); else
// the preview token's channel; else the only channel.
func (a *API) selectChannel(r *http.Request, cl *claims) (*chanMeta, *manage.Error) {
	name := rawParam(r.URL.RawQuery, "channel")
	if tok, present, ok := bearer(r); present {
		if !ok || tok == "" {
			return nil, unauthorized("missing or invalid bearer token")
		}
		ch, found := a.o.Channels.ByToken(tok)
		if !found {
			return nil, unauthorized("missing or invalid bearer token")
		}
		if name != "" && name != ch.Name {
			return nil, manage.BadRequest("the token belongs to another channel than %q", name)
		}
		return a.meta[ch.Name], nil
	}
	var m *chanMeta
	switch {
	case name != "":
		if m = a.meta[name]; m == nil {
			return nil, manage.BadRequest("unknown channel %q", name)
		}
	case cl != nil:
		if m = a.meta[cl.Channel]; m == nil {
			return nil, unauthorized("invalid or expired preview token")
		}
	case a.single != nil:
		m = a.single
	default:
		return nil, manage.BadRequest("channel is required: send a channel token or ?channel=<name>")
	}
	if m.token {
		return nil, unauthorized("missing or invalid bearer token")
	}
	return m, nil
}

// wrap serves a route: channel and preview selection, the cache lookup, then the handler on a miss.
func (a *API) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cl, _ := r.Context().Value(claimsKey{}).(*claims)
		if tok := previewToken(r); tok != "" && cl == nil {
			var err error
			if cl, err = a.verifyToken(r.Context(), tok); err != nil {
				a.previewFailed(w, r, err)
				return
			}
		}
		var m *chanMeta
		var me *manage.Error
		if rt.byName {
			m, me = a.channelByName(r)
		} else {
			m, me = a.selectChannel(r, cl)
		}
		if me != nil {
			a.plainError(w, me)
			return
		}
		if cl != nil && cl.Channel != m.ch.Name {
			a.plainError(w, unauthorized("invalid or expired preview token"))
			return
		}
		var kbuf [256]byte
		var key []byte
		var gen uint64
		if cl == nil && a.cache != nil {
			key = a.appendKey(kbuf[:0], m, r)
			if e := a.cache.get(key); e != nil {
				a.writeEntry(w, r, e)
				return
			}
			gen = a.cache.generation()
		}
		st := &state{a: a, m: m, preview: cl, q: r.URL.Query()}
		if me := st.narrow(); me != nil {
			a.plainError(w, me)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, st))
		c := &capture{h: http.Header{}}
		rt.Handler(c, r)
		a.finish(w, r, st, c, key, gen)
	})
}

// channelByName selects the channel named in the path; a token channel needs its own token.
func (a *API) channelByName(r *http.Request) (*chanMeta, *manage.Error) {
	m := a.meta[r.PathValue("name")]
	if m == nil {
		return nil, manage.NotFound("%s", unavailable)
	}
	if !m.token {
		return m, nil
	}
	tok, present, ok := bearer(r)
	if !present || !ok {
		return nil, unauthorized("missing or invalid bearer token")
	}
	if ch, found := a.o.Channels.ByToken(tok); !found || ch != m.ch {
		return nil, unauthorized("missing or invalid bearer token")
	}
	return m, nil
}

// singleValued are the parameters that may appear once; a repeat would make the canonical cache key depend on
// which value wins.
var singleValued = [...]string{"locale", "channel", "include", "limit", "offset", "q", "sort", "total", "preview"}

// narrow rejects repeated single-valued parameters, then applies ?locale=: that language first, then the
// channel's own order after it.
func (st *state) narrow() *manage.Error {
	for _, k := range singleValued {
		if len(st.q[k]) > 1 {
			return manage.BadRequest("query parameter %q may appear only once", k)
		}
	}
	ch := st.m.ch
	st.eff = ch
	loc := st.q.Get("locale")
	if loc == "" {
		return nil
	}
	if !slices.Contains(ch.Languages, loc) {
		return manage.BadRequest("locale %q is not a language of channel %q", loc, ch.Name)
	}
	st.locale = loc
	st.eff = narrowTo(ch, loc)
	return nil
}

func (a *API) appendKey(dst []byte, m *chanMeta, r *http.Request) []byte {
	dst = append(dst, m.ch.Name...)
	dst = append(dst, 0)
	dst = append(dst, r.Method...)
	dst = append(dst, 0)
	dst = append(dst, r.URL.EscapedPath()...)
	dst = append(dst, 0)
	dst = appendCanonQuery(dst, r.URL.RawQuery)
	for _, n := range m.vary {
		dst = append(dst, 0)
		if v := r.Header[n]; len(v) > 0 {
			dst = append(dst, v[0]...)
		}
	}
	return dst
}

// appendCanonQuery appends the query's parameters ordered by name, so parameter order doesn't split the
// cache. The sort is stable: repeated values of one parameter keep their order, which is part of the key
// because it can change the meaning (the first of several values wins in places).
func appendCanonQuery(dst []byte, raw string) []byte {
	if raw == "" {
		return dst
	}
	var buf [8]string
	parts := buf[:0]
	for raw != "" {
		var p string
		p, raw, _ = strings.Cut(raw, "&")
		if p != "" {
			parts = append(parts, p)
		}
	}
	slices.SortStableFunc(parts, func(a, b string) int {
		ka, _, _ := strings.Cut(a, "=")
		kb, _, _ := strings.Cut(b, "=")
		return strings.Compare(ka, kb)
	})
	for i, p := range parts {
		if i > 0 {
			dst = append(dst, '&')
		}
		dst = append(dst, p...)
	}
	return dst
}

// etagMatches implements If-None-Match for a strong etag (quoted), allowing lists, weak tags and *.
func etagMatches(inm []string, etag string) bool {
	for _, v := range inm {
		if v == etag || v == "*" {
			return true
		}
		for p := range strings.SplitSeq(v, ",") {
			p = strings.TrimSpace(p)
			if p == etag || p == "W/"+etag || p == "*" {
				return true
			}
		}
	}
	return false
}

// writeEntry serves a response from an entry: stored headers and bytes, no database.
func (a *API) writeEntry(w http.ResponseWriter, r *http.Request, e *entry) {
	h := w.Header()
	h["Etag"] = e.hETag
	h["Cache-Control"] = e.hCache
	h["Vary"] = e.hVary
	if e.hSurrogate != nil {
		h["Surrogate-Key"] = e.hSurrogate
		h["Cache-Tag"] = e.hTag
	}
	if inm := r.Header["If-None-Match"]; len(inm) > 0 && etagMatches(inm, e.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h["Content-Type"] = hContentType
	h["X-Content-Type-Options"] = hNoSniff
	h["Content-Length"] = e.hLength
	_, _ = w.Write(e.body)
}

var managedHeaders = map[string]bool{"Etag": true, "Cache-Control": true, "Vary": true, "Surrogate-Key": true,
	"Cache-Tag": true, "Content-Length": true, "X-Content-Type-Options": true}

// finish writes a handler's captured response. Successful ones get an ETag, cache headers and tags and are
// stored in the cache; errors and previews are sent no-store.
func (a *API) finish(w http.ResponseWriter, r *http.Request, st *state, c *capture, key []byte, gen uint64) {
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	h := w.Header()
	for k, v := range c.h {
		if !managedHeaders[k] {
			h[k] = v
		}
	}
	if h.Get("Content-Type") == "" {
		h["Content-Type"] = hContentType
	}
	h["X-Content-Type-Options"] = hNoSniff
	h["Vary"] = st.m.hVary
	body := c.body.Bytes()
	if st.preview != nil && st.preview.share != "" {
		h["X-Robots-Tag"] = hNoIndex
	}
	if status != http.StatusOK || c.noStore || st.noStore || st.preview != nil {
		h["Cache-Control"] = hNoStore
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}
	e := newEntry(st.m, key, body, st.etagOf(body), st.responseTags())
	if a.cache != nil && key != nil {
		a.cache.put(e, gen)
	}
	a.writeEntry(w, r, e)
}

// etagOf returns the quoted strong ETag of a body. A body that is exactly a stored snapshot payload (a module
// route that sends Items' output unchanged) reuses the ETag the store computed when the version was written.
func (st *state) etagOf(body []byte) string {
	for _, s := range st.whole {
		if len(s.payload) == len(body) && bytes.Equal(s.payload, body) {
			return `"` + s.etag + `"`
		}
	}
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// newEntry builds the cache entry of a response; tags are sorted and unique.
func newEntry(m *chanMeta, key []byte, body []byte, etag string, tags []string) *entry {
	e := &entry{key: string(key), body: body, etag: etag, tags: tags}
	e.hETag, e.hCache, e.hVary, e.hLength = []string{etag}, m.hCC, m.hVary, lengthHeader(len(body))
	if len(tags) > 0 {
		e.hSurrogate = []string{strings.Join(tags, " ")}
		e.hTag = []string{strings.Join(tags, ",")}
	}
	return e
}
