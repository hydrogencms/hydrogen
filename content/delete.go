package content

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// deleteItem removes a trashed item for good (Purge). Items that reference it with on_delete block stop it
// (ReferencedError); nullify referrers get a new published version without the reference.
func (s *Service) deleteItem(ctx context.Context, tx store.Tx, it store.Item) error {
	if _, err := s.typeOf(it.Type); err != nil {
		return err
	}
	if _, err := s.runHooks(ctx, tx, it, hooks.BeforeDelete, "", nil); err != nil {
		return err
	}
	c := tx.Content()
	refs, err := c.IncomingRefs(ctx, it.ID)
	if err != nil {
		return err
	}
	refs = slices.DeleteFunc(refs, func(r store.Ref) bool { return r.From == it.ID })
	if err := s.checkNotBlocked(ctx, c, refs); err != nil {
		return err
	}
	if err := s.nullifyReferrers(ctx, tx, it.ID, refs); err != nil {
		return err
	}
	locales, err := publishedLocales(ctx, c, it)
	if err != nil {
		return err
	}
	if err := c.DeleteItem(ctx, it.ID); err != nil {
		return itemErr(it.ID.String(), err)
	}
	actor := audit.ActorFrom(ctx)
	if err := s.record(ctx, tx, actor, "item.purge", it.ID, map[string]any{"type": it.Type}); err != nil {
		return err
	}
	if err := s.emit(ctx, tx, "item.deleted", it, it.PublishedVersion, locales); err != nil {
		return err
	}
	_, err = s.runHooks(ctx, tx, it, hooks.AfterDelete, "", nil)
	return err
}

func publishedLocales(ctx context.Context, c store.ContentStore, it store.Item) ([]string, error) {
	if it.PublishedVersion == 0 {
		return nil, nil
	}
	metas, err := c.ListVersions(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("content: list versions of %s: %w", it.ID, err)
	}
	for _, m := range metas {
		if m.Version == it.PublishedVersion {
			return slices.Sorted(slices.Values(m.Locales)), nil
		}
	}
	return nil, nil
}

func (s *Service) checkNotBlocked(ctx context.Context, c store.ContentStore, refs []store.Ref) error {
	blocking := slices.DeleteFunc(slices.Clone(refs), func(r store.Ref) bool { return r.OnDelete != "block" })
	if len(blocking) == 0 {
		return nil
	}
	rs, hidden, err := s.referrers(ctx, c, blocking, maxNamedReferrers)
	if err != nil {
		return err
	}
	return &ReferencedError{Referrers: rs, Hidden: hidden}
}

// nullifyReferrers gives every item that references target with on_delete nullify a new published version
// without the reference. It is a system change: required fields are not validated.
func (s *Service) nullifyReferrers(ctx context.Context, tx store.Tx, target ids.UUID, refs []store.Ref) error {
	var from []ids.UUID
	for _, r := range refs {
		if r.OnDelete == "nullify" {
			from = append(from, r.From)
		}
	}
	slices.SortFunc(from, func(a, b ids.UUID) int { return slices.Compare(a[:], b[:]) })
	for _, id := range slices.Compact(from) {
		if err := s.nullifyRef(ctx, tx, id, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) nullifyRef(ctx context.Context, tx store.Tx, id, target ids.UUID) error {
	it, err := getItem(ctx, tx, id, true)
	if err != nil {
		return err
	}
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	c := tx.Content()
	if err := st.load(ctx, c); err != nil {
		return err
	}
	if st.pub == nil {
		return nil
	}
	sc, tid := s.o.Schema, target.String()
	sharedData, outs, changed, err := st.removeRef(sc, tid)
	if err != nil || !changed {
		return err
	}
	oldVersion := st.item.PublishedVersion
	if err := s.commitVersion(ctx, c, st, sharedData, outs, audit.System.String()); err != nil {
		return err
	}
	if err := st.removeDraftRefs(ctx, c, sc, tid, oldVersion, s.now()); err != nil {
		return err
	}
	if len(st.drafts) > 0 {
		if err := s.draftChanged(ctx, tx, st); err != nil {
			return err
		}
	}
	if err := s.projectWorking(ctx, c, st); err != nil {
		return err
	}
	data := map[string]any{"type": st.typ.Name, "removedRef": tid, "version": st.item.PublishedVersion}
	if err := s.record(ctx, tx, audit.System, "item.update", id, data); err != nil {
		return err
	}
	return s.emit(ctx, tx, "item.published", st.item, st.item.PublishedVersion, codesOf(s.withDerived(outs)))
}

// nullifies matches the reference fields whose on_delete policy is nullify; only those are cleared when their
// target is deleted (warn fields keep the dangling id, block fields prevented the delete).
func nullifies(sc *schema.Schema) func(*schema.Field) bool {
	return func(f *schema.Field) bool {
		return sc.OnDelete(f) == "nullify"
	}
}

// removeRef removes target from the shared and locale documents of the published version and returns the
// next version's parts; unchanged parts keep their stored bytes.
func (st *itemState) removeRef(sc *schema.Schema, target string) (sharedData []byte, outs []*localeOut, changed bool, err error) {
	shared, err := st.pub.sharedDoc()
	if err != nil {
		return nil, nil, false, err
	}
	sharedData = st.pub.Shared
	if sc.RemoveRefIn(st.typ, shared, target, nullifies(sc)) {
		changed = true
		if sharedData, err = encodeDoc(shared); err != nil {
			return nil, nil, false, err
		}
	}
	for _, code := range st.pub.codes() {
		doc, _, err := st.pub.localeDoc(code)
		if err != nil {
			return nil, nil, false, err
		}
		stored := st.pub.Locales[st.pub.idx[code]]
		data := stored.Data
		if sc.RemoveRefIn(st.typ, doc, target, nullifies(sc)) {
			changed = true
			if data, err = encodeDoc(doc); err != nil {
				return nil, nil, false, err
			}
		}
		outs = append(outs, &localeOut{code: code, data: data, doc: doc, name: stored.Name})
	}
	sparse := make(map[string]map[string]any, len(outs))
	for _, o := range outs {
		sparse[o.code] = o.doc
	}
	for _, o := range outs {
		loc := o.doc
		if chain := st.chain(o.code); chain != nil {
			loc = unionChain(chain, sparse)
		}
		o.doc = mergeParts(shared, loc)
		o.refs = sc.Refs(st.typ, o.doc)
		if st.chain(o.code) != nil {
			o.name = st.nameOf(shared, loc)
		}
	}
	return sharedData, outs, changed, nil
}

// removeDraftRefs removes target from the draft parts too, so the next publish does not resurrect it, and
// rebases drafts that started from the replaced version so the system change is not reported as a conflict.
func (st *itemState) removeDraftRefs(ctx context.Context, c store.ContentStore, sc *schema.Schema, target string,
	oldVersion int, now time.Time) error {
	for _, part := range st.draftNames() {
		d := st.drafts[part]
		doc, err := d.document()
		if err != nil {
			return err
		}
		changed := sc.RemoveRefIn(st.typ, doc, target, nullifies(sc))
		rebase := d.BaseVersion == oldVersion
		if !changed && !rebase {
			continue
		}
		if changed {
			if d.Data, err = encodeDoc(doc); err != nil {
				return err
			}
		}
		if rebase {
			d.BaseVersion = st.item.PublishedVersion
		}
		d.UpdatedAt, d.UpdatedBy = now, audit.System.String()
		if err := st.setDraft(ctx, c, d.DraftPart, doc); err != nil {
			return err
		}
	}
	return nil
}
