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

// itemsFn lists a collection: items "post" (dict "limit" 4 "sort" "-publishedAt"). The page is tagged with what
// it reads, and the items carry their page's URL for link.
func (p *page) itemsFn(typ string, args ...any) ([]*Item, error) {
	if p.f == nil {
		return nil, nil
	}
	q, err := params(args)
	if err != nil {
		return nil, fmt.Errorf("items %s: %w", typ, err)
	}
	if q.Get("locale") == "" && p.locale != "" {
		q.Set("locale", p.locale)
	}
	v, err := p.remember("items:"+typ+"?"+q.Encode(), func() (any, error) {
		l, err := p.f.list(p.context(), typ, q, p.f.preview != "")
		if err != nil || l == nil {
			return []*Item(nil), err
		}
		for _, it := range l.Items {
			p.withPath(it)
		}
		return l.Items, nil
	})
	if err != nil {
		return nil, fmt.Errorf("items %s: %w", typ, err)
	}
	return v.([]*Item), nil
}

// itemFn reads one item by id; nil when it is not available.
func (p *page) itemFn(id string) (*Item, error) {
	if p.f == nil || id == "" {
		return nil, nil
	}
	v, err := p.remember("item:"+id, func() (any, error) {
		it, err := p.f.item(p.context(), id, p.f.preview != "", p.locale)
		return p.withPath(it), err
	})
	if err != nil {
		return nil, fmt.Errorf("item %s: %w", id, err)
	}
	return v.(*Item), nil
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
