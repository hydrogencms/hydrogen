package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

func (a *API) coreRoutes() []route {
	return []route{
		{Route: Route{Method: "GET", Path: "/items/{id}", Handler: a.getItem}},
		{Route: Route{Method: "GET", Path: "/items/by-key/{key}", Handler: a.getByKey}},
		{Route: Route{Method: "GET", Path: "/singletons/{type}", Handler: a.getSingleton}},
		{Route: Route{Method: "GET", Path: "/collections/{type}/items", Handler: a.listItems}},
		{Route: Route{Method: "GET", Path: "/channels/{name}", Handler: a.getChannel}, byName: true},
		{Route: Route{Method: "GET", Path: "/openapi.json"}, raw: http.HandlerFunc(a.serveOpenAPI)},
		{Route: Route{Method: "GET", Path: "/preview/events"}, raw: http.HandlerFunc(a.serveEvents)},
	}
}

// params rejects query parameters the route doesn't know; channel, locale and preview apply everywhere.
func (st *state) params(allowed ...string) error {
	for k := range st.q {
		switch k {
		case "channel", "locale", "preview":
		default:
			if !slices.Contains(allowed, k) {
				return manage.BadRequest("unknown query parameter %q", k)
			}
		}
	}
	return nil
}

// includeDepth reads ?include=0..3.
func (st *state) includeDepth() (int, error) {
	v := st.q.Get("include")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > maxInclude {
		return 0, manage.BadRequest("include must be a whole number from 0 to %d", maxInclude)
	}
	return n, nil
}

// writeItem answers with one item and its includes: {"item":…,"included":{…}}.
func (st *state) writeItem(w http.ResponseWriter, r *http.Request, it *resolved, depth int) {
	ctx := r.Context()
	prim := []*resolved{it}
	inc, err := st.include(ctx, prim, depth)
	if err != nil {
		Fail(w, r, err)
		return
	}
	all := append(slices.Clone(prim), inc...)
	if err := st.decorate(ctx, all); err != nil {
		Fail(w, r, err)
		return
	}
	buf := make([]byte, 0, len(it.payload)+64)
	buf = append(buf, `{"item":`...)
	buf = appendSpliced(buf, it.payload, it.extra)
	buf = appendIncluded(buf, inc)
	buf = append(buf, '}')
	write(w, buf)
}

func write(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", hContentType[0])
	_, _ = w.Write(body)
}

func (a *API) getItem(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	if err := st.params("include", "fields"); err != nil {
		Fail(w, r, err)
		return
	}
	depth, err := st.includeDepth()
	if err != nil {
		Fail(w, r, err)
		return
	}
	f, err := st.fields()
	if err != nil {
		Fail(w, r, err)
		return
	}
	id, perr := ids.Parse(r.PathValue("id"))
	if perr != nil {
		Fail(w, r, errUnavailable())
		return
	}
	list, err := st.load(r.Context(), []ids.UUID{id}, f)
	if err != nil || len(list) == 0 {
		Fail(w, r, orUnavailable(err))
		return
	}
	st.writeItem(w, r, list[0], depth)
}

func orUnavailable(err error) error {
	if err != nil {
		return err
	}
	return errUnavailable()
}

func (a *API) getByKey(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	key := r.PathValue("key")
	if !validKey(key) {
		Fail(w, r, errUnavailable())
		return
	}
	a.byKey(w, r, st, key, "", nil)
}

const maxKeyLen = 128

// validKey applies the content key rule (lowercase letters, digits, '-' and '_', not starting with a symbol,
// at most 128 bytes). No stored item has another key, so anything else is unavailable without a query; this
// keeps NUL bytes and the like from ever reaching the database.
func validKey(k string) bool {
	if k == "" || len(k) > maxKeyLen {
		return false
	}
	for i := range len(k) {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '-' || c == '_') && i > 0:
		default:
			return false
		}
	}
	return true
}

func (a *API) getSingleton(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	typ := r.PathValue("type")
	t, ok := a.o.Schema.Type(typ)
	if !ok || !t.Singleton || !st.m.ch.AllowsCollection(typ) {
		Fail(w, r, errUnavailable())
		return
	}
	a.byKey(w, r, st, typ, cachetag.Singleton(typ), []string{typ})
}

