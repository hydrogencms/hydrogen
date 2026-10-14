CREATE TABLE outbox_event (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic      text        NOT NULL,
    payload    jsonb       NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE outbox_delivery (
    event_id     bigint      NOT NULL REFERENCES outbox_event (id) ON DELETE CASCADE,
    consumer     text        NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL,
    done_at      timestamptz,
    failed_at    timestamptz,
    last_error   text,
    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX outbox_delivery_due ON outbox_delivery (consumer, available_at, event_id)
    WHERE done_at IS NULL AND failed_at IS NULL;

CREATE TABLE audit_event (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at         timestamptz NOT NULL,
    actor_kind text        NOT NULL,
    actor_id   text        NOT NULL DEFAULT '',
    action     text        NOT NULL,
    subject    text        NOT NULL DEFAULT '',
    data       jsonb
);

CREATE INDEX audit_event_subject ON audit_event (subject, id);
CREATE INDEX audit_event_action ON audit_event (action, id);
