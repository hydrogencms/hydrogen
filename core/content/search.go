package content

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

// SearchOptions asks for the best matches of a text across several collection types. Types is required;
// Locale defaults to the service's default locale; Limit is 1..MaxSearchLimit (default 20).
type SearchOptions struct {
	Types  []string
	Locale string
	Text   string
	Limit  int
}

// MaxSearchLimit is the most matches one Search returns.
const MaxSearchLimit = 50

// SearchResult holds the matches best first and, per item, a fragment of the matched text as HTML: escaped,
// with each match in <mark>.
type SearchResult struct {
	Items    []Summary
	Snippets map[ids.UUID]string
}

// Search runs the working-projection full-text search of every given type with the principal's read scope of
// that type (a type the principal cannot read contributes nothing) and merges the matches by rank, ties by
// the most recent update first.
func (s *Service) Search(ctx context.Context, o SearchOptions) (*SearchResult, error) {
	if strings.TrimSpace(o.Text) == "" {
		return nil, badRequest("a search text is required")
	}
	if len(o.Types) == 0 {
		return nil, badRequest("at least one content type is required")
	}
	if o.Limit <= 0 {
		o.Limit = 20
	}
	o.Limit = min(o.Limit, MaxSearchLimit)
	guarded := s.guarded(ctx)
	if guarded {
		if err := s.o.Access.Prepare(ctx); err != nil {
			return nil, err
		}
	}
	var queries []store.Query
	for _, name := range o.Types {
		t, err := s.typeOf(name)
		if err != nil {
			return nil, err
		}
		if t.Singleton {
			return nil, badRequest("type %q is a singleton", name)
		}
		q, err := s.query(ListOptions{Type: name, Locale: o.Locale, Status: store.StatusAny, Search: o.Text, Limit: o.Limit})
		if err != nil {
			return nil, err
		}
		q.Working, q.Highlight, q.NoTotal = true, true, true
		if guarded {
			scoped, terms, err := s.o.Access.ListScope(ctx, access.PolicyContentRead, name)
			if err != nil {
				return nil, err
			}
			if scoped && len(terms) == 0 {
				continue
			}
			q.Scoped, q.Scope = scoped, terms
		}
		queries = append(queries, q)
	}
	var found []store.ItemSummary
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		for _, q := range queries {
			res, err := tx.Content().Query(ctx, q)
			if err != nil {
				return err
			}
			found = append(found, res.Items...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("content: search: %w", err)
	}
	slices.SortStableFunc(found, func(a, b store.ItemSummary) int {
		switch {
		case a.Searched > b.Searched:
			return -1
		case a.Searched < b.Searched:
			return 1
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	found = found[:min(len(found), o.Limit)]
	out := &SearchResult{Items: make([]Summary, len(found)), Snippets: map[ids.UUID]string{}}
	for i, it := range found {
		out.Items[i] = summaryOf(it)
		if it.Snippet != "" {
			out.Snippets[it.ID] = MarkSnippet(it.Snippet)
		}
	}
	return out, nil
}

// MarkSnippet turns a store snippet (raw text with matches between \x02 and \x03) into HTML: the text is
// escaped and each match wrapped in <mark>.
func MarkSnippet(raw string) string {
	r := strings.NewReplacer("\x02", "<mark>", "\x03", "</mark>")
	return r.Replace(html.EscapeString(raw))
}

// SearchPlan is the filters, sort keys and facet fields of a search over several types, checked against their
// indexed fields.
type SearchPlan struct {
	Where  []store.Cond
	Sort   []store.Sort // the field "publishedAt" has no Column
	Facets []string
}

// PublishedAtField is the sort key of a search that orders by publication time.
const PublishedAtField = "publishedAt"

// PlanSearch checks where, sort and facets against the indexed fields of types. A field must be indexed in at
// least one of the types (a type without it simply has no match for a filter on it) and mean the same kind in
// every type that has it. Sort fields are indexed scalar fields or publishedAt; facets are indexed text fields.
func (s *Service) PlanSearch(types []string, where []Filter, sort []SortSpec, facets []string) (SearchPlan, error) {
	var p SearchPlan
	if len(where) > MaxConditions {
		return p, badRequest("too many filters: %d (at most %d)", len(where), MaxConditions)
	}
	if len(sort) > MaxSortKeys {
		return p, badRequest("too many sort keys: %d (at most %d)", len(sort), MaxSortKeys)
	}
	if len(facets) > MaxFacetFields {
		return p, badRequest("too many facets: %d (at most %d)", len(facets), MaxFacetFields)
	}
	ts := make([]*schema.Type, 0, len(types))
	for _, name := range types {
		t, err := s.typeOf(name)
		if err != nil {
			return p, err
		}
		ts = append(ts, t)
	}
	for _, f := range where {
		if f.Op == store.OpUnder {
			return p, badRequest("unknown operator %q", f.Op)
		}
		var cond *store.Cond
		var first error
		for _, t := range ts {
			c, err := condOf(t, f)
			if err != nil {
				first = cmpErr(first, err)
				continue
			}
			if cond != nil && cond.Column != c.Column {
				return p, badRequest("field %q has different types in the searched types", f.Field)
			}
			if cond == nil {
				cond = &c
			}
		}
		if cond == nil {
			return p, first
		}
		p.Where = append(p.Where, *cond)
	}
	for _, sp := range sort {
		if sp.Field == PublishedAtField {
			p.Sort = append(p.Sort, store.Sort{Field: sp.Field, Desc: sp.Desc})
			continue
		}
		var got *store.Sort
		var first error
		for _, t := range ts {
			f, col, err := indexedField(t, sp.Field)
			if err == nil && f.Shape.Base == schema.BaseList {
				err = badRequest("cannot sort by list field %q", sp.Field)
			}
			if err != nil {
				first = cmpErr(first, err)
				continue
			}
			if got != nil && got.Column != col {
				return p, badRequest("field %q has different types in the searched types", sp.Field)
			}
			if got == nil {
				got = &store.Sort{Field: sp.Field, Column: col, Desc: sp.Desc}
			}
		}
		if got == nil {
			return p, first
		}
		p.Sort = append(p.Sort, *got)
	}
	for _, name := range facets {
		var first error
		ok := false
		for _, t := range ts {
			_, col, err := indexedField(t, name)
			if err == nil && col != store.ColText {
				err = badRequest("field %q is not a text field", name)
			}
			if err != nil {
				first = cmpErr(first, err)
				continue
			}
			ok = true
		}
		if !ok {
			return p, first
		}
		p.Facets = append(p.Facets, name)
	}
	return p, nil
}

func cmpErr(first, err error) error {
	if first == nil {
		return err
	}
	return first
}
