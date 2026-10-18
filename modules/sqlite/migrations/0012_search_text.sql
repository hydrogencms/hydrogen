-- content_search stores its text so snippet() can cut highlighted fragments (a contentless table cannot). The old
-- index held no text and cannot be copied: it is emptied, and an item's search rows come back when its
-- projections are written again. `content:reindex` (content.Service.Reindex) rebuilds them all; until then
-- searches find nothing.
DROP TRIGGER content_search_doc_delete;
DROP TABLE content_search;
DELETE FROM content_search_doc;

CREATE VIRTUAL TABLE content_search USING fts5 (
    text,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER content_search_doc_delete AFTER DELETE ON content_search_doc
BEGIN
    DELETE FROM content_search WHERE rowid = old.id;
END;
