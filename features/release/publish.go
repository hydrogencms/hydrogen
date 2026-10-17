package release

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// errItemsFailed aborts the publish transaction when at least one item has an error.
var errItemsFailed = errors.New("release: items failed")

// Publish publishes the release now, as the caller: every item in one transaction. If any item cannot be
// published nothing is, the release becomes failed with the reasons, and the error is a *PublishFailedError. An
// open, scheduled or failed release can be published. force publishes over draft conflicts. Needs
// release.manage, and content.publish on every item.
func (s *Service) Publish(ctx context.Context, id ids.UUID, force bool) (*View, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	if _, err := s.begin(ctx, id, func(r *store.Release) error {
		switch r.Status {
		case StatusOpen, StatusScheduled, StatusFailed:
			return nil
		}
		return stateError("a %s release cannot be published", r.Status)
	}); err != nil {
		return nil, err
	}
	return s.execute(ctx, id, force)
}

// errNoItems is begin's refusal of a release without items.
var errNoItems = manage.BadRequest("a release without items cannot be published")

// begin moves a release to publishing and commits that, so a second publish is refused while one runs. The
// move is a conditional update on the status that was checked, so of two nodes racing for the same release
// exactly one wins and the other gets a 409.
func (s *Service) begin(ctx context.Context, id ids.UUID, allowed func(*store.Release) error) (store.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rel store.Release
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		r, err := loadRelease(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := allowed(&r); err != nil {
			return err
		}
		items, err := tx.Releases().ReleaseItems(ctx, id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errNoItems
		}
		from := r.Status
		r.Status, r.Errors = StatusPublishing, nil
		r.UpdatedBy, r.UpdatedAt = s.actor(ctx), s.o.Clock.Now()
		rel = r
		return transition(ctx, tx, r, from)
	})
	return rel, err
}

// execute runs a release that is in publishing as the principal in ctx: one transaction for all items, and on
// failure a second one that records the errors.
func (s *Service) execute(ctx context.Context, id ids.UUID, force bool) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		_, _ = s.fail(ctx, id, []ItemError{{Code: "internal", Message: "internal error"}}, StatusPublishing)
		return nil, err
	}
	var fails []ItemError
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		fails = fails[:0]
		ctx := store.WithTx(ctx, tx)
		r, err := loadRelease(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Status != StatusPublishing {
			return stateError("a %s release is not being published", r.Status)
		}
		// Lock the row, so a second run of the same release (a retry on another node) waits for this one and
		// then finds it published; then read it again under the lock.
		if err := transition(ctx, tx, r, StatusPublishing); err != nil {
			return err
		}
		if r, err = loadRelease(ctx, tx, id); err != nil {
			return err
		}
		items, err := tx.Releases().ReleaseItems(ctx, id)
		if err != nil {
			return err
		}
		for _, it := range items {
			ie, fatal := s.apply(ctx, tx, it, force)
			if ie != nil {
				fails = append(fails, *ie)
			}
			if fatal {
				break
			}
		}
		if len(fails) > 0 {
			return errItemsFailed
		}
		now := s.o.Clock.Now()
		r.Status, r.PublishedAt, r.Errors = StatusPublished, &now, nil
		r.UpdatedBy, r.UpdatedAt = s.actor(ctx), now
		if err := transition(ctx, tx, r, StatusPublishing); err != nil {
			return err
		}
		list := make([]map[string]string, len(items))
		for i, it := range items {
			list[i] = map[string]string{"item": it.ItemID.String(), "action": it.Action}
		}
		if err := s.record(ctx, tx, "release.publish", id, map[string]any{"name": r.Name, "items": list, "force": force}); err != nil {
			return err
		}
		_, err = s.o.Outbox.Publish(ctx, tx, TopicPublished, map[string]any{"id": id.String(), "name": r.Name,
			"kind": r.Kind, "items": list, "publishedAt": now})
		return err
	})
	switch {
	case err == nil:
		return s.read(ctx, id)
	case errors.Is(err, errItemsFailed):
		recorded, err := s.fail(ctx, id, fails, StatusPublishing)
		switch {
		case err != nil:
			return nil, fmt.Errorf("release: could not record the failure of %s: %w", id, err)
		case !recorded:
			return nil, stateError("the release changed state while it ran")
		}
		v, err := s.read(ctx, id)
		if err != nil {
			return nil, err
		}
		return v, &PublishFailedError{Release: v}
	}
	var me *manage.Error
	if errors.As(err, &me) {
		return nil, err
	}
	s.o.Log.Error("release: publish failed", "release", id.String(), "err", err)
	_, _ = s.fail(ctx, id, []ItemError{{Code: "internal", Message: "internal error"}}, StatusPublishing)
	return nil, err
}

// read loads the detail view for the caller of a publish.
func (s *Service) read(ctx context.Context, id ids.UUID) (*View, error) {
	var v *View
	err := s.o.DB.View(context.WithoutCancel(ctx), func(tx store.Tx) error {
		var err error
		v, err = s.detail(store.WithTx(ctx, tx), tx, id)
		return err
	})
	return v, err
}

