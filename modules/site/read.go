package site

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

const maxPartialDepth = 32

// params turns a template's dict into delivery query parameters: strings, numbers and booleans, and lists as
// repeated parameters.
func params(args []any) (url.Values, error) {
	q := url.Values{}
	if len(args) == 0 || args[0] == nil {
		return q, nil
	}
	m, ok := args[0].(map[string]any)
	if len(args) > 1 || !ok {
		return nil, fmt.Errorf("expected one (dict …) of parameters, got %d arguments", len(args))
	}
	for _, k := range sortedKeys(m) {
		rv := reflect.ValueOf(m[k])
		if m[k] == nil {
			continue
		}
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			for i := 0; i < rv.Len(); i++ {
				s, err := paramValue(rv.Index(i).Interface())
				if err != nil {
					return nil, fmt.Errorf("parameter %s: %w", k, err)
				}
				q.Add(k, s)
			}
			continue
		}
		s, err := paramValue(m[k])
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", k, err)
		}
		q.Set(k, s)
	}
	return q, nil
}

func paramValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	}
	if f, integral, ok := num(v); ok {
		if integral {
			return strconv.FormatInt(int64(f), 10), nil
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("%T is not a string, number, boolean or list", v)
}

// remember runs fn once per key in this render.
func (p *page) remember(key string, fn func() (any, error)) (any, error) {
	if v, ok := p.memo[key]; ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	if p.memo == nil {
		p.memo = map[string]any{}
	}
	p.memo[key] = v
	return v, nil
}

func (p *page) withPath(it *Item) *Item {
	if it != nil && it.Path == "" && p.itemPath != nil {
		it.Path = p.itemPath(it)
	}
	return it
}

// adoptLinks makes the links an item carries available to link and rich text; the page's own win.
func (p *page) adoptLinks(it *Item) {
	if it == nil {
		return
	}
	for k, u := range it.Links {
		if _, ok := p.links[k]; !ok {
			p.links[k] = u
		}
	}
}

// ItemsPage is one page of a listing: Items, whether more follow, and Total, the number of matches in all.
// Total is -1 unless the listing was asked for it with "total" true, because counting costs a query.
type ItemsPage struct {
	Items   []*Item
	HasMore bool
	Total   int
}

// itemsFn lists a collection: items "post" (dict "limit" 4 "sort" "-publishedAt"). The page is tagged with what
// it reads, and the items carry their page's URL for link.
func (p *page) itemsFn(typ string, args ...any) ([]*Item, error) {
	pg, err := p.itemsPageFn(typ, args...)
	if err != nil {
		return nil, err
	}
	return pg.Items, nil
}

// itemsPageFn is itemsFn with hasMore and, with "total" true in the dict, the total number of matches.
func (p *page) itemsPageFn(typ string, args ...any) (*ItemsPage, error) {
	if p.f == nil {
		return &ItemsPage{Total: -1}, nil
	}
	q, err := params(args)
	if err != nil {
		return nil, fmt.Errorf("items %s: %w", typ, err)
	}
	if q.Get("locale") == "" && p.locale != "" {
		q.Set("locale", p.locale)
	}
	p.withLinkFields(q, typ)
	v, err := p.remember("items:"+typ+"?"+q.Encode(), func() (any, error) {
		l, err := p.f.list(p.context(), typ, q, p.f.preview != "")
		if err != nil || l == nil {
			return &ItemsPage{Total: -1}, err
		}
		for _, it := range l.Items {
			p.withPath(it)
			p.adoptLinks(it)
		}
		pg := &ItemsPage{Items: l.Items, HasMore: l.HasMore, Total: -1}
		if l.Total != nil {
			pg.Total = *l.Total
		}
		return pg, nil
	})
	if err != nil {
		return nil, fmt.Errorf("items %s: %w", typ, err)
	}
	return v.(*ItemsPage), nil
}

// withLinkFields adds the fields an item's detail route puts in its URL to a projection, so listed items keep
// their links: of the route of typ, or of every detail route when the type is not known yet.
func (p *page) withLinkFields(q url.Values, typ string) {
	if q.Get("fields") == "" || p.linkFields == nil {
		return
	}
	have := map[string]bool{}
	for _, f := range strings.Split(q.Get("fields"), ",") {
		have[strings.TrimSpace(f)] = true
	}
	out := q.Get("fields")
	for _, f := range p.linkFields(typ) {
		if !have[f] {
			have[f] = true
			out += "," + f
		}
	}
	q.Set("fields", out)
}

// itemFn reads one item by id; nil when it is not available. The optional dict takes fields (a projection, see
// items) and locale.
func (p *page) itemFn(id string, args ...any) (*Item, error) {
	if p.f == nil || id == "" {
		return nil, nil
	}
	q, err := params(args)
	if err != nil {
		return nil, fmt.Errorf("item %s: %w", id, err)
	}
	for k := range q {
		if k != "fields" && k != "locale" {
			return nil, fmt.Errorf("item %s: parameter %s is not supported (fields, locale)", id, k)
		}
	}
	if q.Get("locale") == "" && p.locale != "" {
		q.Set("locale", p.locale)
	}
	p.withLinkFields(q, "")
	v, err := p.remember("item:"+id+"?"+q.Encode(), func() (any, error) {
		it, err := p.f.itemQuery(p.context(), id, p.f.preview != "", q)
		p.adoptLinks(it)
		return p.withPath(it), err
	})
	if err != nil {
		return nil, fmt.Errorf("item %s: %w", id, err)
	}
	return v.(*Item), nil
}

// itemBlockFn reads one block of an item by its id, from any blocks field and any nesting of slots; nil when the
// item or the block is not there. The block renders with blocks, read only.
func (p *page) itemBlockFn(itemID, blockID string) (*BlockView, error) {
	if blockID == "" {
		return nil, nil
	}
	it, err := p.itemFn(itemID)
	if err != nil || it == nil {
		return nil, err
	}
	for _, k := range sortedKeys(it.Fields) {
		if m := findBlock(it.Fields[k], blockID); m != nil {
			return p.parseBlocks([]any{m}, "", true)[0], nil
		}
	}
	return nil, nil
}

// findBlock searches a JSON value for a block (an object with a type, props and the id).
func findBlock(v any, id string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["id"] == id {
			if _, ok := x["type"].(string); ok {
				if _, ok := x["props"].(map[string]any); ok {
					return x
				}
			}
		}
		for _, k := range sortedKeys(x) {
			if m := findBlock(x[k], id); m != nil {
				return m
			}
		}
	case []any:
		for _, e := range x {
			if m := findBlock(e, id); m != nil {
				return m
			}
		}
	}
	return nil
}

