package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ReviewEvent is one entry of a review thread.
type ReviewEvent struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // submitted, resubmitted, approved, changes_requested, withdrawn, reopened, edited, comment, published, discarded
	Comment   string    `json:"comment"`
	Actor     string    `json:"actor"`
	ActorName string    `json:"actorName"`
	At        time.Time `json:"at"`
}

// Review is the review of an item's draft.
type Review struct {
	State           string            `json:"state"` // none, in_review, approved or changes_requested
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
	DraftHash       string            `json:"draftHash"` // pass to [ApproveReviewOptions] to approve what was seen
	Events          []ReviewEvent     `json:"events"`    // oldest first
}

// ReviewRow is an entry of the review queue.
type ReviewRow struct {
	Item            string            `json:"item"`
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	State           string            `json:"state"`
	SubmittedBy     string            `json:"submittedBy"`
	SubmittedByName string            `json:"submittedByName"`
	SubmittedAt     *time.Time        `json:"submittedAt"`
	Reviewers       []string          `json:"reviewers"`
	ReviewerNames   map[string]string `json:"reviewerNames"`
	DecidedBy       string            `json:"decidedBy"`
	DecidedByName   string            `json:"decidedByName"`
	DecidedAt       *time.Time        `json:"decidedAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// ReviewList is a page of the review queue.
type ReviewList struct {
	Items   []ReviewRow `json:"items"`
	Total   int         `json:"total"`
	HasMore bool        `json:"hasMore"`
}

// ReviewListOptions filters [Manage.Reviews].
type ReviewListOptions struct {
	States     []string // default: in_review, changes_requested, approved
	Type       string
	ReviewerMe bool // only reviews that name the caller or one of the caller's groups
	Limit      int
	Offset     int
}

// Review reads the review of an item.
func (m *Manage) Review(ctx context.Context, item string) (*Review, error) {
	return m.review(ctx, http.MethodGet, item, "", nil)
}

// SubmitReview submits the item's draft for review; reviewers are "user:<id>" or "group:<id>" refs.
func (m *Manage) SubmitReview(ctx context.Context, item, comment string, reviewers ...string) (*Review, error) {
	body := map[string]any{}
	if comment != "" {
		body["comment"] = comment
	}
	if len(reviewers) > 0 {
		body["reviewers"] = reviewers
	}
	return m.review(ctx, http.MethodPost, item, "/submit", body)
}

// ApproveReview approves the draft under review (needs review.approve; four eyes apply).
func (m *Manage) ApproveReview(ctx context.Context, item, comment string) (*Review, error) {
	return m.review(ctx, http.MethodPost, item, "/approve", commentBody(comment))
}

// ApproveReviewDraft approves the draft identified by draftHash (Review.DraftHash); when the draft changed
// meanwhile the call fails with code review_stale.
func (m *Manage) ApproveReviewDraft(ctx context.Context, item, comment, draftHash string) (*Review, error) {
	body := commentBody(comment)
	body["draftHash"] = draftHash
	return m.review(ctx, http.MethodPost, item, "/approve", body)
}

// ReviewApprover is a user or group that can approve an item.
type ReviewApprover struct {
	Ref  string `json:"ref"` // user:<id> or group:<id>
	Name string `json:"name"`
	Kind string `json:"kind"` // user or group
}

// ReviewApprovers lists the users and groups that can approve the item, sorted by name (needs review.submit).
func (m *Manage) ReviewApprovers(ctx context.Context, item string) ([]ReviewApprover, error) {
	q := &query{}
	q.add("item", item)
	_, raw, err := m.c.call(ctx, http.MethodGet, "/reviews/approvers", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []ReviewApprover `json:"items"`
	}
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// RequestReviewChanges sends the draft back; the comment is required.
func (m *Manage) RequestReviewChanges(ctx context.Context, item, comment string) (*Review, error) {
	return m.review(ctx, http.MethodPost, item, "/request-changes", commentBody(comment))
}

// WithdrawReview takes the draft out of review.
func (m *Manage) WithdrawReview(ctx context.Context, item, comment string) (*Review, error) {
	return m.review(ctx, http.MethodPost, item, "/withdraw", commentBody(comment))
}

// CommentReview adds a comment to the thread.
func (m *Manage) CommentReview(ctx context.Context, item, comment string) (*Review, error) {
	return m.review(ctx, http.MethodPost, item, "/comments", commentBody(comment))
}

// Reviews lists the review queue, newest submission first.
func (m *Manage) Reviews(ctx context.Context, o *ReviewListOptions) (*ReviewList, error) {
	q := &query{}
	if o != nil {
		if len(o.States) > 0 {
			q.add("state", strings.Join(o.States, ","))
		}
		if o.Type != "" {
			q.add("type", o.Type)
		}
		if o.ReviewerMe {
			q.add("reviewer", "me")
		}
		if o.Limit > 0 {
			q.add("limit", strconv.Itoa(o.Limit))
		}
		if o.Offset > 0 {
			q.add("offset", strconv.Itoa(o.Offset))
		}
	}
	_, raw, err := m.c.call(ctx, http.MethodGet, "/reviews", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out ReviewList
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func commentBody(comment string) map[string]any {
	body := map[string]any{}
	if comment != "" {
		body["comment"] = comment
	}
	return body
}

func (m *Manage) review(ctx context.Context, method, item, verb string, body any) (*Review, error) {
	path := "/items/" + url.PathEscape(item) + "/review"
	if verb != "" {
		path += verb
	}
	_, raw, err := m.c.call(ctx, method, path, "", body, nil, false)
	if err != nil {
		return nil, err
	}
	var r Review
	if err := decode(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
