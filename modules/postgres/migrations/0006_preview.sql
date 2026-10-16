-- Share links and relayed drafts. Neither table has a foreign key to item: deleting an item must not fail on
-- them, and readers ignore rows of missing items. Expired shares and stale relays are purged by the store's
-- Delete* methods.
CREATE TABLE preview_share (
    id         uuid        PRIMARY KEY,
    token_hash bytea       NOT NULL UNIQUE,
    item       uuid        NOT NULL,
    channel    text        NOT NULL,
    locale     text        NOT NULL DEFAULT '',
    created_by text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX preview_share_item ON preview_share (item, created_at);
CREATE INDEX preview_share_expires ON preview_share (expires_at);

CREATE TABLE preview_relay (
    item       uuid        NOT NULL,
    locale     text        NOT NULL,
    fields     jsonb       NOT NULL,
    rev        bigint      NOT NULL,
    updated_by text        NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (item, locale)
);

CREATE INDEX preview_relay_updated ON preview_relay (updated_at);
