// Package search defines the search interface of Hydrogen: a request, a result, the engines that answer
// them and the documents an external engine indexes. The default engine, Database, uses the database
// adapter's full-text search; an external engine (a module) implements Index and is kept in sync by the
// kernel from published content.
package search

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// ErrInvalid wraps every problem with a request or its values (unknown field, bad operator, a limit out of
// range): callers answer it with a client error.
var ErrInvalid = errors.New("search: invalid request")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

const (
	MaxLimit  = 100
	MaxOffset = 1000
	MaxFacets = 5
	MaxTerms  = 16 // words in Text, as for collection lists
)

// PublishedAt is the sort field that orders by the time a document was published.
const PublishedAt = "publishedAt"

// Filter restricts the hits by an indexed field. Value is typed by the field's kind: string, float64 (any Go
// number is accepted), time.Time, bool, ids.UUID (or its string form), and a slice of those for OpIn.
type Filter struct {
	Field string
	Op    store.Op // eq ne lt lte gt gte in contains exists prefix
	Value any
}

// Sort orders by an indexed scalar field or PublishedAt.
type Sort struct {
	Field string
	Desc  bool
}

type Request struct {
	Text      string
	Types     []string // required, non-empty: the caller resolves defaults
	Locale    string   // required
	Where     []Filter
	Facets    []string // indexed text fields (lists count once per value), at most MaxFacets
	Sort      []Sort   // empty: relevance
	Limit     int      // 1..MaxLimit
	Offset    int      // 0..MaxOffset
	Highlight bool
}

// Validate checks the request's shape; engines call it first.
func (r Request) Validate() error {
	switch {
	case len(r.Types) == 0:
		return invalid("types are required")
	case r.Locale == "":
		return invalid("locale is required")
	case r.Limit < 1 || r.Limit > MaxLimit:
		return invalid("limit must be between 1 and %d", MaxLimit)
	case r.Offset < 0 || r.Offset > MaxOffset:
		return invalid("offset must be between 0 and %d", MaxOffset)
	case len(r.Facets) > MaxFacets:
		return invalid("at most %d facets", MaxFacets)
	case len(storeutil.SearchTerms(r.Text)) > MaxTerms:
		return invalid("too many search terms (at most %d)", MaxTerms)
	}
	return nil
}

type Hit struct {
	Item ids.UUID
	Type string
	// Locale is the document's locale: the request's, or the main locale of an always-available item that
	// has no document in it.
	Locale     string
	Score      float64           // engine-specific, higher is better; 0 when the request sorts
	Highlights map[string]string // "name", "text": HTML-escaped snippets with <mark>…</mark>
}

type Facet struct {
	Value string
	Count int
}

type Result struct {
	Hits      []Hit
	Total     int  // matching documents (an estimate when Estimated)
	Estimated bool // the engine only estimates Total
	Facets    map[string][]Facet
}

// Engine answers search requests.
//
// Availability, for every engine: a hit for locale L is a document with Locale == L, or, when the item has no
// document in L and is always available, its main-locale document. Unpublished, trashed and not-searchable
// items are never hits.
type Engine interface {
	Name() string
	Search(ctx context.Context, r Request) (Result, error)
}

// Index is an engine with its own index, kept in sync by the kernel from published content.
type Index interface {
	Engine
	// Sync applies the settings; it returns true when the stored documents must be rebuilt (a new index or a
	// changed fingerprint). An engine must keep returning true until a Rebuild with these settings has
	// completed, so it stores the fingerprint last.
	Sync(ctx context.Context, s Settings) (rebuild bool, err error)
	// Upsert replaces all documents of the items present in docs (every locale of each item).
	Upsert(ctx context.Context, docs []Doc) error
	// Delete removes every locale of these items.
	Delete(ctx context.Context, items []ids.UUID) error
	// Rebuild replaces the whole index with the documents from next (called until it returns nil, nil), with
	// no window where searches see an empty index.
	Rebuild(ctx context.Context, s Settings, next func(ctx context.Context) ([]Doc, error)) error
}

// Doc is one item in one locale, as an external engine indexes it.
type Doc struct {
	Item            ids.UUID
	Type            string
	Locale          string
	MainLocale      string
	AlwaysAvailable bool
	Locales         []string // every locale the item is published in, sorted
	Name            string
	Text            string // Schema.SearchText of the published document in this locale
	// Fields are the indexed fields of the type: string, float64, bool, time.Time, ids.UUID, or for lists a
	// slice: []string (text and reference ids), []float64, []bool or []time.Time. Empty values are absent.
	Fields      map[string]any
	PublishedAt time.Time
}
