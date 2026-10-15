-- Index and search rows exist per projection: scope 'p' is the published version (delivery), 'w' the working
-- copy (the draft overlaid on the published version; management). module is '' for content rows, else the
-- owning module; module rows are written to both scopes.
ALTER TABLE field_index ADD COLUMN scope TEXT NOT NULL DEFAULT 'p' CHECK (scope IN ('p', 'w'));
ALTER TABLE field_index ADD COLUMN module TEXT NOT NULL DEFAULT '';

DROP INDEX field_index_item;
DROP INDEX field_index_text;
DROP INDEX field_index_num;
DROP INDEX field_index_time;
DROP INDEX field_index_bool;
DROP INDEX field_index_ref;

CREATE INDEX field_index_item ON field_index (item_id, scope, field);
CREATE INDEX field_index_text ON field_index (scope, field, v_text, item_id) WHERE v_text IS NOT NULL;
CREATE INDEX field_index_num ON field_index (scope, field, v_num, item_id) WHERE v_num IS NOT NULL;
CREATE INDEX field_index_time ON field_index (scope, field, v_time, item_id) WHERE v_time IS NOT NULL;
CREATE INDEX field_index_bool ON field_index (scope, field, v_bool, item_id) WHERE v_bool IS NOT NULL;
CREATE INDEX field_index_ref ON field_index (scope, field, v_ref, item_id) WHERE v_ref IS NOT NULL;

-- The unique key gains scope; the rows keep their ids, which are the rowids of content_search.
DROP TRIGGER content_search_doc_delete;

CREATE TABLE content_search_doc_new (
    id      INTEGER PRIMARY KEY,
    item_id BLOB NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    scope   TEXT NOT NULL DEFAULT 'p' CHECK (scope IN ('p', 'w')),
    locale  TEXT NOT NULL,
    UNIQUE (item_id, scope, locale)
);

INSERT INTO content_search_doc_new (id, item_id, scope, locale) SELECT id, item_id, 'p', locale FROM content_search_doc;
DROP TABLE content_search_doc;
ALTER TABLE content_search_doc_new RENAME TO content_search_doc;

CREATE TRIGGER content_search_doc_delete AFTER DELETE ON content_search_doc
BEGIN
    DELETE FROM content_search WHERE rowid = old.id;
END;
