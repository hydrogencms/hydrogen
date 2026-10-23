# Changelog

All notable changes to Hydrogen are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/). Hydrogen is 0.x: APIs may change between minor versions.

## [Unreleased]

### Added

- Choose item ids on create: `content.CreateInput.ID`, `id` in the body of `POST /collections/{type}/items` and
  `client.CreateInput.ID`. Any non-nil UUID is accepted; an id already in use, trashed items included, answers 409
  `duplicate_id` (`content.ErrDuplicateID`).

## [0.3.0] - 2026-10-22

### Added

- Site module templates: generic functions (`dict`, `list`, arithmetic, `seq`, `default`, string helpers, `date`,
  `json`, `first`, `last`), project functions with `site.Funcs`, reading helpers `items`, `item` and `query` (they tag
  the page like any read), `partial` for computed template names with `hasTemplate` to test for one, `blocks` over another item's blocks
  value, and `View.Client()` for Go components.
- Rich text: the marks `underline`, `strike`, `superscript` and `subscript`, tables (`table`, with rows and `tableCell`/
  `tableHeader` cells carrying `colspan`, `rowspan` and `colwidth`) and block-level `image` nodes (`src` as an http(s)
  URL or root-relative path, `alt`, `title`, `width`, `height`). The admin editor has buttons, a table menu and an
  image popover (`prosemirror-tables`, in the lazy editor chunk); the site module and the `client`, `react` and `astro`
  renderers output them. The `@hydrogencms/client/richtext` size budget is 1.25 KB.
- Delivery field projection: `?fields=title,seo.title` on `/items/{id}`, `/items/by-key/{key}`, `/singletons/{type}`,
  listings, `/search` and the pages module's `/tree` (which then puts the selected fields of each page's item on its
  node, in one batch). Items keep their envelope, `fields` holds the selection; fields left out are not inlined,
  expanded, encoded, cached or decoded, and `include` and `links` follow the selected fields only. Modules use
  `delivery.FieldsParam` and `delivery.ItemsFields`. The site module's `items`, `item` and `query` helpers take
  `fields` (and keep detail-route links working), the GraphQL module projects every request to the fields a query
  selects, and `@hydrogencms/client` and `api/client` gain a `fields` option.
- Site module custom routes answer raw bodies: `site.Response` gains `Partial` (a template or component without the
  layout: fragments, XML templates), `Body`, `JSON` and `XML`, with `ContentType` and `MaxAge`; a response sets exactly
  one of them (or `Template`/`Redirect`). Raw answers keep the read tags, `Cacheable`, purges and the static export;
  `Cacheable` GET answers carry an `ETag` and answer 304. Components render templates with `View.Partial`, and
  `cachetag.SetHeaders` sets the same cache headers (`Cache-Control`, `Surrogate-Key`, `Cache-Tag`) on `/api/x`
  endpoints.
- Rich text extensibility: a top-level `richtext:` schema key declares custom nodes (block, inline atoms, leaves, and
  fixed sequences of part nodes such as an accordion's summary and body) and marks with typed attributes (`string`,
  `enum`, `int`, `bool`, `url`), a rendered tag from an allowlist and an admin label. One declaration reaches
  validation, field allowlists, `GET /api/manage/v1/richtext`, the admin editor (Insert menu, attribute dialog, marks
  menu, copy/paste) and every renderer, which output `<tag class="rt-name" data-attr="…">`; `hydrogen generate` writes
  `gen/richtext.ts` (`richTextSchema`) for frontends. Renderers take `classes` per node or mark type and overrides:
  `richtext/<type>.html` templates and `wrap` in the site module, `render` in every SDK component (now also Svelte and
  Vue). Built-in additions: `align` and `id` (anchors) on paragraphs and headings, `start` and `tight` on lists, link
  `target: _blank`, and `#id` / `item:<id>#id` hrefs. The SDK components share one walk in
  `@hydrogencms/client/richtext`, whose size budget is 2 KB.
- `DEVELOPMENT.md`: how to run a working copy of Hydrogen with content, the admin and a frontend (fullstack in one
  process, the CMS with a JavaScript starter, the admin with hot reload, a project of your own on the checkout).

### Changed

- The built admin (`admin/dist`) is no longer committed on `main`. The release workflow builds it and records it in
  release commits that the Go module tags point at, so released versions embed the admin as before. In a checkout run
  `make admin` once; a binary built without it serves an "admin not built" page and logs a warning (`admin.Built()`).
  Releases are dispatched from GitHub (`release.yml`) instead of tagging locally; `scripts/release.sh --tag` is gone.
- The static scaffold's dev site runs on `127.0.0.1:4321` (`server.host`, channel `frontend`), like `hydrogen dev`, so
  the admin's preview frame is the same site and keeps the preview cookie.
- A block or token change counts as accounted only when every type whose documents can hold the block (directly or
  through slots) has a content migration listing it; the start names the types still missing.
- A static build warns about a `site`/frontend origin mismatch before Astro lists the pages, on a line of its own. The
  `@hydrogencms/astro` size budget is 8 KB.
