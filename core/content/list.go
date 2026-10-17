package content

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// List queries items by indexed fields, full-text search and status. Only top-level fields declared with
// index: true can filter or sort; query-string values are parsed by the field's type. An empty Locale means
// the service's default locale.
func (s *Service) List(ctx context.Context, o ListOptions) (*ListResult, error) {
	q, err := s.query(o)
	if err != nil {
		return nil, err
	}
	if err := s.checkListFields(o); err != nil {
		return nil, err
	}
	// Management lists what editors see: drafts included, unless only published items are asked for.
	q.Working = o.Status != store.StatusPublished
	if s.guarded(ctx) {
		if o.Type == "" {
			return nil, badRequest("a content type is required")
		}
		if err := s.o.Access.Prepare(ctx); err != nil {
			return nil, err
		}
		scoped, terms, err := s.o.Access.ListScope(ctx, access.PolicyContentRead, o.Type)
		if err != nil {
			return nil, err
		}
		if scoped && len(terms) == 0 {
			return &ListResult{Items: []Summary{}}, nil
		}
		q.Scoped, q.Scope = scoped, terms
	}
	var res store.QueryResult
	var out *ListResult
	err = s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		res, err = tx.Content().Query(ctx, q)
		if err != nil {
			return err
		}
		out = &ListResult{Items: make([]Summary, len(res.Items)), Total: res.Total}
		for i, it := range res.Items {
			out.Items[i] = summaryOf(it)
			if len(o.Fields) > 0 {
				if out.Items[i].Fields, err = s.listFields(ctx, tx, it.Item, q, o.Fields); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("content: list: %w", err)
	}
	return out, nil
}

// Facet limits.
const (
	MaxFacetFields = 5
	MaxFacetValues = 1000 // values returned per field, most items first
)

// Facets counts the items of a type per value of indexed text fields (a list field counts an item once per
// value), in one grouped query per field over the index rows. Like List it covers only what the principal may
// read: a limited read scope is applied inside the query, so the counts are those of the readable items.
func (s *Service) Facets(ctx context.Context, o FacetOptions) (map[string][]FacetValue, error) {
	if o.Type == "" {
		return nil, badRequest("a content type is required")
	}
	if len(o.Fields) == 0 || len(o.Fields) > MaxFacetFields {
		return nil, badRequest("fields needs 1 to %d field names", MaxFacetFields)
	}
	q, err := s.query(ListOptions{Type: o.Type, Locale: o.Locale, Status: o.Status, Where: o.Where})
	if err != nil {
		return nil, err
	}
	t, err := s.typeOf(o.Type)
	if err != nil {
		return nil, err
	}
	for _, name := range o.Fields {
		if _, col, err := indexedField(t, name); err != nil {
			return nil, err
		} else if col != store.ColText {
			return nil, badRequest("field %q is not a text field", name)
		}
	}
	q.Working = o.Status != store.StatusPublished
	if s.guarded(ctx) {
		if err := s.o.Access.Prepare(ctx); err != nil {
			return nil, err
		}
		scoped, terms, err := s.o.Access.ListScope(ctx, access.PolicyContentRead, o.Type)
		if err != nil {
			return nil, err
		}
		q.Scoped, q.Scope = scoped, terms
	}
	out := make(map[string][]FacetValue, len(o.Fields))
	err = s.o.DB.View(ctx, func(tx store.Tx) error {
		for _, name := range o.Fields {
			counts, err := tx.Content().Facet(ctx, q, name, MaxFacetValues)
			if err != nil {
				return err
			}
			vals := make([]FacetValue, len(counts))
			for i, c := range counts {
				vals[i] = FacetValue{Value: c.Value, Count: c.Count}
			}
			out[name] = vals
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("content: facets: %w", err)
	}
	return out, nil
}

// MaxListFields is the most fields one list asks for.
const MaxListFields = 10

func (s *Service) checkListFields(o ListOptions) error {
	if len(o.Fields) == 0 {
		return nil
	}
	if len(o.Fields) > MaxListFields {
		return badRequest("too many fields: %d (at most %d)", len(o.Fields), MaxListFields)
	}
	if o.Type == "" {
		return badRequest("fields need a content type")
	}
	t, err := s.typeOf(o.Type)
	if err != nil {
		return err
	}
	for _, name := range o.Fields {
		if _, ok := t.Field(name); !ok {
			return badRequest("unknown field %q of type %q", name, o.Type)
		}
	}
	return nil
}

// listFields reads the named fields of an item's working (or, for published-only lists, published) document in
// the query's locale; fields the locale lacks fall back to the main locale.
func (s *Service) listFields(ctx context.Context, tx store.Tx, it store.Item, q store.Query, names []string) (map[string]any, error) {
	st, err := s.newState(it)
	if err != nil {
		return nil, err
	}
	if err := st.load(ctx, tx.Content()); err != nil {
		return nil, err
	}
	parts := func(locale string) (shared, loc map[string]any, err error) {
		if q.Working {
			shared, loc, _, err = st.workingParts(locale)
			return
		}
		shared, loc, _, err = st.publishedParts(locale)
		return
	}
	shared, loc, err := parts(q.Locale)
	if err != nil {
		return nil, err
	}
	doc := mergeParts(shared, loc)
	var main map[string]any
	out := make(map[string]any, len(names))
	for _, name := range names {
		v, ok := doc[name]
		if _, inLoc := loc[name]; !inLoc && q.Locale != it.MainLocale {
			if main == nil {
				_, ml, err := parts(it.MainLocale)
				if err != nil {
					return nil, err
				}
				main = ml
			}
			if mv, found := main[name]; found && !ok {
				v, ok = mv, true
			}
		}
		if ok {
			out[name] = v
		}
	}
	return out, nil
}

func summaryOf(it store.ItemSummary) Summary {
	locales := it.Locales
	if locales == nil {
		locales = []string{}
	}
	return Summary{
		ID: it.ID, Type: it.Type, Key: it.Key, Name: it.Name,
		Status: statusOf(it.PublishedVersion, len(it.Drafts)), Version: it.PublishedVersion,
		Locales: locales, DraftParts: partNames(it.Drafts), CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

const (
	defaultLimit = 50
	maxLimit     = 500
)

// Query limits; List rejects anything beyond them with ErrBadRequest.
const (
	MaxConditions  = 32   // Where entries
	MaxInValues    = 1000 // values in one "in" list
	MaxSearchTerms = 16   // words in Search
	MaxSortKeys    = 4
)

func (s *Service) query(o ListOptions) (store.Query, error) {
	if err := checkLimits(o); err != nil {
		return store.Query{}, err
	}
	q := store.Query{Type: o.Type, Locale: o.Locale, Status: o.Status, Search: o.Search, Offset: max(o.Offset, 0)}
	if q.Locale == "" {
		q.Locale = s.o.DefaultLocale
	} else if err := validateLocale(q.Locale); err != nil {
		return q, err
	}
	switch {
	case o.Limit <= 0:
		q.Limit = defaultLimit
	default:
		q.Limit = min(o.Limit, maxLimit)
	}
	var t *schema.Type
	if o.Type != "" {
		var err error
		if t, err = s.typeOf(o.Type); err != nil {
			return q, err
		}
	}
	for _, f := range o.Where {
		cond, err := condOf(t, f)
		if err != nil {
			return q, err
		}
		q.Where = append(q.Where, cond)
	}
	for _, sp := range o.Sort {
		sort, err := sortOf(t, sp)
		if err != nil {
			return q, err
		}
		q.Sort = append(q.Sort, sort)
	}
	return q, nil
}

func checkLimits(o ListOptions) error {
	if len(o.Where) > MaxConditions {
		return badRequest("too many filters: %d (at most %d)", len(o.Where), MaxConditions)
	}
	if len(o.Sort) > MaxSortKeys {
		return badRequest("too many sort keys: %d (at most %d)", len(o.Sort), MaxSortKeys)
	}
	if len(o.Search) > 0 {
		if n := len(storeutil.SearchTerms(o.Search)); n > MaxSearchTerms {
			return badRequest("too many search terms: %d (at most %d)", n, MaxSearchTerms)
		}
	}
	return nil
}

// indexedField finds the indexed top-level field a filter or sort names.
func indexedField(t *schema.Type, name string) (*schema.Field, store.Column, error) {
	if t == nil {
		return nil, "", badRequest("field %q needs a type", name)
	}
	f, ok := t.Field(name)
	if !ok {
		return nil, "", badRequest("unknown field %q of type %q", name, t.Name)
	}
	if !f.Index {
		return nil, "", badRequest("field %q of type %q is not indexed (declare index: true)", name, t.Name)
	}
	elem := f
	if f.Shape.Base == schema.BaseList && f.Of != nil {
		elem = f.Of
	}
	col, ok := columnOf(elem.Shape)
	if !ok {
		return nil, "", badRequest("field %q cannot be filtered", name)
	}
	return f, col, nil
}

func columnOf(sh schema.Shape) (store.Column, bool) {
	switch sh.Base {
	case schema.BaseFile:
		return store.ColText, true
	case schema.BaseString:
		if sh.Format == "date" || sh.Format == "datetime" {
			return store.ColTime, true
		}
		return store.ColText, true
	case schema.BaseNumber:
		return store.ColNum, true
	case schema.BaseBoolean:
		return store.ColBool, true
	case schema.BaseRef:
		return store.ColRef, true
	}
	return "", false
}

func sortOf(t *schema.Type, sp SortSpec) (store.Sort, error) {
	switch sp.Field {
	case "name", "createdAt", "updatedAt", "key":
		return store.Sort{Field: sp.Field, Desc: sp.Desc}, nil
	}
	_, col, err := indexedField(t, sp.Field)
	if err != nil {
		return store.Sort{}, err
	}
	return store.Sort{Field: sp.Field, Column: col, Desc: sp.Desc}, nil
}

func condOf(t *schema.Type, f Filter) (store.Cond, error) {
	field, col, err := indexedField(t, f.Field)
	if err != nil {
		return store.Cond{}, err
	}
	isList := field.Shape.Base == schema.BaseList
	switch f.Op {
	case store.OpContains:
		if !isList {
			return store.Cond{}, badRequest("contains needs a list field, %q is not one", f.Field)
		}
	case store.OpPrefix, store.OpUnder:
		if col != store.ColText {
			return store.Cond{}, badRequest("%s needs a text field, %q is not one", f.Op, f.Field)
		}
	case store.OpLt, store.OpLte, store.OpGt, store.OpGte:
		if col == store.ColBool || col == store.ColRef {
			return store.Cond{}, badRequest("%s does not apply to field %q", f.Op, f.Field)
		}
	case store.OpEq, store.OpNe, store.OpIn, store.OpExists:
	default:
		return store.Cond{}, badRequest("unknown operator %q", f.Op)
	}
	v, err := condValue(field, col, f)
	if err != nil {
		return store.Cond{}, err
	}
	return store.Cond{Field: f.Field, Column: col, Op: f.Op, Value: v}, nil
}

func condValue(field *schema.Field, col store.Column, f Filter) (any, error) {
	switch f.Op {
	case store.OpExists:
		return parseBool(f.Value)
	case store.OpIn:
		return parseList(field, col, f.Value)
	}
	return parseValue(field, col, f.Value)
}

func parseBool(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		b, err := strconv.ParseBool(x)
		if err != nil {
			return false, badRequest("%q is not a boolean", x)
		}
		return b, nil
	}
	return false, badRequest("%v is not a boolean", v)
}

func parseList(field *schema.Field, col store.Column, v any) ([]any, error) {
	var raw []any
	switch x := v.(type) {
	case string:
		for p := range strings.SplitSeq(x, ",") {
			raw = append(raw, strings.TrimSpace(p))
		}
	case []string:
		for _, p := range x {
			raw = append(raw, p)
		}
	case []any:
		raw = x
	default:
		raw = []any{v}
	}
	if len(raw) > MaxInValues {
		return nil, badRequest("too many values in an in list: %d (at most %d)", len(raw), MaxInValues)
	}
	out := make([]any, len(raw))
	for i, r := range raw {
		pv, err := parseValue(field, col, r)
		if err != nil {
			return nil, err
		}
		out[i] = pv
	}
	return out, nil
}

// parseValue converts a typed or query-string value to the Go type of the index column.
func parseValue(field *schema.Field, col store.Column, v any) (any, error) {
	var (
		out any
		err error
	)
	switch col {
	case store.ColText:
		out, err = textValue(v)
	case store.ColNum:
		out, err = numValue(v)
	case store.ColTime:
		out, err = timeValue(v)
	case store.ColBool:
		out, err = parseBool(v)
	case store.ColRef:
		out, err = refValue(v)
	}
	if err != nil {
		return nil, fmt.Errorf("%w (field %q)", err, field.Name)
	}
	return out, nil
}

func textValue(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	return "", badRequest("%v is not text", v)
}

func numValue(v any) (float64, error) {
	if s, ok := v.(string); ok {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, badRequest("%q is not a number", s)
		}
		return f, nil
	}
	if f, ok := numberValue(v); ok {
		return f, nil
	}
	return 0, badRequest("%v is not a number", v)
}

func timeValue(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC(), nil
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t.UTC(), nil
			}
		}
		return time.Time{}, badRequest("%q is not a date or RFC 3339 time", x)
	}
	return time.Time{}, badRequest("%v is not a time", v)
}

func refValue(v any) (ids.UUID, error) {
	switch x := v.(type) {
	case ids.UUID:
		return x, nil
	case Ref:
		return refValue(string(x))
	case string:
		id, err := ids.Parse(x)
		if err != nil {
			return ids.Nil, badRequest("%q is not an item id", x)
		}
		return id, nil
	}
	return ids.Nil, badRequest("%v is not an item id", v)
}
