-- Uploaded files, content-addressed by the SHA-256 of their bytes (lowercase hex). The bytes live in the media
-- storage. touched_at is the last upload of the same file; garbage collection removes files that are old,
-- untouched and referenced by no item.
CREATE TABLE media_upload (
    sha        text        PRIMARY KEY CHECK (length(sha) = 64),
    mime       text        NOT NULL,
    size       bigint      NOT NULL,
    width      integer     NOT NULL DEFAULT 0,
    height     integer     NOT NULL DEFAULT 0,
    created_by text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    touched_at timestamptz NOT NULL
);

CREATE INDEX media_upload_touched ON media_upload (touched_at);
