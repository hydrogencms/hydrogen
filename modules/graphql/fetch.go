package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/core/cachetag"
)

// dropHeaders are not forwarded to the delivery API: its answers must not depend on the client's validators,
// body or preview credentials (the token travels as a parameter).
var dropHeaders = []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Range", "X-Hydrogen-Preview",
	"Content-Length", "Content-Type", "Content-Encoding", "Accept-Encoding", "Origin"}

// maxIncluded is the delivery API's cap on included items per response; a response at the cap may be missing
// references.
const maxIncluded = 100

// maxInclude is the deepest include the delivery API accepts.
const maxInclude = 3

type reply struct {
	status int
	body   []byte
	header http.Header
}

type recorder struct {
	h    http.Header
	code int
	body bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.h }

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}

// fetcher asks the delivery API in process, as the GraphQL request's client: same channel, locale, preview
// token and channel token. It counts the calls, collects the cache tags of every answer and remembers the items
// it loaded, so a reference included by an earlier answer costs nothing.
type fetcher struct {
	h        http.Handler
	hdr      http.Header
	remote   string
	channel  string
	locale   string
	preview  string
	maxCalls int
	maxItems int
	trace    func(request string)

	mu      sync.Mutex
	calls   int
	spent   int
	missing bool // something looked up was not there: the answer carries no tag that would purge it
	tags    map[string]bool
	items   map[string]*item
	absent  map[string]bool
}

func newFetcher(h http.Handler, r *http.Request, ch string, maxCalls, maxItems int) *fetcher {
	hdr := r.Header.Clone()
	for _, k := range dropHeaders {
		hdr.Del(k)
	}
	f := &fetcher{h: h, hdr: hdr, remote: r.RemoteAddr, channel: ch, locale: delivery.Locale(r), maxCalls: maxCalls, maxItems: maxItems,
		tags: map[string]bool{}, items: map[string]*item{}, absent: map[string]bool{}}
	if v := r.Header.Get("X-Hydrogen-Preview"); v != "" {
		f.preview = v
	} else {
		f.preview = r.URL.Query().Get("preview")
	}
	return f
}

