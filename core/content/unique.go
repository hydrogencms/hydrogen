package content

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const codeNotUnique = "not_unique"

// uniqueFields lists a type's unique fields by name, the order in which their locks are taken.
func uniqueFields(t *schema.Type) []*schema.Field {
	var out []*schema.Field
	for _, f := range t.Fields {
		if f.Unique {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b *schema.Field) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// checkUnique reports the unique fields of doc whose value another item of the type already holds in the
// locale, in the working projection (drafts) or the published one. Fields whose value equals prev's are
// skipped, so existing duplicates do not block other edits. The check takes a lock per field until the
// transaction ends, so two writers cannot both pass.
func (s *Service) checkUnique(ctx context.Context, c store.ContentStore, t *schema.Type, self ids.UUID, locale string,
	doc, prev map[string]any, working bool) ([]schema.FieldError, error) {
	var errs []schema.FieldError
	for _, f := range uniqueFields(t) {
		v, _ := doc[f.Name].(string)
		if v == "" {
			continue
		}
		if old, _ := prev[f.Name].(string); old == v {
			continue
		}
		if err := c.LockName(ctx, "unique:"+t.Name+":"+f.Name); err != nil {
			return nil, fmt.Errorf("content: lock unique %s.%s: %w", t.Name, f.Name, err)
		}
		res, err := c.Query(ctx, store.Query{Type: t.Name, Locale: locale, Working: working, NoTotal: true, Limit: 2,
			Where: []store.Cond{{Field: f.Name, Column: store.ColText, Op: store.OpEq, Value: v}}})
		if err != nil {
			return nil, fmt.Errorf("content: check unique %s.%s: %w", t.Name, f.Name, err)
		}
		if slices.ContainsFunc(res.Items, func(it store.ItemSummary) bool { return it.ID != self }) {
			errs = append(errs, schema.FieldError{Path: f.Name, Code: codeNotUnique,
				Message: fmt.Sprintf("%q is already used by another %s", v, t.Label)})
		}
	}
	return errs, nil
}

// workingDoc is the working document of a locale, nil when the item has no draft or published version yet.
func (st *itemState) workingDoc(s *Service, locale string) map[string]any {
	if st.item.PublishedVersion == 0 && len(st.drafts) == 0 {
		return nil
	}
	shared, loc, _, err := st.workingParts(locale)
	if err != nil {
		return nil
	}
	doc := mergeParts(cloneDoc(shared), cloneDoc(loc))
	s.o.Schema.Normalize(st.typ, doc)
	return doc
}

// checkRestoreUnique fails a restore whose working documents hold a unique value another item took while the
// item was in the trash.
func (s *Service) checkRestoreUnique(ctx context.Context, tx store.Tx, it store.Item) error {
	t, err := s.typeOf(it.Type)
	if err != nil || len(uniqueFields(t)) == 0 {
		return err
	}
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if err := st.load(ctx, tx.Content()); err != nil {
		return err
	}
	outs, err := s.workingOuts(st)
	if err != nil {
		return err
	}
	for _, o := range outs {
		errs, err := s.checkUnique(ctx, tx.Content(), t, it.ID, o.code, o.doc, nil, true)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return &ValidationError{Locale: o.code, Errors: errs}
		}
	}
	return nil
}
