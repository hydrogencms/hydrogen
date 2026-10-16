package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// Reasons Closed is called with.
const (
	ReviewClosedPublished = "published"
	ReviewClosedDiscarded = "discarded"
	ReviewClosedTrashed   = "trashed"
)

// ReviewHook is what the review workflow plugs into the service (Options.Review, SetReview). It is only
// called for types with `review: true`, inside the transaction of the operation.
type ReviewHook interface {
	// CheckPublish runs before a publish takes the draft; draftHash identifies the draft parts (DraftHash).
	// It returns a *ReviewRequiredError when the publish may not go ahead. bypassed says the principal
	// published without the approval it would otherwise need (the audit entry records it).
	CheckPublish(ctx context.Context, tx store.Tx, it store.Item, draftHash string) (bypassed bool, err error)
	// DraftChanged runs after the draft of an existing item changed (an edit, a restored version).
	DraftChanged(ctx context.Context, tx store.Tx, it store.Item) error
	// Closed runs when the draft is published, discarded or the item trashed (the ReviewClosed* reasons).
	Closed(ctx context.Context, tx store.Tx, it store.Item, reason string) error
}

// ErrReviewRequired: publishing needs an approved review. A *ReviewRequiredError is also an ErrConflict and
// answers 409 review_required.
var ErrReviewRequired = errors.New("content: review required")

// ReviewRequiredError says why the draft may not be published.
type ReviewRequiredError struct{ Reason string }

func (e *ReviewRequiredError) Error() string {
	return "content: review required: " + e.Reason
}

func (e *ReviewRequiredError) Is(target error) bool {
	return target == ErrReviewRequired || target == ErrConflict
}

func (e *ReviewRequiredError) APICode() string { return "review_required" }

// SetReview installs the review hook after construction (the review service needs this one). Call it once,
// before serving.
func (s *Service) SetReview(h ReviewHook) {
	if h == nil {
		s.review.Store(nil)
		return
	}
	s.review.Store(&h)
}

// reviewHook is the hook for types with review enabled, else nil.
func (s *Service) reviewHook(st *itemState) ReviewHook {
	if st == nil || !st.typ.Review {
		return nil
	}
	if h := s.review.Load(); h != nil {
		return *h
	}
	return nil
}

// hashDrafts identifies the draft of an item: any change to a part, to the key or to the availability changes it.
func hashDrafts(st *itemState) string {
	if len(st.drafts) == 0 {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%d:%s\x00%t\x00", len(st.item.Key), st.item.Key, st.item.AlwaysAvailable)
	for _, name := range st.draftNames() {
		d := st.drafts[name]
		fmt.Fprintf(h, "%d:%s\x00%d:%s\x00%d:", len(name), name, len(d.Name), d.Name, len(d.Data))
		h.Write(d.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DraftInfo describes the draft of an item for the review workflow.
type DraftInfo struct {
	Hash    string // "" without a draft
	Parts   int
	Editors []string // distinct "kind:id" actors behind the draft parts (UpdatedBy of every part), sorted
}

// DraftInfoTx reads the draft of an item (its key and availability are part of the hash).
func (s *Service) DraftInfoTx(ctx context.Context, tx store.Tx, it store.Item) (DraftInfo, error) {
	st := &itemState{item: it, drafts: map[string]*draft{}}
	if err := st.loadDrafts(ctx, tx.Content()); err != nil {
		return DraftInfo{}, err
	}
	return draftInfo(st), nil
}

func draftInfo(st *itemState) DraftInfo {
	info := DraftInfo{Hash: hashDrafts(st), Parts: len(st.drafts)}
	for _, name := range st.draftNames() {
		if by := st.drafts[name].UpdatedBy; by != "" && !slices.Contains(info.Editors, by) {
			info.Editors = append(info.Editors, by)
		}
	}
	slices.Sort(info.Editors)
	return info
}

// draftChanged tells the review hook that the draft of a loaded item changed outside the edit path.
func (s *Service) draftChanged(ctx context.Context, tx store.Tx, st *itemState) error {
	if h := s.reviewHook(st); h != nil {
		return h.DraftChanged(ctx, tx, st.item)
	}
	return nil
}

// ListableTypes keeps the types whose items the principal in ctx may be able to read at all (a type it has no
// read grant for is left out), so listings can narrow a query by type. Call Prepare first.
func (s *Service) ListableTypes(ctx context.Context, types []string) ([]string, error) {
	if !s.guarded(ctx) {
		return types, nil
	}
	var out []string
	for _, t := range types {
		scoped, terms, err := s.o.Access.ListScope(ctx, access.PolicyContentRead, t)
		if err != nil {
			return nil, err
		}
		if !scoped || len(terms) > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

// LoadItemTx reads a live item (a trashed one is not found) for the review workflow and authorizes policy
// on it, reading first: an item the principal may not read is not found. An empty policy checks reading only.
// With lock the row is locked for the transaction. Call Prepare before opening the transaction.
func (s *Service) LoadItemTx(ctx context.Context, tx store.Tx, id ids.UUID, policy string, lock bool) (store.Item, error) {
	ctx = store.WithTx(ctx, tx)
	it, err := getItem(ctx, tx, id, lock)
	if err != nil {
		return it, err
	}
	if policy == "" {
		policy = access.PolicyContentRead
	}
	return it, s.authorizeItem(ctx, tx.Content(), it, policy, nil)
}

// AllowedTx says which of the policies the principal in ctx holds on the item (all of them without a
// principal, and for super principals). Call Prepare before opening the transaction.
func (s *Service) AllowedTx(ctx context.Context, tx store.Tx, it store.Item, policies ...string) (map[string]bool, error) {
	out := make(map[string]bool, len(policies))
	if !s.guarded(ctx) {
		for _, p := range policies {
			out[p] = true
		}
		return out, nil
	}
	ctx = store.WithTx(ctx, tx)
	st, err := s.newState(it)
	if err != nil {
		return nil, err
	}
	if err := st.load(ctx, tx.Content()); err != nil {
		return nil, err
	}
	doc, err := s.ruleDoc(st, true)
	if err != nil {
		return nil, err
	}
	return s.o.Access.Allowed(ctx, s.targetOf(st, nil, doc), policies...)
}

// ReviewTypes lists the types with review enabled, sorted.
func (s *Service) ReviewTypes() []string {
	var out []string
	for _, t := range s.o.Schema.Types() {
		if t.Review {
			out = append(out, t.Name)
		}
	}
	slices.Sort(out)
	return out
}