- `hydrogen users:create-admin` prompts `Enter password:` when it reads the password from stdin
  (`HYDROGEN_ADMIN_PASSWORD` unset).

### Fixed

- Site module: a template that does not parse no longer takes the whole site down. Templates are parsed file by file;
  in dev only the pages that use a broken file fail (with the error on the page), a hot reload keeps the file's last
  good version, and every page shows the parse errors over it. Outside dev the server still refuses to start, now
  naming every file that does not parse. `site:check` reports them all at once.
- Site module in dev: the site directory is checked for template and asset changes at most every 250 ms instead of
  walked on every request (about 5 ms per request with 2,000 files), and a page rendered with templates a reload
  has since replaced is no longer stored in the page cache.
- `url` fields holding `item:<id>` (top level, in lists, groups and block props) are item references like rich text
  links: delivery resolves them in `links` and deleting the target warns. Existing content needs
  `hydrogen content:reindex` to pick them up.
- Custom routes of the site module collect the cache tags of reads made while the template renders, not only
  those of the route function.
- Draft preview on sites with `trailingSlash: 'always'`: the preview hand-off, the path it returns to and the exit URL
  follow it, and `astro dev` adds the slash to the CMS's preview URLs instead of answering 404.
- In the static scaffold the admin (`127.0.0.1`) and the dev site (`localhost`) were different sites, so the browser
  dropped the preview cookie in the admin's frame and Preview showed the published page.
- A migration for one type accounted for a block change in every type, so items of the other types kept the old shape.
- Listings (delivery and management API, Local API) sort by `publishedAt`, the latest publication, with never
  published items last. They answered 400 `unknown field "publishedAt"`, so the blog listing of every starter (Next.js,
  SvelteKit, Nuxt, Astro) failed. The frontend conformance suite checks that `/blog` links every post.
- Admin: menus, popovers and the bulk bar slid in from their own offset (a full width sideways for end-aligned menus);
  they drop down from their trigger, or up when opened or flipped above it.

## [0.2.1] - 2026-10-20

### Added

- Static sites preview drafts in `astro dev`: `@hydrogencms/astro` renders the CMS page routes on demand in dev only
  (`devRoutes`, default `src/pages/[...path].astro` and `404.astro`) and adds the preview route and the bridge, so the
  admin's preview pane shows the draft. `astro build` stays fully static.
- `url` fields take `relative: true` to also accept root-relative paths (`/docs/start?x#top`); the static scaffold's
  button link uses it.
- `hydrogen export -layout items` writes one indented JSON file per item (`items/<type>/<id>.json`).
- Channel info in the delivery API carries the channel's `frontend`.

### Changed

- **Content in git:** `content:save` writes one indented file per item (`content/items/<type>/<id>.json`), so a git
  diff shows the lines an edit changed. Directories in the 0.2.0 layout (`items.ndjson`) still load, are not reported
  as changed, and are converted by the next `content:save`.
- A start that rebuilds the database from `content.files` accepts content saved under an earlier schema: additive
  changes need no `content:save` first, and content migrations apply to the fresh database in any environment.
  Breaking changes without a migration fail with what to do.
- `content:load` and the reload at start check the directory before they back up the database, and move the backup
  back when the import still fails.
- `POST /preview-tokens` picks the channel when the project has one (or one serves the item's type); otherwise it
  answers `channel is required (one of: …)`.
- `href()` in `@hydrogencms/astro` follows Astro's `trailingSlash`, treats the channels' frontend origins as the site,
  and a static build warns once when Astro's `site` and a channel's frontend differ. It also adds Astro's `base` to the
  preview hand-off and exit URLs.
- The static scaffold and the Astro starter use Astro 7, the static scaffold turns share links off, and its layout
  carries the preview markers and the bridge in drafts.
- The admin hides Share when `delivery.share_links` is off, and explains under the home page's slug that it is not
  part of any URL.

### Fixed

- A fresh start of a static site failed with a schema mismatch after any schema change committed without
  `content:save`, breaking its CI build.
- `content:load` suggested `-allow-schema-change`, a flag only `import` has.
- `docs/configuration.md` gave `delivery.cache_bytes` as `32MiB`; it takes bytes as an integer. The pages module's
  management routes are now in `docs/management-api.md`.

## [0.2.0] - 2026-10-20

### Added

- **Content in git.** `content.files: <dir>` keeps the content as a committed export with a SQLite database as the
  working copy. `serve` reconciles the two before it starts (first run imports, a changed directory reloads the
  database after a backup, unsaved edits are kept, a double change refuses to start); `content:save` writes the
  database back and `content:load [-force]` replaces it. A change signal in the sync marker keeps restarts from
  exporting. See `docs/content-in-git.md`.
- **First-run setup.** While no user exists, the server prints a link at start that opens a form in the admin to
  create the first administrator (`GET`/`POST /auth/setup`). The link's token is random per start (stable across
  `hydrogen dev` restarts) and stops working once a user exists. Without the link, the sign-in page says where to
  find it. `users:create-admin` still works.
