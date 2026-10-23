package store

import (
	"context"
	"errors"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// ErrNotFound is returned by content lookups that match nothing.
var ErrNotFound = errors.New("store: not found")

// ErrDuplicateKey is returned when an item key is already taken.
var ErrDuplicateKey = errors.New("store: duplicate key")

// Trash. An item with Item.DeletedAt set is trashed: Query leaves it out unless Query.Trashed says otherwise,
// GetItemByKey does not find it, ItemsAfter/CountItems (export) skip it, and its key is free for another item
// (keys are unique among non-trashed items). GetItem and GetItems return trashed items too, so the service
// decides what a trashed item means to its caller; ItemIDsOfType includes them (content migrations and
// reindexing must keep trashed items current for a later restore).
//
// Unpublish needs no ContentStore method of its own; publish writes snapshots, references, and the published
// index and search rows, and the service removes exactly those with existing calls:
// DeleteSnapshots, ReplaceRefs(id, nil), ReplaceIndex(id, ProjectionPublished, nil),
// ReplaceSearch(id, ProjectionPublished, nil), then UpdateItem with PublishedVersion 0 (the working projection
// and module index rows are the service's to refresh). Trashing = that, then SetDeleted.

// ContentStore persists items, versions, drafts and the projections of published versions. Documents are JSON
// bytes; the content service owns their meaning. Contract: planning/design/content-core.md.
type ContentStore interface {
	SnapshotStore

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
	// UpdateItem writes every mutable column except DeletedAt/DeletedBy (SetDeleted owns those): ErrNotFound,
	// ErrDuplicateKey (a key taken by a non-trashed item; a trashed item's key is not checked).
	UpdateItem(ctx context.Context, it Item) error
	// SetDeleted trashes the item (at set; by is the actor, "kind:id") or restores it (at nil; the actor is
	// cleared). It writes nothing else: the caller unpublishes first (see the note on trashing below). ErrNotFound;
	// ErrDuplicateKey when restoring an item whose key a non-trashed item has taken meanwhile.
	SetDeleted(ctx context.Context, id ids.UUID, at *time.Time, by string) error
	DeleteItem(ctx context.Context, id ids.UUID) error // cascades to versions, drafts, projections

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
	// PublishedLocalesOf lists the locales of the published version of many items in one query, sorted. Items
	// that were never published are absent from the map.
	PublishedLocalesOf(ctx context.Context, items []ids.UUID) (map[ids.UUID][]string, error)

	// MainNamesOf returns the display name of many items in their main locale in one query: the draft's, else
	// the published version's, "" when neither has one. Missing ids are absent.
	MainNamesOf(ctx context.Context, items []ids.UUID) (map[ids.UUID]string, error)

	// Projections of an item's current published version; Replace* deletes the item's old rows first.
	ReplaceRefs(ctx context.Context, from ids.UUID, refs []Ref) error
	IncomingRefs(ctx context.Context, to ids.UUID) ([]Ref, error)
	// Index and search rows exist per projection: published (delivery) and working (the draft overlaid on the
	// published version; management listings). Both replace only the content rows of that projection.
	ReplaceIndex(ctx context.Context, item ids.UUID, p Projection, rows []IndexRow) error
	ReplaceSearch(ctx context.Context, item ids.UUID, p Projection, docs []SearchDoc) error
	// SearchIndexEmpty reports whether items exist but a projection has no search rows at all: published
	// ones for a published item, working ones for any item (a lost index, or the state after a migration that
	// dropped it).
	SearchIndexEmpty(ctx context.Context) (bool, error)
	// ReplaceModuleIndex replaces a module's index rows of an item. Field names are "<module>.<name>"; the rows
	// apply to both projections.
	ReplaceModuleIndex(ctx context.Context, item ids.UUID, module string, rows []IndexRow) error

	Query(ctx context.Context, q Query) (QueryResult, error)

	// Content migrations.
	AppliedContentMigrations(ctx context.Context) ([]string, error)
	RecordContentMigration(ctx context.Context, name string, at time.Time) error
	ItemIDsOfType(ctx context.Context, typ string, after ids.UUID, limit int) ([]ids.UUID, error) // id order

	// Facet counts the items matching q per value of an indexed text field (a list field counts an item once
	// per value), most items first and then by value, at most limit values. Only q's Type, Types, Trashed,
	// Status, Available, Working, Scoped/Scope, Where, Locale, Search and SearchFallback apply; sort and paging
	// are ignored.
	Facet(ctx context.Context, q Query, field string, limit int) ([]FacetCount, error)
}

// FacetCount is one value of a facet and the number of items that have it.
type FacetCount struct {
	Value string
	Count int
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
	DeletedAt        *time.Time // non-nil: the item is in the trash
	DeletedBy        string     // "kind:id" of the actor who trashed it
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
	OpUnder    Op = "under"    // text only: the value itself or a path below it ("/a" matches "/a" and "/a/b", not "/ab")
	// Boolean groups take no field: Cond.Groups lists AND-lists of conditions. OpOr holds when every condition
	// of at least one group does; OpNot holds when no group is fully satisfied (the negation of OpOr). Items
	// without index rows for a field fail every positive condition on it, so they satisfy its negation.
	OpOr  Op = "or"
	OpNot Op = "not"
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
	Groups [][]Cond // OpOr and OpNot only
}

// Sort orders by an indexed field (Column set) or a meta field (Column empty): "name", "createdAt",
// "updatedAt", "key", "deletedAt" (trashed items; others sort as NULL).
type Sort struct {
	Field  string
	Column Column
	Desc   bool
}

// Trashed says what a query does with trashed items. The zero value leaves them out.
type Trashed int

const (
	TrashedExclude Trashed = iota // only items that are not in the trash
	TrashedOnly                   // only trashed items
	TrashedInclude                // both
)

type Query struct {
	Type string
	// Types narrows Type: an item must match Type (when set) and be of one of these types; empty adds no filter.
	Types  []string
	Locale string // for names, locale-specific index rows and search
	Status Status
	// Trashed selects trashed items; the default excludes them from lists, counts, search and scoped queries.
	Trashed Trashed
	// Available, when set, keeps only published items a channel can show: a published snapshot in one of these
	// locales, or published and always available. Totals and paging respect it.
	Available []string
	// Working matches Where, indexed Sort and Search against the working projection instead of the published one.
	Working bool
	// Scoped restricts the query to items matching at least one Scope term (access control); with no terms it
	// matches nothing. Totals and paging respect it.
	Scoped bool
	Scope  []ScopeTerm
	Where  []Cond
	Search string
	// SearchFallback also matches the main-locale search document of an always available item that has none
	// in Locale; rank and snippet then come from that document.
	SearchFallback bool
	// NoTotal skips the total count: QueryResult.Total is then only the page's count plus Offset.
	NoTotal bool
	// Highlight fills ItemSummary.Snippet for a query with Search.
	Highlight bool
	Sort      []Sort
	Limit     int // 1..500
	Offset    int
}

// ScopeTerm ANDs its parts: CreatedBy (any of these actors; empty = any) and Where.
type ScopeTerm struct {
	CreatedBy []string
	Where     []Cond
}

// Projection selects the published or the working index and search rows.
type Projection string

const (
	ProjectionPublished Projection = "p"
	ProjectionWorking   Projection = "w"
)

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
	// Snippet is a fragment of the best matching search text with each match wrapped in \x02 and \x03 (raw
	// text, not escaped); set when Query.Highlight and Query.Search are.
	Snippet string
}