// queryFn requests a delivery path ("/tree", "/search") and returns the decoded JSON of a 200 answer, else nil.
func (p *page) queryFn(path string, args ...any) (map[string]any, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "?") {
		return nil, fmt.Errorf("query: %q must be a delivery path such as /tree, with parameters in the dict", path)
	}
	if p.f == nil {
		return nil, nil
	}
	q, err := params(args)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", path, err)
	}
	v, err := p.remember("query:"+path+"?"+q.Encode(), func() (any, error) {
		c := &Client{ctx: p.context(), f: p.f}
		var out map[string]any
		if _, err := c.Get(path, q, &out); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", path, err)
	}
	return v.(map[string]any), nil
}

// hasTemplateFn tells whether the set has a template of that name, so a template can fall back before partial.
func (p *page) hasTemplateFn(name string) (bool, error) {
	if err := p.ensureSet(); err != nil {
		return false, err
	}
	return p.t.Lookup(name) != nil, nil
}

// partialFn executes a template of the set by a computed name; data defaults to the page's view.
func (p *page) partialFn(name string, data ...any) (template.HTML, error) {
	if err := p.ensureSet(); err != nil {
		return "", err
	}
	if p.t.Lookup(name) == nil {
		if err := p.problem("no template %s for partial", name); err != nil {
			return "", err
		}
		return "", nil
	}
	if p.depth >= maxPartialDepth {
		return "", fmt.Errorf("partial %s: nested more than %d levels deep", name, maxPartialDepth)
	}
	var d any = p.view
	if len(data) > 0 {
		d = data[0]
	}
	p.depth++
	defer func() { p.depth-- }()
	var buf bytes.Buffer
	if err := p.t.ExecuteTemplate(&buf, name, d); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil //nolint:gosec // template output is escaped by html/template
}

// Client is the content client bound to this render: it reads in process like Request's and tags the page.
func (v *View) Client() *Client { return &Client{ctx: v.p.context(), f: v.p.f} }

// ItemsPage reads a listing like the items helper, with hasMore and, when args ask for "total", the total.
func (v *View) ItemsPage(typ string, args ...any) (*ItemsPage, error) {
	return v.p.itemsPageFn(typ, args...)
}

// FindBlock reads one block of an item by id, like the itemBlock helper; nil when it is not there.
func (v *View) FindBlock(itemID, blockID string) (*BlockView, error) {
	return v.p.itemBlockFn(itemID, blockID)
}
