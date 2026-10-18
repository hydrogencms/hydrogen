package search

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// facetLimit bounds the values returned per facet.
const facetLimit = 50

// Option configures the database engine.
type Option func(*database)

// WithSchema gives the engine the schema, which it needs to filter, sort and facet on indexed fields (without
// it only filters whose values show their kind work).
func WithSchema(sc *schema.Schema) Option { return func(d *database) { d.sc = sc } }

// Database is the default engine: the adapter's full-text search on the published projection.
func Database(db store.DB, opts ...Option) Engine {
	d := &database{db: db}
	for _, o := range opts {
		o(d)
	}
	return d
}

type database struct {
	db store.DB
	sc *schema.Schema
}

func (d *database) Name() string { return "database" }

// column describes how an indexed field is stored.
type column struct {
	col  store.Column
	list bool
}

func colOf(kind string) store.Column {
	switch kind {
	case KindNum:
		return store.ColNum
	case KindTime:
		return store.ColTime
	case KindBool:
		return store.ColBool
	case KindRef:
		return store.ColRef
	}
	return store.ColText
}

// field finds an indexed field of the requested types.
func (d *database) field(types []string, name string) (column, bool) {
	if d.sc == nil {
		return column{}, false
	}
	for _, tn := range types {
		t, ok := d.sc.Type(tn)
		if !ok || !t.Searchable() {
			continue
		}
		if f, ok := t.Field(name); ok {
			if fs, ok := indexedField(f); ok {
				return column{colOf(fs.Kind), fs.List}, true
			}
		}
	}
	return column{}, false
}

func (d *database) Search(ctx context.Context, r Request) (Result, error) {
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	if d.sc != nil {
		for _, tn := range r.Types {
			if t, ok := d.sc.Type(tn); !ok || !t.Searchable() {
				return Result{}, invalid("type %q is not searchable", tn)
			}
		}
	}
	q, err := d.query(r)
	if err != nil {
		return Result{}, err
	}
	text := strings.TrimSpace(r.Text)
	q.Search, q.SearchFallback, q.Highlight = text, true, r.Highlight && text != ""
	res, err := d.db.Reader().Query(ctx, q)
	if err != nil {
		return Result{}, fmt.Errorf("search: %w", err)
	}
	out := Result{Total: res.Total, Hits: make([]Hit, len(res.Items))}
	terms := storeutil.SearchTerms(text)
	for i, it := range res.Items {
		h := Hit{Item: it.ID, Type: it.Type, Locale: r.Locale}
		if len(r.Sort) == 0 {
			h.Score = it.Searched
		}
		if q.Highlight {
			h.Highlights = map[string]string{}
			if marked := MarkTerms(it.Name, terms); marked != it.Name {
				h.Highlights["name"] = Marked(marked)
			}
			if it.Snippet != "" {
				h.Highlights["text"] = Marked(it.Snippet)
			}
		}
		out.Hits[i] = h
	}
	if err := d.fallbackLocales(ctx, r.Locale, res.Items, out.Hits); err != nil {
		return Result{}, err
	}
	if len(r.Facets) > 0 {
		if out.Facets, err = d.facets(ctx, r, q); err != nil {
			return Result{}, err
		}
	}
	return out, nil
}

// fallbackLocales sets the main locale on the hits of always-available items that have no document in the
// requested locale. The item's stored locales cannot tell (a locale that inherits from another has documents
// without being stored), so the snapshots decide.
func (d *database) fallbackLocales(ctx context.Context, locale string, items []store.ItemSummary, hits []Hit) error {
	var maybe []ids.UUID
	for _, it := range items {
		if it.AlwaysAvailable && it.MainLocale != locale && !slices.Contains(it.Locales, locale) {
			maybe = append(maybe, it.ID)
		}
	}
	if len(maybe) == 0 {
		return nil
	}
	snaps, err := d.db.Reader().Snapshots(ctx, maybe, []string{locale})
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	has := make(map[ids.UUID]bool, len(snaps))
	for _, sn := range snaps {
		has[sn.ItemID] = true
	}
	for i, it := range items {
		if slices.Contains(maybe, it.ID) && !has[it.ID] {
			hits[i].Locale = it.MainLocale
		}
	}
	return nil
}

