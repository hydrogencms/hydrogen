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
	Status          string          `json:"status"` // draft, published, changed, unpublished
	Version         int             `json:"version"`
	Draft           bool            `json:"draft"`
	Name            string          `json:"name"`
	Fields          json.RawMessage `json:"fields"`
	Locales         []string        `json:"locales"`
	DraftParts      []string        `json:"draftParts"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	PublishedAt     *time.Time      `json:"publishedAt"`
	// DeletedAt, DeletedBy and DeletedByName are set on an item read from the trash.
	DeletedAt     *time.Time `json:"deletedAt"`
	DeletedBy     string     `json:"deletedBy"`
	DeletedByName string     `json:"deletedByName"`
}

// TrashSummary is one row of the trash listing.
type TrashSummary struct {
	Summary
	DeletedAt     time.Time `json:"deletedAt"`
	DeletedBy     string    `json:"deletedBy"`
	DeletedByName string    `json:"deletedByName"`
}

// TrashList is a page of the trash.
type TrashList struct {
	Items []TrashSummary `json:"items"`
	Total int            `json:"total"`
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

// Delete moves an item to the trash; it is Trash. A published item is unpublished first. Items that others
// reference with on_delete block refuse (an *Error with code "referenced" and its Referrers); a page with
// children refuses with code "has_children".
func (m *Manage) Delete(ctx context.Context, id string) error { return m.Trash(ctx, id) }

// Trash moves an item to the trash (DELETE /items/{id}).
func (m *Manage) Trash(ctx context.Context, id string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/items/"+url.PathEscape(id), "", nil, nil, false)
	return err
}

// Purge deletes a trashed item for good with its versions and drafts (an *Error with code "conflict" when it
// is not in the trash).
func (m *Manage) Purge(ctx context.Context, id string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/items/"+url.PathEscape(id), "permanent=true", nil, nil, false)
	return err
}

// Restore takes an item out of the trash; it comes back unpublished.
func (m *Manage) Restore(ctx context.Context, id string) (*View, error) {
	return m.view(ctx, http.MethodPost, "/items/"+url.PathEscape(id)+"/restore", "", nil)
}

// Unpublish stops delivering an item; its draft and versions stay. It returns the draft view.
func (m *Manage) Unpublish(ctx context.Context, id string) (*View, error) {
	return m.view(ctx, http.MethodPost, "/items/"+url.PathEscape(id)+"/unpublish", "", nil)
}

// GetTrashed reads an item that is in the trash (as its draft view); a live item reads as with Get.
func (m *Manage) GetTrashed(ctx context.Context, id, locale string) (*View, error) {
	q := &query{}
	if locale != "" {
		q.add("locale", locale)
	}
	q.add("trashed", "true")
	return m.view(ctx, http.MethodGet, "/items/"+url.PathEscape(id), q.String(), nil)
}

// ListTrash lists trashed items, newest deletion first. typ, limit and offset are optional (zero values).
func (m *Manage) ListTrash(ctx context.Context, typ string, limit, offset int) (*TrashList, error) {
	q := &query{}
	if typ != "" {
		q.add("type", typ)
	}
	if limit > 0 {
		q.add("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.add("offset", strconv.Itoa(offset))
	}
	_, raw, err := m.c.call(ctx, http.MethodGet, "/trash", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out TrashList
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewTokenInput asks for a preview token.
type PreviewTokenInput struct {
	Channel string
	Item    string        // optional: only this item previews as draft
	Locale  string        // optional, with Item: the language of the URL; default the item's main locale if the channel serves it
	TTL     time.Duration // 0: the server's default (15 minutes); at most one hour
}

// PreviewToken is a minted preview token; pass Token to WithPreview.
type PreviewToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	// URL is where the channel's frontend shows the item, with the token appended; only when the input had an
	// Item and a URL resolves.
	URL string `json:"url,omitempty"`
}

// PreviewToken mints a token that makes the delivery API of a channel show drafts.
func (m *Manage) PreviewToken(ctx context.Context, in PreviewTokenInput) (*PreviewToken, error) {
	body := map[string]any{"channel": in.Channel}
	if in.Item != "" {
		body["item"] = in.Item
	}
	if in.Locale != "" {
		body["locale"] = in.Locale
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

// ShareInput asks for a share link: a preview of one item that anyone holding the link can open.
type ShareInput struct {
	Item    string
	Channel string
	Locale  string        // optional; default the item's main locale if the channel serves it
	TTL     time.Duration // 0: the server's default (24 hours); at most delivery.share_max_ttl
}

// Share is a created share link. Token and URL are only in the answer to [Manage.CreateShare].
type Share struct {
	ID        string `json:"id"`
	Token     string `json:"token,omitempty"`
	URL       string `json:"url,omitempty"`
	Channel   string `json:"channel"`
	Locale    string `json:"locale"`
	CreatedBy string `json:"createdBy,omitempty"`
	// CreatedByName is the creator's display name, empty when unknown (listings only).
	CreatedByName string     `json:"createdByName,omitempty"`
	CreatedAt     time.Time  `json:"createdAt,omitempty"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	// Viewers is the number of live event streams of the link on the server that answered (listings only).
	Viewers int `json:"viewers,omitempty"`
}

