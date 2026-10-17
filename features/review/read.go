package review

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// View is the review of an item as the API shows it.
type View struct {
	State           string            `json:"state"`
	Required        bool              `json:"required"`
	SubmittedBy     string            `json:"submittedBy"`
	SubmittedByName string            `json:"submittedByName"`
	SubmittedAt     *time.Time        `json:"submittedAt"`
	Reviewers       []string          `json:"reviewers"`
	ReviewerNames   map[string]string `json:"reviewerNames"`
	DecidedBy       string            `json:"decidedBy"`
	DecidedByName   string            `json:"decidedByName"`
	DecidedAt       *time.Time        `json:"decidedAt"`
	CanSubmit       bool              `json:"canSubmit"`
	CanApprove      bool              `json:"canApprove"`
	CanWithdraw     bool              `json:"canWithdraw"`
	// DraftHash identifies the draft as it is now; send it back on approve to approve what you saw.
	DraftHash string      `json:"draftHash"`
	Events    []EventView `json:"events"`
}

// EventView is one entry of the thread, oldest first.
type EventView struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Comment   string          `json:"comment"`
	Actor     string          `json:"actor"`
	ActorName string          `json:"actorName"`
	At        time.Time       `json:"at"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// Row is an entry of the review queue.
type Row struct {
	Item            string     `json:"item"`
	Type            string     `json:"type"`
	Name            string     `json:"name"`
	State           string     `json:"state"`
	SubmittedBy     string     `json:"submittedBy"`
	SubmittedByName string     `json:"submittedByName"`
	SubmittedAt     *time.Time `json:"submittedAt"`
	Reviewers       []string   `json:"reviewers"`
	// ReviewerNames resolves Reviewers (ref to name), like the item view.
	ReviewerNames map[string]string `json:"reviewerNames"`
	DecidedBy     string            `json:"decidedBy"`
	DecidedByName string            `json:"decidedByName"`
	DecidedAt     *time.Time        `json:"decidedAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// QueueOptions filters the queue.
type QueueOptions struct {
	// States defaults to in_review, changes_requested and approved.
	States []string
	Type   string
	// Me keeps the reviews that name the principal (or one of its groups) as a reviewer.
	Me     bool
	Limit  int // default 50, at most 200
	Offset int
}

// Get returns the review of an item. Needs read access to the item.
func (s *Service) Get(ctx context.Context, id ids.UUID) (*View, error) {
	if err := s.o.Content.Prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := s.o.Content.LoadItemTx(ctx, tx, id, "", false)
		if err != nil {
			return err
		}
		v, err = s.view(ctx, tx, it)
		return err
	})
	return v, err
}

func (s *Service) view(ctx context.Context, tx store.Tx, it store.Item) (*View, error) {
	cur, err := s.current(ctx, tx, it.ID)
	if err != nil {
		return nil, err
	}
	evs, err := tx.Reviews().Events(ctx, it.ID, maxEvents, ids.Nil)
	if err != nil {
		return nil, err
	}
	slices.Reverse(evs)
	actors := []string{cur.SubmittedBy, cur.DecidedBy}
	for _, e := range evs {
		actors = append(actors, e.Actor)
	}
	names, err := s.names(ctx, tx, append(actors, cur.Reviewers...)...)
	if err != nil {
		return nil, err
	}
	required := false
	if t, ok := s.o.Content.Schema().Type(it.Type); ok {
		required = t.Review
	}
	v := &View{State: cur.State, Required: required, SubmittedBy: cur.SubmittedBy, SubmittedByName: names[cur.SubmittedBy],
		SubmittedAt: cur.SubmittedAt, Reviewers: nonNil(cur.Reviewers), ReviewerNames: map[string]string{},
		DecidedBy: cur.DecidedBy, DecidedByName: names[cur.DecidedBy], DecidedAt: cur.DecidedAt, Events: make([]EventView, 0, len(evs))}
	for _, r := range v.Reviewers {
		v.ReviewerNames[r] = names[r]
	}
	for _, e := range evs {
		v.Events = append(v.Events, EventView{ID: e.ID.String(), Kind: e.Kind, Comment: e.Comment, Actor: e.Actor,
			ActorName: names[e.Actor], At: e.At, Data: e.Data})
	}
	if !required {
		return v, nil
	}
	got, err := s.o.Content.AllowedTx(ctx, tx, it, access.PolicyReviewSubmit, access.PolicyReviewApprove, access.PolicyReviewBypass)
	if err != nil {
		return nil, err
	}
	info, err := s.o.Content.DraftInfoTx(ctx, tx, it)
	if err != nil {
		return nil, err
	}
	v.DraftHash = info.Hash
	stale := cur.State == StateApproved && cur.DraftHash != info.Hash
	v.CanSubmit = got[access.PolicyReviewSubmit] && info.Parts > 0 && (cur.State == StateNone || cur.State == StateChangesRequested || stale)
	if got[access.PolicyReviewApprove] && cur.State == StateInReview {
		blocked, err := s.blocked(ctx, tx, cur, info, got[access.PolicyReviewBypass])
		if err != nil {
			return nil, err
		}
		v.CanApprove = !blocked
	}
	v.CanWithdraw = cur.State != StateNone && (cur.SubmittedBy == s.actor(ctx) || got[access.PolicyReviewApprove])
	return v, nil
}

