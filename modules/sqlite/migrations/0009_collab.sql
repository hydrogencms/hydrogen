-- Collaboration sessions: the update log, part claims and clients of items being co-edited. Transient state, deleted
-- with the item. seq counts the updates of one item (1, 2, 3, ...: the client detects a missed update by a gap); SQLite has
-- one writer, so seq order is commit order.
CREATE TABLE collab_update (
    seq        INTEGER NOT NULL,
    item_id    BLOB    NOT NULL CHECK (length(item_id) = 16) REFERENCES item (id) ON DELETE CASCADE,
    client     TEXT    NOT NULL,
    data       BLOB    NOT NULL,
    init       TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    PRIMARY KEY (item_id, seq)
) WITHOUT ROWID;

CREATE TABLE collab_part (
    item_id    BLOB    NOT NULL CHECK (length(item_id) = 16) REFERENCES item (id) ON DELETE CASCADE,
    part       TEXT    NOT NULL,
    claimed_by TEXT    NOT NULL,
    claimed_at INTEGER NOT NULL,
    seeded     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, part)
) WITHOUT ROWID;

CREATE INDEX collab_part_claimed ON collab_part (claimed_at) WHERE seeded = 0;

CREATE TABLE collab_client (
    client    TEXT    NOT NULL PRIMARY KEY,
    item_id   BLOB    NOT NULL CHECK (length(item_id) = 16) REFERENCES item (id) ON DELETE CASCADE,
    principal TEXT    NOT NULL DEFAULT '',
    name      TEXT    NOT NULL DEFAULT '',
    locale    TEXT    NOT NULL DEFAULT '',
    focus     TEXT    NOT NULL DEFAULT '',
    idle      INTEGER NOT NULL DEFAULT 0,
    edit      INTEGER NOT NULL DEFAULT 0,
    color     TEXT    NOT NULL DEFAULT '',
    joined_at INTEGER NOT NULL,
    seen_at   INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX collab_client_item ON collab_client (item_id, joined_at);
CREATE INDEX collab_client_seen ON collab_client (seen_at);
