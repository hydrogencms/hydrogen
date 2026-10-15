package content

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

// project rebuilds the reference, index and search projections of an item's current published version.
func (s *Service) project(ctx context.Context, c store.ContentStore, st *itemState, outs []*localeOut) error {
	id := st.item.ID
	refs := projectRefs(id, outs)
	if err := c.ReplaceRefs(ctx, id, refs); err != nil {
		return fmt.Errorf("content: project refs of %s: %w", id, err)
	}
	if err := c.ReplaceIndex(ctx, id, store.ProjectionPublished, s.projectIndex(st, outs)); err != nil {
		return fmt.Errorf("content: project index of %s: %w", id, err)
	}
	docs := make([]store.SearchDoc, len(outs))
	for i, o := range outs {
		text := capSearchText(strings.TrimSpace(o.name + " " + s.o.Schema.SearchText(st.typ, o.doc)))
		docs[i] = store.SearchDoc{Locale: o.code, Text: text}
	}
	if err := c.ReplaceSearch(ctx, id, store.ProjectionPublished, docs); err != nil {
		return fmt.Errorf("content: project search of %s: %w", id, err)
	}
	return nil
}

// projectRefs lists the references of every locale, once per (path, target, kind).
func projectRefs(from ids.UUID, outs []*localeOut) []store.Ref {
	type key struct {
		path, to, kind string
	}
	seen := map[key]bool{}
	var refs []store.Ref
	for _, o := range outs {
		for _, r := range o.refs {
			k := key{r.Path, r.To, r.Kind}
			if seen[k] {
				continue
			}
			seen[k] = true
			to, err := ids.Parse(r.To)
			if err != nil {
				continue // not an item id: validation reports ref fields; links to other things are not references
			}
			refs = append(refs, store.Ref{From: from, FieldPath: r.Path, To: to, Kind: r.Kind, OnDelete: r.OnDelete})
		}
	}
	return refs
}

// projectIndex emits shared fields once under locale "" (from the main locale's document) and translatable
// fields per locale.
func (s *Service) projectIndex(st *itemState, outs []*localeOut) []store.IndexRow {
	var rows []store.IndexRow
	primary := outs[0]
	for _, o := range outs {
		if o.code == st.item.MainLocale {
			primary = o
		}
	}
	for _, o := range outs {
		for _, iv := range s.o.Schema.IndexValues(st.typ, o.doc) {
			f, ok := st.typ.Field(iv.Field)
			if !ok {
				continue
			}
			switch {
			case f.Translatable:
				rows = append(rows, indexRow(o.code, iv))
			case o == primary:
				rows = append(rows, indexRow("", iv))
			}
		}
	}
	return rows
}

func indexRow(locale string, iv schema.IndexValue) store.IndexRow {
	row := store.IndexRow{Locale: locale, Field: iv.Field, Text: iv.Text, Num: iv.Num, Bool: iv.Bool}
	if iv.Time != nil {
		t := time.UnixMilli(*iv.Time).UTC()
		row.Time = &t
	}
	if iv.Ref != "" {
		if id, err := ids.Parse(iv.Ref); err == nil {
			row.Ref = &id
		}
	}
	return row
}

// checkRefs verifies that every reference field points at an existing item of an allowed type. One batched
// read covers all locales; problems are added to the locale's errors with code ref_target.
func (s *Service) checkRefs(ctx context.Context, c store.ContentStore, t *schema.Type, outs []*localeOut) error {
	targets := map[ids.UUID]bool{}
	for _, o := range outs {
		for _, r := range o.refs {
			if id, err := ids.Parse(r.To); err == nil && r.Kind == "field" {
				targets[id] = true
			}
		}
	}
	found := map[ids.UUID]string{}
	if len(targets) > 0 {
		list := make([]ids.UUID, 0, len(targets))
		for id := range targets {
			list = append(list, id)
		}
		slices.SortFunc(list, func(a, b ids.UUID) int { return slices.Compare(a[:], b[:]) })
		items, err := c.GetItems(ctx, list)
		if err != nil {
			return fmt.Errorf("content: check references: %w", err)
		}
		for _, it := range items {
			found[it.ID] = it.Type
		}
	}
	for _, o := range outs {
		for _, r := range o.refs {
			if r.Kind != "field" {
				continue
			}
			if msg := refProblem(t, r, found); msg != "" {
				o.errs = append(o.errs, schema.FieldError{Path: r.Path, Code: "ref_target", Message: msg})
			}
		}
	}
	return nil
}

func refProblem(t *schema.Type, r schema.RefValue, found map[ids.UUID]string) string {
	id, err := ids.Parse(r.To)
	if err != nil {
		return fmt.Sprintf("%q is not an item id", r.To)
	}
	typ, ok := found[id]
	if !ok {
		return fmt.Sprintf("item %s does not exist", r.To)
	}
	if to := refTo(t, r.Path); len(to) > 0 && !slices.Contains(to, typ) {
		return fmt.Sprintf("item %s is a %s; allowed: %s", r.To, typ, strings.Join(to, ", "))
	}
	return ""
}

// refTo finds the allowed target types of the ref field at a document path such as "tags[2]" or "seo.card".
func refTo(t *schema.Type, path string) []string {
	var f *schema.Field
	fields := t.Fields
	for path != "" {
		switch path[0] {
		case '.':
			path = path[1:]
		case '[':
			end := strings.IndexByte(path, ']')
			if end < 0 || f == nil || f.Of == nil {
				return nil
			}
			if _, err := strconv.Atoi(path[1:end]); err != nil {
				return nil
			}
			f, fields, path = f.Of, f.Of.Fields, path[end+1:]
		default:
			end := strings.IndexAny(path, ".[")
			if end < 0 {
				end = len(path)
			}
			idx := slices.IndexFunc(fields, func(x *schema.Field) bool { return x.Name == path[:end] })
			if idx < 0 {
				return nil
			}
			f = fields[idx]
			fields, path = f.Fields, path[end:]
		}
	}
	if f == nil {
		return nil
	}
	return f.Shape.To
}

// maxSearchRunes keeps a document's search text under Postgres' 1 MB tsvector limit; it equals the
// adapters' own bound (storeutil.MaxSearchChars).
const maxSearchRunes = storeutil.MaxSearchChars

// capSearchText truncates text to maxSearchRunes at a word boundary.
func capSearchText(text string) string {
	if utf8.RuneCountInString(text) <= maxSearchRunes {
		return text
	}
	cut := 0
	for i := range text {
		if maxSearchRunes == cut {
			text = text[:i]
			break
		}
		cut++
	}
	if i := strings.LastIndexAny(text, " \t\n"); i > 0 {
		text = text[:i]
	}
	return text
}
