// Package exportfmt reads and writes the portable export format (format 2) and runs export and import.
//
// An export is a directory:
//
//	manifest.json              format, hydrogen version, source dialect, schema hash, counts, sha-256 per file
//	schema/compiled.json       the compiled schema of the exporting project
//	schema/revisions/<hash>.json   every schema revision a version refers to
//	items.ndjson               one item per line: item row, versions (with locales and schema hash), draft parts
//	content_migrations.ndjson  applied content migrations
//	modules/<name>.ndjson      one file per module exporter, records are the module's own
//	uploads.ndjson             one record per uploaded file the items refer to (media only)
//	files/<sha>                the bytes of each of those files, named by their SHA-256
//
// Format 1 (no uploads) is still read. An import restores files first, into the project's media storage.
//
// Items in the trash are not exported (the store skips them), so an import has no trashed items; module
// exporters should skip records of trashed items the same way.
//
// Files are written and read as streams: nothing but one item (or one module record) is held in memory.
// The manifest is written last; its checksums cover every other file and are verified before an import writes.
package exportfmt
