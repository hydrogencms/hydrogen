package store

import (
	"context"
	"errors"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// ErrNotFound is returned by content lookups that match nothing.
var ErrNotFound = errors.New("store: not found")

// ErrDuplicateKey is returned when an item key is already taken.
var ErrDuplicateKey = errors.New("store: duplicate key")

// ContentStore persists items, versions, drafts and the projections of published versions. Documents are JSON
// bytes; the content service owns their meaning. Contract: planning/design/content-core.md.
type ContentStore interface {
	// Schema revisions. AddSchemaRev is idempotent on hash.
	SchemaRev(ctx context.Context, hash string) (rev int, ok bool, err error)
	AddSchemaRev(ctx context.Context, hash string, compiled []byte, at time.Time) (rev int, err error)
	LatestSchemaRev(ctx context.Context) (rev int, hash string, compiled []byte, err error) // rev 0: none

	InsertItem(ctx context.Context, it Item) error // ErrDuplicateKey
	// GetItem and GetItemByKey return ErrNotFound. ForUpdate locks the row until the transaction ends
	// (Postgres SELECT … FOR UPDATE; SQLite's single writer already serialises).
	GetItem(ctx context.Context, id ids.UUID, forUpdate bool) (Item, error)
	GetItemByKey(ctx context.Context, key string) (Item, error)
	GetItems(ctx context.Context, ids []ids.UUID) ([]Item, error) // missing ids are skipped; any order
	UpdateItem(ctx context.Context, it Item) error                // all mutable columns; ErrNotFound, ErrDuplicateKey
	DeleteItem(ctx context.Context, id ids.UUID) error            // cascades to versions, drafts, projections

	InsertVersion(ctx context.Context, v Version) error
	// GetVersion returns the version with all its locales.
	GetVersion(ctx context.Context, id ids.UUID, version int) (Version, error)
	ListVersions(ctx context.Context, id ids.UUID) ([]VersionMeta, error) // newest first

	PutDraftPart(ctx context.Context, p DraftPart) error // insert or replace
	GetDraftParts(ctx context.Context, id ids.UUID) ([]DraftPart, error)
	// DraftPartsOf lists the draft part names ("" = shared) of many items in one query, sorted. Items
	// without draft parts are absent from the map.
	DraftPartsOf(ctx context.Context, items []ids.UUID) (map[ids.UUID][]string, error)
	DeleteDraftParts(ctx context.Context, id ids.UUID, parts []string) error // nil parts: all

	// Projections of an item's current published version; Replace* deletes the item's old rows first.
	ReplaceRefs(ctx context.Context, from ids.UUID, refs []Ref) error
	IncomingRefs(ctx context.Context, to ids.UUID) ([]Ref, error)
	ReplaceIndex(ctx context.Context, item ids.UUID, rows []IndexRow) error
	ReplaceSearch(ctx context.Context, item ids.UUID, docs []SearchDoc) error

	Query(ctx context.Context, q Query) (QueryResult, error)

	// Content migrations.
	AppliedContentMigrations(ctx context.Context) ([]string, error)
	RecordContentMigration(ctx context.Context, name string, at time.Time) error
	ItemIDsOfType(ctx context.Context, typ string, after ids.UUID, limit int) ([]ids.UUID, error) // id order
}

type Item struct {
	ID               ids.UUID
	Key              string // "" = none
	Type             string
	MainLocale       string
	AlwaysAvailable  bool
	PublishedVersion int // 0: never published
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CreatedBy        string // "kind:id" of the actor
	UpdatedBy        string
}

type Version struct {
	ItemID    ids.UUID
	Version   int
	SchemaRev int
	Shared    []byte // JSON object
	Locales   []VersionLocale
	CreatedAt time.Time
	CreatedBy string
}

type VersionLocale struct {
	Locale string
	Name   string
	Data   []byte // JSON object
}

type VersionMeta struct {
	Version   int
	SchemaRev int
	Locales   []string
	CreatedAt time.Time
	CreatedBy string
}

// DraftPart is one independently publishable part of an item's working copy: Part "" is the shared
// (non-translatable) document, any other value is a locale.
type DraftPart struct {
	ItemID      ids.UUID
	Part        string
	BaseVersion int
	Name        string // locale parts: display name
	Data        []byte // JSON object
	UpdatedAt   time.Time
	UpdatedBy   string
}

type Ref struct {
	From      ids.UUID
	FieldPath string
	To        ids.UUID
	Kind      string // "field" or "link"
	OnDelete  string // "block", "nullify", "warn"
}

// IndexRow projects one value of an indexed field. Locale "" means shared (applies to every locale).
// Exactly one value column is set.
type IndexRow struct {
	Locale string
	Field  string
	Text   *string
	Num    *float64
	Time   *time.Time
	Bool   *bool
	Ref    *ids.UUID
}

// SearchDoc is the searchable text of an item in one locale.
type SearchDoc struct {
	Locale string
	Text   string
}

type Status int

const (
	StatusAny Status = iota
	StatusPublished
	StatusDraft
)

type Op string

const (
	OpEq       Op = "eq"
	OpNe       Op = "ne"
	OpLt       Op = "lt"
	OpLte      Op = "lte"
	OpGt       Op = "gt"
	OpGte      Op = "gte"
	OpIn       Op = "in"
	OpContains Op = "contains" // any value of a list field equals Value
	OpExists   Op = "exists"   // Value is bool
	OpPrefix   Op = "prefix"   // text only
)

// Column says which field_index column a condition or sort uses.
type Column string

const (
	ColText Column = "text"
	ColNum  Column = "num"
	ColTime Column = "time"
	ColBool Column = "bool"
	ColRef  Column = "ref"
)

// Cond filters on an indexed field. Value types by column: text string, num float64, time time.Time,
// bool bool, ref ids.UUID; OpIn takes a slice of those.
type Cond struct {
	Field  string
	Column Column
	Op     Op
	Value  any
}

// Sort orders by an indexed field (Column set) or a meta field (Column empty): "name", "createdAt",
// "updatedAt", "key".
type Sort struct {
	Field  string
	Column Column
	Desc   bool
}

type Query struct {
	Type   string
	Locale string // for names, locale-specific index rows and search
	Status Status
	Where  []Cond
	Search string
	Sort   []Sort
	Limit  int // 1..500
	Offset int
}

type QueryResult struct {
	Items []ItemSummary
	Total int
}

type ItemSummary struct {
	Item
	Name     string   // in Query.Locale, else the main locale; drafts' names for never-published items
	Drafts   []string // draft part names ("" = shared)
	Locales  []string // locales in the published version
	Searched float64  // search rank, when Search is set
}
