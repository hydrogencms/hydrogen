-- A page can serve a path other than its tree position; its descendants build on it.
ALTER TABLE page_slug ADD COLUMN canonical TEXT NOT NULL DEFAULT '';
