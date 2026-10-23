CREATE TABLE site_limit (
    bucket       TEXT   NOT NULL,
    window_start BIGINT NOT NULL, -- Unix seconds
    hits         BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL, -- Unix seconds
    PRIMARY KEY (bucket, window_start)
);

CREATE INDEX site_limit_expires ON site_limit (expires_at);
