package content

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// Delete moves an item to the trash; it is Trash. The permanent delete is Purge.
func (s *Service) Delete(ctx context.Context, id ids.UUID) error { return s.Trash(ctx, id) }

// Trash unpublishes an item if it is published and hides it everywhere: lists, reads, search, reference
// pickers and delivery. Its versions and drafts stay for Restore, and its key is free for another item.
// Items that reference it with on_delete block stop the trash (ReferencedError); nullify and warn referrers
// are handled when the item is purged. Singletons cannot be trashed (ErrBadRequest).
func (s *Service) Trash(ctx context.Context, id ids.UUID) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err := s.trashItem(ctx, tx, it); err != nil {
			return fmt.Errorf("content: trash %s: %w", id, err)
		}
		return nil
	})
}

func (s *Service) trashItem(ctx context.Context, tx store.Tx, it store.Item) error {
	t, err := s.typeOf(it.Type)
	if err != nil {
		return err
	}
	if t.Singleton {
		return badRequest("%s is a singleton: it cannot be moved to the trash", it.Type)
	}
	c := tx.Content()
	if err := s.authorizeItem(ctx, c, it, access.PolicyContentDelete, nil); err != nil {
		return err
	}
	if err := s.checkState(ctx, it); err != nil {
		return err
	}
	refs, err := c.IncomingRefs(ctx, it.ID)
	if err != nil {
		return err
	}
	refs = slices.DeleteFunc(refs, func(r store.Ref) bool { return r.From == it.ID })
	if err := s.checkNotBlocked(ctx, c, refs); err != nil {
		return err
	}
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if err := st.load(ctx, c); err != nil {
		return err
	}
	locales := []string(nil)
	if st.pub != nil {
		locales = s.withDescendants(st.pub.codes(), nil)
	}
	version := it.PublishedVersion
	if err := s.takeOffline(ctx, tx, st); err != nil {
		return err
	}
	if h := s.reviewHook(st); h != nil {
		if err := h.Closed(ctx, tx, st.item, ReviewClosedTrashed); err != nil {
			return err
		}
	}
	actor, now := audit.ActorFrom(ctx), s.now()
	st.item.UpdatedAt, st.item.UpdatedBy = now, actor.String()
	if err := c.UpdateItem(ctx, st.item); err != nil {
		return itemErr(it.ID.String(), err)
	}
	if err := c.SetDeleted(ctx, it.ID, &now, actor.String()); err != nil {
		return itemErr(it.ID.String(), err)
	}
	if err := s.record(ctx, tx, actor, "item.trash", it.ID, map[string]any{"type": it.Type}); err != nil {
		return err
	}
	return s.emit(ctx, tx, "item.trashed", it, version, locales)
}

// Restore takes an item out of the trash. It comes back unpublished, with its draft and versions. It fails
// with ErrDuplicateKey when another item took its key meanwhile, and with ErrConflict when the item is not in
// the trash.
func (s *Service) Restore(ctx context.Context, id ids.UUID) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItemAny(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if it.DeletedAt == nil {
			return fmt.Errorf("%w: %s is not in the trash", ErrConflict, id)
		}
		c := tx.Content()
		if err := s.authorizeItem(ctx, c, it, access.PolicyContentDelete, nil); err != nil {
			return err
		}
		if err := s.checkState(ctx, it); err != nil {
			return err
		}
		if err := c.SetDeleted(ctx, id, nil, ""); err != nil {
			return itemErr("key "+it.Key, err)
		}
		actor := audit.ActorFrom(ctx)
		it.DeletedAt, it.DeletedBy = nil, ""
		it.UpdatedAt, it.UpdatedBy = s.now(), actor.String()
		if err := c.UpdateItem(ctx, it); err != nil {
			return itemErr(id.String(), err)
		}
		if err := s.record(ctx, tx, actor, "item.restore", id, map[string]any{"type": it.Type}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, "item.restored", it, 0, nil); err != nil {
			return err
		}
		v, err = s.viewOf(ctx, tx, it, GetOptions{Draft: true})
		return err
	})
	return v, err
}

// Purge deletes a trashed item for good, with its versions and drafts. Referrers with on_delete nullify get
// a new published version without the reference. An item that is not in the trash is ErrConflict.
func (s *Service) Purge(ctx context.Context, id ids.UUID) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		return s.purgeTx(store.WithTx(ctx, tx), tx, id)
	})
}

func (s *Service) purgeTx(ctx context.Context, tx store.Tx, id ids.UUID) error {
	it, err := getItemAny(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if it.DeletedAt == nil {
		return fmt.Errorf("%w: %s is not in the trash: move it there first", ErrConflict, id)
	}
	if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentDelete, nil); err != nil {
		return err
	}
	if err := s.checkState(ctx, it); err != nil {
		return err
	}
	if err := s.deleteItem(ctx, tx, it); err != nil {
		return fmt.Errorf("content: purge %s: %w", id, err)
	}
	return nil
}

