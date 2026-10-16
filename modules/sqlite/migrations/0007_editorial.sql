-- Trash: a trashed item keeps its rows but is hidden from queries; its key is freed for another item, so the
-- key is unique among items that are not trashed only (a partial unique index).
ALTER TABLE item ADD COLUMN deleted_at INTEGER;
ALTER TABLE item ADD COLUMN deleted_by TEXT NOT NULL DEFAULT '';

DROP INDEX item_key;
CREATE UNIQUE INDEX item_key ON item (key) WHERE deleted_at IS NULL;
CREATE INDEX item_deleted ON item (deleted_at) WHERE deleted_at IS NOT NULL;

-- Releases: a named set of item actions published together, now or at scheduled_at. Purging an item removes it
-- from every release.
CREATE TABLE release (
    id           BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    name         TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    kind         TEXT    NOT NULL DEFAULT 'release',
    status       TEXT    NOT NULL,
    scheduled_at INTEGER,
    published_at INTEGER,
    errors       TEXT,
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_by   TEXT    NOT NULL DEFAULT '',
    scheduled_by TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX release_due ON release (scheduled_at) WHERE status = 'scheduled';
CREATE INDEX release_created ON release (created_at, id);

CREATE TABLE release_item (
    release_id BLOB    NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    item_id    BLOB    NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    action     TEXT    NOT NULL CHECK (action IN ('publish', 'unpublish')),
    locales    TEXT    NOT NULL DEFAULT '[]',
    added_by   TEXT    NOT NULL DEFAULT '',
    added_at   INTEGER NOT NULL,
    PRIMARY KEY (release_id, item_id)
) WITHOUT ROWID;

CREATE INDEX release_item_item ON release_item (item_id);
