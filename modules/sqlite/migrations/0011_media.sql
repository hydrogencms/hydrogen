-- Uploaded files, content-addressed by the SHA-256 of their bytes (lowercase hex). The bytes live in the media
-- storage. touched_at is the last upload of the same file; garbage collection removes files that are old,
-- untouched and referenced by no item.
CREATE TABLE media_upload (
    sha        TEXT    NOT NULL PRIMARY KEY CHECK (length(sha) = 64),
    mime       TEXT    NOT NULL,
    size       INTEGER NOT NULL,
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0,
    created_by TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    touched_at INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX media_upload_touched ON media_upload (touched_at);
