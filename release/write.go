package release

import (
	"context"
	"errors"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/store"
)

// mutate runs fn on the release in a transaction, serialised with every other state change in this process.
// fn changes r in place; mutate stores it afterwards with the actor and time.
func (s *Service) mutate(ctx context.Context, id ids.UUID, fn func(ctx context.Context, tx store.Tx, r *store.Release) error) (*View, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		r, err := loadRelease(ctx, tx, id)
		if err != nil {
			return err
		}
		before := r.Status
		if err := fn(ctx, tx, &r); err != nil {
			return err
		}
		r.UpdatedBy, r.UpdatedAt = s.actor(ctx), s.o.Clock.Now()
		if err := transition(ctx, tx, r, before); err != nil {
			return err
		}
		v, err = s.detail(ctx, tx, id)
		return err
	})
	return v, err
}

// checkItem validates that an item can go into a release: it exists, is not in the trash, the caller can read
// it and an unpublish applies to its type. Publishing it is checked when the release runs.
func (s *Service) checkItem(ctx context.Context, tx store.Tx, in ItemInput) error {
	it, err := tx.Content().GetItem(ctx, in.Item, false)
	if errors.Is(err, store.ErrNotFound) {
		return manage.NotFound("item %s not found", in.Item)
	}
	if err != nil {
		return err
	}
	if ok, err := s.o.Content.CanReadTx(ctx, tx, it); err != nil {
		return err
	} else if !ok {
		return manage.NotFound("item %s not found", in.Item)
	}
	if it.DeletedAt != nil {
		return manage.Conflict("trashed", "item %s is in the trash", in.Item)
	}
	if in.Action == ActionUnpublish {
		if t, ok := s.o.Content.Schema().Type(it.Type); ok && t.Singleton {
			return manage.BadRequest("a singleton cannot be unpublished")
		}
	}
	return nil
}

func (s *Service) putItem(ctx context.Context, tx store.Tx, id ids.UUID, in ItemInput) error {
	if err := s.checkItem(ctx, tx, in); err != nil {
		return err
	}
	return tx.Releases().PutReleaseItem(ctx, store.ReleaseItem{ReleaseID: id, ItemID: in.Item, Action: in.Action,
		Locales: in.Locales, AddedBy: s.actor(ctx), AddedAt: s.o.Clock.Now()})
}

// Create makes an open release, scheduled at once when ScheduledAt is set. Needs release.manage.
func (s *Service) Create(ctx context.Context, in CreateInput) (*View, error) {
	name, err := validName(in.Name)
	if err != nil {
		return nil, err
	}
	if _, err := validDescription(in.Description); err != nil {
		return nil, err
	}
	kind := in.Kind
	if kind == "" {
		kind = KindRelease
	}
	if kind != KindRelease && kind != KindItem {
		return nil, manage.BadRequest("kind must be release or item")
	}
	items := make([]ItemInput, len(in.Items))
	for i, it := range in.Items {
		if items[i], err = normalizeItem(it); err != nil {
			return nil, err
		}
	}
	if in.ScheduledAt != nil {
		if err := s.checkSchedule(*in.ScheduledAt); err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, manage.BadRequest("a release without items cannot be scheduled")
		}
	}
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		now, actor := s.o.Clock.Now(), s.actor(ctx)
		r := store.Release{ID: s.o.IDs.New(), Name: name, Description: in.Description, Kind: kind, Status: StatusOpen,
			CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if in.ScheduledAt != nil {
			at := in.ScheduledAt.UTC()
			r.Status, r.ScheduledAt, r.ScheduledBy = StatusScheduled, &at, actor
		}
		if err := tx.Releases().CreateRelease(ctx, r); err != nil {
			return err
		}
		for _, it := range items {
			if err := s.putItem(ctx, tx, r.ID, it); err != nil {
				return err
			}
		}
		if err := s.record(ctx, tx, "release.create", r.ID, map[string]any{"name": name, "kind": kind, "items": len(items)}); err != nil {
			return err
		}
		if r.ScheduledAt != nil {
			if err := s.record(ctx, tx, "release.schedule", r.ID, map[string]any{"at": r.ScheduledAt}); err != nil {
				return err
			}
		}
		var err error
		v, err = s.detail(ctx, tx, r.ID)
		return err
	})
	return v, err
}