// Snapshot is the delivery JSON of one item in one locale, built when a version is written. Payload is stored
// and served byte-for-byte. Contract: planning/design/delivery-h4.md.
type Snapshot struct {
	ItemID          ids.UUID
	Locale          string
	Version         int
	Type            string
	Key             string
	MainLocale      string
	AlwaysAvailable bool
	Payload         []byte     // JSON object
	ETag            string     // hex SHA-256 of Payload
	Refs            []ids.UUID // referenced items, sorted, unique
	PublishedAt     time.Time
}

// SnapshotStore writes snapshots inside a transaction (ContentStore embeds it).
type SnapshotStore interface {
	// PutSnapshots replaces every locale of the item.
	PutSnapshots(ctx context.Context, item ids.UUID, snaps []Snapshot) error
	DeleteSnapshots(ctx context.Context, item ids.UUID) error
}

// Reader serves the delivery hot path: read-only, without a transaction (each call sees its own snapshot).
type Reader interface {
	// Snapshots returns the rows of those items in those locales (any order; missing ones absent).
	Snapshots(ctx context.Context, items []ids.UUID, locales []string) ([]Snapshot, error)
	// SnapshotsByKey returns the rows of the item with that key in those locales.
	SnapshotsByKey(ctx context.Context, key string, locales []string) ([]Snapshot, error)
	// Query has the same semantics as ContentStore.Query.
	Query(ctx context.Context, q Query) (QueryResult, error)
	// Facet has the same semantics as ContentStore.Facet.
	Facet(ctx context.Context, q Query, field string, limit int) ([]FacetCount, error)
}