- New projects start with an example `article` type in `schema/article.yaml`, so the admin has something to edit.
- A web channel can serve every URL with `match: { prefix: / }`; more specific channels still win where they fit.
- `@hydrogencms/astro` builds fully static sites (`output: 'static'`, no adapter, e.g. GitHub Pages): the
  integration skips the preview, revalidation and bridge routes, the middleware resolves prerendered requests with
  Astro's `base`, `site` and no cookies, `hydrogen.staticPaths()` lists every URL the channels route for
  `getStaticPaths`, and `href()` (also in `Astro.locals.hydrogen`) gives CMS URLs the base.
- **`hydrogen new -static`** creates a static website whose content lives in git, ready to run and deploy to GitHub
  Pages: SQLite and the pages module, `content.files: content` with starter content (a home page, a child page and the
  site settings; rebuilt deterministically by `make static-content` and checked by a test), page and block types, one
  web channel (`match: { prefix: / }`), an Astro site in `site/` (`@hydrogencms/astro`, `output: 'static'`, a base path
  from `SITE_BASE`, navigation from the page tree, block components) and `.github/workflows/deploy.yml`. It also runs
  `generate`, so `gen/types.ts` is there for the site. `-core` points the site's npm packages at the checkout too.
  `make static-e2e` runs it end to end.
- A YAML error caused by an unquoted `${env:…}` inside `{ }` or `[ ]` now says to quote the value.

### Changed

- The quick start is three commands: `go install`, `hydrogen new mysite`, `hydrogen dev`. `-module` is optional, and
  `migrate` and `users:create-admin` are no longer needed to try Hydrogen.
- The block editor counts top-level blocks, with nested ones apart ("6 blocks, 12 inside them").
- A delivery `/route` request that no channel matches explains how to add a catch-all `match`.

### Removed

- The documentation site build (`website/`). The guides stay as Markdown under `docs/`.

## [0.1.0] - 2026-10-19

First release. Every Go module (`github.com/hydrogencms/hydrogen` and the modules under `modules/` that have their
own `go.mod`) and every `@hydrogencms/*` npm package is versioned 0.1.0.

### Added

- **Content model.** Types, collections and singletons described in YAML, with field types, validation, relations
  and typed Go and TypeScript generated from the schema. Items have versions, drafts, publish and unpublish,
  a trash, and content migrations.
- **Locales and overlays.** Translatable fields, per-locale fallbacks, and overlays for markets.
- **Channels.** Per-consumer configuration of languages, collections, settings, templates, tokens and preview
  patterns, including web channels with match rules, canonical URLs and hreflang.
- **Pages.** A page tree per site with mounts, links, routes with history, manual redirects and URL resolution.
- **Page builder and visual editing.** Typed blocks and design tokens, shared blocks, a live preview of the real
  frontend with outlines, drag and drop and inline text editing.
- **Preview and share links.** Preview tokens for drafts and share links for review by people without accounts.
- **Editorial workflow.** Scheduled releases, review requests and approvals, item history and diffs, and
  real-time collaboration on one item.
- **Media.** An asset library with upload types, image variants and storage on disk or S3.
- **Search.** Built-in full-text search and a Meilisearch module.
- **APIs.** The delivery API (`/api/deliver/v1`) with ETags, caching purged by tag and signed webhooks, the
  management API (`/api/manage/v1`), OpenAPI documents, a Go client, and export and import.
- **GraphQL.** An optional module that serves a GraphQL schema generated from your content.
- **Access control.** Roles, policies and limitations, groups, API tokens, code access rules, sessions and an
  audit log.
- **Admin.** A Svelte 5 admin app embedded in the binary, with extension points for custom elements: widgets,
  cells, panels, actions, dashboards, views, slots, themes and strings, declared in the project or by modules.
- **Customisation in Go.** Typed hooks, endpoints, jobs, commands, and outbox consumers.
- **WebAssembly hooks.** Write hooks in TypeScript or any language that compiles to WebAssembly and load them
  with the wasmhooks module (`hydrogen hooks build`).
- **Modules and CLI.** SQLite and Postgres adapters (no cgo), and optional modules wired from `hydrogen.yaml`.
  The `hydrogen` CLI creates projects (`new`), runs them with live reload (`dev`), generates typed code
  (`generate`), manages modules (`add`, `remove`, `modules`), builds (`build`) and reports its version
  (`version`).
- **Frontend SDK and adapters.** `@hydrogencms/client`, `router`, `bridge`, `react`, `admin-sdk`, `hook` and
  `adapter-test`, with adapters for Next.js, SvelteKit, Nuxt and Astro, starter projects for each, and an adapter
  conformance suite.
- **Fullstack sites.** A site module that renders websites with Go templates from the same binary, a static site
  export, and a proxy that puts a site in front of an existing frontend.
- **Documentation.** Guides and references under `docs/`, published as a documentation site.
- **Releases.** Tagged releases with `hydrogen` binaries for Linux, macOS and Windows (amd64 and arm64).

[Unreleased]: https://github.com/hydrogencms/hydrogen/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hydrogencms/hydrogen/releases/tag/v0.1.0