// Update changes the name and description. A failed release reopens. Needs release.manage.
func (s *Service) Update(ctx context.Context, id ids.UUID, in UpdateInput) (*View, error) {
	var name, desc *string
	if in.Name != nil {
		n, err := validName(*in.Name)
		if err != nil {
			return nil, err
		}
		name = &n
	}
	if in.Description != nil {
		d, err := validDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		desc = &d
	}
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		switch r.Status {
		case StatusOpen, StatusScheduled:
		case StatusFailed:
			reopen(r)
		default:
			return stateError("a %s release cannot be edited", r.Status)
		}
		if name != nil {
			r.Name = *name
		}
		if desc != nil {
			r.Description = *desc
		}
		return s.record(ctx, tx, "release.update", r.ID, map[string]any{"name": r.Name})
	})
}

// Delete removes an open or cancelled release with its items. Needs release.manage.
func (s *Service) Delete(ctx context.Context, id ids.UUID) error {
	if err := s.requireManage(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		r, err := loadRelease(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Status != StatusOpen && r.Status != StatusCancelled {
			return stateError("a %s release cannot be deleted; cancel it first", r.Status)
		}
		if err := transition(ctx, tx, r, r.Status); err != nil { // lock the row against a publish on another node
			return err
		}
		if err := tx.Releases().DeleteRelease(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, tx, "release.delete", id, map[string]any{"name": r.Name})
	})
}

// AddItem puts an item in an open (or failed) release, replacing its action and locales if it is there already.
// Needs release.manage.
func (s *Service) AddItem(ctx context.Context, id ids.UUID, in ItemInput) (*View, error) {
	in, err := normalizeItem(in)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		if !editable(r.Status) {
			return stateError("a %s release cannot change its items%s", r.Status, unscheduleHint(r.Status))
		}
		reopen(r)
		if err := s.putItem(ctx, tx, id, in); err != nil {
			return err
		}
		return s.record(ctx, tx, "release.update", id, map[string]any{"add": in.Item.String(), "action": in.Action})
	})
}

// RemoveItem takes an item out of an open (or failed) release; an item that is not in it is fine. Needs
// release.manage.
func (s *Service) RemoveItem(ctx context.Context, id, item ids.UUID) (*View, error) {
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		if !editable(r.Status) {
			return stateError("a %s release cannot change its items%s", r.Status, unscheduleHint(r.Status))
		}
		reopen(r)
		if err := tx.Releases().RemoveReleaseItem(ctx, id, item); err != nil {
			return err
		}
		return s.record(ctx, tx, "release.update", id, map[string]any{"remove": item.String()})
	})
}

func unscheduleHint(status string) string {
	if status == StatusScheduled {
		return "; unschedule it first"
	}
	return ""
}

// Schedule publishes the release at the given time, as the caller. An open, failed or already scheduled
// release can be (re)scheduled; it needs at least one item. Needs release.manage.
func (s *Service) Schedule(ctx context.Context, id ids.UUID, at time.Time) (*View, error) {
	if err := s.checkSchedule(at); err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		switch r.Status {
		case StatusOpen, StatusScheduled, StatusFailed:
		default:
			return stateError("a %s release cannot be scheduled", r.Status)
		}
		items, err := tx.Releases().ReleaseItems(ctx, id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return manage.BadRequest("a release without items cannot be scheduled")
		}
		at := at.UTC()
		r.Status, r.Errors, r.ScheduledAt, r.ScheduledBy = StatusScheduled, nil, &at, s.actor(ctx)
		return s.record(ctx, tx, "release.schedule", id, map[string]any{"at": at})
	})
}

// Unschedule turns a scheduled release back into an open one. Needs release.manage.
func (s *Service) Unschedule(ctx context.Context, id ids.UUID) (*View, error) {
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		if r.Status != StatusScheduled {
			return stateError("a %s release is not scheduled", r.Status)
		}
		reopen(r)
		return s.record(ctx, tx, "release.update", id, map[string]any{"unscheduled": true})
	})
}

// Cancel retires an open or scheduled release for good. Needs release.manage.
func (s *Service) Cancel(ctx context.Context, id ids.UUID) (*View, error) {
	return s.mutate(ctx, id, func(ctx context.Context, tx store.Tx, r *store.Release) error {
		if r.Status != StatusOpen && r.Status != StatusScheduled {
			return stateError("a %s release cannot be cancelled", r.Status)
		}
		r.Status = StatusCancelled
		return s.record(ctx, tx, "release.cancel", id, nil)
	})
}
