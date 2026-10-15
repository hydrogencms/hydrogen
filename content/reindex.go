package content

import (
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

const reindexLock = "content:reindex"

// projectionsPrefix starts the name recorded (as a content migration name, which no registered migration can
// use) once every item's projections have been built for one schema, suffixed with the schema's hash.
const projectionsPrefix = "system:projections:"

func (s *Service) projectionsMarker() string { return projectionsPrefix + s.o.Schema.Hash() }

// Reindex rebuilds every item's derived data (references, field index, search text, delivery snapshots and the
// working projection) from the stored versions and drafts, one transaction per item, and returns the number of
// items. progress, when set, is called after each batch with the running count.
func (s *Service) Reindex(ctx context.Context, progress func(done int)) (int, error) {
	lock, ok, err := s.o.DB.Locker().TryLock(ctx, reindexLock)
	if err != nil {
		return 0, fmt.Errorf("content: reindex: %w", err)
	}
	if !ok {
		return 0, fmt.Errorf("content: reindex: another process is reindexing")
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	n, err := s.eachItem(ctx, progress, s.rebuildAll)
	if err != nil {
		return n, err
	}
	return n, s.markWorkingBuilt(ctx)
}

func (s *Service) rebuildAll(ctx context.Context, c store.ContentStore, it store.Item) error {
	var v *store.Version
	if it.PublishedVersion > 0 {
		pv, err := c.GetVersion(ctx, it.ID, it.PublishedVersion)
		if err != nil {
			return itemErr(fmt.Sprintf("%s version %d", it.ID, it.PublishedVersion), err)
		}
		v = &pv
	}
	return s.RebuildProjections(ctx, c, it, v)
}

// EnsureWorkingProjections rebuilds every item's projections (index, search, references, snapshots and the
// working copy) once per schema, so rows missing or stale after a schema change (a field newly indexed) are
// repaired rather than trusted: edits skip rewriting rows that equal what memory derives. It also builds the
// working projection for databases that existed before it did. It is idempotent and recorded by the schema's
// hash; when another process holds the lock it returns at once, since that process is building it. built
// reports whether it ran.
func (s *Service) EnsureWorkingProjections(ctx context.Context) (built bool, err error) {
	done, err := s.workingBuilt(ctx)
	if err != nil || done {
		return false, err
	}
	lock, ok, err := s.o.DB.Locker().TryLock(ctx, reindexLock)
	if err != nil {
		return false, fmt.Errorf("content: working projections: %w", err)
	}
	if !ok {
		return false, nil
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	if done, err = s.workingBuilt(ctx); err != nil || done {
		return false, err
	}
	if _, err := s.eachItem(ctx, nil, s.rebuildAll); err != nil {
		return false, err
	}
	return true, s.markWorkingBuilt(ctx)
}

func (s *Service) workingBuilt(ctx context.Context) (bool, error) {
	var applied []string
	err := s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		applied, err = tx.Content().AppliedContentMigrations(ctx)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("content: working projections: %w", err)
	}
	return slices.Contains(applied, s.projectionsMarker()), nil
}

func (s *Service) markWorkingBuilt(ctx context.Context) error {
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		return tx.Content().RecordContentMigration(ctx, s.projectionsMarker(), s.now())
	})
}

// eachItem runs fn in one transaction per item of every type in the schema, in id order per type.
func (s *Service) eachItem(ctx context.Context, progress func(int),
	fn func(context.Context, store.ContentStore, store.Item) error) (int, error) {
	total := 0
	for _, t := range s.o.Schema.Types() {
		var after ids.UUID
		for {
			var batch []ids.UUID
			err := s.o.DB.View(ctx, func(tx store.Tx) (err error) {
				batch, err = tx.Content().ItemIDsOfType(ctx, t.Name, after, migrateBatch)
				return err
			})
			if err != nil {
				return total, err
			}
			for _, id := range batch {
				err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
					it, err := getItem(ctx, tx, id, true)
					if err != nil {
						return err
					}
					return fn(ctx, tx.Content(), it)
				})
				if err != nil {
					return total, fmt.Errorf("content: item %s: %w", id, err)
				}
				total++
			}
			if progress != nil {
				progress(total)
			}
			if len(batch) < migrateBatch {
				break
			}
			after = batch[len(batch)-1]
		}
	}
	return total, nil
}
