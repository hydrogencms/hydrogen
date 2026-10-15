-- Delivery snapshots: the JSON of one published item in one locale, built when a version is written.
-- payload is TEXT and returned byte for byte; refs is the referenced item ids as concatenated 16-byte values
-- (sorted, unique), so no JSON has to be decoded to read them.
CREATE TABLE published_snapshot (
    item_id          BLOB    NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale           TEXT    NOT NULL,
    version          INTEGER NOT NULL,
    type             TEXT    NOT NULL,
    key              TEXT,
    main_locale      TEXT    NOT NULL,
    always_available INTEGER NOT NULL DEFAULT 0,
    payload          TEXT    NOT NULL,
    etag             TEXT    NOT NULL,
    refs             BLOB    NOT NULL,
    published_at     INTEGER NOT NULL,
    PRIMARY KEY (item_id, locale)
) WITHOUT ROWID;

CREATE INDEX published_snapshot_key ON published_snapshot (key, locale) WHERE key IS NOT NULL;