func (f *fetcher) get(ctx context.Context, path string, q url.Values) (reply, error) {
	f.mu.Lock()
	f.calls++
	over := f.calls > f.maxCalls
	f.mu.Unlock()
	if over {
		return reply{}, &apiError{msg: fmt.Sprintf("the query needs more than %d delivery calls; select less or use include-friendly shapes", f.maxCalls),
			code: "TOO_MANY_CALLS", status: http.StatusBadRequest}
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("channel", f.channel)
	if f.preview != "" {
		q.Set("preview", f.preview)
	}
	if f.trace != nil {
		f.trace(path + "?" + q.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path+"?"+q.Encode(), nil)
	if err != nil {
		return reply{}, err
	}
	req.Header = f.hdr.Clone()
	req.RemoteAddr = f.remote
	req.RequestURI = req.URL.RequestURI()
	rec := &recorder{h: http.Header{}}
	f.h.ServeHTTP(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	out := reply{status: rec.code, body: rec.body.Bytes(), header: rec.h}
	f.mu.Lock()
	for _, t := range strings.Fields(rec.h.Get("Surrogate-Key")) {
		f.tags[t] = true
	}
	f.mu.Unlock()
	return out, nil
}

// tagList is the union of the answers' tags. Beyond the delivery API's own bound for item tags the item tags
// are replaced by the listing tags of the channel's collections, which keeps the headers small.
func (f *fetcher) tagList(allowed []string, singleton func(string) bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items, rest []string
	for t := range f.tags {
		if strings.HasPrefix(t, cachetag.Item("")) {
			items = append(items, t)
		} else {
			rest = append(rest, t)
		}
	}
	if len(items) > delivery.MaxItemTags {
		items = items[:0]
		for _, typ := range allowed {
			if singleton(typ) {
				items = append(items, cachetag.Singleton(typ))
			} else {
				items = append(items, cachetag.Collection(typ))
			}
		}
	}
	out := append(rest, items...)
	slices.Sort(out)
	return slices.Compact(out)
}

func (f *fetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// apiError is an error a GraphQL response reports with extensions.code and extensions.status.
type apiError struct {
	msg    string
	code   string
	status int
}

func (e *apiError) Error() string { return e.msg }

func (e *apiError) Extensions() map[string]any {
	return map[string]any{"code": e.code, "status": e.status}
}

func userError(format string, args ...any) error {
	return &apiError{msg: fmt.Sprintf(format, args...), code: "BAD_USER_INPUT", status: http.StatusBadRequest}
}

// replyError maps a failed delivery answer; a 404 is not an error (the item is not available).
func replyError(rep reply) error {
	switch {
	case rep.status == http.StatusNotFound:
		return nil
	case rep.status >= 500:
		return &apiError{msg: "internal error", code: "INTERNAL", status: rep.status}
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rep.body, &env)
	msg := env.Error.Message
	if msg == "" {
		msg = http.StatusText(rep.status)
	}
	code := strings.ToUpper(env.Error.Code)
	if code == "" {
		code = "DELIVERY_ERROR"
	}
	return &apiError{msg: msg, code: code, status: rep.status}
}

func decodeBody(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("decode delivery reply: %w", err)
	}
	return nil
}

// item is a delivery item as the resolvers see it.
type item struct {
	m      map[string]any
	fields map[string]any
	id     string
	typ    string
	loc    string          // the locale parameter requests for its references carry
	left   int             // include depth its response was made with: its references are in the registry or unavailable
	have   map[string]bool // the fields a projection kept; nil when the item is whole
}

func newItem(m map[string]any, loc string, left int) *item {
	it := &item{m: m, loc: loc, left: left}
	it.id, _ = m["id"].(string)
	it.typ, _ = m["type"].(string)
	it.fields, _ = m["fields"].(map[string]any)
	if it.fields == nil {
		it.fields = map[string]any{}
	}
	return it
}

func regKey(loc, id string) string { return loc + "\x00" + id }

// covers reports whether the item holds every field in need.
func (it *item) covers(need []string) bool {
	if it.have == nil {
		return true
	}
	for _, n := range need {
		if !it.have[n] {
			return false
		}
	}
	return true
}

// register remembers an item for later references; a whole item beats a projected one, then a deeper include.
func (f *fetcher) register(it *item) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := regKey(it.loc, it.id)
	old := f.items[k]
	switch {
	case old == nil, old.have != nil && it.have == nil:
		f.items[k] = it
	case old.have == nil && it.have != nil:
	case old.left < it.left:
		f.items[k] = it
	}
}

// lookup returns the remembered item when it holds the fields in need; partial reports an item that is known
// but lacks some.
func (f *fetcher) lookup(loc, id string, need []string) (it *item, absent, partial bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := regKey(loc, id)
	it = f.items[k]
	if it != nil && !it.covers(need) {
		return nil, false, true
	}
	return it, f.absent[k], false
}

func (f *fetcher) markAbsent(loc, id string) {
	f.mu.Lock()
	f.absent[regKey(loc, id)] = true
	f.mu.Unlock()
}

// decodeItems reads the items of an answer and registers them with the items it included.
// The items of the answer are projected to the fields in proj (nil: whole); the ones it included never are.
func (f *fetcher) decodeItems(body map[string]any, primary []any, loc string, depth int, proj []string) []*item {
	inc, _ := body["included"].(map[string]any)
	left := depth
	if len(inc) >= maxIncluded {
		left = 0
	}
	for _, v := range inc {
		if m, ok := v.(map[string]any); ok {
			f.register(newItem(m, loc, 0))
		}
	}
	out := make([]*item, 0, len(primary))
	for _, v := range primary {
		if m, ok := v.(map[string]any); ok {
			it := newItem(m, loc, left)
			if proj != nil {
				it.have = make(map[string]bool, len(proj))
				for _, n := range proj {
					it.have[n] = true
				}
			}
			f.register(it)
			out = append(out, it)
		}
	}
	return out
}

// loadOne fetches a path that answers {item, included}; nil when the item is not available.
func (f *fetcher) loadOne(ctx context.Context, path string, q url.Values, loc string, depth int, proj []string) (*item, error) {
	if q == nil {
		q = url.Values{}
	}
	if proj != nil {
		q.Set("fields", strings.Join(proj, ","))
	}
	if loc != "" {
		q.Set("locale", loc)
	}
	if depth > 0 {
		q.Set("include", fmt.Sprint(depth))
	}
	if err := f.spend(1, depth); err != nil {
		return nil, err
	}
	rep, err := f.get(ctx, path, q)
	if err != nil {
		return nil, err
	}
	if rep.status != http.StatusOK {
		if rep.status == http.StatusNotFound {
			f.markMissing()
		}
		return nil, replyError(rep)
	}
	var body map[string]any
	if err := decodeBody(rep.body, &body); err != nil {
		return nil, err
	}
	its := f.decodeItems(body, []any{body["item"]}, loc, depth, proj)
	if len(its) == 0 {
		f.markMissing()
		return nil, nil
	}
	return its[0], nil
}

// caps asks the delivery API which optional routes exist.
func (m *module) probe() caps {
	var c caps
	if m.svc == nil || m.svc.Delivery == nil {
		return c
	}
	req, err := http.NewRequest(http.MethodGet, "/openapi.json", nil)
	if err != nil {
		return c
	}
	rec := &recorder{h: http.Header{}}
	m.svc.Delivery.Handler().ServeHTTP(rec, req)
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(rec.body.Bytes(), &doc) != nil {
		return c
	}
	_, c.route = doc.Paths["/route"]
	_, c.search = doc.Paths["/search"]
	return c
}

// spend charges the item budget with the items a call may bring: its page size times its include depth plus one.
func (f *fetcher) spend(pageSize, include int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spent += pageSize * (include + 1)
	if f.spent > f.maxItems {
		return &apiError{msg: fmt.Sprintf("the query asks for more than %d items in all; use smaller pages or less include depth", f.maxItems),
			code: "QUERY_TOO_EXPENSIVE", status: http.StatusBadRequest}
	}
	return nil
}

func (f *fetcher) markMissing() {
	f.mu.Lock()
	f.missing = true
	f.mu.Unlock()
}

func (f *fetcher) wasMissing() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.missing
}
