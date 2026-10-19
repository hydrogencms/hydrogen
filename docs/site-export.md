# Static export

The `site-export` module renders the website of the [site module](site.md) to files that a CDN or static host
serves. The binary stays private as the authoring tool (admin, preview, exporter) and public traffic never
reaches it. It needs the `site` and `pages` modules.

```sh
hydrogen add site-export
hydrogen site:export                 # render everything, write what changed, publish
hydrogen site:export -channel en     # one channel
hydrogen site:export -out build      # another work tree for this run
hydrogen site:export -full           # ignore the manifest: rewrite and publish everything
```

## What is written

The work tree (`modules.site-export.out`, default `site-export`) holds, per rendered web channel, a directory
named after its `match.host` (else the channel name):

| File | Content |
|---|---|
| `<root>/<path>/index.html` | every page, mounted or routed item, listing page (`/blog/page/2`) and custom route with static params, as a visitor receives them |
| `<root>/<prefix>/404.html` | the channel's 404 page (rendered with status 404) |
| `<root>/__hydrogen/assets/…` | the fingerprinted theme and project assets the pages link to |
| `<root>/_redirects`, `<root>/s3-routing-rules.json`, `<old path>/index.html`, and `redirects.nginx.conf` at the tree root | redirects, per configured format |
| `manifest.json` | every file with its content hash, kind, URL and cache tags |

The live bridge is not exported. Uploaded files (`/assets/…`) and image variants (`/images/…`) stay with the CMS or
its storage; point `media.public_url` at a CDN if the static pages should not link to the CMS host. A URL with a file
extension that answers something other than HTML (a sitemap) is written as that file; an extensionless route that
answers XML, JSON or plain text is written with the matching extension (`/feed` becomes `feed.xml`, with a warning:
serve it at `/feed` with a rewrite or link to the file); everything else gets `index.html` in a directory. Two
files that differ only in case fail the export. Percent-escapes in paths become the decoded file name (`/tags/b%20c` is
`tags/b c/index.html`), which is what static hosts look up.

Every route is rendered through the very code paths a live request takes, one request at a time per worker
(`concurrency`, default 8). A 5xx answer fails the export: files that rendered stay, nothing is deleted or
published, and the next run retries.

## Redirects

Redirects are not pages. The module reads the pages module's route table (former paths of moved items and manual
redirect items), asks the site renderer what a visitor requesting each of them gets, and writes the answers that
are redirects. Targets that answer a redirect and routes that disappeared in an incremental export are included.
`redirects` selects the formats (default `[html]`):

| Format | File | Notes |
|---|---|---|
| `netlify` | `_redirects` | Netlify and Cloudflare Pages: `from to status` |
| `nginx` | `redirects.nginx.conf` (one file at the tree root, for every host) | two `map $host$uri` blocks (`$hydrogen_redirect_to`, `$hydrogen_redirect_status`); `include` the file inside `http {}` and add `if ($hydrogen_redirect_to) { return $hydrogen_redirect_status $hydrogen_redirect_to; }` to each server. A channel that matches any host is keyed by a regular expression |
| `s3` | `s3-routing-rules.json` | the `RoutingRules` of an S3 static website; every rule fires only on a 404 (`HttpErrorCodeReturnedEquals`), so a live sibling such as `/news-archive` is never caught by `/news`; at most 50 rules, longest prefix first |
| `html` | `<old path>/index.html` | a refresh page with a canonical link, for hosts without redirect rules; never replaces a real page |

Paths that a format cannot express safely are left out with a warning.

## Incremental export

Each page records the cache tags it touched in the manifest. With `auto: true` the serving process runs an outbox
consumer (`site.export`): for every event it maps the event to cache tags (the same mapping as cache purges),
purges the renderer's caches, enumerates the targets again and

- renders pages that are new or whose recorded tags intersect the event's tags,
- deletes files of routes that disappeared (and writes redirects when the route table has one),
- skips files whose content hash did not change,
- rewrites the redirect files and hands the changes to the publisher.

An event that invalidates everything (`*`, such as an import) renders every target; unchanged files are still not
rewritten. Without a manifest the first event does a complete export. An export under way blocks the next (a
database lock), so a command run and the consumer never write the same tree at once. Failures are retried by the
outbox with backoff.

Code can run exports too: `kernel.LookupService[*siteexport.Service](services).Export(ctx, siteexport.Options{…})`.

## Configuration

```yaml
modules:
  site-export:
    out: site-export
    redirects: [netlify, html]
    auto: false
    concurrency: 8
    publisher:                 # at most one
      dir:     { path: public, keep: 3 }
      # s3:
      #   bucket: my-site
      #   region: eu-west-1
      #   endpoint: ""          # default AWS; set for MinIO, R2, …
      #   path_style: false
      #   prefix: ""
      #   access_key: ${env:S3_ACCESS_KEY}
      #   secret_key: ${env:S3_SECRET_KEY}
      #   purge_command: [./purge-cdn.sh]
      # command: { argv: [./deploy.sh], timeout: 10m }
```

## Publishers

Without a publisher the tree in `out` is the result. A publisher runs whenever the tree differs from what it last delivered
successfully (recorded beside the work tree in `.<out>.published.json`), so a failed publish is retried by the next
run, and after every `-full` run:

- **dir**: writes a release `<path>/releases/<time>` (files that did not change are hard links to the previous
  release), then points the symlink `<path>/current` at it with an atomic rename; point the web server's root at
  `<path>/current`. The newest `keep` releases stay (default 3). `current` must not exist as a real directory.
- **s3**: uploads changed files and deletes the ones that left the export (redirect configuration files stay out of the bucket), assets first, with the SigV4 signer of the
  media storage (any S3-compatible service). HTML is sent with `Cache-Control: public, max-age=0, must-revalidate`,
  fingerprinted assets with `immutable`. The bucket's state is the object `.hydrogen-export-state.json` (file to
  hash) under the prefix, so any machine can publish and a failed run is finished by the next one; `-full`
  uploads everything and still deletes objects the export no longer has. `purge_command` (argv, no shell) runs after a change with `HYDROGEN_EXPORT_DIR` and
  `HYDROGEN_EXPORT_CHANGED` (a file listing the changed and deleted keys) in its environment. The keys come from
  `${env:…}`.
- **command**: runs `argv` without a shell with the work tree appended as the last argument (an argument containing
  `{dir}` gets it substituted instead); `HYDROGEN_EXPORT_DIR` is set too. A non-zero exit fails the export.

## What static cannot do

Search, forms, personalisation and gated content need a server. Serve those routes from the CMS (they are not
exported unless a custom route declares static params), call the delivery API from the browser, or post forms to a
Hydrogen endpoint. Preview and visual editing keep using the live renderer inside the CMS.
