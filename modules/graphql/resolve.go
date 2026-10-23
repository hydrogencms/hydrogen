package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	gql "github.com/graphql-go/graphql"
)

// node is a value of a generated object type: a content item (v is its fields) or a group inside one.
type node struct {
	it *item
	v  map[string]any
}

type listVal struct {
	items   []*node
	hasMore bool
	total   int // -1 when not counted
}

type routeVal struct {
	kind       string
	status     int
	location   string
	canonical  string
	alternates any
	item       *node
}

type searchVal struct {
	items  []*node
	hits   any
	total  int
	facets any
}

type rcKey struct{}

// reqCtx is what a GraphQL request's resolvers share.
type reqCtx struct {
	ctx      context.Context
	f        *fetcher
	cs       *chanSchema
	maxNodes int64
	nodes    atomic.Int64
	needs    sync.Map // first *ast.Field of a resolved field → the content fields its selection reads
}

// spendNodes charges the response-size budget.
func (rc *reqCtx) spendNodes(n int) error {
	if rc.nodes.Add(int64(n)) > rc.maxNodes {
		return &apiError{msg: fmt.Sprintf("the response would hold more than %d objects; select less or use smaller pages", rc.maxNodes),
			code: "QUERY_TOO_EXPENSIVE", status: http.StatusBadRequest}
	}
	return nil
}

func reqOf(ctx context.Context) *reqCtx {
	rc, _ := ctx.Value(rcKey{}).(*reqCtx)
	return rc
}

func (rc *reqCtx) nodeOf(it *item) (*node, error) {
	if err := rc.spendNodes(1); err != nil {
		return nil, err
	}
	return &node{it: it, v: it.fields}, nil
}

func (rc *reqCtx) nodesOf(its []*item) ([]*node, error) {
	if err := rc.spendNodes(len(its)); err != nil {
		return nil, err
	}
	out := make([]*node, len(its))
	for i, it := range its {
		out[i] = &node{it: it, v: it.fields}
	}
	return out, nil
}

func convString(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	return fmt.Sprint(v), nil
}

func convLong(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) { return longOf(v), nil }

func convInt(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) { return intOf(v), nil }

func convFloat(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil, nil
		}
		return f, nil
	case float64:
		return x, nil
	}
	return nil, nil
}

func convPlain(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) { return v, nil }

func convMap(_ *reqCtx, _ *node, v any, _ gql.ResolveInfo) (any, error) {
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return nil, nil
}

func convGroup(rc *reqCtx, n *node, v any, _ gql.ResolveInfo) (any, error) {
	if m, ok := v.(map[string]any); ok {
		if err := rc.spendNodes(1); err != nil {
			return nil, err
		}
		return &node{it: n.it, v: m}, nil
	}
	return nil, nil
}

// intOf reads a JSON number as an int; nil when it is not whole or does not fit GraphQL's Int.
func intOf(v any) any {
	var f float64
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return fitInt(n)
		}
		g, err := x.Float64()
		if err != nil {
			return nil
		}
		f = g
	case float64:
		f = x
	case int:
		return fitInt(int64(x))
	case int64:
		return fitInt(x)
	default:
		return nil
	}
	if f != math.Trunc(f) {
		return nil
	}
	return fitInt(int64(f))
}

func fitInt(n int64) any {
	if n > math.MaxInt32 || n < math.MinInt32 {
		return nil
	}
	return int(n)
}

// resolveRef shows the item a reference points at: from what earlier answers loaded, else from the delivery
// API. A reference to an item the channel cannot show, or of a type the field does not allow, is null.
func (rc *reqCtx) resolveRef(parent *node, id string, allowed []string, info gql.ResolveInfo) (any, error) {
	if rc == nil {
		return nil, fmt.Errorf("no request context")
	}
	loc := parent.it.loc
	need := rc.selectionOf(info)
	it, absent, partial := rc.f.lookup(loc, id, need)
	if it == nil {
		if absent || (parent.it.left > 0 && !partial) {
			rc.f.markMissing()
			return nil, nil
		}
		var err error
		depth := rc.depthOf(info, "")
		if it, err = rc.f.loadOne(rc.ctx, "/items/"+url.PathEscape(id), nil, loc, depth, rc.cs.projection(need, "")); err != nil {
			return nil, err
		}
		if it == nil {
			rc.f.markAbsent(loc, id)
			return nil, nil
		}
	}
	if len(allowed) > 0 && !slices.Contains(allowed, it.typ) {
		return nil, nil
	}
	return rc.nodeOf(it)
}

func (rc *reqCtx) depthOf(info gql.ResolveInfo, target string) int {
	if len(info.FieldASTs) == 0 {
		return 0
	}
	return min(includeDepth(info.FieldASTs[0].SelectionSet, target, info.Fragments, rc.cs.meta), maxInclude)
}

