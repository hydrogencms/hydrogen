-- Text that is compared, ordered or matched by range uses the byte-order "C" collation, so results do not
-- depend on the database's locale and agree with the other adapters.
CREATE TABLE page_node (
    id          uuid        PRIMARY KEY,
    site        text        NOT NULL,
    parent_id   uuid        REFERENCES page_node (id) ON DELETE RESTRICT,
    path        text COLLATE "C" NOT NULL, -- "/<id>/<id>/" ancestry including the node itself
    depth       integer     NOT NULL,
    sort_key    text COLLATE "C" NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('page', 'mount', 'link')),
    item_id     uuid        UNIQUE,
    hidden      boolean     NOT NULL DEFAULT false,
    data        jsonb,
    main_locale text        NOT NULL DEFAULT '', -- pages: main locale of the item, for slug fallback
    collection  text        NOT NULL DEFAULT '', -- mounts: the mounted content type
    link_item   uuid,        -- links: the item they point at, to clear them when it goes
    link_node   uuid,        -- links: the node they point at
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

CREATE UNIQUE INDEX page_node_root ON page_node (site) WHERE parent_id IS NULL;
CREATE INDEX page_node_children ON page_node (parent_id, sort_key);
CREATE INDEX page_node_path ON page_node (path);
CREATE INDEX page_node_mounts ON page_node (collection, site) WHERE kind = 'mount';
CREATE INDEX page_node_link_item ON page_node (link_item) WHERE link_item IS NOT NULL;
CREATE INDEX page_node_link_node ON page_node (link_node) WHERE link_node IS NOT NULL;

CREATE TABLE page_slug (
    node_id uuid NOT NULL REFERENCES page_node (id) ON DELETE CASCADE,
    locale  text NOT NULL,
    slug    text NOT NULL,
    name    text NOT NULL,
    vanity  text NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, locale)
);

CREATE TABLE page_mount_item (
    node_id uuid NOT NULL REFERENCES page_node (id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    locale  text NOT NULL,
    rel     text COLLATE "C" NOT NULL, -- path relative to the mount
    PRIMARY KEY (node_id, item_id, locale)
);

CREATE INDEX page_mount_item_item ON page_mount_item (item_id);

CREATE TABLE route (
    site       text        NOT NULL,
    locale     text        NOT NULL, -- '' on manual redirects of every locale
    path       text COLLATE "C" NOT NULL,
    state      text        NOT NULL CHECK (state IN ('current', 'alias', 'redirect', 'manual')),
    item_id    uuid,
    node_id    uuid,
    target     text,
    status     integer     NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (site, locale, path)
);

CREATE INDEX route_item ON route (item_id, site, locale, state);
CREATE INDEX route_site_path ON route (site, path);
CREATE INDEX route_node ON route (node_id, state) WHERE node_id IS NOT NULL;
