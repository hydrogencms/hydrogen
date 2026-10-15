-- Delivery snapshots: the JSON of one published item in one locale, built when a version is written.
-- payload is text (never jsonb) and returned byte for byte; refs is a uuid[] of the referenced item ids
-- (sorted, unique).
CREATE TABLE published_snapshot (
    item_id          uuid        NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    locale           text        NOT NULL,
    version          integer     NOT NULL,
    type             text        NOT NULL,
    key              text COLLATE "C",
    main_locale      text        NOT NULL,
    always_available boolean     NOT NULL DEFAULT false,
    payload          text        NOT NULL,
    etag             text        NOT NULL,
    refs             uuid[]      NOT NULL DEFAULT '{}',
    published_at     timestamptz NOT NULL,
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX published_snapshot_key ON published_snapshot (key, locale) WHERE key IS NOT NULL;
