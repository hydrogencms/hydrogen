// Package review is the review workflow of content types with `review: true`: a draft is submitted, approved
// or sent back with comments, and publishing it needs an approved review unless the publisher holds
// review.bypass. Contract: docs/reviews.md.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
)

// States of a review.
const (
	StateNone             = "none"
	StateInReview         = "in_review"
	StateApproved         = "approved"
	StateChangesRequested = "changes_requested"
)

// Kinds of thread events.
const (
	EventSubmitted        = "submitted"
	EventResubmitted      = "resubmitted"
	EventApproved         = "approved"
	EventChangesRequested = "changes_requested"
	EventWithdrawn        = "withdrawn"
	EventReopened         = "reopened"
	EventEdited           = "edited"
	EventComment          = "comment"
	EventPublished        = "published"
	EventDiscarded        = "discarded"
)

// Outbox topics, also the audit actions.
const (
	TopicSubmitted        = "review.submitted"
	TopicApproved         = "review.approved"
	TopicChangesRequested = "review.changes_requested"
	TopicWithdrawn        = "review.withdrawn"
)

const (
	maxComment   = 4000
	maxReviewers = 20
	maxEvents    = 200
	maxScan      = 5000
	scanPage     = 200
	maxPage      = 200
)

// Options configures New.
type Options struct {
	DB      store.DB
	Content *content.Service
	// Access authorizes the principal in the context; nil lets everything pass (tests without access control).
	Access *access.Checker
	Outbox *outbox.Outbox
	Audit  *audit.Log
	Clock  clock.Clock
	IDs    *ids.Generator
	Log    *slog.Logger
	// FourEyes keeps the principal who submitted or last edited a draft from approving it (unless they hold
	// review.bypass).
	FourEyes bool
}

// Service is the review service. It is safe for concurrent use and implements content.ReviewHook.
type Service struct{ o Options }

var _ content.ReviewHook = (*Service)(nil)

// New returns the review service.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("review: DB is required")
	case o.Content == nil:
		return nil, errors.New("review: Content is required")
	case o.Outbox == nil || o.Audit == nil || o.Clock == nil || o.IDs == nil:
		return nil, errors.New("review: Outbox, Audit, Clock and IDs are required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{o: o}, nil
}

func stateError(format string, args ...any) *manage.Error {
	return manage.Conflict("review_state", format, args...)
}

func fourEyesError() *manage.Error {
	return &manage.Error{Status: http.StatusForbidden, Code: "four_eyes",
		Message: "you submitted or last edited this draft: another reviewer has to approve it"}
}

func (s *Service) actor(ctx context.Context) string { return audit.ActorFrom(ctx).String() }

// required fails for a type without review.
func (s *Service) required(it store.Item) error {
	if t, ok := s.o.Content.Schema().Type(it.Type); !ok || !t.Review {
		return manage.BadRequest("type %q does not use reviews (set review: true in its schema)", it.Type)
	}
	return nil
}

func validComment(c string, required bool) (string, error) {
	c = strings.TrimSpace(c)
	switch {
	case required && c == "":
		return "", manage.BadRequest("a comment is required")
	case utf8.RuneCountInString(c) > maxComment:
		return "", manage.BadRequest("the comment is longer than %d characters", maxComment)
	}
	return c, nil
}

// SubmitInput submits a draft.
type SubmitInput struct {
	Comment string
	// Reviewers are "user:<id>" or "group:<id>" refs; informational (the "assigned to me" queue). A
	// resubmission without reviewers keeps the previous ones.
	Reviewers []string
}

func (s *Service) validReviewers(ctx context.Context, tx store.Tx, in []string) ([]string, error) {
	if len(in) > maxReviewers {
		return nil, manage.BadRequest("at most %d reviewers", maxReviewers)
	}
	var out []string
	for _, ref := range in {
		kind, raw, ok := strings.Cut(ref, ":")
		id, err := ids.Parse(raw)
		if !ok || err != nil || (kind != "user" && kind != "group") {
			return nil, manage.BadRequest("reviewer %q must be user:<id> or group:<id>", ref)
		}
		ref = kind + ":" + id.String()
		if slices.Contains(out, ref) {
			continue
		}
		if kind == "user" {
			_, err = tx.Auth().GetUser(ctx, id)
		} else {
			_, err = tx.Auth().GetGroup(ctx, id)
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil, manage.BadRequest("reviewer %q does not exist", ref)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}

// mutate loads the item locked, authorizes policy ("" reads only) and runs fn in one transaction.
func (s *Service) mutate(ctx context.Context, id ids.UUID, policy string,
	fn func(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview) error) (*View, error) {
	if err := s.o.Content.Prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := s.o.Content.LoadItemTx(ctx, tx, id, policy, true)
		if err != nil {
			return err
		}
		if err := s.required(it); err != nil {
			return err
		}
		cur, err := s.current(ctx, tx, it.ID)
		if err != nil {
			return err
		}
		if err := fn(ctx, tx, it, cur); err != nil {
			return err
		}
		v, err = s.view(ctx, tx, it)
		return err
	})
	return v, err
}

