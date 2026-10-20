// Package exportfmt reads and writes the portable export format (format 2) and runs export and import.
//
// An export is a directory:
//
//	manifest.json              format, hydrogen version, source dialect, schema hash, counts, sha-256 per file
//	schema/compiled.json       the compiled schema of the exporting project
//	schema/revisions/<hash>.json   every schema revision a version refers to
//	items.ndjson               one item per line: item row, versions (with locales and schema hash), draft parts
//	items/<type>/<id>.json     or, in the items layout, one indented document per item with the same record
//	content_migrations.ndjson  applied content migrations
//	modules/<name>.ndjson      one file per module exporter, records are the module's own
//	uploads.ndjson             one record per uploaded file the items refer to (media only)
//	files/<sha>                the bytes of each of those files, named by their SHA-256
//
// The ndjson layout suits large exports; the items layout (ExportOptions.Layout, hydrogen export -layout items)
// gives a version-controlled directory line-level diffs. An import reads either. Manifest.ItemStream digests the
// records as items.ndjson would hold them, so Manifest.Fingerprint is the same for both layouts (and for exports
// that predate the field). Readers before the items layout expect items.ndjson.
//
// Format 1 (no uploads) is still read. An import restores files first, into the project's media storage.
//
// Items in the trash are not exported (the store skips them), so an import has no trashed items; module
// exporters should skip records of trashed items the same way.
//
// Files are written and read as streams: nothing but one item (or one module record) is held in memory.
// The manifest is written last; its checksums cover every other file and are verified before an import writes.
package exportfmt
