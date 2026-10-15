package site

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/modules/pages"
)

// Item is a delivery item as templates see it.
type Item struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Locale   string            `json:"locale"`
	Name     string            `json:"name"`
	Key      string            `json:"key"`
	URL      string            `json:"url"`
	Fallback bool              `json:"fallback"`
	Fields   map[string]any    `json:"fields"`
	Links    map[string]string `json:"links"`
	Path     string            `json:"-"` // listings: the item's page on this site, when it has one
}

type routeReply struct {
	Located *pages.Located `json:"located"`
	Item    *Item          `json:"item"`
}

type treeNode struct {
	Kind  string     `json:"kind"`
	Name  string     `json:"name"`
	Path  string     `json:"path"`
	URL   string     `json:"url"`
	Nodes []treeNode `json:"nodes"`
}

type reply struct {
	status int
	body   []byte
	header http.Header
}

func (r reply) tags() []string { return strings.Fields(r.header.Get("Surrogate-Key")) }

func (r reply) decode(v any) error {
	if err := json.Unmarshal(r.body, v); err != nil {
		return fmt.Errorf("decode delivery reply: %w", err)
	}
	return nil
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

// dropHeaders are not forwarded to the delivery API: its answers must not depend on the browser's validators
// or credentials.
var dropHeaders = []string{"Authorization", "Cookie", "If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Range", "X-Hydrogen-Preview"}

// fetcher asks the delivery API in process, as a public client of one channel, and collects the cache tags of
// every answer: the page is tagged with the union.
type fetcher struct {
	h       http.Handler
	hdr     http.Header
	channel string
	preview string
	tags    map[string]bool
}

func (s *server) fetcher(r *http.Request, channel, preview string) *fetcher {
	hdr := r.Header.Clone()
	for _, k := range dropHeaders {
		hdr.Del(k)
	}
	return &fetcher{h: s.svc.Delivery.Handler(), hdr: hdr, channel: channel, preview: preview, tags: map[string]bool{}}
}

// get requests a delivery path; withPreview adds the preview token (answers are then uncached).
func (f *fetcher) get(ctx context.Context, path string, q url.Values, withPreview bool) reply {
	if q == nil {
		q = url.Values{}
	}
	q.Set("channel", f.channel)
	if withPreview && f.preview != "" {
		q.Set("preview", f.preview)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path+"?"+q.Encode(), nil)
	if err != nil {
		return reply{status: http.StatusInternalServerError}
	}
	req.Header = f.hdr.Clone()
	req.RequestURI = req.URL.RequestURI()
	rec := &recorder{h: http.Header{}}
	f.h.ServeHTTP(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	out := reply{status: rec.code, body: rec.body.Bytes(), header: rec.h}
	for _, t := range out.tags() {
		f.tags[t] = true
	}
	return out
}

func (f *fetcher) tagList() []string {
	out := make([]string, 0, len(f.tags))
	for t := range f.tags {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

func (f *fetcher) route(ctx context.Context, rawURL string) (*routeReply, reply, error) {
	rep := f.get(ctx, "/route", url.Values{"url": {rawURL}}, true)
	if rep.status != http.StatusOK {
		return nil, rep, nil
	}
	var rr routeReply
	if err := rep.decode(&rr); err != nil {
		return nil, rep, err
	}
	return &rr, rep, nil
}

// item returns one item; nil when it is not available.
func (f *fetcher) item(ctx context.Context, id string, withPreview bool, locale string) (*Item, error) {
	q := url.Values{}
	if locale != "" {
		q.Set("locale", locale)
	}
	rep := f.get(ctx, "/items/"+url.PathEscape(id), q, withPreview)
	if rep.status != http.StatusOK {
		return nil, replyError(rep)
	}
	var out struct {
		Item *Item `json:"item"`
	}
	if err := rep.decode(&out); err != nil {
		return nil, err
	}
	return out.Item, nil
}

// replyError is nil for an unavailable item (404) and an error for anything else that failed.
func replyError(rep reply) error {
	if rep.status == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("delivery answered %d: %s", rep.status, truncate(string(rep.body), 200))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (f *fetcher) singleton(ctx context.Context, typ string) (*Item, error) {
	rep := f.get(ctx, "/singletons/"+url.PathEscape(typ), nil, false)
	if rep.status != http.StatusOK {
		return nil, replyError(rep)
	}
	var out struct {
		Item *Item `json:"item"`
	}
	if err := rep.decode(&out); err != nil {
		return nil, err
	}
	return out.Item, nil
}

func (f *fetcher) tree(ctx context.Context, depth int) ([]treeNode, error) {
	rep := f.get(ctx, "/tree", url.Values{"depth": {fmt.Sprint(depth)}}, false)
	if rep.status != http.StatusOK {
		return nil, replyError(rep)
	}
	var out struct {
		Nodes []treeNode `json:"nodes"`
	}
	if err := rep.decode(&out); err != nil {
		return nil, err
	}
	return out.Nodes, nil
}

type listing struct {
	Items   []*Item `json:"items"`
	HasMore bool    `json:"hasMore"`
}

func (f *fetcher) list(ctx context.Context, typ string, q url.Values, withPreview bool) (*listing, error) {
	rep := f.get(ctx, "/collections/"+url.PathEscape(typ)+"/items", q, withPreview)
	if rep.status != http.StatusOK {
		if rep.status == http.StatusBadRequest {
			return nil, &badRequest{string(rep.body)}
		}
		return nil, replyError(rep)
	}
	var out listing
	if err := rep.decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

type badRequest struct{ body string }

func (b *badRequest) Error() string { return "delivery rejected the request: " + truncate(b.body, 200) }
