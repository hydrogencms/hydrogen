CREATE TABLE page_node (
    id          BLOB(16) PRIMARY KEY,
    site        TEXT     NOT NULL,
    parent_id   BLOB(16) REFERENCES page_node (id) ON DELETE RESTRICT,
    path        TEXT     NOT NULL, -- "/<id>/<id>/" ancestry including the node itself
    depth       INTEGER  NOT NULL,
    sort_key    TEXT     NOT NULL,
    kind        TEXT     NOT NULL CHECK (kind IN ('page', 'mount', 'link')),
    item_id     BLOB(16) UNIQUE,
    hidden      INTEGER  NOT NULL DEFAULT 0,
    data        TEXT,
    main_locale TEXT     NOT NULL DEFAULT '', -- pages: main locale of the item, for slug fallback
    collection  TEXT     NOT NULL DEFAULT '', -- mounts: the mounted content type
    link_item   BLOB(16), -- links: the item they point at, to clear them when it goes
    link_node   BLOB(16), -- links: the node they point at
    created_at  INTEGER  NOT NULL,
    updated_at  INTEGER  NOT NULL
);

CREATE UNIQUE INDEX page_node_root ON page_node (site) WHERE parent_id IS NULL;
CREATE INDEX page_node_children ON page_node (parent_id, sort_key);
CREATE INDEX page_node_path ON page_node (path);
CREATE INDEX page_node_mounts ON page_node (collection, site) WHERE kind = 'mount';
CREATE INDEX page_node_link_item ON page_node (link_item) WHERE link_item IS NOT NULL;
CREATE INDEX page_node_link_node ON page_node (link_node) WHERE link_node IS NOT NULL;

CREATE TABLE page_slug (
    node_id BLOB(16) NOT NULL REFERENCES page_node (id) ON DELETE CASCADE,
    locale  TEXT     NOT NULL,
    slug    TEXT     NOT NULL,
    name    TEXT     NOT NULL,
    vanity  TEXT     NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, locale)
);

CREATE TABLE page_mount_item (
    node_id BLOB(16) NOT NULL REFERENCES page_node (id) ON DELETE CASCADE,
    item_id BLOB(16) NOT NULL,
    locale  TEXT     NOT NULL,
    rel     TEXT     NOT NULL, -- path relative to the mount
    PRIMARY KEY (node_id, item_id, locale)
);

CREATE INDEX page_mount_item_item ON page_mount_item (item_id);

CREATE TABLE route (
    site       TEXT     NOT NULL,
    locale     TEXT     NOT NULL, -- '' on manual redirects of every locale
    path       TEXT     NOT NULL,
    state      TEXT     NOT NULL CHECK (state IN ('current', 'alias', 'redirect', 'manual')),
    item_id    BLOB(16),
    node_id    BLOB(16),
    target     TEXT,
    status     INTEGER  NOT NULL DEFAULT 0,
    updated_at INTEGER  NOT NULL,
    PRIMARY KEY (site, locale, path)
);

CREATE INDEX route_item ON route (item_id, site, locale, state);
CREATE INDEX route_site_path ON route (site, path);
CREATE INDEX route_node ON route (node_id, state) WHERE node_id IS NOT NULL;
