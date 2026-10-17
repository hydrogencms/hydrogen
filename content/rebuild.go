package content

import (
	"context"
	"fmt"

	"github.com/hydrogencms/hydrogen/store"
)

// RebuildProjections rebuilds the derived data of an item whose rows an import wrote itself: references, field
// index, search text and delivery snapshots from the published version v, and the working index and search
// rows from the drafts laid over it. It runs no hooks and emits no events. An item that was never published
// (v nil) has only working rows. The item's type must exist in the schema.
func (s *Service) RebuildProjections(ctx context.Context, c store.ContentStore, it store.Item, v *store.Version) error {
	if v != nil && it.PublishedVersion > 0 {
		if err := s.rebuild(ctx, c, it, v, true); err != nil {
			return err
		}
	}
	return s.rebuildWorking(ctx, c, it)
}

// rebuildWorking rebuilds the working index and search rows of an item from its stored drafts and published
// version.
func (s *Service) rebuildWorking(ctx context.Context, c store.ContentStore, it store.Item) error {
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if err := st.load(ctx, c); err != nil {
		return err
	}
	return s.projectWorking(ctx, c, st)
}

// rebuildSnapshots rebuilds only the delivery snapshots of a published item (its key or availability changed).
func (s *Service) rebuildSnapshots(ctx context.Context, c store.ContentStore, it store.Item) error {
	if it.PublishedVersion == 0 {
		return c.DeleteSnapshots(ctx, it.ID)
	}
	return s.rebuild(ctx, c, it, nil, false)
}

// rebuild is the one implementation of rebuilding from a stored version: v, or the item's published version
// when nil. Stored documents are exactly what publish built from (hook changes are persisted), so the
// result equals the publish-time snapshots.
func (s *Service) rebuild(ctx context.Context, c store.ContentStore, it store.Item, v *store.Version, projections bool) error {
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if v == nil {
		if err := st.loadPub(ctx, c); err != nil {
			return err
		}
	} else {
		st.pub = newPubVersion(*v)
	}
	outs, err := s.publishedOuts(st)
	if err != nil {
		return err
	}
	if len(outs) == 0 {
		return fmt.Errorf("content: published version %d of %s has no locales", it.PublishedVersion, it.ID)
	}
	if projections {
		if err := s.project(ctx, c, st, outs); err != nil {
			return err
		}
	}
	return s.putSnapshots(ctx, c, st, outs, it.PublishedVersion, st.pub.CreatedAt)
}
