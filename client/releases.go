package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ReleaseError is one reason a release did not publish; Item is empty for problems of the release itself.
type ReleaseError struct {
	Item    string `json:"item,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// ReleaseItem is one item of a release as the detail view shows it.
type ReleaseItem struct {
	Item    string    `json:"item"`
	Type    string    `json:"type"`
	Name    string    `json:"name"`
	Action  string    `json:"action"` // publish or unpublish
	Locales []string  `json:"locales"`
	State   string    `json:"state"` // changes, current, trashed, missing or hidden
	AddedBy string    `json:"addedBy"`
	AddedAt time.Time `json:"addedAt"`
}

// Release groups item actions that are published together, now or at ScheduledAt.
type Release struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Kind          string         `json:"kind"`   // release or item (a one-item schedule)
	Status        string         `json:"status"` // open, scheduled, publishing, published, failed, cancelled
	ScheduledAt   *time.Time     `json:"scheduledAt"`
	PublishedAt   *time.Time     `json:"publishedAt"`
	Errors        []ReleaseError `json:"errors"`
	CreatedBy     string         `json:"createdBy"`
	CreatedByName string         `json:"createdByName"`
	UpdatedBy     string         `json:"updatedBy"`
	ScheduledBy   string         `json:"scheduledBy"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	ItemCount     int            `json:"itemCount"`
	// Items is set by [Manage.Release] and the actions that return the detail view, not by listings.
	Items []ReleaseItem `json:"items"`
	// Action and Locales are set by [Manage.ItemReleases]: what the release does with that item.
	Action  string   `json:"action"`
	Locales []string `json:"locales"`
}

// ReleaseList is a page of releases.
type ReleaseList struct {
	Items   []Release `json:"items"`
	Total   int       `json:"total"`
	HasMore bool      `json:"hasMore"`
}

// ReleaseListOptions filters [Manage.Releases].
type ReleaseListOptions struct {
	Statuses []string
	Kind     string
	Item     string // only releases that contain this item
	Limit    int
	Offset   int
}

// ReleaseItemInput is an item for a release: Action is publish (default) or unpublish; Locales limits a publish.
type ReleaseItemInput struct {
	Item    string   `json:"item"`
	Action  string   `json:"action,omitempty"`
	Locales []string `json:"locales,omitempty"`
}

// CreateReleaseInput creates a release; Kind is release (default) or item. With ScheduledAt the release is
// scheduled at once.
type CreateReleaseInput struct {
	Name        string
	Description string
	Kind        string
	Items       []ReleaseItemInput
	ScheduledAt *time.Time
}

// Releases lists releases, newest first.
func (m *Manage) Releases(ctx context.Context, o *ReleaseListOptions) (*ReleaseList, error) {
	q := &query{}
	if o != nil {
		if len(o.Statuses) > 0 {
			q.add("status", strings.Join(o.Statuses, ","))
		}
		if o.Kind != "" {
			q.add("kind", o.Kind)
		}
		if o.Item != "" {
			q.add("item", o.Item)
		}
		if o.Limit > 0 {
			q.add("limit", strconv.Itoa(o.Limit))
		}
		if o.Offset > 0 {
			q.add("offset", strconv.Itoa(o.Offset))
		}
	}
	_, raw, err := m.c.call(ctx, http.MethodGet, "/releases", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out ReleaseList
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ItemReleases lists the open and scheduled releases that contain an item.
func (m *Manage) ItemReleases(ctx context.Context, item string) ([]Release, error) {
	_, raw, err := m.c.call(ctx, http.MethodGet, "/items/"+url.PathEscape(item)+"/releases", "", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out ReleaseList
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Release reads a release with its items.
func (m *Manage) Release(ctx context.Context, id string) (*Release, error) {
	return m.release(ctx, http.MethodGet, "/releases/"+url.PathEscape(id), nil)
}

// CreateRelease creates a release (needs release.manage).
func (m *Manage) CreateRelease(ctx context.Context, in CreateReleaseInput) (*Release, error) {
	body := map[string]any{"name": in.Name}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Kind != "" {
		body["kind"] = in.Kind
	}
	if len(in.Items) > 0 {
		body["items"] = in.Items
	}
	if in.ScheduledAt != nil {
		body["scheduledAt"] = in.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	return m.release(ctx, http.MethodPost, "/releases", body)
}

// UpdateRelease changes the name or description; nil fields stay.
func (m *Manage) UpdateRelease(ctx context.Context, id string, name, description *string) (*Release, error) {
	body := map[string]any{}
	if name != nil {
		body["name"] = *name
	}
	if description != nil {
		body["description"] = *description
	}
	return m.release(ctx, http.MethodPatch, "/releases/"+url.PathEscape(id), body)
}

// DeleteRelease deletes an open or cancelled release.
func (m *Manage) DeleteRelease(ctx context.Context, id string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/releases/"+url.PathEscape(id), "", nil, nil, false)
	return err
}

// PutReleaseItem adds an item to a release or replaces its action and locales.
func (m *Manage) PutReleaseItem(ctx context.Context, id string, in ReleaseItemInput) (*Release, error) {
	body := map[string]any{}
	if in.Action != "" {
		body["action"] = in.Action
	}
	if len(in.Locales) > 0 {
		body["locales"] = in.Locales
	}
	return m.release(ctx, http.MethodPut, "/releases/"+url.PathEscape(id)+"/items/"+url.PathEscape(in.Item), body)
}

// RemoveReleaseItem takes an item out of a release.
func (m *Manage) RemoveReleaseItem(ctx context.Context, id, item string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/releases/"+url.PathEscape(id)+"/items/"+url.PathEscape(item), "", nil, nil, false)
	return err
}

// ScheduleRelease publishes the release at a future time as the caller.
func (m *Manage) ScheduleRelease(ctx context.Context, id string, at time.Time) (*Release, error) {
	return m.release(ctx, http.MethodPost, "/releases/"+url.PathEscape(id)+"/schedule",
		map[string]any{"at": at.UTC().Format(time.RFC3339Nano)})
}

// UnscheduleRelease turns a scheduled release back into an open one.
func (m *Manage) UnscheduleRelease(ctx context.Context, id string) (*Release, error) {
	return m.release(ctx, http.MethodPost, "/releases/"+url.PathEscape(id)+"/unschedule", nil)
}

// CancelRelease retires an open or scheduled release.
func (m *Manage) CancelRelease(ctx context.Context, id string) (*Release, error) {
	return m.release(ctx, http.MethodPost, "/releases/"+url.PathEscape(id)+"/cancel", nil)
}

// PublishRelease publishes a release now. When nothing could be published the error is an *Error with code
// "release_failed" and the returned release (status failed) lists the reasons in Errors.
func (m *Manage) PublishRelease(ctx context.Context, id string, force bool) (*Release, error) {
	body := map[string]any{}
	if force {
		body["force"] = true
	}
	rel, err := m.release(ctx, http.MethodPost, "/releases/"+url.PathEscape(id)+"/publish", body)
	var e *Error
	if errors.As(err, &e) && e.Code == "release_failed" {
		if cur, gerr := m.Release(ctx, id); gerr == nil {
			return cur, err
		}
	}
	return rel, err
}

func (m *Manage) release(ctx context.Context, method, path string, body any) (*Release, error) {
	if body == nil && method != http.MethodGet {
		body = map[string]any{}
	}
	_, raw, err := m.c.call(ctx, method, path, "", body, nil, false)
	if err != nil {
		return nil, err
	}
	var r Release
	if err := decode(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
