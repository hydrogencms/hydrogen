-- Trash: a trashed item keeps its rows but is hidden from queries; its key is freed for another item, so the
-- key is unique among items that are not trashed only (a partial unique index).
ALTER TABLE item ADD COLUMN deleted_at timestamptz;
ALTER TABLE item ADD COLUMN deleted_by text NOT NULL DEFAULT '';

DROP INDEX item_key;
CREATE UNIQUE INDEX item_key ON item (key) WHERE deleted_at IS NULL;
CREATE INDEX item_deleted ON item (deleted_at) WHERE deleted_at IS NOT NULL;

-- Releases: a named set of item actions published together, now or at scheduled_at. Purging an item removes it
-- from every release.
CREATE TABLE release (
    id           uuid        PRIMARY KEY,
    name         text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    kind         text        NOT NULL DEFAULT 'release',
    status       text        NOT NULL,
    scheduled_at timestamptz,
    published_at timestamptz,
    errors       jsonb,
    created_by   text        NOT NULL DEFAULT '',
    updated_by   text        NOT NULL DEFAULT '',
    scheduled_by text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);

CREATE INDEX release_due ON release (scheduled_at) WHERE status = 'scheduled';
CREATE INDEX release_created ON release (created_at, id);

CREATE TABLE release_item (
    release_id uuid        NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    item_id    uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    action     text        NOT NULL CHECK (action IN ('publish', 'unpublish')),
    locales    jsonb       NOT NULL DEFAULT '[]',
    added_by   text        NOT NULL DEFAULT '',
    added_at   timestamptz NOT NULL,
    PRIMARY KEY (release_id, item_id)
);

CREATE INDEX release_item_item ON release_item (item_id);
