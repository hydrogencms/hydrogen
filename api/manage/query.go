package manage

import (
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
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

// parseWhere reads where[field][op]=value (op defaults to eq); repeated keys are ANDed. Boolean groups:
// where[or][i][field][op]=v ORs the groups i (the conditions of one group are ANDed), and where[not][i]... holds
// when none of its groups is fully satisfied; where[not][field][op]=v is the group 0 of not.
func parseWhere(q url.Values) ([]content.Filter, error) {
	var keys []string
	for k := range q {
		if strings.HasPrefix(k, "where[") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out []content.Filter
	groups := map[store.Op]map[int][]content.Filter{}
	n := 0
	for _, k := range keys {
		w, err := splitWhereKey(k)
		if err != nil {
			return nil, err
		}
		for _, v := range q[k] {
			if n++; n > content.MaxConditions {
				return nil, BadRequest("too many filters (at most %d)", content.MaxConditions)
			}
			f, err := whereFilter(k, w, v)
			if err != nil {
				return nil, err
			}
			if w.group == "" {
				out = append(out, f)
				continue
			}
			if groups[w.group] == nil {
				groups[w.group] = map[int][]content.Filter{}
			}
			groups[w.group][w.index] = append(groups[w.group][w.index], f)
		}
	}
	for _, op := range []store.Op{store.OpOr, store.OpNot} {
		g := groups[op]
		if len(g) == 0 {
			continue
		}
		f := content.Filter{Op: op}
		for _, i := range slices.Sorted(maps.Keys(g)) {
			f.Groups = append(f.Groups, g[i])
		}
		out = append(out, f)
	}
	return out, nil
}

func whereFilter(k string, w whereKey, v string) (content.Filter, error) {
	if w.op == store.OpIn {
		if n := strings.Count(v, ",") + 1; n > content.MaxInValues {
			return content.Filter{}, BadRequest("too many values in %s: %d (at most %d)", k, n, content.MaxInValues)
		}
	}
	var val any = v
	if w.op == store.OpExists {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return content.Filter{}, BadRequest("%s needs true or false", k)
		}
		val = b
	}
	return content.Filter{Field: w.field, Op: w.op, Value: val}, nil
}

// whereKey is a parsed filter key; group is store.OpOr or store.OpNot for a condition inside a boolean group.
type whereKey struct {
	group store.Op
	index int
	field string
	op    store.Op
}

const whereUsage = "use where[field][op]=value, where[or][0][field][op]=value or where[not][field][op]=value"

func splitWhereKey(k string) (whereKey, error) {
	var w whereKey
	rest, _ := strings.CutPrefix(k, "where")
	var segs []string
	for rest != "" {
		s, ok := strings.CutPrefix(rest, "[")
		name, after, ok2 := strings.Cut(s, "]")
		if !ok || !ok2 || name == "" || strings.Contains(name, "[") {
			return w, BadRequest("malformed filter %q; %s", k, whereUsage)
		}
		segs = append(segs, name)
		rest = after
	}
	if len(segs) > 0 && (segs[0] == "or" || segs[0] == "not") {
		w.group = store.Op(segs[0])
		segs = segs[1:]
		if len(segs) > 0 {
			if n, err := strconv.Atoi(segs[0]); err == nil {
				if n < 0 || n >= content.MaxConditions {
					return w, BadRequest("group index in %q must be from 0 to %d", k, content.MaxConditions-1)
				}
				w.index = n
				segs = segs[1:]
			} else if w.group == store.OpOr {
				return w, BadRequest("malformed filter %q; where[or] needs a group index: where[or][0][field][op]=value", k)
			}
		}
		if len(segs) == 0 {
			return w, BadRequest("malformed filter %q; %s", k, whereUsage)
		}
	}
	if len(segs) == 0 || len(segs) > 2 || strings.ContainsAny(segs[0], "]") {
		return w, BadRequest("malformed filter %q; %s", k, whereUsage)
	}
	w.field, w.op = segs[0], store.OpEq
	if len(segs) == 2 {
		w.op = store.Op(segs[1])
		if !slices.Contains(ops, w.op) {
			return w, BadRequest("unknown filter operator %q in %q", segs[1], k)
		}
	}
	return w, nil
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