// names resolves display names of "kind:id" refs: a user's name (else login), a token's or group's name; ""
// when unknown or gone. Each lookup finishes before the next starts.
func (s *Service) names(ctx context.Context, tx store.Tx, refs ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range refs {
		if _, done := out[a]; done {
			continue
		}
		out[a] = ""
		kind, raw, ok := strings.Cut(a, ":")
		id, err := ids.Parse(raw)
		if !ok || err != nil {
			continue
		}
		switch kind {
		case "user":
			u, err := tx.Auth().GetUser(ctx, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			out[a] = cmp.Or(u.Name, u.Login)
		case "group":
			g, err := tx.Auth().GetGroup(ctx, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			out[a] = g.Name
		case "token", "agent":
			t, err := tx.Auth().GetToken(ctx, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			out[a] = t.Name
		}
	}
	return out, nil
}

// reviewerRefs are the refs a principal is addressed by: itself (users only) and its groups.
func reviewerRefs(ctx context.Context) []string {
	p := access.PrincipalFrom(ctx)
	if p == nil {
		return nil
	}
	var out []string
	if p.Kind == access.KindUser && !p.ID.IsZero() {
		out = append(out, "user:"+p.ID.String())
	}
	for _, g := range p.Groups {
		out = append(out, "group:"+g.String())
	}
	return out
}

// Queue lists the reviews the principal can read, newest submission first. Items it may not read are left
// out. It stops scanning once it has seen one readable row past the page, so hasMore is exact and total is
// exact only when hasMore is false (otherwise offset+limit+1, a lower bound). The scan covers at most a few
// thousand reviews.
func (s *Service) Queue(ctx context.Context, o QueueOptions) (rows []Row, total int, hasMore bool, err error) {
	states := o.States
	if len(states) == 0 {
		states = []string{StateInReview, StateChangesRequested, StateApproved}
	}
	for _, st := range states {
		if !slices.Contains([]string{StateNone, StateInReview, StateApproved, StateChangesRequested}, st) {
			return nil, 0, false, manage.BadRequest("unknown state %q", st)
		}
	}
	f := store.ReviewFilter{States: states}
	if o.Me {
		if f.Reviewers = reviewerRefs(ctx); len(f.Reviewers) == 0 {
			return []Row{}, 0, false, nil
		}
	}
	limit := min(max(cmp.Or(o.Limit, 50), 1), maxPage)
	offset := max(o.Offset, 0)
	if err := s.o.Content.Prepare(ctx); err != nil {
		return nil, 0, false, err
	}
	// Narrow by type in the query: only review types, only those the principal may read at all.
	types, err := s.o.Content.ListableTypes(ctx, s.o.Content.ReviewTypes())
	if err != nil {
		return nil, 0, false, err
	}
	if o.Type != "" {
		types = slices.DeleteFunc(types, func(t string) bool { return t != o.Type })
	}
	if len(types) == 0 {
		return []Row{}, 0, false, nil
	}
	f.Types = types
	need := offset + limit + 1
	out := []Row{}
	err = s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		var seen []store.ReviewRow
		size := min(scanPage, max(need, 25))
		for off := 0; off < maxScan && len(seen) < need; off += size {
			page, _, err := tx.Reviews().List(ctx, f, store.Page{Limit: size, Offset: off})
			if err != nil {
				return err
			}
			raw := len(page)
			if page, err = s.readable(ctx, tx, page); err != nil {
				return err
			}
			seen = append(seen, page...)
			if raw < size {
				break
			}
		}
		total, hasMore = min(len(seen), need), len(seen) > offset+limit
		var err error
		out, err = s.rows(ctx, tx, seen[min(offset, len(seen)):min(offset+limit, len(seen))])
		return err
	})
	return out, total, hasMore, err
}

// readable keeps the rows whose items the principal may read.
func (s *Service) readable(ctx context.Context, tx store.Tx, rows []store.ReviewRow) ([]store.ReviewRow, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	list := make([]ids.UUID, len(rows))
	for i, r := range rows {
		list[i] = r.Item
	}
	items, err := tx.Content().GetItems(ctx, list)
	if err != nil {
		return nil, err
	}
	byID := make(map[ids.UUID]store.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := rows[:0:0]
	for _, r := range rows {
		it, ok := byID[r.Item]
		if !ok || it.DeletedAt != nil {
			continue
		}
		if ok, err := s.o.Content.CanReadTx(ctx, tx, it); err != nil {
			return nil, err
		} else if ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Service) rows(ctx context.Context, tx store.Tx, in []store.ReviewRow) ([]Row, error) {
	list := make([]ids.UUID, len(in))
	var actors []string
	for i, r := range in {
		list[i] = r.Item
		actors = append(actors, r.SubmittedBy, r.DecidedBy)
		actors = append(actors, r.Reviewers...)
	}
	nameOf, err := tx.Content().MainNamesOf(ctx, list)
	if err != nil {
		return nil, err
	}
	names, err := s.names(ctx, tx, actors...)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(in))
	for _, r := range in {
		rn := make(map[string]string, len(r.Reviewers))
		for _, ref := range r.Reviewers {
			rn[ref] = names[ref]
		}
		out = append(out, Row{Item: r.Item.String(), Type: r.Type, Name: nameOf[r.Item], State: r.State,
			SubmittedBy: r.SubmittedBy, SubmittedByName: names[r.SubmittedBy], SubmittedAt: r.SubmittedAt,
			Reviewers: nonNil(r.Reviewers), ReviewerNames: rn, DecidedBy: r.DecidedBy, DecidedByName: names[r.DecidedBy],
			DecidedAt: r.DecidedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
