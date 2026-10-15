-- Index and search rows exist per projection: scope 'p' is the published version (delivery), 'w' the working
-- copy (the draft overlaid on the published version; management). module is '' for content rows, else the
-- owning module; module rows are written to both scopes.
ALTER TABLE field_index ADD COLUMN scope text NOT NULL DEFAULT 'p' CHECK (scope IN ('p', 'w'));
ALTER TABLE field_index ADD COLUMN module text NOT NULL DEFAULT '';

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

ALTER TABLE content_search ADD COLUMN scope text NOT NULL DEFAULT 'p' CHECK (scope IN ('p', 'w'));
ALTER TABLE content_search DROP CONSTRAINT content_search_pkey;
ALTER TABLE content_search ADD PRIMARY KEY (item_id, scope, locale);

-- One text index per scope: queries name the scope as a literal, so each uses its own partial index.
DROP INDEX content_search_tsv;
CREATE INDEX content_search_tsv_p ON content_search USING gin (tsv) WHERE scope = 'p';
CREATE INDEX content_search_tsv_w ON content_search USING gin (tsv) WHERE scope = 'w';
