package content

import (
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// Unpublish stops delivering an item: its snapshots, references, published index rows and routes go, and
// PublishedVersion becomes 0. Its versions stay and its draft is kept; parts without a draft get one copied
// from the last published version, so the editor still shows the content and the next publish rebuilds the
// item from it. It returns the draft view.
func (s *Service) Unpublish(ctx context.Context, id ids.UUID) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		if err := s.UnpublishTx(ctx, tx, id); err != nil {
			return err
		}
		it, err := getItem(store.WithTx(ctx, tx), tx, id, false)
		if err != nil {
			return err
		}
		v, err = s.viewOf(store.WithTx(ctx, tx), tx, it, GetOptions{Draft: true})
		return err
	})
	return v, err
}

// UnpublishTx is Unpublish inside the caller's transaction (releases). It needs content.publish on the item,
// refuses singletons and items that are not published (ErrBadRequest), and runs the unpublish hooks. Like
// PublishTx it needs Prepare to have run before the transaction opened.
func (s *Service) UnpublishTx(ctx context.Context, tx store.Tx, id ids.UUID) error {
	ctx = store.WithTx(ctx, tx)
	it, err := getItem(ctx, tx, id, true)
	if err != nil {
		return err
	}
	t, err := s.typeOf(it.Type)
	if err != nil {
		return err
	}
	if t.Singleton {
		return badRequest("%s is a singleton: it cannot be unpublished", it.Type)
	}
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if err := st.load(ctx, tx.Content()); err != nil {
		return err
	}
	if st.pub == nil {
		return badRequest("%s is not published", id)
	}
	if err := s.check(ctx, access.PolicyContentPublish, st, st.pub.codes()); err != nil {
		return err
	}
	if err := s.checkState(ctx, it); err != nil {
		return err
	}
	locales := st.pub.codes()
	if err := s.takeOffline(ctx, tx, st); err != nil {
		return fmt.Errorf("content: unpublish %s: %w", id, err)
	}
	// The withdrawal turned the published content into draft parts.
	if err := s.draftChanged(ctx, tx, st); err != nil {
		return err
	}
	actor := audit.ActorFrom(ctx)
	if err := s.record(ctx, tx, actor, "item.unpublish", id, map[string]any{"type": it.Type, "version": it.PublishedVersion}); err != nil {
		return err
	}
	return s.emit(ctx, tx, "item.unpublished", st.item, it.PublishedVersion, locales)
}

// takeOffline runs the unpublish hooks and withdraws the published version of a loaded item (published or
// not: an unpublished item only runs the hooks, which let modules drop their own state). The caller checked
// access and records the audit entry and the event.
func (s *Service) takeOffline(ctx context.Context, tx store.Tx, st *itemState) error {
	if _, err := s.runHooks(ctx, tx, st.item, hooks.BeforeUnpublish, "", nil); err != nil {
		return err
	}
	if st.pub != nil {
		if err := s.withdraw(ctx, tx, st); err != nil {
			return err
		}
	}
	_, err := s.runHooks(ctx, tx, st.item, hooks.AfterUnpublish, "", nil)
	return err
}

// withdraw keeps the published content as draft parts, then removes what publish wrote for delivery.
func (s *Service) withdraw(ctx context.Context, tx store.Tx, st *itemState) error {
	c := tx.Content()
	now, actor := s.now(), audit.ActorFrom(ctx).String()
	version := st.pub.Version.Version
	shared, err := st.pub.sharedDoc()
	if err != nil {
		return err
	}
	if st.drafts[""] == nil {
		p := store.DraftPart{ItemID: st.item.ID, Part: "", BaseVersion: version, Data: slices.Clone(st.pub.Shared),
			UpdatedAt: now, UpdatedBy: actor}
		if len(p.Data) == 0 {
			p.Data = []byte("{}")
		}
		if err := st.setDraft(ctx, c, p, shared); err != nil {
			return err
		}
	}
	for _, l := range st.pub.Locales {
		if st.drafts[l.Locale] != nil {
			continue
		}
		doc, _, err := st.pub.localeDoc(l.Locale)
		if err != nil {
			return err
		}
		p := store.DraftPart{ItemID: st.item.ID, Part: l.Locale, BaseVersion: version, Name: l.Name,
			Data: slices.Clone(l.Data), UpdatedAt: now, UpdatedBy: actor}
		if err := st.setDraft(ctx, c, p, doc); err != nil {
			return err
		}
	}
	if err := c.DeleteSnapshots(ctx, st.item.ID); err != nil {
		return fmt.Errorf("delete snapshots: %w", err)
	}
	if err := c.ReplaceRefs(ctx, st.item.ID, nil); err != nil {
		return fmt.Errorf("clear references: %w", err)
	}
	if err := c.ReplaceIndex(ctx, st.item.ID, store.ProjectionPublished, nil); err != nil {
		return fmt.Errorf("clear index: %w", err)
	}
	if err := c.ReplaceSearch(ctx, st.item.ID, store.ProjectionPublished, nil); err != nil {
		return fmt.Errorf("clear search: %w", err)
	}
	st.item.PublishedVersion, st.item.UpdatedAt, st.item.UpdatedBy = 0, now, actor
	if err := c.UpdateItem(ctx, st.item); err != nil {
		return itemErr(st.item.ID.String(), err)
	}
	st.pub, st.prev = nil, nil
	return s.projectWorking(ctx, c, st)
}
