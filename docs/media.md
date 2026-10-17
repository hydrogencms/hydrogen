# Media

Files (images, PDFs, video, anything) are stored by the media service and managed as content. A type marked
`upload:` in the schema is a **collection of assets**: each of its items is one file plus the fields you define
for it (alt text, caption, credits). Other types point at assets with `asset` fields, and a plain `file` field
holds a file directly. Media is on when the schema has an upload type or a `file` field.

```yaml
types:
  image:
    upload: { accept: [image/*], max_size: 20MB }
    fields:
      alt:     { type: string, translatable: true, max: 300 }
      caption: { type: text, translatable: true }
```

An upload type gets three built-in fields: `file` (required), `folder` (a path such as `/marketing/2026`, default
`/`) and `tags`. Versioning, drafts, access rules, review, releases and the trash apply to assets like any item.

### Browsing assets

Two additions to the management API serve a media library:

- `where[folder][under]=/a` (any indexed text field accepts `under`) matches `/a` itself and everything below it
  (`/a/b`, `/a/b/c`), never a sibling such as `/ab`. A trailing slash is ignored; `/` matches every folder.
- `GET /collections/{type}/facets?fields=folder,tags` counts the items per value of up to five indexed text
  fields in one grouped query per field, and answers `{"folder": [{"value": "/a", "count": 3}, …], "tags": […]}`:
  most items first, then by value, at most 1000 values per field. A list field (`tags`) counts an item once per
  value. `locale`, `status` and `where[…]` narrow the items counted like in a list (`status=published` counts
  the published content, anything else the drafts too); trashed items are not counted. The counts cover what the
  principal may read: a read grant limited to own items counts own items only (the scope is part of the query).

## Files

A file is stored once, under the SHA-256 of its bytes. The `file` value of an item is

```json
{ "id": "<sha256 hex>", "name": "cat.png", "mime": "image/png", "size": 2048, "width": 800, "height": 600,
  "focal": { "x": 0.4, "y": 0.3 } }
```

The client chooses only `name` and `focal`. The server fills in the rest from what it stored, so a client cannot
claim another type or size.

### Uploading

`POST /api/manage/v1/uploads` takes `multipart/form-data` with one `file` part and, optionally and **before** the
file, a `type` field naming the content type the file is for. With `type` the type's `accept` patterns and
`max_size` are checked right away; without it only the global limit applies. The request needs `content.create`
or `content.edit` on that type (on any type that holds files when `type` is omitted).

The answer is the `file` value to put into an item (create or update the item with it, then publish):

| Status | Meaning |
| --- | --- |
| `201` | stored; `Location` is the file's URL |
| `200` | these bytes were already stored (the same JSON; the upload only refreshed the record) |
| `400` | not multipart, no file, an empty file, a `type` after the file, an unknown `type` (`unknown_type`) |
| `413` `too_large` | over `media.max_upload` or the type's `max_size` |
| `415` `unsupported_media_type` | the detected type is not in the type's `accept`, or an image that does not parse |

The type is **detected from the content**, never taken from the client's header or file name: JPEG, PNG, GIF and
WebP are recognised by their signature and their dimensions read; SVG is recognised by an `<svg>` root element
(its size comes from `width`/`height`, else the `viewBox`); other files get the standard content sniffing, and the
file name is only a hint for formats with no signature, such as CSV. A name can never turn a file into HTML, XML,
SVG or an image. The name refines the type of a **new** file only: uploading bytes that are already stored keeps
the type of the first record (also for the `accept` check), so the same CSV uploaded as `a.csv` and later as `a.txt`
stays `text/csv`.

Limits that protect the server: at most 8 uploads are handled at once per server (more get `429 busy` with
`Retry-After`), and the body of one upload must arrive within 10 minutes (`408 request_timeout`).

### Serving

`GET /assets/{sha}/{name}` is public and outside `/api`. The `name` is only the download name; the file is found by
its hash. Responses carry:

