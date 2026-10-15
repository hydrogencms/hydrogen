package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Manage writes through the management API (/api/manage/v1). It is safe for concurrent use.
type Manage struct{ c core }

// NewManage returns a client for the server at baseURL authenticated with the management token.
func NewManage(baseURL, token string, opts ...Option) *Manage {
	c := newCore(baseURL, managePrefix, opts)
	c.s.token = token
	return &Manage{c: c}
}

// View is an item in one locale as the management API shows it.
type View struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Key             string          `json:"key"`
	Locale          string          `json:"locale"`
	MainLocale      string          `json:"mainLocale"`
	AlwaysAvailable bool            `json:"alwaysAvailable"`
	Status          string          `json:"status"` // draft, published, changed
	Version         int             `json:"version"`
	Draft           bool            `json:"draft"`
	Name            string          `json:"name"`
	Fields          json.RawMessage `json:"fields"`
	Locales         []string        `json:"locales"`
	DraftParts      []string        `json:"draftParts"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	PublishedAt     *time.Time      `json:"publishedAt"`
}

// Summary is one row of a listing.
type Summary struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Key        string    `json:"key"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Version    int       `json:"version"`
	Locales    []string  `json:"locales"`
	DraftParts []string  `json:"draftParts"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Fields decodes a view's fields into T, typically a struct from gen/content.
func Fields[T any](v *View) (T, error) {
	var t T
	if err := decode(v.Fields, &t); err != nil {
		return t, err
	}
	return t, nil
}

// CreateInput creates a draft item.
type CreateInput struct {
	Locale          string // default: the server's default locale
	Key             string
	AlwaysAvailable bool
	Fields          any // a map or a generated struct; it is sent as JSON
}

// UpdateInput changes a draft. Fields is a JSON merge patch: a map entry set to nil removes the field. With a
// struct, omitempty fields that are empty are left alone.
type UpdateInput struct {
	Locale          string
	Fields          any
	Key             *string
	AlwaysAvailable *bool
}

// PublishInput chooses what to publish; the zero value publishes every draft part.
type PublishInput struct {
	Locales []string
	Force   bool // publish over conflicts
}

// ManageListOptions are the list options plus the status filter of the management API.
type ManageListOptions struct {
	ListOptions
	Status string // "", "any", "published" or "draft"
}

// ManageList is a page of a management listing.
type ManageList struct {
	Items []Summary `json:"items"`
	Total int       `json:"total"`
}

// Create makes a draft item of a collection type.
func (m *Manage) Create(ctx context.Context, typ string, in CreateInput) (*View, error) {
	body := map[string]any{"fields": in.Fields}
	if in.Fields == nil {
		body["fields"] = map[string]any{}
	}
	if in.Locale != "" {
		body["locale"] = in.Locale
	}
	if in.Key != "" {
		body["key"] = in.Key
	}
	if in.AlwaysAvailable {
		body["alwaysAvailable"] = true
	}
	return m.view(ctx, http.MethodPost, "/collections/"+url.PathEscape(typ)+"/items", "", body)
}

// Update applies a merge patch to the draft of an item in one locale.
func (m *Manage) Update(ctx context.Context, id string, in UpdateInput) (*View, error) {
	body := map[string]any{}
	if in.Locale != "" {
		body["locale"] = in.Locale
	}
	if in.Fields != nil {
		body["fields"] = in.Fields
	}
	if in.Key != nil {
		body["key"] = *in.Key
	}
	if in.AlwaysAvailable != nil {
		body["alwaysAvailable"] = *in.AlwaysAvailable
	}
	return m.view(ctx, http.MethodPatch, "/items/"+url.PathEscape(id), "", body)
}

// Publish publishes the draft parts of an item. A nil in publishes everything.
func (m *Manage) Publish(ctx context.Context, id string, in *PublishInput) (*View, error) {
	body := map[string]any{}
	if in != nil {
		if len(in.Locales) > 0 {
			body["locales"] = in.Locales
		}
		if in.Force {
			body["force"] = true
		}
	}
	return m.view(ctx, http.MethodPost, "/items/"+url.PathEscape(id)+"/publish", "", body)
}

// Get reads an item in one locale; with draft true the draft parts are overlaid.
func (m *Manage) Get(ctx context.Context, id, locale string, draft bool) (*View, error) {
	q := &query{}
	if locale != "" {
		q.add("locale", locale)
	}
	if draft {
		q.add("draft", "true")
	}
	return m.view(ctx, http.MethodGet, "/items/"+url.PathEscape(id), q.String(), nil)
}

// List lists the items of a collection type with their status. A nil o means defaults.
func (m *Manage) List(ctx context.Context, typ string, o *ManageListOptions) (*ManageList, error) {
	q := &query{}
	if o != nil {
		if o.Status != "" {
			q.add("status", o.Status)
		}
		lo := o.ListOptions
		lo.Include, lo.Total = 0, false // delivery-only options
		if err := lo.encode(q); err != nil {
			return nil, err
		}
	}
	_, raw, err := m.c.call(ctx, http.MethodGet, "/collections/"+url.PathEscape(typ)+"/items", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out ManageList
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes an item with all its versions. Items that others reference may refuse (an *Error with code
// "referenced" and its Referrers) depending on the reference's on_delete policy.
func (m *Manage) Delete(ctx context.Context, id string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/items/"+url.PathEscape(id), "", nil, nil, false)
	return err
}

// PreviewTokenInput asks for a preview token.
type PreviewTokenInput struct {
	Channel string
	Item    string        // optional: only this item previews as draft
	TTL     time.Duration // 0: the server's default (15 minutes); at most one hour
}

// PreviewToken is a minted preview token; pass Token to WithPreview.
type PreviewToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// PreviewToken mints a token that makes the delivery API of a channel show drafts.
func (m *Manage) PreviewToken(ctx context.Context, in PreviewTokenInput) (*PreviewToken, error) {
	body := map[string]any{"channel": in.Channel}
	if in.Item != "" {
		body["item"] = in.Item
	}
	if in.TTL > 0 {
		body["ttl"] = strconv.FormatFloat(in.TTL.Seconds(), 'f', -1, 64) + "s"
	}
	_, raw, err := m.c.call(ctx, http.MethodPost, "/preview-tokens", "", body, nil, false)
	if err != nil {
		return nil, err
	}
	var out PreviewToken
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *Manage) view(ctx context.Context, method, path, query string, body any) (*View, error) {
	_, raw, err := m.c.call(ctx, method, path, query, body, nil, false)
	if err != nil {
		return nil, err
	}
	var v View
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
