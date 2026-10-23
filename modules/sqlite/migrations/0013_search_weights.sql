-- Search text has three relevance tiers, one FTS5 column each (high, text = normal, low). The stored text of the
-- old table is the normal tier, which is what an unweighted schema indexes. bm25 weights the columns; the
-- 'rank' setting is stored with the table, so every query's rank column uses it.
DROP TRIGGER content_search_doc_delete;

CREATE VIRTUAL TABLE content_search_new USING fts5 (
    high,
    text,
    low,
    tokenize = 'unicode61 remove_diacritics 2'
);

INSERT INTO content_search_new (rowid, high, text, low) SELECT rowid, '', text, '' FROM content_search;

DROP TABLE content_search;
ALTER TABLE content_search_new RENAME TO content_search;

INSERT INTO content_search (content_search, rank) VALUES ('rank', 'bm25(2.5, 1.0, 0.5)');

CREATE TRIGGER content_search_doc_delete AFTER DELETE ON content_search_doc
BEGIN
    DELETE FROM content_search WHERE rowid = old.id;
END;
