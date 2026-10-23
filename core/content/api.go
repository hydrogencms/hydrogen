// Package content is Hydrogen's content service and Local API: drafts, publishing, validation, projections,
// delete policies, singletons, hooks and content migrations. The management API, AI tools and project code
// all write through it, so every rule applies everywhere.
//
// Outbox topics (payload: id, type, version, locales): item.created, item.updated, item.published,
// item.unpublished (the item stopped being published), item.trashed (moved to the trash, unpublished if it
// was published), item.restored (out of the trash, unpublished) and item.deleted (purged for good). Audit
// actions: item.unpublish, item.trash, item.restore, item.purge.
//
// Async consumers that must react when an item goes offline subscribe to item.unpublished and item.trashed
// (a trash emits only item.trashed, not item.unpublished as well); the AfterDelete hook, in the transaction
// or async, fires on a purge only. A purge of an item that is already trashed has nothing left to take
// offline, so item.deleted is the topic for dropping everything of it.
//
// Contract: planning/design/content-core.md.
package content

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

var (
	ErrNotFound     = errors.New("content: not found")
	ErrUnknownType  = errors.New("content: unknown type")
	ErrInvalid      = errors.New("content: invalid")
	ErrConflict     = errors.New("content: conflict")
	ErrReferenced   = errors.New("content: referenced by other items")
	ErrDuplicateKey = errors.New("content: key already in use")
	ErrDuplicateID  = errors.New("content: id already in use")
	ErrBadRequest   = errors.New("content: bad request") // malformed input outside field validation
)

// ValidationError lists field problems; errors.Is(err, ErrInvalid) holds.
type ValidationError struct {
	Locale string
	Errors []schema.FieldError
}

func (e *ValidationError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, fe := range e.Errors {
		msgs = append(msgs, fe.Error())
	}
	return "content: invalid: " + strings.Join(msgs, "; ")
}

func (e *ValidationError) Is(target error) bool { return target == ErrInvalid }

// ConflictError: the published version changed the same parts since the draft started; errors.Is ErrConflict.
type ConflictError struct {
	Parts []string // "shared" or locale codes
}

func (e *ConflictError) Error() string {
	return "content: conflict: published since this draft started: " + strings.Join(e.Parts, ", ")
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// ReferencedError: delete blocked by items whose fields point here; errors.Is ErrReferenced.
type ReferencedError struct {
	Referrers []Referrer
	Hidden    int // referrers the caller may not read
}

func (e *ReferencedError) Error() string {
	return "content: referenced by " + strings.Join(func() []string {
		s := make([]string, 0, len(e.Referrers))
		for _, r := range e.Referrers {
			s = append(s, r.ItemID.String()+" ("+r.FieldPath+")")
		}
		return s
	}(), ", ")
}

func (e *ReferencedError) Is(target error) bool { return target == ErrReferenced }

// Value types used by generated code.
type (
	Ref      string          // item id
	Date     string          // "2026-10-14"
	DateTime string          // RFC 3339, UTC
	RichText json.RawMessage // ProseMirror document
)

func (r RichText) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return json.RawMessage(r).MarshalJSON()
}

func (r *RichText) UnmarshalJSON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

// Status values in views.
const (
	StatusDraft     = "draft"     // never published
	StatusPublished = "published" // published, no pending draft parts
	StatusChanged   = "changed"   // published, with draft parts
	// StatusUnpublished: not published now, but it was (it has versions). Single-item views report it; list
	// summaries show such items as draft.
	StatusUnpublished = "unpublished"
)

// SharedPart names the shared (non-translatable) draft part in APIs; storage uses "".
const SharedPart = "shared"

