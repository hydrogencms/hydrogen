CREATE TABLE site_limit (
    bucket       TEXT    NOT NULL,
    window_start INTEGER NOT NULL, -- Unix seconds
    hits         INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL, -- Unix seconds
    PRIMARY KEY (bucket, window_start)
);

CREATE INDEX site_limit_expires ON site_limit (expires_at);
