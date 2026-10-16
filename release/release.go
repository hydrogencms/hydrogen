// Package release groups draft changes of several items into a named release that is published (or
// unpublished) in one transaction, now or at a scheduled time. Contract: docs/releases.md.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

// Statuses of a release.
const (
	StatusOpen       = "open"
	StatusScheduled  = "scheduled"
	StatusPublishing = "publishing"
	StatusPublished  = "published"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// Kinds of a release: a regular one, or the one-item release behind "schedule publish" in the admin.
const (
	KindRelease = "release"
	KindItem    = "item"
)

// Actions of a release item.
const (
	ActionPublish   = "publish"
	ActionUnpublish = "unpublish"
)

// Topics and audit actions.
const (
	TopicPublished = "release.published"
)

const (
	// MaxScheduleAhead is how far in the future a release may be scheduled.
	MaxScheduleAhead = 366 * 24 * time.Hour
	// DefaultStuckAfter is how long a release may stay in publishing before the job runs it again.
	DefaultStuckAfter = 10 * time.Minute

	maxName        = 200
	maxDescription = 2000
	dueBatch       = 50
	maxPage        = 200
)

// Options configures New.
type Options struct {
	DB      store.DB
	Content *content.Service
	// Access authorizes the principal in the context; nil lets everything pass (tests without access control).
	Access *access.Checker
	// Auth rebuilds the principal that scheduled a release when the job runs it. Without it scheduled releases
	// run as the system.
	Auth   *access.Authenticator
	Outbox *outbox.Outbox
	Audit  *audit.Log
	Clock  clock.Clock
	IDs    *ids.Generator
	Log    *slog.Logger
	// StuckAfter is how long a release may stay in publishing (a crashed run) before the job runs it again.
	StuckAfter time.Duration
}

// Service is the release service. It is safe for concurrent use.
type Service struct {
	o  Options
	mu sync.Mutex // serialises state changes of releases in this process
}

// New returns the release service.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("release: DB is required")
	case o.Content == nil:
		return nil, errors.New("release: Content is required")
	case o.Outbox == nil || o.Audit == nil || o.Clock == nil || o.IDs == nil:
		return nil, errors.New("release: Outbox, Audit, Clock and IDs are required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.StuckAfter <= 0 {
		o.StuckAfter = DefaultStuckAfter
	}
	return &Service{o: o}, nil
}

