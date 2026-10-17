package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// PreviewChannel carries preview notifications: "<item>|<locale>|<rev>" when a relayed draft changed or was
// deleted (rev 0), "share|<id>" when a share session was revoked, "relays|purged" when the cleanup dropped stale
// relayed drafts.
const PreviewChannel = "hydrogen_preview"

// ShareSession is a link to one item's preview. Only the double SHA-256 of the token is stored.
type ShareSession struct {
	ID                   ids.UUID
	TokenHash            []byte
	Item                 ids.UUID
	Channel, Locale      string
	CreatedBy            string
	CreatedAt, ExpiresAt time.Time
	RevokedAt            *time.Time
}

// RelayDraft is an editor's unsaved working document, one per item and locale.
type RelayDraft struct {
	Item      ids.UUID
	Locale    string
	Fields    json.RawMessage
	Rev       int64
	UpdatedBy string
	UpdatedAt time.Time
}

// PreviewStore keeps share sessions and relayed drafts. Neither has a foreign key to the item: deleting an item
// never fails on them, and readers ignore rows of missing items.
type PreviewStore interface {
	CreateShare(ctx context.Context, s ShareSession) error              // ErrDuplicateKey on token_hash or id
	ShareByHash(ctx context.Context, hash []byte) (ShareSession, error) // ErrNotFound
	GetShare(ctx context.Context, id ids.UUID) (ShareSession, error)    // ErrNotFound
	// ListShares returns an item's sessions, newest first, including expired and revoked ones.
	ListShares(ctx context.Context, item ids.UUID) ([]ShareSession, error)
	// RevokeShare is idempotent (the first revocation time is kept); ErrNotFound for an unknown id.
	RevokeShare(ctx context.Context, id ids.UUID, at time.Time) error
	// DeleteExpiredShares removes sessions that expired or were revoked before the given time.
	DeleteExpiredShares(ctx context.Context, before time.Time) (int, error)

	// PutRelay upserts and returns the new rev (previous rev + 1, starting at 1).
	PutRelay(ctx context.Context, d RelayDraft) (int64, error)
	GetRelay(ctx context.Context, item ids.UUID, locale string) (RelayDraft, error) // ErrNotFound
	// GetRelays returns the relayed drafts of these items in every locale (unordered), for one listing's overlay.
	GetRelays(ctx context.Context, items []ids.UUID) ([]RelayDraft, error)
	DeleteRelay(ctx context.Context, item ids.UUID, locale string) error // idempotent
	// DeleteStaleRelays removes relayed drafts last updated before the given time.
	DeleteStaleRelays(ctx context.Context, before time.Time) (int, error)
}