// View is an item in one locale: published content, or with Draft set, published content overlaid with the
// draft parts.
type View struct {
	ID              ids.UUID       `json:"id"`
	Type            string         `json:"type"`
	Key             string         `json:"key,omitempty"`
	Locale          string         `json:"locale"`
	MainLocale      string         `json:"mainLocale"`
	AlwaysAvailable bool           `json:"alwaysAvailable"`
	Status          string         `json:"status"`
	Version         int            `json:"version"`
	Draft           bool           `json:"draft"`
	Name            string         `json:"name"`
	Fields          map[string]any `json:"fields"`
	Locales         []string       `json:"locales"`
	// AvailableLocales are the stored locales plus the overlay locales that inherit from one.
	AvailableLocales []string `json:"availableLocales"`
	// Inherits is the direct parent of an overlay locale; Overrides lists the fields the locale stores and
	// InheritedFrom the locale each other field comes from (both always present, and only, with Inherits).
	Inherits      string            `json:"inherits,omitempty"`
	Overrides     []string          `json:"-"`
	InheritedFrom map[string]string `json:"-"`
	DraftParts    []string          `json:"draftParts"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	PublishedAt   *time.Time        `json:"publishedAt,omitempty"`
	// DeletedAt, DeletedBy and DeletedByName are set on a trashed item (GetOptions.Trashed).
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
	DeletedBy     string     `json:"deletedBy,omitempty"`
	DeletedByName string     `json:"deletedByName,omitempty"`
	// Actions says what the calling principal may do with the item; nil when no principal is acting.
	Actions *Actions `json:"actions,omitempty"`
}

// Actions are the item operations a principal may perform, for the admin's buttons. The services still check
// every operation.
type Actions struct {
	Edit    bool `json:"edit"`
	Publish bool `json:"publish"`
	Delete  bool `json:"delete"`
}

type Summary struct {
	ID         ids.UUID  `json:"id"`
	Type       string    `json:"type"`
	Key        string    `json:"key,omitempty"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Version    int       `json:"version"`
	Locales    []string  `json:"locales"`
	DraftParts []string  `json:"draftParts"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	// Fields holds the requested top-level fields (ListOptions.Fields) of the working document.
	Fields map[string]any `json:"fields,omitempty"`
}

// TrashSummary is a trashed item in a trash listing.
type TrashSummary struct {
	Summary
	DeletedAt     time.Time `json:"deletedAt"`
	DeletedBy     string    `json:"deletedBy"`
	DeletedByName string    `json:"deletedByName"`
}

// TrashOptions selects the trashed items ListTrash returns, newest deletion first.
type TrashOptions struct {
	Type   string // optional
	Locale string // names are read in this locale; default: the service's default locale
	Limit  int
	Offset int
}

type TrashResult struct {
	Items []TrashSummary `json:"items"`
	Total int            `json:"total"`
}

// VersionView is one stored version with its documents: Shared holds the non-translatable fields, Locales the
// translatable ones per locale.
type VersionView struct {
	Version   int                   `json:"version"`
	SchemaRev int                   `json:"schemaRev"`
	CreatedAt time.Time             `json:"createdAt"`
	CreatedBy string                `json:"createdBy"`
	Shared    VersionDoc            `json:"shared"`
	Locales   map[string]VersionDoc `json:"locales"`
}

// VersionDoc is one document of a version; Name is the locale's display name.
type VersionDoc struct {
	Name   string         `json:"name,omitempty"`
	Fields map[string]any `json:"fields"`
}

type Referrer struct {
	ItemID    ids.UUID `json:"id"`
	Type      string   `json:"type"`
	Name      string   `json:"name"`
	FieldPath string   `json:"field"`
	Kind      string   `json:"kind"`
	OnDelete  string   `json:"onDelete"`
}

type CreateInput struct {
	Type string
	// ID is the new item's id; the zero value generates a UUIDv7. Any other UUID is accepted (listings tiebreak
	// on the id but never rely on its time order); an id that exists, trashed items included, is ErrDuplicateID.
	ID              ids.UUID
	Locale          string // default: the service's default locale
	Key             string
	AlwaysAvailable bool
	Fields          map[string]any // decoded with UseNumber
}

type GetOptions struct {
	Locale string // default: the item's main locale
	Draft  bool   // overlay draft parts
	// Trashed also reads an item in the trash (always as its draft view: a trashed item is unpublished).
	// Without it a trashed item is ErrNotFound.
	Trashed bool
}

type UpdateInput struct {
	Locale          string
	Fields          map[string]any // JSON merge patch: nil removes a field
	Key             *string
	AlwaysAvailable *bool
}

type PublishOptions struct {
	Locales []string // locale parts to publish; nil: every draft locale part. The shared part always goes.
	Force   bool     // publish over conflicts
}

// Filter is one list condition; Value comes from a typed caller or from a query string (string values are
// parsed by the field's type).
type Filter struct {
	Field string
	Op    store.Op
	Value any
}

type SortSpec struct {
	Field string // index field or "name", "createdAt", "updatedAt", "key"
	Desc  bool
}

type ListOptions struct {
	Type   string
	Locale string
	Status store.Status
	Where  []Filter
	Search string
	Sort   []SortSpec
	Limit  int
	Offset int
	// Fields names top-level fields of Type (at most MaxListFields) to return in each summary.
	Fields []string
}

// FacetOptions asks for the values of indexed text fields with the number of items that have each. Status,
// Locale and Where narrow the items like in a list.
type FacetOptions struct {
	Type   string
	Locale string
	Status store.Status
	Where  []Filter
	// Fields names the indexed text fields (at most MaxFacetFields).
	Fields []string
}

// FacetValue is one value of a facet and the number of items that have it.
type FacetValue struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type ListResult struct {
	Items []Summary `json:"items"`
	Total int       `json:"total"`
}

// Migration transforms stored documents of one type when the schema changes in a breaking way.
type Migration struct {
	Name    string   // ordered by name, applied once, e.g. "0001_split_name"
	Type    string   // content type it migrates
	Changes []string // breaking schema changes it accounts for, e.g. "person.fullName"
	Up      func(doc *MigrationDoc) error
}

// MigrationDoc is one item's document in one locale during a migration.
type MigrationDoc struct {
	ItemID ids.UUID
	Locale string
	Fields map[string]any // merged shared + locale fields; changes are written back split by the new schema
}

type Options struct {
	DB            store.DB
	Schema        *schema.Schema
	Hooks         *hooks.Registry
	Outbox        *outbox.Outbox
	Audit         *audit.Log
	Clock         clock.Clock
	IDs           *ids.Generator
	DefaultLocale string // default "en"
	Migrations    []Migration
	// Access authorizes the principal in a context (access.WithPrincipal); nil allows everything.
	Access Access
	// Review plugs the review workflow into types with `review: true`; nil without the workflow. SetReview
	// installs it later.
	Review ReviewHook
	// Overlays resolves locales that inherit from others (market overlays); nil: every locale is independent.
	Overlays Overlays
	// Files looks up the files `file` fields point to (the media module); required when the schema has any.
	Files Files
}