// current is the stored review, or a "none" one for an item without.
func (s *Service) current(ctx context.Context, tx store.Tx, id ids.UUID) (store.ItemReview, error) {
	r, err := tx.Reviews().Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.ItemReview{Item: id, State: StateNone}, nil
	}
	return r, err
}

// save writes the review: a transition guarded by the state it was read with, or a first write.
func (s *Service) save(ctx context.Context, tx store.Tx, before, r store.ItemReview) error {
	r.UpdatedAt = s.o.Clock.Now()
	err := tx.Reviews().Transition(ctx, r, []string{before.State})
	switch {
	case errors.Is(err, store.ErrStateChanged):
		return stateError("the review changed state meanwhile; reload it and try again")
	case errors.Is(err, store.ErrNotFound):
		return tx.Reviews().Put(ctx, r)
	}
	return err
}

func (s *Service) addEvent(ctx context.Context, tx store.Tx, item ids.UUID, kind, comment string, data any) error {
	e := store.ReviewEvent{ID: s.o.IDs.New(), Item: item, Kind: kind, Comment: comment, Actor: s.actor(ctx), At: s.o.Clock.Now()}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		e.Data = raw
	}
	return tx.Reviews().AddEvent(ctx, e)
}

type eventPayload struct {
	Item      ids.UUID `json:"item"`
	Type      string   `json:"type"`
	State     string   `json:"state"`
	Actor     string   `json:"actor"`
	Reviewers []string `json:"reviewers"`
}

// announce records the audit entry and the outbox event of a decision.
func (s *Service) announce(ctx context.Context, tx store.Tx, topic string, it store.Item, r store.ItemReview) error {
	p := eventPayload{Item: it.ID, Type: it.Type, State: r.State, Actor: s.actor(ctx), Reviewers: nonNil(r.Reviewers)}
	if err := s.o.Audit.Write(ctx, tx, audit.Entry{Actor: audit.ActorFrom(ctx), Action: topic, Subject: it.ID.String(), Data: p}); err != nil {
		return fmt.Errorf("review: audit %s: %w", topic, err)
	}
	if _, err := s.o.Outbox.Publish(ctx, tx, topic, p); err != nil {
		return fmt.Errorf("review: outbox %s: %w", topic, err)
	}
	return nil
}

// Submit sends the item's draft for review (a first submission, or after changes were requested).
func (s *Service) Submit(ctx context.Context, id ids.UUID, in SubmitInput) (*View, error) {
	comment, err := validComment(in.Comment, false)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, access.PolicyReviewSubmit, func(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview) error {
		info, err := s.o.Content.DraftInfoTx(ctx, tx, it)
		if err != nil {
			return err
		}
		// An approval of another draft than the current one (a change nobody reported) can be submitted again.
		staleApproval := cur.State == StateApproved && cur.DraftHash != info.Hash
		if cur.State == StateInReview || (cur.State == StateApproved && !staleApproval) {
			return stateError("the draft is already %s", strings.ReplaceAll(cur.State, "_", " "))
		}
		if info.Parts == 0 {
			return stateError("the item has no draft changes to review")
		}
		reviewers, err := s.validReviewers(ctx, tx, in.Reviewers)
		if err != nil {
			return err
		}
		kind, now := EventSubmitted, s.o.Clock.Now()
		next := store.ItemReview{Item: it.ID, State: StateInReview, DraftHash: info.Hash, SubmittedBy: s.actor(ctx),
			SubmittedAt: &now, Reviewers: reviewers}
		if cur.State != StateNone {
			kind = EventResubmitted
			if len(in.Reviewers) == 0 {
				next.Reviewers = cur.Reviewers
			}
		}
		if err := s.save(ctx, tx, cur, next); err != nil {
			return err
		}
		if err := s.addEvent(ctx, tx, it.ID, kind, comment, nil); err != nil {
			return err
		}
		return s.announce(ctx, tx, TopicSubmitted, it, next)
	})
}