// PurgeExpired purges the items trashed before the given time, oldest first, each in its own transaction as
// the caller's principal (the system in the retention job). It returns how many it purged; an item that
// cannot be purged is skipped and reported in the error.
func (s *Service) PurgeExpired(ctx context.Context, before time.Time) (int, error) {
	if err := s.prepare(ctx); err != nil {
		return 0, err
	}
	var (
		purged, skipped int
		errs            []error
	)
	for {
		var due []ids.UUID
		err := s.o.DB.View(ctx, func(tx store.Tx) error {
			res, err := tx.Content().Query(ctx, store.Query{Trashed: store.TrashedOnly, Locale: s.o.DefaultLocale,
				Sort: []store.Sort{{Field: "deletedAt"}}, Offset: skipped, Limit: 200})
			if err != nil {
				return err
			}
			for _, it := range res.Items {
				if it.DeletedAt != nil && it.DeletedAt.Before(before) {
					due = append(due, it.ID)
				}
			}
			return nil
		})
		if err != nil {
			return purged, errors.Join(append(errs, fmt.Errorf("content: list expired trash: %w", err))...)
		}
		if len(due) == 0 {
			return purged, errors.Join(errs...)
		}
		for _, id := range due {
			err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
				return s.purgeTx(store.WithTx(ctx, tx), tx, id)
			})
			if err != nil {
				skipped++
				errs = append(errs, err)
				continue
			}
			purged++
		}
	}
}

// ListTrash lists trashed items the principal may read, newest deletion first.
func (s *Service) ListTrash(ctx context.Context, o TrashOptions) (*TrashResult, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	if o.Type != "" {
		if _, err := s.typeOf(o.Type); err != nil {
			return nil, err
		}
	}
	q := store.Query{Type: o.Type, Locale: o.Locale, Trashed: store.TrashedOnly, Working: true,
		Sort: []store.Sort{{Field: "deletedAt", Desc: true}}}
	if q.Locale == "" {
		q.Locale = s.o.DefaultLocale
	} else if err := validateLocale(q.Locale); err != nil {
		return nil, err
	}
	limit, offset := o.Limit, max(o.Offset, 0)
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	filter := s.guarded(ctx)
	if filter {
		if err := s.o.Access.Prepare(ctx); err != nil {
			return nil, err
		}
		if o.Type != "" {
			scoped, terms, err := s.o.Access.ListScope(ctx, access.PolicyContentRead, o.Type)
			if err != nil {
				return nil, err
			}
			if scoped && len(terms) == 0 {
				return &TrashResult{Items: []TrashSummary{}}, nil
			}
			q.Scoped, q.Scope = scoped, terms
			filter = false
		}
	}
	var out *TrashResult
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		var rows []store.ItemSummary
		total := 0
		if filter {
			// Several types: the principal's scope differs per type, so read access is checked per item. The
			// trash is small, and the scan is capped.
			q.Limit = maxLimit
			for q.Offset = 0; q.Offset < maxTrashScan; q.Offset += maxLimit {
				res, err := tx.Content().Query(ctx, q)
				if err != nil {
					return err
				}
				for _, it := range res.Items {
					if ok, err := s.readable(ctx, tx.Content(), it.Item); err != nil {
						return err
					} else if ok {
						rows = append(rows, it)
					}
				}
				if len(res.Items) < maxLimit {
					break
				}
			}
			total = len(rows)
			rows = rows[min(offset, len(rows)):min(offset+limit, len(rows))]
		} else {
			q.Limit, q.Offset = limit, offset
			res, err := tx.Content().Query(ctx, q)
			if err != nil {
				return err
			}
			rows, total = res.Items, res.Total
		}
		by := make([]string, len(rows))
		for i, it := range rows {
			by[i] = it.DeletedBy
		}
		names := s.actorNames(ctx, tx, by)
		out = &TrashResult{Items: make([]TrashSummary, len(rows)), Total: total}
		for i, it := range rows {
			out.Items[i] = TrashSummary{Summary: summaryOf(it), DeletedBy: it.DeletedBy, DeletedByName: names[it.DeletedBy]}
			if it.DeletedAt != nil {
				out.Items[i].DeletedAt = it.DeletedAt.UTC()
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("content: list trash: %w", err)
	}
	return out, nil
}

// maxTrashScan bounds the items one unscoped-by-type trash listing examines.
const maxTrashScan = 5000

// actorNames resolves "kind:id" actors to display names: a user's name (else login), a token's name. Unknown
// and system actors have no name. Lookup failures leave the name empty.
func (s *Service) actorNames(ctx context.Context, tx store.Tx, actors []string) map[string]string {
	names := map[string]string{}
	for _, a := range actors {
		if _, done := names[a]; done {
			continue
		}
		names[a] = ""
		kind, raw, ok := strings.Cut(a, ":")
		id, err := ids.Parse(raw)
		if !ok || err != nil {
			continue
		}
		switch kind {
		case "user":
			if u, err := tx.Auth().GetUser(ctx, id); err == nil {
				names[a] = cmp.Or(u.Name, u.Login)
			}
		case "token", "agent":
			if t, err := tx.Auth().GetToken(ctx, id); err == nil {
				names[a] = t.Name
			}
		}
	}
	return names
}
