// Package content is Hydrogen's content service and Local API: drafts, publishing, validation, projections,
// delete policies, singletons, hooks and content migrations. The management API, AI tools and project code
// all write through it, so every rule applies everywhere.
//
// Contract: planning/design/content-core.md.
package content

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

var (
	ErrNotFound     = errors.New("content: not found")
	ErrUnknownType  = errors.New("content: unknown type")
	ErrInvalid      = errors.New("content: invalid")
	ErrConflict     = errors.New("content: conflict")
	ErrReferenced   = errors.New("content: referenced by other items")
	ErrDuplicateKey = errors.New("content: key already in use")
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
	DraftParts      []string       `json:"draftParts"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	PublishedAt     *time.Time     `json:"publishedAt,omitempty"`
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
	Type            string
	Locale          string // default: the service's default locale
	Key             string
	AlwaysAvailable bool
	Fields          map[string]any // decoded with UseNumber
}

type GetOptions struct {
	Locale string // default: the item's main locale
	Draft  bool   // overlay draft parts
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
}
