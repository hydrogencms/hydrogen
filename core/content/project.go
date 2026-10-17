package content

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// rowset is the index and search rows of one projection of an item.
type rowset struct {
	index  []store.IndexRow
	search []store.SearchDoc
}

func (s *Service) rowsOf(st *itemState, outs []*localeOut) *rowset {
	return &rowset{index: s.projectIndex(st, outs), search: s.searchDocs(st, outs)}
}

// rememberRows records the rows the item's projections hold now, from the state loaded before an operation
// changes it, so the operation can skip rewriting a projection whose rows come out the same. The rows are
// derived from the documents in memory, which the stored rows follow (EnsureWorkingProjections builds them for
// databases that predate them); an item that fails to derive keeps no memory and is written in full.
func (s *Service) rememberRows(st *itemState) {
	st.prev = nil
	w, err := s.workingOuts(st)
	if err != nil {
		return
	}
	prev := &prevRows{working: s.rowsOf(st, w)}
	if st.pub != nil {
		p, err := s.publishedOuts(st)
		if err != nil {
			return
		}
		prev.published = s.rowsOf(st, p)
	}
	st.prev = prev
}

// publishedOuts lists the locales of the published version, overlay locales resolved, with the overlay locales
// that inherit from them.
func (s *Service) publishedOuts(st *itemState) ([]*localeOut, error) {
	sc := s.o.Schema
	var outs []*localeOut
	for _, code := range st.availableLocales(false) {
		shared, loc, ok, err := st.publishedParts(code)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		doc := mergeParts(cloneDoc(shared), cloneDoc(loc))
		sc.Normalize(st.typ, doc)
		sc.Prune(st.typ, doc)
		name := sc.Name(st.typ, doc)
		if st.chain(code) == nil {
			name = st.pub.Locales[st.pub.idx[code]].Name
		}
		outs = append(outs, &localeOut{code: code, doc: doc, name: name, refs: sc.Refs(st.typ, doc), final: true})
	}
	return outs, nil
}

// project rebuilds the reference, index and search projections of an item's current published version. Index
// and search rows equal to the remembered ones are not rewritten.
func (s *Service) project(ctx context.Context, c store.ContentStore, st *itemState, outs []*localeOut) error {
	id := st.item.ID
	refs := projectRefs(id, outs)
	if err := c.ReplaceRefs(ctx, id, refs); err != nil {
		return fmt.Errorf("content: project refs of %s: %w", id, err)
	}
	var prev *rowset
	if st.prev != nil {
		prev = st.prev.published
	}
	if err := s.writeRows(ctx, c, id, store.ProjectionPublished, s.rowsOf(st, outs), prev); err != nil {
		return fmt.Errorf("content: project %s: %w", id, err)
	}
	return nil
}

// writeRows replaces the rows of a projection, leaving a part alone when prev (the rows it holds) equals it.
func (s *Service) writeRows(ctx context.Context, c store.ContentStore, id ids.UUID, p store.Projection, rows, prev *rowset) error {
	if prev == nil || !reflect.DeepEqual(rows.index, prev.index) {
		if err := c.ReplaceIndex(ctx, id, p, rows.index); err != nil {
			return fmt.Errorf("index: %w", err)
		}
	}
	if prev == nil || !reflect.DeepEqual(rows.search, prev.search) {
		if err := c.ReplaceSearch(ctx, id, p, rows.search); err != nil {
			return fmt.Errorf("search: %w", err)
		}
	}
	return nil
}

func (s *Service) searchDocs(st *itemState, outs []*localeOut) []store.SearchDoc {
	docs := make([]store.SearchDoc, len(outs))
	for i, o := range outs {
		text := capSearchText(strings.TrimSpace(o.name + " " + s.o.Schema.SearchText(st.typ, o.doc)))
		docs[i] = store.SearchDoc{Locale: o.code, Text: text}
	}
	return docs
}

// workingOuts lists the locales of an item's working view: its draft parts laid over the published version.
func (s *Service) workingOuts(st *itemState) ([]*localeOut, error) {
	sc := s.o.Schema
	outs := make([]*localeOut, 0, len(st.drafts)+1)
	for _, code := range st.availableLocales(true) {
		shared, loc, _, err := st.workingParts(code)
		if err != nil {
			return nil, err
		}
		doc := mergeParts(cloneDoc(shared), cloneDoc(loc))
		sc.Normalize(st.typ, doc)
		sc.Prune(st.typ, doc)
		// The name comes from the merged document: a shared draft edit can change it in locales without a draft.
		outs = append(outs, &localeOut{code: code, doc: doc, name: sc.Name(st.typ, doc)})
	}
	return outs, nil
}

// projectWorking rebuilds the working index and search rows of an item (management listings), skipping what
// is unchanged since rememberRows. st must hold the drafts and the published version.
func (s *Service) projectWorking(ctx context.Context, c store.ContentStore, st *itemState) error {
	outs, err := s.workingOuts(st)
	if err != nil {
		return err
	}
	var prev *rowset
	if st.prev != nil {
		prev = st.prev.working
	}
	if err := s.writeRows(ctx, c, st.item.ID, store.ProjectionWorking, s.rowsOf(st, outs), prev); err != nil {
		return fmt.Errorf("content: project working %s: %w", st.item.ID, err)
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
	if len(outs) == 0 {
		return nil
	}
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
			if it.DeletedAt == nil {
				found[it.ID] = it.Type
			}
		}
	}
	for _, o := range outs {
		for _, r := range o.refs {
			if r.Kind != "field" {
				continue
			}
			if msg := refProblem(r, found); msg != "" {
				o.errs = append(o.errs, schema.FieldError{Path: r.Path, Code: "ref_target", Message: msg})
			}
		}
	}
	return nil
}

func refProblem(r schema.RefValue, found map[ids.UUID]string) string {
	id, err := ids.Parse(r.To)
	if err != nil {
		return fmt.Sprintf("%q is not an item id", r.To)
	}
	typ, ok := found[id]
	if !ok {
		return fmt.Sprintf("item %s does not exist", r.To)
	}
	if len(r.Types) > 0 && !slices.Contains(r.Types, typ) {
		return fmt.Sprintf("item %s is a %s; allowed: %s", r.To, typ, strings.Join(r.Types, ", "))
	}
	return ""
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
