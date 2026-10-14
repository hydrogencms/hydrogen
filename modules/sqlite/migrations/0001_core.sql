CREATE TABLE outbox_event (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    topic      TEXT    NOT NULL,
    payload    BLOB    NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE outbox_delivery (
    event_id     INTEGER NOT NULL REFERENCES outbox_event (id) ON DELETE CASCADE,
    consumer     TEXT    NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    available_at INTEGER NOT NULL,
    done_at      INTEGER,
    failed_at    INTEGER,
    last_error   TEXT,
    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX outbox_delivery_due ON outbox_delivery (consumer, available_at, event_id)
    WHERE done_at IS NULL AND failed_at IS NULL;

CREATE TABLE audit_event (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    at         INTEGER NOT NULL,
    actor_kind TEXT    NOT NULL,
    actor_id   TEXT    NOT NULL DEFAULT '',
    action     TEXT    NOT NULL,
    subject    TEXT    NOT NULL DEFAULT '',
    data       BLOB
);

CREATE INDEX audit_event_subject ON audit_event (subject, id);
CREATE INDEX audit_event_action ON audit_event (action, id);