// fail records a failed run in its own transaction, but only if the release still has the status it was moved
// from (publishing for a run; scheduled for a release that could not start): a release another node published
// meanwhile is never turned into failed. recorded says whether the failure was written. The grants are loaded
// before the transaction, which only writes.
func (s *Service) fail(ctx context.Context, id ids.UUID, errs []ItemError, from ...string) (recorded bool, err error) {
	ctx = context.WithoutCancel(ctx)
	if err := s.prepare(ctx); err != nil {
		s.o.Log.Warn("release: loading grants before recording a failure", "release", id.String(), "err", err)
	}
	raw, _ := json.Marshal(errs)
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		r, err := loadRelease(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(from, r.Status) {
			return nil
		}
		prev := r.Status
		r.Status, r.Errors = StatusFailed, raw
		r.UpdatedBy, r.UpdatedAt = s.actor(ctx), s.o.Clock.Now()
		if err := tx.Releases().TransitionRelease(ctx, r, []string{prev}); err != nil {
			if errors.Is(err, store.ErrStateChanged) {
				return nil
			}
			return err
		}
		recorded = true
		return s.record(ctx, tx, "release.fail", id, map[string]any{"name": r.Name, "errors": errs})
	})
	if err != nil {
		s.o.Log.Error("release: recording a failure failed", "release", id.String(), "err", err)
		return false, err
	}
	return recorded, nil
}

// apply runs one release item in the publish transaction. A nil error means done. fatal says the transaction
// is no longer usable (an unexpected error), so the remaining items are not tried.
func (s *Service) apply(ctx context.Context, tx store.Tx, ri store.ReleaseItem, force bool) (ie *ItemError, fatal bool) {
	item := ri.ItemID.String()
	it, err := tx.Content().GetItem(ctx, ri.ItemID, false)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &ItemError{Item: item, Code: "not_found", Message: "the item no longer exists"}, false
	case err != nil:
		s.o.Log.Error("release: load item", "item", item, "err", err)
		return &ItemError{Item: item, Code: "internal", Message: "internal error"}, true
	case it.DeletedAt != nil:
		return &ItemError{Item: item, Code: "trashed", Message: "the item is in the trash"}, false
	}
	switch ri.Action {
	case ActionUnpublish:
		if it.PublishedVersion == 0 {
			return nil, false
		}
		err = s.o.Content.UnpublishTx(ctx, tx, ri.ItemID)
	default:
		_, err = s.o.Content.PublishTx(ctx, tx, ri.ItemID, content.PublishOptions{Locales: ri.Locales, Force: force})
	}
	if err == nil {
		return nil, false
	}
	e, soft := classify(err)
	if !soft {
		s.o.Log.Error("release: item failed", "item", item, "action", ri.Action, "err", err)
	}
	e.Item = item
	return &e, !soft
}

// classify turns a content error into the item error shown to the editor; soft errors leave the transaction
// usable. Anything unrecognised is internal and its details stay in the log.
func classify(err error) (ItemError, bool) {
	var ve *content.ValidationError
	var ce *content.ConflictError
	var re *content.ReferencedError
	switch {
	case errors.As(err, &ve):
		e := ItemError{Code: "invalid", Message: "validation failed"}
		if len(ve.Errors) > 0 {
			e.Path, e.Message = ve.Errors[0].Path, ve.Errors[0].Message
			if ve.Locale != "" {
				e.Message = ve.Locale + ": " + e.Message
			}
		}
		return e, true
	case errors.As(err, &ce):
		return ItemError{Code: "conflict", Message: ce.Error()}, true
	case errors.As(err, &re):
		return ItemError{Code: "referenced", Message: "the item is referenced by other items"}, true
	case errors.Is(err, access.ErrForbidden):
		return ItemError{Code: "forbidden", Message: forbiddenMessage(err)}, true
	case errors.Is(err, content.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return ItemError{Code: "not_found", Message: "the item no longer exists"}, true
	case errors.Is(err, content.ErrBadRequest):
		msg := err.Error()
		if strings.Contains(msg, "nothing to publish") {
			return ItemError{Code: "nothing_to_publish", Message: "the item has no draft changes to publish"}, true
		}
		return ItemError{Code: "bad_request", Message: msg}, true
	case errors.Is(err, content.ErrInvalid):
		return ItemError{Code: "invalid", Message: err.Error()}, true
	case errors.Is(err, content.ErrReviewRequired):
		var rr *content.ReviewRequiredError
		msg := "the draft needs an approved review"
		if errors.As(err, &rr) {
			msg = "review required: " + rr.Reason
		}
		return ItemError{Code: "review_required", Message: msg}, true
	case errors.Is(err, content.ErrConflict):
		return ItemError{Code: "conflict", Message: err.Error()}, true
	}
	return ItemError{Code: "internal", Message: "internal error"}, false
}

func forbiddenMessage(err error) string {
	const marker = "access: forbidden: "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return "not allowed"
	}
	msg = msg[i+len(marker):]
	if j := strings.IndexAny(msg, ":\n"); j >= 0 {
		msg = msg[:j]
	}
	return "not allowed: " + strings.TrimSpace(msg)
}

