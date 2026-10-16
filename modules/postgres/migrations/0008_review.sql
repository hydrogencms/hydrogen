-- Review workflow: one review per item (about its current draft) and an append-only thread of events. Purging an
-- item removes both.
CREATE TABLE item_review (
    item_id      uuid        PRIMARY KEY REFERENCES item (id) ON DELETE CASCADE,
    state        text        NOT NULL,
    draft_hash   text        NOT NULL DEFAULT '',
    submitted_by text        NOT NULL DEFAULT '',
    submitted_at timestamptz,
    reviewers    jsonb       NOT NULL DEFAULT '[]',
    decided_by   text        NOT NULL DEFAULT '',
    decided_at   timestamptz,
    updated_at   timestamptz NOT NULL
);

CREATE INDEX item_review_state ON item_review (state, submitted_at);

CREATE TABLE review_event (
    id      uuid        PRIMARY KEY,
    item_id uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    kind    text        NOT NULL,
    comment text        NOT NULL DEFAULT '',
    actor   text        NOT NULL DEFAULT '',
    at      timestamptz NOT NULL,
    data    jsonb
);

CREATE INDEX review_event_item ON review_event (item_id, at, id);
