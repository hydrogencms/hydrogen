-- A mounted item that redirects stores its target with its membership (the route row carries it).
ALTER TABLE page_mount_item ADD COLUMN redirect TEXT NOT NULL DEFAULT '';

-- Forced redirects win over pages and mounted items at their path, so they cannot share the route table's key.
CREATE TABLE route_force (
    site       TEXT     NOT NULL,
    locale     TEXT     NOT NULL, -- '' for every locale
    path       TEXT     NOT NULL,
    item_id    BLOB(16) NOT NULL,
    target     TEXT     NOT NULL,
    status     INTEGER  NOT NULL,
    updated_at INTEGER  NOT NULL,
    PRIMARY KEY (site, locale, path)
);

CREATE INDEX route_force_item ON route_force (item_id);
CREATE INDEX route_force_site_path ON route_force (site, path);