func (rc *reqCtx) locale(args map[string]any) string {
	if s, ok := args["locale"].(string); ok && s != "" {
		return s
	}
	return rc.f.locale
}

func (b *builder) resolveGet(typ, target string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		id, _ := p.Args["id"].(string)
		key, _ := p.Args["key"].(string)
		if (id == "") == (key == "") {
			return nil, userError("pass exactly one of id and key")
		}
		path := "/items/by-key/" + url.PathEscape(key)
		if id != "" {
			path = "/items/" + url.PathEscape(id)
		}
		it, err := rc.f.loadOne(p.Context, path, nil, rc.locale(p.Args), rc.depthOf(p.Info, target), rc.fieldsFor(p.Info, typ))
		if err != nil || it == nil || it.typ != typ {
			return nil, err
		}
		return rc.nodeOf(it)
	}
}

func (b *builder) resolveSingleton(typ, target string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		it, err := rc.f.loadOne(p.Context, "/singletons/"+url.PathEscape(typ), nil, rc.locale(p.Args), rc.depthOf(p.Info, target), rc.fieldsFor(p.Info, typ))
		if err != nil || it == nil {
			return nil, err
		}
		return rc.nodeOf(it)
	}
}

func (b *builder) resolveAny() gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		id, _ := p.Args["id"].(string)
		it, err := rc.f.loadOne(p.Context, "/items/"+url.PathEscape(id), nil, rc.locale(p.Args), rc.depthOf(p.Info, "Item"), rc.fieldsFor(p.Info, ""))
		if err != nil || it == nil {
			return nil, err
		}
		if _, ok := rc.cs.objects[it.typ]; !ok {
			rc.f.markMissing()
			return nil, nil
		}
		return rc.nodeOf(it)
	}
}

func (b *builder) resolveList(typ, target string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		q := url.Values{}
		if err := whereParams(p.Args["where"], q); err != nil {
			return nil, err
		}
		pagingParams(p.Args, q)
		loc := rc.locale(p.Args)
		if loc != "" {
			q.Set("locale", loc)
		}
		wantTotal := false
		if len(p.Info.FieldASTs) > 0 {
			wantTotal = selects(p.Info.FieldASTs[0].SelectionSet, "total", p.Info.Fragments)
		}
		if wantTotal {
			q.Set("total", "true")
		}
		depth := rc.depthOf(p.Info, target)
		if depth > 0 {
			q.Set("include", strconv.Itoa(depth))
		}
		proj := rc.fieldsFor(p.Info, typ)
		if proj != nil {
			q.Set("fields", strings.Join(proj, ","))
		}
		if err := rc.f.spend(pageSize(p.Args), depth); err != nil {
			return nil, err
		}
		rep, err := rc.f.get(p.Context, "/collections/"+url.PathEscape(typ)+"/items", q)
		if err != nil {
			return nil, err
		}
		if rep.status != http.StatusOK {
			if rep.status == http.StatusNotFound {
				rc.f.markMissing()
				return &listVal{items: []*node{}, total: -1}, nil
			}
			return nil, replyError(rep)
		}
		var body map[string]any
		if err := decodeBody(rep.body, &body); err != nil {
			return nil, err
		}
		prim, _ := body["items"].([]any)
		nodes, err := rc.nodesOf(rc.f.decodeItems(body, prim, loc, depth, proj))
		if err != nil {
			return nil, err
		}
		out := &listVal{items: nodes, total: -1}
		out.hasMore, _ = body["hasMore"].(bool)
		if n, ok := body["total"].(json.Number); ok {
			if v, err := n.Int64(); err == nil {
				out.total = int(v)
			}
		}
		return out, nil
	}
}

// pageSize is the page a list or search asks for; the delivery API's default is 50, and search's 10.
func pageSize(args map[string]any) int {
	if n, ok := args["limit"].(int); ok && n > 0 {
		return n
	}
	return 50
}

func pagingParams(args map[string]any, q url.Values) {
	if s, ok := args["sort"].(string); ok && s != "" {
		q.Set("sort", s)
	}
	for _, k := range []string{"limit", "offset"} {
		if n, ok := args[k].(int); ok {
			q.Set(k, strconv.Itoa(n))
		}
	}
}

