-- Collaboration sessions: the update log, part claims and clients of items being co-edited. Transient state, deleted
-- with the item. seq counts the updates of one item (1, 2, 3, ...: the client detects a missed update by a gap); appends of
-- one item are serialized by a transaction-scoped advisory lock taken before the seq is drawn, so seq order is
-- commit order and no gap fills later.
CREATE TABLE collab_update (
    seq        bigint      NOT NULL,
    item_id    uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    client     text        NOT NULL,
    data       bytea       NOT NULL,
    init       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    PRIMARY KEY (item_id, seq)
);

CREATE TABLE collab_part (
    item_id    uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    part       text        NOT NULL,
    claimed_by text        NOT NULL,
    claimed_at timestamptz NOT NULL,
    seeded     boolean     NOT NULL DEFAULT false,
    PRIMARY KEY (item_id, part)
);

CREATE INDEX collab_part_claimed ON collab_part (claimed_at) WHERE NOT seeded;

CREATE TABLE collab_client (
    client    text        PRIMARY KEY,
    item_id   uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    principal text        NOT NULL DEFAULT '',
    name      text        NOT NULL DEFAULT '',
    locale    text        NOT NULL DEFAULT '',
    focus     text        NOT NULL DEFAULT '',
    idle      boolean     NOT NULL DEFAULT false,
    edit      boolean     NOT NULL DEFAULT false,
    color     text        NOT NULL DEFAULT '',
    joined_at timestamptz NOT NULL,
    seen_at   timestamptz NOT NULL
);

CREATE INDEX collab_client_item ON collab_client (item_id, joined_at);
CREATE INDEX collab_client_seen ON collab_client (seen_at);