// byKey answers with the item that has key; types are those the item can have (nil: any the channel shows).
func (a *API) byKey(w http.ResponseWriter, r *http.Request, st *state, key, tag string, types []string) {
	if err := st.params("include", "fields"); err != nil {
		Fail(w, r, err)
		return
	}
	depth, err := st.includeDepth()
	if err != nil {
		Fail(w, r, err)
		return
	}
	f, err := st.fields(types...)
	if err != nil {
		Fail(w, r, err)
		return
	}
	it, err := st.loadKey(r.Context(), key, f)
	if err != nil || it == nil {
		Fail(w, r, orUnavailable(err))
		return
	}
	if tag != "" {
		st.tags = append(st.tags, tag)
	}
	st.writeItem(w, r, it, depth)
}

// listParams are the query parameters of a listing besides where[field][op].
var listParams = []string{"include", "fields", "locale", "q", "sort", "limit", "offset", "total", "channel", "preview", "facets"}

func (a *API) listItems(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	for k := range st.q {
		if !slices.Contains(listParams, k) && !strings.HasPrefix(k, "where[") {
			Fail(w, r, manage.BadRequest("unknown query parameter %q", k))
			return
		}
	}
	depth, err := st.includeDepth()
	if err != nil {
		Fail(w, r, err)
		return
	}
	typ := r.PathValue("type")
	t, ok := a.o.Schema.Type(typ)
	if !ok || t.Singleton || !st.m.ch.AllowsCollection(typ) {
		Fail(w, r, errUnavailable())
		return
	}
	wantTotal, err := st.totalFlag()
	if err != nil {
		Fail(w, r, err)
		return
	}
	facetFields, err := st.facetFields(typ)
	if err != nil {
		Fail(w, r, err)
		return
	}
	f, err := st.fields(typ)
	if err != nil {
		Fail(w, r, err)
		return
	}
	sq, err := st.listQuery(typ)
	if err != nil {
		Fail(w, r, err)
		return
	}
	ctx := r.Context()
	pv, err := st.previewed(ctx, typ, sq)
	if err != nil {
		Fail(w, r, err)
		return
	}
	// The previewed draft is row 0 of the listing; the store serves the rest, shifted by one.
	limit, offset := sq.Limit, sq.Offset
	want := limit
	if pv != nil {
		if offset == 0 {
			want--
		} else {
			sq.Offset--
		}
	}
	probe := !wantTotal && want < maxPage
	sq.Limit = want
	if probe {
		sq.Limit++ // one more row than the page tells whether another page follows, and spares the count
	}
	sq.Limit = max(sq.Limit, 1)
	res, err := a.o.Reader.Query(ctx, sq)
	if err != nil {
		Fail(w, r, err)
		return
	}
	var facets map[string][]facet
	if len(facetFields) > 0 {
		if facets, err = st.facets(ctx, sq, facetFields); err != nil {
			Fail(w, r, err)
			return
		}
	}
	hasMore := sq.Offset+min(len(res.Items), want) < res.Total
	if probe {
		hasMore = len(res.Items) > want
	}
	res.Items = res.Items[:min(len(res.Items), want)]
	items, err := st.loadSummaries(ctx, res.Items, f)
	if err != nil {
		Fail(w, r, err)
		return
	}
	if pv != nil {
		if err := st.project([]*resolved{pv}, f); err != nil {
			Fail(w, r, err)
			return
		}
		res.Total++
		if offset == 0 {
			items = slices.Insert(items, 0, pv)
		}
	}
	inc, err := st.include(ctx, items, depth)
	if err != nil {
		Fail(w, r, err)
		return
	}
	if err := st.decorate(ctx, append(slices.Clone(items), inc...)); err != nil {
		Fail(w, r, err)
		return
	}
	st.tags = append(st.tags, cachetag.Collection(typ))
	total := -1
	if wantTotal {
		total = res.Total
	}
	write(w, appendList(make([]byte, 0, 64+len(items)*256), items, inc, hasMore, total, facets))
}

// listQuery turns the request's parameters into the store query of a listing. Published listings are limited
// to items the channel can show, so pages hold exactly the items the channel can show; a preview of the whole
// channel lists drafts too and is shown from the drafts.
func (st *state) listQuery(typ string) (store.Query, error) {
	q := url.Values{}
	for k, v := range st.q {
		switch k {
		case "include", "fields", "locale", "channel", "preview", "total", "facets":
		default:
			q[k] = v
		}
	}
	o, err := manage.ParseListQuery(typ, q)
	if err != nil {
		return store.Query{}, err
	}
	o.Locale = st.eff.Languages[0]
	o.Status = store.StatusPublished
	if st.preview != nil && st.preview.Item == "" {
		o.Status = store.StatusAny
	}
	sq, err := st.a.o.Content.ListQuery(o)
	if err != nil {
		return store.Query{}, err
	}
	if o.Status == store.StatusPublished {
		sq.Available = st.eff.Languages
	}
	return sq, nil
}