```
Content-Type: <detected type>          ETag: "<sha>"
Cache-Control: public, max-age=31536000, immutable
X-Content-Type-Options: nosniff
Content-Disposition: inline; filename=…   (images, video, audio, PDF; otherwise attachment)
Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox   (SVG, HTML, XML)
```

Range requests, conditional requests (`If-None-Match`, `If-Modified-Since`) and `HEAD` work. A revalidation that
the validators settle (a matching `If-None-Match`) is answered with `304` without asking the storage; a plain `GET`
opens the object once, and an object whose bytes are missing is a `404` before any file header is sent. **Anyone with the
URL can read the file**; the hash is unguessable without the file, but treat an asset URL like a public link.
Unpublished assets are reachable by URL too; the URL appears only to those who can read the item.

With `media.public_url` set (a CDN that serves `{sha}/{name}`, usually by fetching `/assets/…` from this server),
delivery responses use `<public_url>/<sha>/<name>` instead of `/assets/<sha>/<name>`, and the admin reads the same
base from its `config.json` (`media.assetBase`, next to `media.maxUpload`).

**The hardening headers must reach the browser.** A CDN or `public_url` host should front this server's `/assets`
(pass the origin's response headers through), or add the same headers itself: `X-Content-Type-Options: nosniff`,
`Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox` on SVG, HTML and XML files, and a
`Content-Disposition` (`attachment` except for images, video, audio and PDF). Without them, an uploaded SVG or HTML
file opened at that host would run with the host's origin.

### Resized images

Delivery shows the original file. For resized renditions (thumbnails, `srcset`) add the images module; see
[images.md](images.md).

### In delivery

The delivery API shows an `asset` field as the asset itself, in the response's locale:

```json
{ "id": "<asset item id>", "url": "/assets/<sha>/cat.png", "name": "cat.png", "mime": "image/png", "size": 2048,
  "width": 800, "height": 600, "focal": { "x": 0.4, "y": 0.3 }, "fields": { "alt": "A cat", "caption": "…" } }
```

The asset is looked up like any item: it is `null` unless it is published (or, in a preview, has a draft) and the
channel delivers its type. **Add the upload type to the `collections` of every channel that shows assets.** The
response carries the asset's cache tag, so publishing the asset purges the responses that show it. A trashed asset
shows as `null` too, but referrers keep its id until it is purged (so a restore brings it back); the purge then
applies the field's `on_delete` (default `nullify`).

## Configuration

```yaml
media:
  max_upload: 50MB                      # default 50MB; a type's upload.max_size can only lower it
  storage:
    driver: local                       # local (default) or s3
    dir: data/media                     # local: relative to the working directory
  public_url: ""                        # optional CDN base for delivery URLs
```

Sizes accept `B`, `KB`, `MB`, `GB` (1024-based). `config:check` validates the section when the schema needs media
and warns when `media` is configured but nothing uses it.

### S3-compatible storage

```yaml
media:
  storage:
    driver: s3
    bucket: my-site-media
    region: eu-west-1                   # default us-east-1
    endpoint: ""                        # default https://s3.<region>.amazonaws.com; e.g. http://minio:9000
    path_style: false                   # true for MinIO and most self-hosted services
    prefix: media/                      # keys are stored under this prefix
    access_key: ${env:S3_ACCESS_KEY}    # secrets must come from the environment
    secret_key: ${env:S3_SECRET_KEY}
    session_token: ${env:S3_SESSION_TOKEN}   # optional, for temporary credentials
```

Works with AWS S3, MinIO and Cloudflare R2 (endpoint `https://<account>.r2.cloudflarestorage.com`, region `auto`).
Requests are signed with AWS Signature Version 4 by hydrogen itself (no SDK), each file is one `PUT` (so the object
size is bounded by `max_upload`), and the payload is hashed and signed. Objects are written with
`Content-Type`, `Cache-Control: public, max-age=31536000, immutable` and a `Content-Disposition` (`inline` for
images except SVG, video, audio and PDF; `attachment` for everything else, SVG, HTML and XML included), so a CDN or
public bucket can serve them directly. S3 cannot store a `Content-Security-Policy` or `X-Content-Type-Options`
with an object: serving straight from a bucket needs a CDN rule that adds them (see above), or serve through
`/assets`.

Permissions: `s3:PutObject`, `s3:GetObject` and `s3:DeleteObject` on the prefix, and `s3:ListBucket` for garbage
collection of stored files without a record. Without `s3:ListBucket`, S3 answers `403` (not `404`) for a missing
key; hydrogen reads a `403` on its existence check (`HEAD`) as "missing", so uploads still work, but the check
cannot tell a missing key from a real permission problem, and `media.gc` fails when it lists the bucket.

The local driver writes a temporary file and renames it into place, so a crash or a concurrent reader never sees a
partial file; files live under `ab/cd/<sha>`.

## Garbage collection

Uploads that no item uses are removed by the job `media.gc` (daily; `hydrogen job:run media.gc` runs it now). An
upload is collected when

- it was last uploaded more than 24 hours ago (uploading the same bytes again starts the 24 hours over), and
- no **version or draft of any item**, trashed items included, holds its hash.

An editor therefore has a day to attach a file to an item. Purging an item (or the trash retention job) leaves its
files for the next run. The job also deletes stored bytes that have no upload record and are older than a day
(left by an upload that failed half way). Each run is audited as `media.gc`, uploads as `media.upload`; outbox
topics `media.uploaded` and `media.deleted` carry the hashes. A run that finds nothing stale does not scan the
content at all.

What the job reads and what it does not:

- **Every item in the database is read, whatever its type**, not only the types that have a file field in today's
  schema: a field that was removed or renamed keeps the files of the versions that had it. Export reads every item
  the same way.
- Writing an item that attaches a file touches the upload inside the write's transaction, so a file attached while
  a run is in progress is kept; a file that was collected before the write is an ordinary `file_not_found` error.
- A file that lives **only in unsaved collaborative editing state or in a relayed preview draft** is invisible to
  the job (both are opaque to it). It could be collected if it stays there for longer than the 24 hours since its
  upload. Relayed drafts expire after one hour and a collaboration session ends minutes after its last client
  leaves, and `media` refuses a collection age below 24 hours, so in practice a file only disappears from such a
  state when nobody saved it for a day.
- Removing the bytes of a collected record is serialised with storing the same file in the same process and
  re-checks the record just before the delete. Between servers an upload that stored its bytes while another
  server collected the record looks again after its record is committed and stores the bytes once more if they
  were removed.

## For module authors

`media.Service` (`Services.Media`, nil when media is off) offers `Upload`, `Lookup`, `Open`, `Touch`, `URL`,
`Records` and `Restore` (import). `Restore` hashes the stream while storing it and refuses a file whose SHA-256
(lowercase hex), size, sniffed type or image dimensions differ from the export's record, so an import cannot
plant bytes under another file's hash. Content stays independent of it through the small `content.Files` interface, which the kernel
satisfies with the service. Storage drivers implement `media.Storage` (`Put`, `Get`, `GetRange`, `Stat`, `Delete`,
`List`).

## Tests

`go test ./media` covers the signer (AWS's published test vectors), both drivers and the S3 client against a fake
service. To run the driver contract against a real S3-compatible endpoint set `HYDROGEN_TEST_S3_BUCKET`,
`HYDROGEN_TEST_S3_ACCESS_KEY`, `HYDROGEN_TEST_S3_SECRET_KEY` and optionally `HYDROGEN_TEST_S3_ENDPOINT`,
`HYDROGEN_TEST_S3_REGION` and `HYDROGEN_TEST_S3_PATH_STYLE=1`; it works under a random prefix and cleans up.
