package exportfmt

import (
	"encoding/json"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// ItemRecord is one line of items.ndjson.
type ItemRecord struct {
	ID               ids.UUID        `json:"id"`
	Key              string          `json:"key,omitempty"`
	Type             string          `json:"type"`
	MainLocale       string          `json:"mainLocale"`
	AlwaysAvailable  bool            `json:"alwaysAvailable,omitempty"`
	PublishedVersion int             `json:"publishedVersion"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	CreatedBy        string          `json:"createdBy,omitempty"`
	UpdatedBy        string          `json:"updatedBy,omitempty"`
	Versions         []VersionRecord `json:"versions"`
	Drafts           []DraftRecord   `json:"drafts,omitempty"`
}

// VersionRecord is one immutable version. Schema is the hash of the schema revision it was written under.
type VersionRecord struct {
	Version   int             `json:"version"`
	Schema    string          `json:"schema"`
	CreatedAt time.Time       `json:"createdAt"`
	CreatedBy string          `json:"createdBy,omitempty"`
	Shared    json.RawMessage `json:"shared"`
	Locales   []LocaleRecord  `json:"locales"`
}

type LocaleRecord struct {
	Locale string          `json:"locale"`
	Name   string          `json:"name,omitempty"`
	Data   json.RawMessage `json:"data"`
}

// DraftRecord is one draft part: Part "" is the shared document, any other value a locale.
type DraftRecord struct {
	Part        string          `json:"part,omitempty"`
	BaseVersion int             `json:"baseVersion"`
	Name        string          `json:"name,omitempty"`
	Data        json.RawMessage `json:"data"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	UpdatedBy   string          `json:"updatedBy,omitempty"`
}

// RevisionFile is schema/revisions/<hash>.json.
type RevisionFile struct {
	Hash      string          `json:"hash"`
	Rev       int             `json:"rev"`
	CreatedAt time.Time       `json:"createdAt"`
	Compiled  json.RawMessage `json:"compiled"`
}

// MigrationRecord is one line of content_migrations.ndjson.
type MigrationRecord struct {
	Name      string    `json:"name"`
	AppliedAt time.Time `json:"appliedAt"`
}
