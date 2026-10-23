package delivery

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/search"
)

// Search limits.
const (
	maxSearchText     = 200
	defaultSearchPage = 10
	maxSearchPage     = search.MaxLimit
	maxSearchOffset   = 1000
)

// searchParams are the query parameters of /search besides where[field][op].
var searchParams = []string{"q", "type", "facets", "sort", "limit", "offset", "include", "fields", "highlight", "locale", "channel", "preview"}

// searchTypes are the types the channel can search: its collections that are searchable.
func (a *API) searchTypes(st *state) []string {
	var out []string
	for _, t := range a.o.Schema.Types() {
		if !t.Singleton && t.Searchable() && st.m.ch.AllowsCollection(t.Name) {
			out = append(out, t.Name)
		}
	}
	return out
}

type searchHit struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Locale     string            `json:"locale"`
	Score      float64           `json:"score"`
	Highlights map[string]string `json:"highlights,omitempty"`
}

// searchReq reads and checks the parameters of /search into an engine request.
func (st *state) searchReq() (search.Request, int, error) {
	a := st.a
	var req search.Request
	for k := range st.q {
		if !slices.Contains(searchParams, k) && !strings.HasPrefix(k, "where[") {
			return req, 0, manage.BadRequest("unknown query parameter %q", k)
		}
	}
	for _, k := range []string{"type", "facets", "highlight"} {
		if len(st.q[k]) > 1 {
			return req, 0, manage.BadRequest("query parameter %q may appear only once", k)
		}
	}
	depth, err := st.includeDepth()
	if err != nil {
		return req, 0, err
	}
	text := st.q.Get("q")
	switch n := utf8.RuneCountInString(text); {
	case strings.TrimSpace(text) == "":
		return req, 0, manage.BadRequest("q is required")
	case n > maxSearchText:
		return req, 0, manage.BadRequest("q is too long (at most %d characters)", maxSearchText)
	}
	req.Text = text
	allowed := a.searchTypes(st)
	if raw := st.q.Get("type"); raw != "" {
		for name := range strings.SplitSeq(raw, ",") {
			name = strings.TrimSpace(name)
			if !slices.Contains(allowed, name) {
				return req, 0, manage.BadRequest("type %q is not a searchable collection of this channel", name)
			}
			if !slices.Contains(req.Types, name) {
				req.Types = append(req.Types, name)
			}
		}
	} else {
		req.Types = allowed
	}
	req.Locale = st.eff.Languages[0]
	if req.Limit, err = intParam(st.q, "limit", defaultSearchPage, 1, maxSearchPage); err != nil {
		return req, 0, err
	}
	if req.Offset, err = intParam(st.q, "offset", 0, 0, maxSearchOffset); err != nil {
		return req, 0, err
	}
	switch st.q.Get("highlight") {
	case "", "true":
		req.Highlight = true
	case "false":
	default:
		return req, 0, manage.BadRequest("highlight must be true or false")
	}
	var facets []string
	if raw := st.q.Get("facets"); raw != "" {
		for f := range strings.SplitSeq(raw, ",") {
			if f = strings.TrimSpace(f); f == "" {
				return req, 0, manage.BadRequest("facets must be a comma-separated list of field names")
			}
			facets = append(facets, f)
		}
	}
	var sorts []content.SortSpec
	if raw := st.q.Get("sort"); raw != "" {
		for p := range strings.SplitSeq(raw, ",") {
			p = strings.TrimSpace(p)
			desc := strings.HasPrefix(p, "-")
			if p = strings.TrimPrefix(p, "-"); p == "" {
				return req, 0, manage.BadRequest("sort has an empty field name")
			}
			sorts = append(sorts, content.SortSpec{Field: p, Desc: desc})
		}
	}
	where := url.Values{}
	for k, v := range st.q {
		if strings.HasPrefix(k, "where[") {
			where[k] = v
		}
	}
	filters, err := manage.ParseWhere(where)
	if err != nil {
		return req, 0, err
	}
	plan, err := a.o.Content.PlanSearch(req.Types, filters, sorts, facets)
	if err != nil {
		return req, 0, err
	}
	for _, c := range plan.Where {
		req.Where = append(req.Where, search.Filter{Field: c.Field, Op: c.Op, Value: c.Value})
	}
	for _, s := range plan.Sort {
		req.Sort = append(req.Sort, search.Sort{Field: s.Field, Desc: s.Desc})
	}
	req.Facets = plan.Facets
	return req, depth, nil
}

func intParam(q url.Values, key string, def, lo, hi int) (int, error) {
	s := q.Get(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, manage.BadRequest("%s must be a whole number from %d to %d", key, lo, hi)
	}
	return n, nil
}

// searchItems answers /search: the engine's hits, each shown as a list shows the item.
func (a *API) searchItems(w http.ResponseWriter, r *http.Request) {
	st := stateOf(r)
	req, depth, err := st.searchReq()
	if err != nil {
		Fail(w, r, err)
		return
	}
	f, err := st.fields(req.Types...)
	if err != nil {
		Fail(w, r, err)
		return
	}
	ctx := r.Context()
	for _, t := range req.Types {
		st.tags = append(st.tags, cachetag.Collection(t))
	}
	var res search.Result
	if len(req.Types) > 0 {
		if res, err = a.o.Search.Search(ctx, req); err != nil {
			Fail(w, r, err)
			return
		}
	}
	list := make([]ids.UUID, len(res.Hits))
	for i, h := range res.Hits {
		list[i] = h.Item
	}
	items, err := st.load(ctx, list, f)
	if err != nil {
		Fail(w, r, err)
		return
	}
	shown := make(map[ids.UUID]bool, len(items))
	for _, it := range items {
		shown[it.id] = true
	}
	hits := make([]searchHit, 0, len(items))
	for _, h := range res.Hits {
		if !shown[h.Item] {
			continue
		}
		sh := searchHit{ID: h.Item.String(), Type: h.Type, Locale: h.Locale, Score: h.Score}
		if req.Highlight && len(h.Highlights) > 0 {
			sh.Highlights = h.Highlights
		}
		hits = append(hits, sh)
		shown[h.Item] = false // a repeated hit is shown once
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
	write(w, appendSearch(make([]byte, 0, 128+len(items)*256), items, inc, hits, res, req.Facets))
}

// facet is one value of a facet and the number of items that have it.
type facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// appendSearch appends {"items":[…],"hits":[…],"total":n,"estimated":b,"facets":{…},"included":{…}}.
func appendSearch(buf []byte, items, inc []*resolved, hits []searchHit, res search.Result, facets []string) []byte {
	buf = append(buf, `{"items":[`...)
	for i, it := range items {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendSpliced(buf, it.payload, it.extra)
	}
	hb, _ := marshal(hits)
	buf = append(buf, `],"hits":`...)
	buf = append(buf, hb...)
	buf = append(buf, `,"total":`...)
	buf = strconv.AppendInt(buf, int64(res.Total), 10)
	buf = append(buf, `,"estimated":`...)
	buf = strconv.AppendBool(buf, res.Estimated)
	if len(facets) > 0 {
		out := make(map[string][]facet, len(facets))
		for _, f := range facets {
			vals := make([]facet, 0, len(res.Facets[f]))
			for _, v := range res.Facets[f] {
				vals = append(vals, facet{v.Value, v.Count})
			}
			out[f] = vals
		}
		fb, _ := marshal(out)
		buf = append(buf, `,"facets":`...)
		buf = append(buf, fb...)
	}
	buf = appendIncluded(buf, inc)
	return append(buf, '}')
}
