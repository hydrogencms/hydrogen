-- Users, groups, roles and their assignments, API tokens and sessions. role_assignment has no foreign key to
-- its principal (a user, group or token), so the store deletes a principal's assignments with it. policies and
-- limitations are JSON text, read whole and never queried.
CREATE TABLE app_user (
    id            BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    login         TEXT    NOT NULL UNIQUE,
    email         TEXT    NOT NULL DEFAULT '',
    name          TEXT    NOT NULL DEFAULT '',
    password_hash TEXT    NOT NULL DEFAULT '',
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER
) WITHOUT ROWID;

CREATE TABLE user_group (
    id          BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE group_member (
    group_id BLOB NOT NULL REFERENCES user_group (id) ON DELETE CASCADE,
    user_id  BLOB NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
) WITHOUT ROWID;

CREATE INDEX group_member_user ON group_member (user_id);

CREATE TABLE role (
    id          BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    policies    TEXT    NOT NULL DEFAULT '[]',
    template    TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) WITHOUT ROWID;

-- The unique index also serves lookups by role_id (its first column).
CREATE TABLE role_assignment (
    id             BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    role_id        BLOB    NOT NULL REFERENCES role (id) ON DELETE CASCADE,
    principal_kind TEXT    NOT NULL CHECK (principal_kind IN ('user', 'group', 'token')),
    principal_id   BLOB    NOT NULL CHECK (length(principal_id) = 16),
    limitations    TEXT    NOT NULL DEFAULT '{}',
    created_at     INTEGER NOT NULL,
    UNIQUE (role_id, principal_kind, principal_id, limitations)
) WITHOUT ROWID;

CREATE INDEX role_assignment_principal ON role_assignment (principal_kind, principal_id);

CREATE TABLE api_token (
    id           BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    name         TEXT    NOT NULL,
    kind         TEXT    NOT NULL CHECK (kind IN ('token', 'agent')),
    secret_hash  BLOB    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,
    created_by   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER,
    revoked_at   INTEGER
) WITHOUT ROWID;

CREATE TABLE session (
    id_hash      BLOB    NOT NULL PRIMARY KEY,
    user_id      BLOB    NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    csrf         TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    ip           TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT ''
) WITHOUT ROWID;

CREATE INDEX session_by_user ON session (user_id);
CREATE INDEX session_expires ON session (expires_at);

CREATE TABLE auth_meta (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;
