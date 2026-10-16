-- Share links and relayed drafts. Neither table has a foreign key to item: deleting an item must not fail on
-- them, and readers ignore rows of missing items. Expired shares and stale relays are purged by the store's
-- Delete* methods.
CREATE TABLE preview_share (
    id         BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    token_hash BLOB    NOT NULL UNIQUE,
    item       BLOB    NOT NULL CHECK (length(item) = 16),
    channel    TEXT    NOT NULL,
    locale     TEXT    NOT NULL DEFAULT '',
    created_by TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
) WITHOUT ROWID;

CREATE INDEX preview_share_item ON preview_share (item, created_at);
CREATE INDEX preview_share_expires ON preview_share (expires_at);

CREATE TABLE preview_relay (
    item       BLOB    NOT NULL CHECK (length(item) = 16),
    locale     TEXT    NOT NULL,
    fields     TEXT    NOT NULL,
    rev        INTEGER NOT NULL,
    updated_by TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (item, locale)
) WITHOUT ROWID;

CREATE INDEX preview_relay_updated ON preview_relay (updated_at);
