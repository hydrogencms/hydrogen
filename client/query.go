package client

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Op is a filter operator of a list query.
type Op string

// The operators the APIs accept. An empty Op in a [Filter] means equality (where[field]=value).
const (
	Eq       Op = "eq"
	Ne       Op = "ne"
	Lt       Op = "lt"
	Lte      Op = "lte"
	Gt       Op = "gt"
	Gte      Op = "gte"
	In       Op = "in" // Value is a slice; sent comma-separated
	Contains Op = "contains"
	Exists   Op = "exists" // Value is a bool
	Prefix   Op = "prefix"
)

// Filter is one list condition, sent as where[field][op]=value. Filters are ANDed.
type Filter struct {
	Field string
	Op    Op
	Value any // string, bool, number, time.Time, fmt.Stringer, or a slice of those for In
}

// Sort is one sort key; the first key leads.
type Sort struct {
	Field string
	Desc  bool
}

// Asc and Desc build sort keys.
func Asc(field string) Sort  { return Sort{Field: field} }
func Desc(field string) Sort { return Sort{Field: field, Desc: true} }

// ListOptions narrows a listing. The zero value lists with the API's defaults.
type ListOptions struct {
	Locale string
	Where  []Filter
	Sort   []Sort
	Search string // q: full-text search
	Limit  int    // 0: the API's default
	Offset int
	// Total (delivery only) asks for the exact number of matches; counting costs more than the page itself.
	Total bool
	// Include (delivery only) adds the items the results reference to the answer, 1 to 3 levels deep.
	Include int
	// IfNoneMatch (delivery only) sends If-None-Match; a matching ETag gives a result whose Meta.NotModified is true.
	IfNoneMatch string
}

// GetOptions are the options of the single-item calls.
type GetOptions struct {
	Locale string
	// Include (delivery only) adds referenced items, 1 to 3 levels deep.
	Include     int
	IfNoneMatch string
}

// query builds the query string in a fixed order (channel, locale, include, filters, q, sort, limit, offset),
// so equal requests give equal URLs and caches key on them. Brackets stay literal.
type query struct{ parts []string }

func (q *query) add(key, value string) {
	q.parts = append(q.parts, key+"="+url.QueryEscape(value))
}

func (q *query) String() string { return strings.Join(q.parts, "&") }

func (o *ListOptions) encode(q *query) error {
	if o == nil {
		return nil
	}
	if o.Locale != "" {
		q.add("locale", o.Locale)
	}
	if o.Include > 0 {
		q.add("include", strconv.Itoa(o.Include))
	}
	for _, f := range o.Where {
		if f.Field == "" || strings.ContainsAny(f.Field, "[]&=#") {
			return fmt.Errorf("hydrogen: invalid filter field %q", f.Field)
		}
		v, err := filterValue(f.Value)
		if err != nil {
			return fmt.Errorf("hydrogen: filter %q: %w", f.Field, err)
		}
		key := "where[" + f.Field + "]"
		if f.Op != "" {
			key += "[" + string(f.Op) + "]"
		}
		q.add(key, v)
	}
	if o.Search != "" {
		q.add("q", o.Search)
	}
	if len(o.Sort) > 0 {
		keys := make([]string, len(o.Sort))
		for i, s := range o.Sort {
			if s.Field == "" || strings.Contains(s.Field, ",") {
				return fmt.Errorf("hydrogen: invalid sort field %q", s.Field)
			}
			keys[i] = s.Field
			if s.Desc {
				keys[i] = "-" + s.Field
			}
		}
		q.add("sort", strings.Join(keys, ","))
	}
	if o.Limit > 0 {
		q.add("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.add("offset", strconv.Itoa(o.Offset))
	}
	if o.Total {
		q.add("total", "true")
	}
	return nil
}

func (o *GetOptions) encode(q *query) {
	if o == nil {
		return
	}
	if o.Locale != "" {
		q.add("locale", o.Locale)
	}
	if o.Include > 0 {
		q.add("include", strconv.Itoa(o.Include))
	}
}

func filterValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fmt.Errorf("value is required")
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
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano), nil
	case fmt.Stringer:
		return x.String(), nil
	case []string:
		return strings.Join(x, ","), nil
	case []int:
		return joinAny(x)
	case []any:
		return joinAny(x)
	}
	return "", fmt.Errorf("unsupported value type %T", v)
}

func joinAny[T any](list []T) (string, error) {
	parts := make([]string, len(list))
	for i, e := range list {
		s, err := filterValue(e)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, ","), nil
}