// identities are the refs an actor stands for: itself and, for a token or agent, whoever created it (and
// who created that), so that a token is the same person as its creator for four eyes.
func (s *Service) identities(ctx context.Context, tx store.Tx, refs ...string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, ref := range refs {
		for depth := 0; ref != "" && !out[ref] && depth < 4; depth++ {
			out[ref] = true
			kind, raw, ok := strings.Cut(ref, ":")
			id, err := ids.Parse(raw)
			if !ok || err != nil || (kind != "token" && kind != "agent") {
				break
			}
			t, err := tx.Auth().GetToken(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				break
			}
			if err != nil {
				return nil, err
			}
			ref = t.CreatedBy
			if ref == audit.System.String() {
				break // created by code, not by a person
			}
		}
	}
	return out, nil
}

// blocked says whether four-eyes keeps the principal from deciding the review: the principal (or its token's
// creator) submitted the draft or edited any part of it.
func (s *Service) blocked(ctx context.Context, tx store.Tx, cur store.ItemReview, info content.DraftInfo, bypass bool) (bool, error) {
	if !s.o.FourEyes || bypass {
		return false, nil
	}
	p := access.PrincipalFrom(ctx)
	if p == nil || p.Kind == access.KindSystem {
		return false, nil
	}
	mine, err := s.identities(ctx, tx, s.actor(ctx))
	if err != nil {
		return false, err
	}
	theirs, err := s.identities(ctx, tx, append([]string{cur.SubmittedBy}, info.Editors...)...)
	if err != nil {
		return false, err
	}
	for ref := range mine {
		if theirs[ref] {
			return true, nil
		}
	}
	return false, nil
}

// ApproveInput approves a draft.
type ApproveInput struct {
	Comment string
	// DraftHash is the draft the reviewer looked at (View.DraftHash); when it differs from the current draft
	// the approval is refused with 409 review_stale. Empty: no check.
	DraftHash string
}

func staleError() *manage.Error {
	return manage.Conflict("review_stale", "the draft changed since you opened it; reload and review it again")
}

// Approve approves the draft under review. The principal who submitted or edited it (or whose token did) may
// not (four eyes), unless they hold review.bypass.
func (s *Service) Approve(ctx context.Context, id ids.UUID, in ApproveInput) (*View, error) {
	comment, err := validComment(in.Comment, false)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, access.PolicyReviewApprove, func(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview) error {
		if cur.State != StateInReview {
			return stateError("only a draft in review can be approved")
		}
		info, err := s.o.Content.DraftInfoTx(ctx, tx, it)
		if err != nil {
			return err
		}
		allowed, err := s.o.Content.AllowedTx(ctx, tx, it, access.PolicyReviewBypass)
		if err != nil {
			return err
		}
		if blocked, err := s.blocked(ctx, tx, cur, info, allowed[access.PolicyReviewBypass]); err != nil {
			return err
		} else if blocked {
			return fourEyesError()
		}
		if in.DraftHash != "" && in.DraftHash != info.Hash {
			return staleError()
		}
		return s.decide(ctx, tx, it, cur, info, StateApproved, EventApproved, TopicApproved, comment)
	})
}

// RequestChanges sends the draft back to its editors; the comment says what to change.
func (s *Service) RequestChanges(ctx context.Context, id ids.UUID, comment string) (*View, error) {
	comment, err := validComment(comment, true)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, access.PolicyReviewApprove, func(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview) error {
		if cur.State != StateInReview {
			return stateError("changes can only be requested on a draft in review")
		}
		info, err := s.o.Content.DraftInfoTx(ctx, tx, it)
		if err != nil {
			return err
		}
		return s.decide(ctx, tx, it, cur, info, StateChangesRequested, EventChangesRequested, TopicChangesRequested, comment)
	})
}

func (s *Service) decide(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview, info content.DraftInfo,
	state, kind, topic, comment string) error {
	now := s.o.Clock.Now()
	next := cur
	next.State, next.DraftHash, next.DecidedBy, next.DecidedAt = state, info.Hash, s.actor(ctx), &now
	if err := s.save(ctx, tx, cur, next); err != nil {
		return err
	}
	if err := s.addEvent(ctx, tx, it.ID, kind, comment, nil); err != nil {
		return err
	}
	return s.announce(ctx, tx, topic, it, next)
}

