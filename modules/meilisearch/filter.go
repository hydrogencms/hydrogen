package meilisearch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

var fieldName = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", search.ErrInvalid, fmt.Sprintf(format, args...))
}

// quote writes a string as a Meilisearch filter literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// target is a request field resolved to an attribute.
type target struct {
	attr string
	col  store.Column
	list bool
	// sortable is meaningful for sorting only.
	sortable bool
}

func colOf(kind string) store.Column {
	switch kind {
	case search.KindNum:
		return store.ColNum
	case search.KindTime:
		return store.ColTime
	case search.KindBool:
		return store.ColBool
	case search.KindRef:
		return store.ColRef
	}
	return store.ColText
}

// resolve finds the attribute of a request field. known is false when no settings are loaded yet, and the
// kind is then guessed from the value (guess).
func (ix *Index) resolve(name string, guess func() store.Column) (target, error) {
	if name == search.PublishedAt {
		return target{attr: attrPublishedAt, col: store.ColTime, sortable: true}, nil
	}
	if !fieldName.MatchString(name) {
		return target{}, invalid("%q is not an indexed field", name)
	}
	ix.mu.RLock()
	fields, loaded := ix.fields, ix.fields != nil
	ix.mu.RUnlock()
	if loaded {
		f, ok := fields[name]
		if !ok {
			return target{}, invalid("%q is not an indexed field", name)
		}
		return target{attr: fieldPrefix + name, col: colOf(f.Kind), list: f.List, sortable: f.Sortable}, nil
	}
	return target{attr: fieldPrefix + name, col: guess(), sortable: true}, nil
}

func guessCol(v any) store.Column {
	switch x := v.(type) {
	case []any:
		if len(x) > 0 {
			return guessCol(x[0])
		}
	case []string:
		return store.ColText
	case []float64, []int, []int64:
		return store.ColNum
	case []time.Time:
		return store.ColTime
	case []bool:
		return store.ColBool
	}
	switch v.(type) {
	case float64, float32, int, int32, int64:
		return store.ColNum
	case time.Time:
		return store.ColTime
	case bool:
		return store.ColBool
	}
	return store.ColText
}

// literal writes a value (already converted by storeutil) as a filter literal.
func literal(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case time.Time:
		return strconv.FormatInt(x.Unix(), 10)
	case bool:
		return strconv.FormatBool(x)
	case fmt.Stringer:
		return quote(x.String())
	}
	return quote(fmt.Sprint(v))
}

// filterExpr translates one request filter.
func (ix *Index) filterExpr(f search.Filter) (string, error) {
	t, err := ix.resolve(f.Field, func() store.Column { return guessCol(f.Value) })
	if err != nil {
		return "", err
	}
	switch f.Op {
	case store.OpExists:
		b, ok := f.Value.(bool)
		if !ok {
			return "", invalid("exists on %q needs true or false", f.Field)
		}
		if b {
			return t.attr + " EXISTS", nil
		}
		return "NOT " + t.attr + " EXISTS", nil
	case store.OpIn:
		items, err := storeutil.List(t.col, f.Value)
		if err != nil {
			return "", invalid("field %q: %v", f.Field, err)
		}
		lits := make([]string, len(items))
		for i, it := range items {
			lits[i] = literal(it)
		}
		return t.attr + " IN [" + strings.Join(lits, ", ") + "]", nil
	case store.OpEq, store.OpNe, store.OpContains, store.OpLt, store.OpLte, store.OpGt, store.OpGte:
	case store.OpPrefix:
		return "", invalid("prefix filters are not supported by Meilisearch")
	default:
		return "", invalid("unknown operator %q", f.Op)
	}
	if f.Op == store.OpContains && !t.list {
		return "", invalid("contains needs a list field, %q is not one", f.Field)
	}
	if (f.Op == store.OpLt || f.Op == store.OpLte || f.Op == store.OpGt || f.Op == store.OpGte) && t.col != store.ColNum && t.col != store.ColTime {
		return "", invalid("%s applies to numbers and times only, not field %q", f.Op, f.Field)
	}
	v, err := storeutil.Scalar(t.col, f.Value)
	if err != nil {
		return "", invalid("field %q: %v", f.Field, err)
	}
	op := map[store.Op]string{store.OpEq: "=", store.OpContains: "=", store.OpNe: "!=", store.OpLt: "<", store.OpLte: "<=",
		store.OpGt: ">", store.OpGte: ">="}[f.Op]
	return t.attr + " " + op + " " + literal(v), nil
}

// filter builds the whole filter of a request: the types, the availability of the locale, and the conditions.
func (ix *Index) filter(r search.Request) (string, error) {
	types := make([]string, len(r.Types))
	for i, t := range r.Types {
		types[i] = quote(t)
	}
	loc := quote(r.Locale)
	parts := []string{
		attrType + " IN [" + strings.Join(types, ", ") + "]",
		"(" + attrLocale + " = " + loc + " OR (" + attrAlways + " = true AND " + attrMain + " = true AND NOT " +
			attrLocales + " = " + loc + "))",
	}
	for _, f := range r.Where {
		e, err := ix.filterExpr(f)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, " AND "), nil
}

// sortExprs translates the request's sort.
func (ix *Index) sortExprs(sorts []search.Sort) ([]string, error) {
	var out []string
	for _, s := range sorts {
		t, err := ix.resolve(s.Field, func() store.Column { return store.ColText })
		if err != nil {
			return nil, err
		}
		if t.list || !t.sortable {
			return nil, invalid("cannot sort by %q: it is not an indexed scalar field", s.Field)
		}
		dir := "asc"
		if s.Desc {
			dir = "desc"
		}
		out = append(out, t.attr+":"+dir)
	}
	return out, nil
}
