-- One row per session: the draft state its seeding is based on and the outstanding compaction request.
CREATE TABLE collab_session (
    item_id        BLOB    NOT NULL PRIMARY KEY CHECK (length(item_id) = 16) REFERENCES item (id) ON DELETE CASCADE,
    etag           TEXT    NOT NULL DEFAULT '',
    started_at     INTEGER NOT NULL,
    compact_client TEXT    NOT NULL DEFAULT '',
    compact_upto   INTEGER NOT NULL DEFAULT 0,
    compact_at     INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