// Withdraw takes a draft out of review (in review, approved or sent back). The principal who submitted it
// can, and anyone with review.approve.
func (s *Service) Withdraw(ctx context.Context, id ids.UUID, comment string) (*View, error) {
	comment, err := validComment(comment, false)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, "", func(ctx context.Context, tx store.Tx, it store.Item, cur store.ItemReview) error {
		if cur.State == StateNone {
			return stateError("there is no review to withdraw")
		}
		if cur.SubmittedBy != s.actor(ctx) {
			if _, err := s.o.Content.LoadItemTx(ctx, tx, it.ID, access.PolicyReviewApprove, false); err != nil {
				return err
			}
		}
		next := store.ItemReview{Item: it.ID, State: StateNone}
		if err := s.save(ctx, tx, cur, next); err != nil {
			return err
		}
		if err := s.addEvent(ctx, tx, it.ID, EventWithdrawn, comment, nil); err != nil {
			return err
		}
		return s.announce(ctx, tx, TopicWithdrawn, it, next)
	})
}

// Comment adds a comment to the thread without changing the state. Needs review.submit or review.approve.
func (s *Service) Comment(ctx context.Context, id ids.UUID, comment string) (*View, error) {
	comment, err := validComment(comment, true)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, "", func(ctx context.Context, tx store.Tx, it store.Item, _ store.ItemReview) error {
		got, err := s.o.Content.AllowedTx(ctx, tx, it, access.PolicyReviewSubmit, access.PolicyReviewApprove)
		if err != nil {
			return err
		}
		if !got[access.PolicyReviewSubmit] && !got[access.PolicyReviewApprove] {
			return fmt.Errorf("%w: %s", access.ErrForbidden, access.PolicyReviewSubmit)
		}
		return s.addEvent(ctx, tx, it.ID, EventComment, comment, nil)
	})
}

// CheckPublish implements content.ReviewHook: the draft needs an approval of exactly these parts, or the
// publisher the right to publish without one.
func (s *Service) CheckPublish(ctx context.Context, tx store.Tx, it store.Item, draftHash string) (bool, error) {
	cur, err := s.current(ctx, tx, it.ID)
	if err != nil {
		return false, err
	}
	reason := ""
	switch {
	case cur.State == StateApproved && cur.DraftHash == draftHash:
		return false, nil
	case cur.State == StateApproved:
		reason = "the draft changed after it was approved"
	case cur.State == StateInReview:
		reason = "the draft is waiting for a review"
	case cur.State == StateChangesRequested:
		reason = "changes were requested on the draft"
	default:
		reason = "the draft has not been submitted for review"
	}
	allowed, err := s.o.Content.AllowedTx(ctx, tx, it, access.PolicyReviewBypass)
	if err != nil {
		return false, err
	}
	if allowed[access.PolicyReviewBypass] {
		return true, nil
	}
	return false, &content.ReviewRequiredError{Reason: reason}
}

// DraftChanged implements content.ReviewHook: an edit after approval reopens the review, and an edit while it
// waits for a decision is noted in the thread (once per run of edits by the same actor).
func (s *Service) DraftChanged(ctx context.Context, tx store.Tx, it store.Item) error {
	cur, err := tx.Reviews().Get(ctx, it.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	switch cur.State {
	case StateInReview:
		return s.noteEdit(ctx, tx, it.ID, EventEdited)
	case StateApproved:
		next := cur
		next.State, next.DraftHash, next.DecidedBy, next.DecidedAt = StateInReview, "", "", nil
		if err := s.save(ctx, tx, cur, next); err != nil {
			if errors.As(err, new(*manage.Error)) {
				return nil // another transaction already moved it
			}
			return err
		}
		return s.addEvent(ctx, tx, it.ID, EventReopened, "", nil)
	}
	return nil
}

// noteEdit adds an edited event unless the thread already ends in an edit or reopening by the same actor.
func (s *Service) noteEdit(ctx context.Context, tx store.Tx, item ids.UUID, kind string) error {
	last, err := tx.Reviews().Events(ctx, item, 1, ids.Nil)
	if err != nil {
		return err
	}
	if len(last) == 1 && last[0].Actor == s.actor(ctx) && (last[0].Kind == EventEdited || last[0].Kind == EventReopened) {
		return nil
	}
	return s.addEvent(ctx, tx, item, kind, "", nil)
}

// Closed implements content.ReviewHook: the review ends with the draft it was about; the thread stays.
func (s *Service) Closed(ctx context.Context, tx store.Tx, it store.Item, reason string) error {
	cur, err := tx.Reviews().Get(ctx, it.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && cur.State == StateNone) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.save(ctx, tx, cur, store.ItemReview{Item: it.ID, State: StateNone}); err != nil {
		return err
	}
	kind := EventDiscarded
	if reason == content.ReviewClosedPublished {
		kind = EventPublished
	}
	return s.addEvent(ctx, tx, it.ID, kind, "", map[string]string{"reason": reason})
}
