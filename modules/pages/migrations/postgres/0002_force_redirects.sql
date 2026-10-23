-- A mounted item that redirects stores its target with its membership (the route row carries it).
ALTER TABLE page_mount_item ADD COLUMN redirect text NOT NULL DEFAULT '';

-- Forced redirects win over pages and mounted items at their path, so they cannot share the route table's key.
CREATE TABLE route_force (
    site       text        NOT NULL,
    locale     text        NOT NULL, -- '' for every locale
    path       text COLLATE "C" NOT NULL,
    item_id    uuid        NOT NULL,
    target     text        NOT NULL,
    status     integer     NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (site, locale, path)
);

CREATE INDEX route_force_item ON route_force (item_id);
CREATE INDEX route_force_site_path ON route_force (site, path);
