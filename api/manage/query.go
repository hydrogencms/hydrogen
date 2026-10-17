package manage

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

const (
	defaultLimit = 50
	maxLimit     = 500
	maxOffset    = 1_000_000
)

var ops = []store.Op{store.OpEq, store.OpNe, store.OpLt, store.OpLte, store.OpGt, store.OpGte,
	store.OpIn, store.OpContains, store.OpExists, store.OpPrefix, store.OpUnder}

// single returns the one value of a query parameter; repeating it is an error.
func single(q url.Values, key string) (string, error) {
	switch v := q[key]; len(v) {
	case 0:
		return "", nil
	case 1:
		return v[0], nil
	}
	return "", BadRequest("query parameter %q given more than once", key)
}

func parseStatus(s string) (store.Status, error) {
	switch s {
	case "", "any":
		return store.StatusAny, nil
	case "published":
		return store.StatusPublished, nil
	case "draft":
		return store.StatusDraft, nil
	}
	return 0, BadRequest("status must be any, published or draft")
}

func parseInt(q url.Values, key string, def, lo, hi int) (int, error) {
	s, err := single(q, key)
	if err != nil || s == "" {
		return def, err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, BadRequest("%s must be a whole number from %d to %d", key, lo, hi)
	}
	return n, nil
}

func parseSort(s string) ([]content.SortSpec, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > content.MaxSortKeys {
		return nil, BadRequest("too many sort keys: %d (at most %d)", len(parts), content.MaxSortKeys)
	}
	out := make([]content.SortSpec, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		desc := strings.HasPrefix(p, "-")
		p = strings.TrimPrefix(p, "-")
		if p == "" {
			return nil, BadRequest("sort has an empty field name")
		}
		out = append(out, content.SortSpec{Field: p, Desc: desc})
	}
	return out, nil
}

// parseWhere reads where[field][op]=value (op defaults to eq); repeated keys are ANDed.
func parseWhere(q url.Values) ([]content.Filter, error) {
	var keys []string
	for k := range q {
		if strings.HasPrefix(k, "where[") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out []content.Filter
	for _, k := range keys {
		field, op, err := splitWhereKey(k)
		if err != nil {
			return nil, err
		}
		for _, v := range q[k] {
			if len(out) >= content.MaxConditions {
				return nil, BadRequest("too many filters (at most %d)", content.MaxConditions)
			}
			if op == store.OpIn {
				if n := strings.Count(v, ",") + 1; n > content.MaxInValues {
					return nil, BadRequest("too many values in %s: %d (at most %d)", k, n, content.MaxInValues)
				}
			}
			var val any = v
			if op == store.OpExists {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return nil, BadRequest("%s needs true or false", k)
				}
				val = b
			}
			out = append(out, content.Filter{Field: field, Op: op, Value: val})
		}
	}
	return out, nil
}

func splitWhereKey(k string) (string, store.Op, error) {
	rest, _ := strings.CutPrefix(k, "where[")
	field, rest, ok := strings.Cut(rest, "]")
	if !ok || field == "" || strings.ContainsAny(field, "[") {
		return "", "", BadRequest("malformed filter %q; use where[field][op]=value", k)
	}
	op := store.OpEq
	if rest != "" {
		name, ok := strings.CutPrefix(rest, "[")
		name, ok2 := strings.CutSuffix(name, "]")
		if !ok || !ok2 || strings.ContainsAny(name, "[]") {
			return "", "", BadRequest("malformed filter %q; use where[field][op]=value", k)
		}
		op = store.Op(name)
		if !slices.Contains(ops, op) {
			return "", "", BadRequest("unknown filter operator %q in %q", name, k)
		}
	}
	return field, op, nil
}

func parseFacets(typ string, q url.Values) (content.FacetOptions, error) {
	o := content.FacetOptions{Type: typ}
	for k := range q {
		switch k {
		case "locale", "status", "fields":
		default:
			if !strings.HasPrefix(k, "where[") {
				return o, BadRequest("unknown query parameter %q", k)
			}
		}
	}
	var err error
	if o.Locale, err = single(q, "locale"); err != nil {
		return o, err
	}
	st, err := single(q, "status")
	if err != nil {
		return o, err
	}
	if o.Status, err = parseStatus(st); err != nil {
		return o, err
	}
	fields, err := single(q, "fields")
	if err != nil {
		return o, err
	}
	if fields == "" {
		return o, BadRequest("fields is required: comma-separated indexed text fields")
	}
	for _, f := range strings.Split(fields, ",") {
		if f = strings.TrimSpace(f); f == "" {
			return o, BadRequest("fields must be a comma-separated list of field names")
		}
		o.Fields = append(o.Fields, f)
	}
	if len(o.Fields) > content.MaxFacetFields {
		return o, BadRequest("too many fields: %d (at most %d)", len(o.Fields), content.MaxFacetFields)
	}
	o.Where, err = parseWhere(q)
	return o, err
}

func parseList(typ string, q url.Values) (content.ListOptions, error) {
	o := content.ListOptions{Type: typ}
	for k := range q {
		switch k {
		case "locale", "status", "q", "sort", "limit", "offset", "fields":
		default:
			if !strings.HasPrefix(k, "where[") {
				return o, BadRequest("unknown query parameter %q", k)
			}
		}
	}
	var err error
	if o.Locale, err = single(q, "locale"); err != nil {
		return o, err
	}
	st, err := single(q, "status")
	if err != nil {
		return o, err
	}
	if o.Status, err = parseStatus(st); err != nil {
		return o, err
	}
	if o.Search, err = single(q, "q"); err != nil {
		return o, err
	}
	if n := len(storeutil.SearchTerms(o.Search)); n > content.MaxSearchTerms {
		return o, BadRequest("too many search terms: %d (at most %d)", n, content.MaxSearchTerms)
	}
	sort, err := single(q, "sort")
	if err != nil {
		return o, err
	}
	if o.Sort, err = parseSort(sort); err != nil {
		return o, err
	}
	if o.Limit, err = parseInt(q, "limit", defaultLimit, 1, maxLimit); err != nil {
		return o, err
	}
	if o.Offset, err = parseInt(q, "offset", 0, 0, maxOffset); err != nil {
		return o, err
	}
	fields, err := single(q, "fields")
	if err != nil {
		return o, err
	}
	if fields != "" {
		o.Fields = strings.Split(fields, ",")
		for i, f := range o.Fields {
			if o.Fields[i] = strings.TrimSpace(f); o.Fields[i] == "" {
				return o, BadRequest("fields must be a comma-separated list of field names")
			}
		}
	}
	o.Where, err = parseWhere(q)
	return o, err
}