// query maps the request, without its text, to a store query on the published projection.
func (d *database) query(r Request) (store.Query, error) {
	q := store.Query{Types: slices.Clone(r.Types), Locale: r.Locale, Status: store.StatusPublished,
		Available: []string{r.Locale}, Limit: r.Limit, Offset: r.Offset}
	for _, f := range r.Where {
		c, err := d.cond(r.Types, f)
		if err != nil {
			return q, err
		}
		q.Where = append(q.Where, c)
	}
	for _, s := range r.Sort {
		if s.Field == PublishedAt {
			q.Sort = append(q.Sort, store.Sort{Field: PublishedAt, Desc: s.Desc})
			continue
		}
		c, ok := d.field(r.Types, s.Field)
		if !ok || c.list {
			return q, invalid("cannot sort by %q: it is not an indexed scalar field of the searched types", s.Field)
		}
		q.Sort = append(q.Sort, store.Sort{Field: s.Field, Column: c.col, Desc: s.Desc})
	}
	return q, nil
}

func (d *database) cond(types []string, f Filter) (store.Cond, error) {
	c, ok := d.field(types, f.Field)
	if !ok && d.sc == nil {
		c, ok = inferred(f)
	}
	if !ok {
		return store.Cond{}, invalid("cannot filter by %q: it is not an indexed field of the searched types", f.Field)
	}
	switch f.Op {
	case store.OpContains:
		if !c.list {
			return store.Cond{}, invalid("contains needs a list field, %q is not one", f.Field)
		}
	case store.OpPrefix:
		if c.col != store.ColText {
			return store.Cond{}, invalid("prefix needs a text field, %q is not one", f.Field)
		}
	case store.OpLt, store.OpLte, store.OpGt, store.OpGte:
		if c.col == store.ColBool || c.col == store.ColRef {
			return store.Cond{}, invalid("%s does not apply to field %q", f.Op, f.Field)
		}
	case store.OpEq, store.OpNe, store.OpIn, store.OpExists:
	default:
		return store.Cond{}, invalid("unknown operator %q", f.Op)
	}
	var v any
	var err error
	switch f.Op {
	case store.OpExists:
		b, isBool := f.Value.(bool)
		if !isBool {
			return store.Cond{}, invalid("exists on %q needs true or false", f.Field)
		}
		v = b
	case store.OpIn:
		v, err = storeutil.List(c.col, f.Value)
	default:
		v, err = storeutil.Scalar(c.col, f.Value)
	}
	if err != nil {
		return store.Cond{}, invalid("field %q: %v", f.Field, err)
	}
	return store.Cond{Field: f.Field, Column: c.col, Op: f.Op, Value: v}, nil
}

// inferred guesses the column from the filter's value, for an engine without a schema.
func inferred(f Filter) (column, bool) {
	v := f.Value
	if f.Op == store.OpExists {
		return column{col: store.ColText}, true
	}
	if l, ok := v.([]any); ok && f.Op == store.OpIn && len(l) > 0 {
		v = l[0]
	}
	switch v.(type) {
	case string, []string:
		return column{col: store.ColText, list: f.Op == store.OpContains}, true
	case float64, float32, int, int32, int64, []float64, []int:
		return column{col: store.ColNum, list: f.Op == store.OpContains}, true
	case time.Time, []time.Time:
		return column{col: store.ColTime, list: f.Op == store.OpContains}, true
	case bool, []bool:
		return column{col: store.ColBool, list: f.Op == store.OpContains}, true
	case ids.UUID, []ids.UUID:
		return column{col: store.ColRef, list: f.Op == store.OpContains}, true
	}
	return column{}, false
}

// facets counts, per requested field, the hits that have each of its values (the store counts them among the
// items matching the text and the filters).
func (d *database) facets(ctx context.Context, r Request, base store.Query) (map[string][]Facet, error) {
	out := make(map[string][]Facet, len(r.Facets))
	for _, name := range r.Facets {
		c, ok := d.field(r.Types, name)
		if !ok || c.col != store.ColText {
			return nil, invalid("cannot facet by %q: it is not an indexed text field of the searched types", name)
		}
		q := base
		q.Sort, q.Highlight, q.Limit, q.Offset = nil, false, 1, 0
		var values []store.FacetCount
		err := d.db.View(ctx, func(tx store.Tx) error {
			var err error
			values, err = tx.Content().Facet(ctx, q, name, facetLimit)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("search: facet %q: %w", name, err)
		}
		list := make([]Facet, len(values))
		for i, v := range values {
			list[i] = Facet{Value: v.Value, Count: v.Count}
		}
		out[name] = list
	}
	return out, nil
}
