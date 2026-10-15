-- Users, groups, roles and their assignments, API tokens and sessions. role_assignment has no foreign key to
-- its principal (a user, group or token), so the store deletes a principal's assignments with it. policies and
-- limitations are JSON text, read whole and never queried. Names are compared in byte order ("C").
CREATE TABLE app_user (
    id            uuid        PRIMARY KEY,
    login         text COLLATE "C" NOT NULL UNIQUE,
    email         text        NOT NULL DEFAULT '',
    name          text        NOT NULL DEFAULT '',
    password_hash text        NOT NULL DEFAULT '',
    disabled      boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    last_login_at timestamptz
);

CREATE TABLE user_group (
    id          uuid        PRIMARY KEY,
    name        text COLLATE "C" NOT NULL UNIQUE,
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

CREATE TABLE group_member (
    group_id uuid NOT NULL REFERENCES user_group (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_member_user ON group_member (user_id);

CREATE TABLE role (
    id          uuid        PRIMARY KEY,
    name        text COLLATE "C" NOT NULL UNIQUE,
    description text        NOT NULL DEFAULT '',
    policies    text        NOT NULL DEFAULT '[]',
    template    text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- The unique constraint also serves lookups by role_id (its first column).
CREATE TABLE role_assignment (
    id             uuid        PRIMARY KEY,
    role_id        uuid        NOT NULL REFERENCES role (id) ON DELETE CASCADE,
    principal_kind text        NOT NULL CHECK (principal_kind IN ('user', 'group', 'token')),
    principal_id   uuid        NOT NULL,
    limitations    text        NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL,
    UNIQUE (role_id, principal_kind, principal_id, limitations)
);

CREATE INDEX role_assignment_principal ON role_assignment (principal_kind, principal_id);

CREATE TABLE api_token (
    id           uuid        PRIMARY KEY,
    name         text        NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('token', 'agent')),
    secret_hash  bytea       NOT NULL UNIQUE,
    prefix       text        NOT NULL,
    created_by   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE TABLE session (
    id_hash      bytea       PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    csrf         text        NOT NULL,
    created_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    ip           text        NOT NULL DEFAULT '',
    user_agent   text        NOT NULL DEFAULT ''
);

CREATE INDEX session_by_user ON session (user_id);
CREATE INDEX session_expires ON session (expires_at);

CREATE TABLE auth_meta (
    key   text PRIMARY KEY,
    value text NOT NULL
);
