package content

import (
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// RestoreVersion writes the shared part and the given locales of a stored version into the draft, through the
// draft write path (hooks, validation, projections, events). A nil or empty locales restores every locale of
// the version. Delivery is unchanged until the draft is published. Trashed items and unknown versions are not
// found; the view is the draft in the item's main locale when restored, else in the last restored locale.
func (s *Service) RestoreVersion(ctx context.Context, id ids.UUID, version int, locales []string) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		st, err := s.newState(it)
		if err != nil {
			return err
		}
		if err := st.load(ctx, tx.Content()); err != nil {
			return err
		}
		sv, err := tx.Content().GetVersion(ctx, id, version)
		if err != nil {
			return itemErr(fmt.Sprintf("%s version %d", id, version), err)
		}
		pv := newPubVersion(sv)
		targets := slices.Clone(locales)
		if len(targets) == 0 {
			targets = pv.codes()
		}
		slices.Sort(targets)
		targets = slices.Compact(targets)
		// A version without locales still restores its shared part, through the main locale.
		write := targets
		if len(write) == 0 {
			write = []string{it.MainLocale}
		}
		if err := s.check(ctx, access.PolicyContentEdit, st, write); err != nil {
			return err
		}
		if err := s.checkState(ctx, it); err != nil {
			return err
		}
		for _, l := range targets {
			if err := validateLocale(l); err != nil {
				return err
			}
			if _, ok, err := pv.localeDoc(l); err != nil {
				return err
			} else if !ok {
				return badRequest("version %d has no locale %q", version, l)
			}
		}
		shared, err := pv.sharedDoc()
		if err != nil {
			return err
		}
		var main *View
		for _, l := range write {
			loc, _, err := pv.localeDoc(l)
			if err != nil {
				return err
			}
			base, err := s.baseDoc(st, l)
			if err != nil {
				return err
			}
			wv, err := s.writeDraft(ctx, tx, st, writeReq{locale: l, patch: docDiff(base, mergeParts(cloneDoc(shared), cloneDoc(loc))), loaded: true, audited: true})
			if err != nil {
				return fmt.Errorf("content: restore %s version %d: %w", id, version, err)
			}
			if v == nil || l == it.MainLocale {
				main = wv
			}
			v = wv
		}
		if main != nil {
			v = main
		}
		return s.record(ctx, tx, audit.ActorFrom(ctx), "item.restore_version", id,
			map[string]any{"version": version, "locales": write})
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// docDiff is the JSON merge patch that turns base into target (nil removes a key; lists replace).
func docDiff(base, target map[string]any) map[string]any {
	patch := map[string]any{}
	for k, tv := range target {
		if tv == nil {
			continue
		}
		bv, ok := base[k]
		tm, tIsMap := tv.(map[string]any)
		bm, bIsMap := bv.(map[string]any)
		switch {
		case ok && tIsMap && bIsMap:
			if d := docDiff(bm, tm); len(d) > 0 {
				patch[k] = d
			}
		case !ok || !equalJSON(bv, tv):
			patch[k] = tv
		}
	}
	for k := range base {
		if tv, ok := target[k]; !ok || tv == nil {
			patch[k] = nil
		}
	}
	return patch
}
