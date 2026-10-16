-- Review workflow: one review per item (about its current draft) and an append-only thread of events. Purging an
-- item removes both.
CREATE TABLE item_review (
    item_id      BLOB    NOT NULL PRIMARY KEY REFERENCES item (id) ON DELETE CASCADE,
    state        TEXT    NOT NULL,
    draft_hash   TEXT    NOT NULL DEFAULT '',
    submitted_by TEXT    NOT NULL DEFAULT '',
    submitted_at INTEGER,
    reviewers    TEXT    NOT NULL DEFAULT '[]',
    decided_by   TEXT    NOT NULL DEFAULT '',
    decided_at   INTEGER,
    updated_at   INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX item_review_state ON item_review (state, submitted_at);

CREATE TABLE review_event (
    id      BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    item_id BLOB    NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL,
    comment TEXT    NOT NULL DEFAULT '',
    actor   TEXT    NOT NULL DEFAULT '',
    at      INTEGER NOT NULL,
    data    TEXT
) WITHOUT ROWID;

CREATE INDEX review_event_item ON review_event (item_id, at, id);