func (b *builder) resolveSearch() gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		q := url.Values{}
		q.Set("q", p.Args["q"].(string))
		if err := whereParams(p.Args["where"], q); err != nil {
			return nil, err
		}
		pagingParams(p.Args, q)
		for arg, param := range map[string]string{"types": "type", "facets": "facets"} {
			if l, ok := p.Args[arg].([]any); ok && len(l) > 0 {
				parts := make([]string, len(l))
				for i, x := range l {
					parts[i], _ = x.(string)
				}
				q.Set(param, strings.Join(parts, ","))
			}
		}
		loc := rc.locale(p.Args)
		if loc != "" {
			q.Set("locale", loc)
		}
		depth := rc.depthOf(p.Info, "SearchResult")
		if depth > 0 {
			q.Set("include", strconv.Itoa(depth))
		}
		if err := rc.f.spend(pageSize(p.Args), depth); err != nil {
			return nil, err
		}
		rep, err := rc.f.get(p.Context, "/search", q)
		if err != nil {
			return nil, err
		}
		if rep.status != http.StatusOK {
			return nil, replyError(rep)
		}
		var body map[string]any
		if err := decodeBody(rep.body, &body); err != nil {
			return nil, err
		}
		prim, _ := body["items"].([]any)
		nodes, err := rc.nodesOf(rc.f.decodeItems(body, prim, loc, depth, nil))
		if err != nil {
			return nil, err
		}
		out := &searchVal{items: nodes, hits: body["hits"], facets: body["facets"]}
		if n, ok := body["total"].(json.Number); ok {
			if v, err := n.Int64(); err == nil {
				out.total = int(v)
			}
		}
		return out, nil
	}
}

func (b *builder) resolveRoute() gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		rc := reqOf(p.Context)
		rep, err := rc.f.get(p.Context, "/route", url.Values{"url": {p.Args["url"].(string)}})
		if err != nil {
			return nil, err
		}
		if rep.status != http.StatusOK {
			if rep.status == http.StatusNotFound {
				rc.f.markMissing()
			}
			return nil, replyError(rep)
		}
		var body struct {
			Located struct {
				Kind       string `json:"kind"`
				Locale     string `json:"locale"`
				Canonical  string `json:"canonical"`
				Alternates any    `json:"alternates"`
				Resolution struct {
					Status   int    `json:"status"`
					Location string `json:"location"`
				} `json:"resolution"`
			} `json:"located"`
			Item map[string]any `json:"item"`
		}
		if err := decodeBody(rep.body, &body); err != nil {
			return nil, err
		}
		l := body.Located
		out := &routeVal{kind: l.Kind, status: l.Resolution.Status, location: l.Resolution.Location, canonical: l.Canonical, alternates: l.Alternates}
		if body.Item != nil {
			it := newItem(body.Item, l.Locale, 0)
			rc.f.register(it)
			if _, ok := rc.cs.objects[it.typ]; ok {
				if out.item, err = rc.nodeOf(it); err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	}
}

// whereParams writes a where argument ({"field": {"op": value}} or {"field": value}) as where[field][op]=value.
// "or" (a list of objects, any of which must match) and "not" (an object, or a list of objects none of which may
// match) are the delivery API's boolean groups; their objects hold plain conditions.
func whereParams(v any, q url.Values) error {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return userError("where must be an object such as {\"title\": {\"eq\": \"x\"}}")
	}
	for _, field := range sortedKeys(m) {
		switch field {
		case "or", "not":
			groups, isList := m[field].([]any)
			if !isList {
				groups = []any{m[field]}
			}
			if field == "or" && !isList {
				return userError("where.or must be a list of objects")
			}
			for i, g := range groups {
				gm, ok := g.(map[string]any)
				if !ok {
					return userError("where.%s must hold objects of conditions", field)
				}
				prefix := "where[" + field + "][" + strconv.Itoa(i) + "]"
				if err := whereConds(prefix, "where."+field, gm, q); err != nil {
					return err
				}
			}
		default:
			if err := whereConds("where", "where", map[string]any{field: m[field]}, q); err != nil {
				return err
			}
		}
	}
	return nil
}

func whereConds(prefix, label string, m map[string]any, q url.Values) error {
	for _, field := range sortedKeys(m) {
		if sub, ok := m[field].(map[string]any); ok {
			for _, op := range sortedKeys(sub) {
				s, err := whereValue(sub[op])
				if err != nil {
					return userError("%s.%s.%s: %v", label, field, op, err)
				}
				q.Add(prefix+"["+field+"]["+op+"]", s)
			}
			continue
		}
		s, err := whereValue(m[field])
		if err != nil {
			return userError("%s.%s: %v", label, field, err)
		}
		q.Add(prefix+"["+field+"]", s)
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func whereValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if _, nested := e.([]any); nested {
				return "", fmt.Errorf("a value list cannot hold lists")
			}
			s, err := whereValue(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return strings.Join(parts, ","), nil
	}
	return "", fmt.Errorf("unsupported value")
}

// longOf reads a JSON number as an int64; nil when it is not whole.
func longOf(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		if f, err := x.Float64(); err == nil && f == math.Trunc(f) && math.Abs(f) < 1<<63 {
			return int64(f)
		}
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<63 {
			return int64(x)
		}
	case int:
		return int64(x)
	case int64:
		return x
	}
	return nil
}