// previewed returns the draft an item-scoped preview token adds to a listing: the covered item when it was
// never published (so published listings cannot hold it and a draft has no index rows to query), is of the
// listed type and its draft matches the filters. A search term never matches. It is shown first on the first page.
func (st *state) previewed(ctx context.Context, typ string, sq store.Query) (*resolved, error) {
	if st.preview == nil || st.preview.Item == "" || sq.Search != "" {
		return nil, nil
	}
	id, err := ids.Parse(st.preview.Item)
	if err != nil {
		return nil, nil
	}
	v, err := st.a.o.Content.GetForChannel(ctx, id, st.eff, content.ChannelGetOptions{Draft: true})
	if errors.Is(err, content.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.Type != typ || v.Status != content.StatusDraft {
		return nil, nil
	}
	t, ok := st.a.o.Schema.Type(typ)
	if !ok || !storeutil.MatchWhere(sq.Where, st.a.o.Schema.IndexValues(t, v.Fields)) {
		return nil, nil
	}
	st.look(id, typ)
	return st.resolveDraft(ctx, v)
}

// maxPage is the largest page a store returns; a page that size cannot be probed with one more row.
const maxPage = 500

// facetFields reads ?facets=a,b: indexed text fields of the listed type.
func (st *state) facetFields(typ string) ([]string, error) {
	raw := st.q.Get("facets")
	if raw == "" {
		return nil, nil
	}
	var fields []string
	for f := range strings.SplitSeq(raw, ",") {
		if f = strings.TrimSpace(f); f == "" {
			return nil, manage.BadRequest("facets must be a comma-separated list of field names")
		}
		fields = append(fields, f)
	}
	if err := st.a.o.Content.CheckFacetFields(typ, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// facets counts the values of fields over the items sq selects: the listing's published, channel-available
// set before paging.
func (st *state) facets(ctx context.Context, sq store.Query, fields []string) (map[string][]facet, error) {
	out := make(map[string][]facet, len(fields))
	for _, name := range fields {
		counts, err := st.a.o.Reader.Facet(ctx, sq, name, content.MaxFacetValues)
		if err != nil {
			return nil, err
		}
		vals := make([]facet, len(counts))
		for i, c := range counts {
			vals[i] = facet{c.Value, c.Count}
		}
		out[name] = vals
	}
	return out, nil
}

// totalFlag reads ?total=: true makes the listing count its matches.
func (st *state) totalFlag() (bool, error) {
	switch st.q.Get("total") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, manage.BadRequest("total must be true or false")
}

// appendList appends {"items":[…],"hasMore":b,"total":n,"facets":{…},"included":{…}}; total only when it is not
// negative, facets only when asked for.
func appendList(buf []byte, items, inc []*resolved, hasMore bool, total int, facets map[string][]facet) []byte {
	buf = append(buf, `{"items":[`...)
	for i, it := range items {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendSpliced(buf, it.payload, it.extra)
	}
	buf = append(buf, `],"hasMore":`...)
	buf = strconv.AppendBool(buf, hasMore)
	if total >= 0 {
		buf = append(buf, `,"total":`...)
		buf = strconv.AppendInt(buf, int64(total), 10)
	}
	if facets != nil {
		fb, _ := marshal(facets)
		buf = append(buf, `,"facets":`...)
		buf = append(buf, fb...)
	}
	buf = appendIncluded(buf, inc)
	return append(buf, '}')
}

func (a *API) getChannel(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	if err := st.params(); err != nil {
		Fail(w, r, err)
		return
	}
	st.tags = append(st.tags, cachetag.Channel(st.m.ch.Name))
	write(w, st.m.info)
}

func (a *API) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h["Etag"] = a.oaETag
	h["Cache-Control"] = hOpenAPICache
	h["Vary"] = hNoVary
	h["X-Content-Type-Options"] = hNoSniff
	if etagMatches(r.Header["If-None-Match"], a.oaETag[0]) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h["Content-Type"] = hContentType
	_, _ = w.Write(a.openapi)
}

var (
	hOpenAPICache = []string{"public, max-age=300"}
	hNoVary       = []string{"Accept-Encoding"}
)