// RunDue publishes the scheduled releases that are due, each as the principal who scheduled it, and runs again
// the releases stuck in publishing for longer than Options.StuckAfter (a crashed run). It returns how many it
// ran. A release that fails is recorded as such and does not stop the others. It is what the releases job calls.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.o.Clock.Now()
	var due, stuck []store.Release
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		if due, err = tx.Releases().DueReleases(ctx, now, dueBatch); err != nil {
			return err
		}
		all, _, err := tx.Releases().ListReleases(ctx, store.ReleaseFilter{Statuses: []string{StatusPublishing}}, store.Page{Limit: 100})
		for _, r := range all {
			if !r.UpdatedAt.After(now.Add(-s.o.StuckAfter)) {
				stuck = append(stuck, r)
			}
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range due {
		if ctx.Err() != nil {
			break
		}
		if s.runScheduled(ctx, r, now) {
			n++
		}
	}
	for _, r := range stuck {
		if ctx.Err() != nil {
			break
		}
		s.o.Log.Warn("release: running a release stuck in publishing again", "release", r.ID.String(), "since", r.UpdatedAt)
		s.runAs(ctx, r.ID, cmp.Or(r.ScheduledBy, r.UpdatedBy), false)
		n++
	}
	return n, nil
}

// runScheduled starts a due release: it moves it to publishing (once, even with several nodes) and runs it as
// the principal who scheduled it.
func (s *Service) runScheduled(ctx context.Context, due store.Release, now time.Time) bool {
	r, err := s.begin(audit.WithActor(ctx, actorOf(due.ScheduledBy)), due.ID, func(r *store.Release) error {
		if r.Status != StatusScheduled || r.ScheduledAt == nil || r.ScheduledAt.After(now) {
			return stateError("not due")
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errNoItems) {
			// Nothing will ever change that on its own (the items were purged): fail it once, with the reason.
			msg := []ItemError{{Code: "no_items", Message: "the release has no items left to publish"}}
			if ok, err := s.fail(audit.WithActor(ctx, actorOf(due.ScheduledBy)), due.ID, msg, StatusScheduled); err != nil || !ok {
				return false
			}
			return true
		}
		var me *manage.Error
		if !errors.As(err, &me) {
			s.o.Log.Error("release: start scheduled release", "release", due.ID.String(), "err", err)
		}
		return false
	}
	s.runAs(ctx, r.ID, due.ScheduledBy, false)
	return true
}

// actorOf parses "kind:id" into an audit actor for records written before the principal is loaded.
func actorOf(s string) audit.Actor {
	kind, id, _ := strings.Cut(s, ":")
	return audit.Actor{Kind: kind, ID: id}
}

// runAs executes a release that is in publishing as the principal behind the actor string. A principal that is
// gone, or no longer holds release.manage, fails the release with forbidden.
func (s *Service) runAs(ctx context.Context, id ids.UUID, actor string, force bool) {
	p := (*access.Principal)(nil)
	if s.o.Auth == nil && s.o.Access != nil && actor != audit.System.String() {
		// Without a way to load the principal nothing may run in its name: only the system is unchecked.
		s.failAs(ctx, id, actor, "the principal who scheduled this release cannot be loaded, so it cannot run")
		return
	}
	if s.o.Auth != nil {
		var err error
		if p, err = s.o.Auth.PrincipalOf(ctx, actor); err != nil {
			if !errors.Is(err, access.ErrUnauthenticated) {
				s.o.Log.Error("release: load principal", "release", id.String(), "actor", actor, "err", err)
				return
			}
			s.failAs(ctx, id, actor, "the principal who scheduled this release no longer exists or may no longer act")
			return
		}
	}
	if p != nil {
		ctx = access.WithPrincipal(ctx, p)
	} else {
		ctx = audit.WithActor(ctx, actorOf(actor))
	}
	if err := s.requireManage(ctx); err != nil {
		s.failAs(ctx, id, actor, "the principal who scheduled this release may no longer manage releases")
		return
	}
	if _, err := s.execute(ctx, id, force); err != nil {
		var pf *PublishFailedError
		var me *manage.Error
		if !errors.As(err, &pf) && !errors.As(err, &me) {
			s.o.Log.Error("release: scheduled publish failed", "release", id.String(), "err", err)
		}
	}
}

func (s *Service) failAs(ctx context.Context, id ids.UUID, actor, msg string) {
	_, _ = s.fail(audit.WithActor(ctx, actorOf(actor)), id, []ItemError{{Code: "forbidden", Message: msg}}, StatusPublishing)
}
