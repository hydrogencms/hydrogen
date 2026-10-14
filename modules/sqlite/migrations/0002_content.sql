CREATE TABLE schema_rev (
    rev        INTEGER PRIMARY KEY AUTOINCREMENT,
    hash       TEXT    NOT NULL UNIQUE,
    compiled   TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE item (
    id                BLOB    NOT NULL PRIMARY KEY CHECK (length(id) = 16),
    key               TEXT,
    type              TEXT    NOT NULL,
    main_locale       TEXT    NOT NULL,
    always_available  INTEGER NOT NULL DEFAULT 0,
    published_version INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    created_by        TEXT    NOT NULL DEFAULT '',
    updated_by        TEXT    NOT NULL DEFAULT ''
) WITHOUT ROWID;

-- A key is optional: NULL may repeat, any other value is unique.
CREATE UNIQUE INDEX item_key ON item (key);
CREATE INDEX item_type_updated ON item (type, updated_at, id);

CREATE TABLE item_version (
    item_id    BLOB    NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    schema_rev INTEGER NOT NULL,
    shared     TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    created_by TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, version)
) WITHOUT ROWID;

CREATE TABLE item_version_locale (
    item_id BLOB    NOT NULL,
    version INTEGER NOT NULL,
    locale  TEXT    NOT NULL,
    name    TEXT    NOT NULL DEFAULT '',
    data    TEXT    NOT NULL,
    PRIMARY KEY (item_id, version, locale),
    FOREIGN KEY (item_id, version) REFERENCES item_version (item_id, version) ON DELETE CASCADE
) WITHOUT ROWID;

CREATE TABLE draft_part (
    item_id      BLOB    NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    part         TEXT    NOT NULL,
    base_version INTEGER NOT NULL DEFAULT 0,
    name         TEXT    NOT NULL DEFAULT '',
    data         TEXT    NOT NULL,
    updated_at   INTEGER NOT NULL,
    updated_by   TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, part)
) WITHOUT ROWID;

CREATE TABLE ref (
    from_item  BLOB NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    field_path TEXT NOT NULL,
    to_item    BLOB NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('field', 'link')),
    on_delete  TEXT NOT NULL CHECK (on_delete IN ('block', 'nullify', 'warn'))
);

CREATE INDEX ref_from ON ref (from_item, field_path);
CREATE INDEX ref_to ON ref (to_item, from_item, field_path);

-- One row per value of an indexed field; exactly one value column is set.
CREATE TABLE field_index (
    item_id BLOB NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL DEFAULT '',
    field   TEXT NOT NULL,
    v_text  TEXT,
    v_num   REAL,
    v_time  INTEGER,
    v_bool  INTEGER,
    v_ref   BLOB,
    CHECK ((v_text IS NOT NULL) + (v_num IS NOT NULL) + (v_time IS NOT NULL) + (v_bool IS NOT NULL)
         + (v_ref IS NOT NULL) = 1)
);

CREATE INDEX field_index_item ON field_index (item_id, field);
CREATE INDEX field_index_text ON field_index (field, v_text, item_id) WHERE v_text IS NOT NULL;
CREATE INDEX field_index_num ON field_index (field, v_num, item_id) WHERE v_num IS NOT NULL;
CREATE INDEX field_index_time ON field_index (field, v_time, item_id) WHERE v_time IS NOT NULL;
CREATE INDEX field_index_bool ON field_index (field, v_bool, item_id) WHERE v_bool IS NOT NULL;
CREATE INDEX field_index_ref ON field_index (field, v_ref, item_id) WHERE v_ref IS NOT NULL;

-- content_search holds the text of one (item, locale) document under the rowid of its content_search_doc row.
CREATE TABLE content_search_doc (
    id      INTEGER PRIMARY KEY,
    item_id BLOB NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    UNIQUE (item_id, locale)
);

CREATE VIRTUAL TABLE content_search USING fts5 (
    text,
    content = '',
    contentless_delete = 1,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER content_search_doc_delete AFTER DELETE ON content_search_doc
BEGIN
    DELETE FROM content_search WHERE rowid = old.id;
END;

CREATE TABLE content_migration (
    name       TEXT    NOT NULL PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