// CreateShare creates a share link to an item's preview (needs content.preview.share).
func (m *Manage) CreateShare(ctx context.Context, in ShareInput) (*Share, error) {
	body := map[string]any{"item": in.Item, "channel": in.Channel}
	if in.Locale != "" {
		body["locale"] = in.Locale
	}
	if in.TTL > 0 {
		body["ttl"] = strconv.FormatFloat(in.TTL.Seconds(), 'f', -1, 64) + "s"
	}
	_, raw, err := m.c.call(ctx, http.MethodPost, "/preview-shares", "", body, nil, false)
	if err != nil {
		return nil, err
	}
	var out Share
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Shares lists the share links of an item, newest first, including expired and revoked ones. Tokens are not
// listed.
func (m *Manage) Shares(ctx context.Context, item string) ([]Share, error) {
	q := &query{}
	q.add("item", item)
	_, raw, err := m.c.call(ctx, http.MethodGet, "/preview-shares", q.String(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		Shares []Share `json:"shares"`
	}
	if err := decode(raw, &out); err != nil {
		return nil, err
	}
	return out.Shares, nil
}

// RevokeShare revokes a share link; it is idempotent.
func (m *Manage) RevokeShare(ctx context.Context, id string) error {
	_, _, err := m.c.call(ctx, http.MethodDelete, "/preview-shares/"+url.PathEscape(id), "", nil, nil, false)
	return err
}

// RelayDraft relays an editor's unsaved working document of an item (all its fields, as a draft update
// would carry them) to previews: share-link viewers and the admin's preview frame show it instead of the saved
// draft until it is dropped, superseded by a save or an hour old. Nothing is saved. It returns the revision.
// An empty locale means the item's main locale.
func (m *Manage) RelayDraft(ctx context.Context, item, locale string, fields any) (int64, error) {
	q := &query{}
	if locale != "" {
		q.add("locale", locale)
	}
	_, raw, err := m.c.call(ctx, http.MethodPut, "/preview-relay/"+url.PathEscape(item), q.String(), map[string]any{"fields": fields}, nil, false)
	if err != nil {
		return 0, err
	}
	var out struct {
		Rev int64 `json:"rev"`
	}
	if err := decode(raw, &out); err != nil {
		return 0, err
	}
	return out.Rev, nil
}

// DropRelay removes the relayed working document of an item and locale: previews show the saved draft again.
func (m *Manage) DropRelay(ctx context.Context, item, locale string) error {
	q := &query{}
	if locale != "" {
		q.add("locale", locale)
	}
	_, _, err := m.c.call(ctx, http.MethodDelete, "/preview-relay/"+url.PathEscape(item), q.String(), nil, nil, false)
	return err
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
