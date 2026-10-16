-- One row per session: the draft state its seeding is based on and the outstanding compaction request.
CREATE TABLE collab_session (
    item_id        uuid        PRIMARY KEY REFERENCES item (id) ON DELETE CASCADE,
    etag           text        NOT NULL DEFAULT '',
    started_at     timestamptz NOT NULL,
    compact_client text        NOT NULL DEFAULT '',
    compact_upto   bigint      NOT NULL DEFAULT 0,
    compact_at     timestamptz
);
