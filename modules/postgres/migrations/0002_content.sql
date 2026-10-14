CREATE TABLE schema_rev (
    rev        integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    hash       text        NOT NULL UNIQUE,
    compiled   jsonb       NOT NULL,
    created_at timestamptz NOT NULL
);

-- Text that is compared, ordered or matched by prefix uses the byte-order "C" collation, so results do not
-- depend on the database's locale and agree with the other adapters.
CREATE TABLE item (
    id                uuid        PRIMARY KEY,
    key               text COLLATE "C",
    type              text        NOT NULL,
    main_locale       text        NOT NULL,
    always_available  boolean     NOT NULL DEFAULT false,
    published_version integer     NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    created_by        text        NOT NULL DEFAULT '',
    updated_by        text        NOT NULL DEFAULT ''
);

-- A key is optional: NULL may repeat, any other value is unique.
CREATE UNIQUE INDEX item_key ON item (key);
CREATE INDEX item_type_updated ON item (type, updated_at, id);

CREATE TABLE item_version (
    item_id    uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    version    integer     NOT NULL,
    schema_rev integer     NOT NULL,
    shared     jsonb       NOT NULL,
    created_at timestamptz NOT NULL,
    created_by text        NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, version)
);

CREATE TABLE item_version_locale (
    item_id uuid    NOT NULL,
    version integer NOT NULL,
    locale  text    NOT NULL,
    name    text    NOT NULL DEFAULT '',
    data    jsonb   NOT NULL,
    PRIMARY KEY (item_id, version, locale),
    FOREIGN KEY (item_id, version) REFERENCES item_version (item_id, version) ON DELETE CASCADE
);

CREATE TABLE draft_part (
    item_id      uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    part         text        NOT NULL,
    base_version integer     NOT NULL DEFAULT 0,
    name         text        NOT NULL DEFAULT '',
    data         jsonb       NOT NULL,
    updated_at   timestamptz NOT NULL,
    updated_by   text        NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, part)
);

CREATE TABLE ref (
    from_item  uuid NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    field_path text NOT NULL,
    to_item    uuid NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('field', 'link')),
    on_delete  text NOT NULL CHECK (on_delete IN ('block', 'nullify', 'warn'))
);

CREATE INDEX ref_from ON ref (from_item, field_path);
CREATE INDEX ref_to ON ref (to_item, from_item, field_path);

-- One row per value of an indexed field; exactly one value column is set.
CREATE TABLE field_index (
    item_id uuid             NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale  text             NOT NULL DEFAULT '',
    field   text             NOT NULL,
    v_text  text COLLATE "C",
    v_num   double precision,
    v_time  timestamptz,
    v_bool  boolean,
    v_ref   uuid,
    CHECK (num_nonnulls(v_text, v_num, v_time, v_bool, v_ref) = 1)
);

CREATE INDEX field_index_item ON field_index (item_id, field);
CREATE INDEX field_index_text ON field_index (field, v_text, item_id) WHERE v_text IS NOT NULL;
CREATE INDEX field_index_num ON field_index (field, v_num, item_id) WHERE v_num IS NOT NULL;
CREATE INDEX field_index_time ON field_index (field, v_time, item_id) WHERE v_time IS NOT NULL;
CREATE INDEX field_index_bool ON field_index (field, v_bool, item_id) WHERE v_bool IS NOT NULL;
CREATE INDEX field_index_ref ON field_index (field, v_ref, item_id) WHERE v_ref IS NOT NULL;

-- tsv is built with the 'simple' configuration (no stemming, no stop words) until per-locale languages exist;
-- plain keeps the source text so the index can be rebuilt with another configuration.
CREATE TABLE content_search (
    item_id uuid     NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale  text     NOT NULL,
    tsv     tsvector NOT NULL,
    plain   text     NOT NULL,
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX content_search_tsv ON content_search USING gin (tsv);

CREATE TABLE content_migration (
    name       text        PRIMARY KEY,
    applied_at timestamptz NOT NULL
);