// ItemError is one reason a release did not publish. Item is empty for problems of the release itself.
type ItemError struct {
	Item    string `json:"item,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// View is a release as the API shows it.
type View struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	Kind          string      `json:"kind"`
	Status        string      `json:"status"`
	ScheduledAt   *time.Time  `json:"scheduledAt,omitempty"`
	PublishedAt   *time.Time  `json:"publishedAt,omitempty"`
	Errors        []ItemError `json:"errors"`
	CreatedBy     string      `json:"createdBy"`
	CreatedByName string      `json:"createdByName"`
	UpdatedBy     string      `json:"updatedBy"`
	ScheduledBy   string      `json:"scheduledBy,omitempty"`
	CreatedAt     time.Time   `json:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`
	ItemCount     int         `json:"itemCount"`
	Items         []ItemView  `json:"items,omitempty"`
	Action        string      `json:"action,omitempty"`  // OfItem: what the release does with that item
	Locales       []string    `json:"locales,omitempty"` // OfItem
}

// ItemView is one item of a release with what the editor needs to show it.
type ItemView struct {
	Item    string    `json:"item"`
	Type    string    `json:"type"`
	Name    string    `json:"name"`
	Action  string    `json:"action"`
	Locales []string  `json:"locales"`
	State   string    `json:"state"` // changes, current, trashed, missing or hidden (the caller may not read it)
	AddedBy string    `json:"addedBy"`
	AddedAt time.Time `json:"addedAt"`
}

// Item states of ItemView.
const (
	StateChanges = "changes"
	StateCurrent = "current"
	StateTrashed = "trashed"
	StateMissing = "missing"
	StateHidden  = "hidden"
)

// ItemInput adds an item to a release.
type ItemInput struct {
	Item    ids.UUID
	Action  string // publish (default) or unpublish
	Locales []string
}

// CreateInput creates a release.
type CreateInput struct {
	Name        string
	Description string
	Kind        string // release (default) or item
	Items       []ItemInput
	// ScheduledAt, if set, schedules the new release in the same step.
	ScheduledAt *time.Time
}

// UpdateInput changes a release's text; nil fields stay.
type UpdateInput struct {
	Name        *string
	Description *string
}

// ListOptions filters ListReleases.
type ListOptions struct {
	Statuses []string
	Kind     string
	Item     ids.UUID
	Limit    int // default 50, at most 200
	Offset   int
}

// PublishFailedError is returned by Publish and the job when the release did not publish: nothing was
// published, and Release (status failed) lists the reasons.
type PublishFailedError struct{ Release *View }

func (e *PublishFailedError) Error() string {
	return fmt.Sprintf("release: %q did not publish: %d error(s)", e.Release.Name, len(e.Release.Errors))
}

func stateError(format string, args ...any) *manage.Error {
	return manage.Conflict("release_state", format, args...)
}

func notFound() *manage.Error { return manage.NotFound("release not found") }

func (s *Service) actor(ctx context.Context) string { return audit.ActorFrom(ctx).String() }

func (s *Service) requireManage(ctx context.Context) error {
	if s.o.Access == nil {
		return nil
	}
	return s.o.Access.Holds(ctx, access.PolicyReleaseManage)
}

func (s *Service) requireRead(ctx context.Context) error {
	if s.o.Access == nil {
		return nil
	}
	return s.o.Access.Holds(ctx, access.PolicyContentRead)
}

func (s *Service) prepare(ctx context.Context) error {
	if s.o.Access == nil {
		return nil
	}
	return s.o.Access.Prepare(ctx)
}

func (s *Service) record(ctx context.Context, tx store.Tx, action string, id ids.UUID, data any) error {
	err := s.o.Audit.Write(ctx, tx, audit.Entry{Actor: audit.ActorFrom(ctx), Action: action, Subject: id.String(), Data: data})
	if err != nil {
		return fmt.Errorf("release: audit %s: %w", action, err)
	}
	return nil
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", manage.BadRequest("name is required")
	case utf8.RuneCountInString(name) > maxName:
		return "", manage.BadRequest("name is longer than %d characters", maxName)
	}
	return name, nil
}

func validDescription(d string) (string, error) {
	if utf8.RuneCountInString(d) > maxDescription {
		return "", manage.BadRequest("description is longer than %d characters", maxDescription)
	}
	return d, nil
}

var localeRE = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// normalizeItem validates the action and locales of an item input.
func normalizeItem(in ItemInput) (ItemInput, error) {
	if in.Action == "" {
		in.Action = ActionPublish
	}
	switch in.Action {
	case ActionPublish:
	case ActionUnpublish:
		if len(in.Locales) > 0 {
			return in, manage.BadRequest("locales apply to the publish action only")
		}
	default:
		return in, manage.BadRequest("action must be publish or unpublish")
	}
	if in.Item.IsZero() {
		return in, manage.BadRequest("item is required")
	}
	for _, l := range in.Locales {
		if !localeRE.MatchString(l) {
			return in, manage.BadRequest("invalid locale %q", l)
		}
	}
	in.Locales = slices.Clone(in.Locales)
	slices.Sort(in.Locales)
	in.Locales = slices.Compact(in.Locales)
	return in, nil
}

func (s *Service) checkSchedule(at time.Time) error {
	now := s.o.Clock.Now()
	switch {
	case !at.After(now):
		return manage.BadRequest("the time must be in the future")
	case at.After(now.Add(MaxScheduleAhead)):
		return manage.BadRequest("the time is more than a year ahead")
	}
	return nil
}

// editable says whether items can change in the status; a failed release reopens on an edit.
func editable(status string) bool { return status == StatusOpen || status == StatusFailed }

func loadRelease(ctx context.Context, tx store.Tx, id ids.UUID) (store.Release, error) {
	r, err := tx.Releases().GetRelease(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return r, notFound()
	}
	return r, err
}

// transition stores r (every mutable column) only if the release still has the status it was read with, so a
// change on another node in between is a 409 instead of a lost update. It locks the row until commit.
func transition(ctx context.Context, tx store.Tx, r store.Release, from ...string) error {
	err := tx.Releases().TransitionRelease(ctx, r, from)
	switch {
	case errors.Is(err, store.ErrStateChanged):
		return stateError("the release changed state meanwhile; reload it and try again")
	case errors.Is(err, store.ErrNotFound):
		return notFound()
	}
	return err
}

// reopen clears what a failed (or edited) release carries over: the errors and any schedule.
func reopen(r *store.Release) {
	r.Status = StatusOpen
	r.Errors = nil
	r.ScheduledAt, r.ScheduledBy = nil, ""
}

func decodeErrors(raw json.RawMessage) []ItemError {
	out := []ItemError{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []ItemError{}
	}
	return out
}

func itemRelease(r store.Release, count int, names map[string]string) View {
	return View{ID: r.ID.String(), Name: r.Name, Description: r.Description, Kind: r.Kind, Status: r.Status,
		ScheduledAt: r.ScheduledAt, PublishedAt: r.PublishedAt, Errors: decodeErrors(r.Errors),
		CreatedBy: r.CreatedBy, CreatedByName: names[r.CreatedBy], UpdatedBy: r.UpdatedBy, ScheduledBy: r.ScheduledBy,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ItemCount: count}
}
