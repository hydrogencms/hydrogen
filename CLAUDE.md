# CLAUDE.md

hydrogencms is a headless, composable CMS in Go (SQLite or Postgres through swappable adapters, Svelte 5 admin,
TypeScript frontend SDK).

**Core principle: lightweight and powerful.** Judge every change by both: does it keep the binary, memory, startup,
admin bundle and dependency list small, and does it add real capability? Prefer the standard library, no cgo,
no web framework or ORM, lazy-load heavy admin features, and put anything most projects don't need in a
module.

If `planning/` exists, read [`planning/README.md`](planning/README.md) first. It is private and gitignored:
never commit it, quote it in commit messages or public docs, or move its content into tracked files unless asked.
It is its own git repository: commit planning changes there (`git -C planning …`), alongside the code commits.

## Layout

- Core module `github.com/hydrogencms/hydrogen` at the repo root: `hydrogen*.go` (the root package: project entry
  point and commands), `cmd/hydrogen` (developer CLI).
- Grouping: `core/` holds the services and the libraries modules and projects build on (`kernel`, `config`,
  `store` (interfaces; `core/store/storetest` is the adapter conformance suite), `content`, `schema`, `channel`,
  `access`, `hooks`, `outbox`, `audit`, `jobs`, `cachetag`, `exportfmt`, `clock`, `ids`); `api/` the HTTP APIs and
  the Go client (`manage`, `delivery`, `openapi`, `webhook`, `client`); `features/` the built-in editorial
  features (`collab`, `media`, `release`, `review`); `internal/` what only hydrogen itself uses (`codegen`,
  `scaffold`, `devwatch`); `modules/` optional modules; `packages/` the npm packages and `packages/starters`.
- `modules/<name>`: optional modules. Those with third-party dependencies have their own `go.mod`
  (`modules/sqlite`: modernc.org/sqlite; `modules/postgres`: pgx). `go.work` joins everything for development;
  each module also builds alone (`GOWORK=off`).
- Content core (H1): `core/schema/` (YAML schema → compiled schema, field types, validation), `core/content/` (the
  content service = Local API: drafts, publish, projections, delete policies, hooks, content migrations),
  `core/hooks/`, `api/manage/` (management REST API + OpenAPI at `/api/manage/v1`), `internal/codegen/` (typed Go and
  TypeScript), `core/store/storeutil` (dialect-neutral helpers shared by the adapters' query compilers).
- Module extension points (H2): `Tx.SQL()` / `DB.Read()` (portable SQL for modules' own tables: `?`
  placeholders, ids.UUID/time conversion), schema type options (`Registry.TypeOption`), module management routes
  (`Registry.ManageRoute`, `manage.WriteJSON/DecodeJSON/Fail`).
- `modules/pages`: page tree per site, mounts, links, routes with history, manual redirects, Resolve; its SQL lives
  in `modules/pages/migrations/{sqlite,postgres}` and its contract in `planning/design/pages-h2.md`.
- Channels (H3): `core/channel/` (channels.yaml → compiled set: languages, collections, settings cascade, templates,
  tokens, preview patterns, module options), `config.Node` (position-aware config for modules), web channels in
  `modules/pages/web*.go` (match/site/reserved options, Match, Locate, canonical, hreflang, URL/LinkURL).
  Contract: `planning/design/channels-h3.md`.
- Delivery (H4): published snapshots (`core/content/snapshot.go`, `store.Reader`, `RebuildProjections`),
  `api/delivery/` (read API at `/api/deliver/v1`: channel/locale selection, `include`, LRU cache purged by tag, ETag,
  preview tokens; `POST /preview-tokens` with an `item` also returns the frontend `url`: a module's
  `Registry.PreviewURL` resolver (pages: the item's route), else the channel's `preview` pattern, plus
  `?preview=<token>`), `core/cachetag/` (tags and the `cache.purge` consumer), `api/webhook/` (signed
  `Hydrogen-Signature`), `core/jobs/` (cron jobs under a lock), `core/kernel/endpoints.go` (`/api/x`), export/import
  (`core/exportfmt/`, `core/store/export.go`, `hydrogen_export.go`), `api/client/` (Go client), `hydrogen_frontend.go`
  (`check-frontend`). Pages adds `/route` and `/tree`. Contract: `planning/design/delivery-h4.md`.
- Access (H5a): `core/access/` (principals, RBAC policies and limitations, code access rules, passwords, sessions, API
  tokens, `Authenticator`, `Checker`, `Admin` for users/groups/roles/tokens/audit), `store.AuthStore` (migration
  0004), working projections (0005). First run: with no user, the server logs `<admin>/login#setup=<token>`
  (`Admin.SetupToken`; stable in dev) and `POST /auth/setup` creates the first administrator. `content.Service` checks the principal in the context (HTTP entry points
  always set one; no principal = the system, unchecked); the management API authenticates every request.
  Contract: `planning/design/auth-h5a.md`.
- Admin (H5b): `admin/` serves the Svelte 5 app (`admin/ui`, built into `admin/dist`, committed and embedded;
  strict CSP, `admin.prefix` default `/admin`, `admin.enabled`). The app is an API client only (`/api/manage/v1`);
  it reads `<prefix>/config.json` at start. Foundation docs: `admin/ui/src/lib/README.md`. Contract:
  `planning/design/admin-h5b.md`.
- Admin extensions (H5c): custom elements declared in the project's `admin:` manifest (`elements`, `theme`,
  `strings`, `widgets`, `cells`, `panels`, `actions`, `dashboard`, `views`, `slots`; validated by `config:check`,
  served under `<prefix>/x/`) or by modules (`Registry.AdminExtension`); contracts and helpers in
  `packages/admin-sdk`; loader in `admin/ui/src/lib/extensions.svelte.ts` + `lib/ext/`. `examples/content/admin`
  uses every kind; `hydrogen new` ships an `admin/` starter. Contract: `planning/design/admin-h5c.md`.
- TypeScript SDK (H4b): `packages/` (npm workspaces: `client`, `router`, `next`; zero runtime dependencies,
  gzip budgets in `packages/scripts/size.mjs`). `examples/next-site`: Next.js sample against `examples/multisite`
  (one deployment, every web channel; the proxy answers CMS redirects and 404s; `"use cache"` entries tagged from
  `Surrogate-Key`, revalidated by the signed webhook). Contract: `planning/design/sdk-h4b.md`.
- Page builder (H6a): `core/schema/blocks.go` (top-level `tokens` and `blocks` schema keys, the `blocks` and `token`
  field types, `blockset` types and the built-in `shared` block), block ids/validation/projections in the same
  package, `manage` `GET /blocks` (block types + token sets), codegen `Block`/`Blocks<…>`/`DecodeProps`, and
  `examples/starter` (schema, channels and the committed `fixture/` export; `make starter-fixture` rebuilds it
  deterministically). Contract: `planning/design/pagebuilder-h6.md`.
- Frontend starters (H6b): `packages/starters/next` (Next.js starter on `examples/starter`), `packages/adapter-test`
  (adapter conformance suite), `docs/adapter-kit.md` and `docs/bridge-protocol.md` (adapters; preview bridge).
- Site module (H6c): `modules/site` renders a website with `html/template` for web channels that set `render: go`
  (channel option; needs `match`, public access). It calls the delivery API in process (so shared blocks, links,
  preview drafts and cache tags are delivery's), keeps its own page cache purged by the same `cache.purge`
  notifications, and serves `/__hydrogen/bridge.js` (embedded copy of `packages/bridge`'s IIFE: `make site-bridge`,
  verified by `site-bridge-check` in `make check`) and fingerprinted assets. Templates: project `site/templates`
  over the embedded `themes/starter` by file name (`layout.html`, `pages/<type>.html` or `pages/default.html`,
  `collections/<template>.html`, `blocks/<type>.html`, `404.html`; block roots carry `{{.Attrs}}`). Config
  `modules.site` (`dir`, `admin_url` (required outside dev), `preview_csp`, `cache_bytes`, `root`, `routes`).
  Preview pages send a strict CSP and `Referrer-Policy: no-referrer`, and carry `<meta name="hydrogen-preview">`
  with the SHA-256 of the token. Same-origin hosting is trusted by default (project code and theme scripts run with
  the site's origin; editor content never can); the admin locks its preview frame when same-origin; a separate
  host or port is recommended for production (startup warns). `hydrogen site:check
  [-db]` renders everything against schema-generated data. `examples/fullstack` (schema and fixture of
  `examples/starter`); `make site-e2e` runs the conformance suite against it.
- Visual editing (H7): the admin draws the canvas (`admin/ui/src/lib/canvas`: outlines, block toolbar, drag, palette
  insert, keyboard reorder) over the preview iframe from the `layout` the bridge reports; the bridge only measures,
  reports `hover`/`select` and edits plain text inline (`editText` → `inlineEdit`, committed on Enter/blur). Plain
  text is found by the admin sending `overlay.texts` (string/text fields and block props) and the bridge matching
  them in the DOM; explicit `data-h-path` markers win. `ready.edit: true` advertises support. Protocol:
  `docs/bridge-protocol.md`. Contract: `planning/design/visual-h7.md`.
- Share links and relayed previews: `store.PreviewStore` (`preview_share`, `preview_relay`; both adapters),
  `delivery` share tokens (`hps_…`, stored as SHA-256(SHA-256(token)), looked up with a short cache), the relay
  overlay on draft views, the SSE stream `GET /api/deliver/v1/preview/events?proof=` (the proof is the page's
  `hydrogen-preview` meta), manage routes `/preview-shares` and `/preview-relay/{item}`, policy
  `content.preview.share`, config `delivery.share_links|share_default_ttl|share_max_ttl`, cleanup job
  `preview.cleanup`. The bridge's viewer mode follows the stream outside a frame. Public doc:
  `docs/share-links.md`. Contract: `planning/design/share-h7s.md`.
- Publishing (H8a): `content` unpublish, trash (`DELETE /items/{id}`), restore, purge (`?permanent=true`, trashed
  items only) and the `content.trash` retention job (`content.trash_retention`); trashed items are hidden by every
  store query unless `store.Query.Trashed` says otherwise, and free their key. `features/release/` groups items
  (publish or unpublish actions) and publishes them in one transaction, now or scheduled (job `releases`, run as the
  principal who scheduled, re-loaded by `access.Authenticator.PrincipalOf`); one-item schedules are releases of kind
  `item`. Policy `release.manage`. Docs: `docs/releases.md`. Contract: `planning/design/editorial-h8a.md`.
- Review (H8b): types with `review: true` publish only an approved draft (`features/review/`, hook `content.ReviewHook`:
  `CheckPublish` on every publish path, `DraftChanged` on every draft change, `Closed` on publish/discard/trash);
  policies `review.submit|approve|bypass`; four eyes (`content.review_four_eyes`). Versions: `POST
  /items/{id}/versions/{n}/restore`; the admin compares versions client-side (`admin/ui/src/lib/diff`). Docs:
  `docs/reviews.md`. Contract: `planning/design/editorial-h8b.md`.
- Collaboration (H8c): `features/collab/` relays and stores opaque Yjs updates per item (join with per-part seeding
  claims, SSE stream, presence heartbeats, compaction, cleanup job `collab.cleanup`; config `collab.enabled`, exposed
  in the admin's config.json). Content events carry the saving collab client, so sessions get `saved` or `reset`. The
  admin binds the form to Yjs in `admin/ui/src/lib/collab` (lazy chunk; rich text through y-prosemirror) with a
  convergence fuzz test. Docs: `docs/collaboration.md`. Contract: `planning/design/collab-h8c.md`.
- Locale overlays (H8d): `modules/overlays` gives the content service each language's `inherits` chain
  (`content.Overlays`); an overlay locale stores only the translatable top-level fields it overrides (`null` in a
  draft PATCH removes one) and resolves the rest field by field; publishing a parent rebuilds its descendants'
  snapshots, projections and pages routes. Views carry `inherits`, `overrides`, `inheritedFrom`, `availableLocales`.
  Without the module `inherits` is ignored (config:check warns). `examples/multisite` registers it. Docs:
  `docs/locales.md`. Contract: `planning/design/overlays-h8d.md`.
- Media (H9a): `features/media/` stores files by SHA-256 (`media.Storage`: local, or S3-compatible with in-house SigV4),
  records them in `media_upload`, serves `/assets/{sha}/{name}` with hardening headers and runs the `media.gc` job.
  Types marked `upload:` are asset collections (built-in `file`, `folder`, `tags`); `asset` fields reference them and
  delivery expands them (`api/delivery/assets.go`); `content` reads server-owned file metadata through `content.Files`.
  The admin library is `/assets` (`admin/ui/src/lib/media`). Docs: `docs/media.md`. Contract:
  `planning/design/media-h9a.md`.
- Images (H9b): `modules/images` (opt-in, pure Go) turns uploads into preset variants at
  `/images/{preset}/{v}/{w}/{focal}/{sha}/{name}` (`v` hashes the preset; `focal` is `c` or `XXYY`), stored as media
  derived files (`<sha>-<preset>-<v>-<w>-<focal>`, removed with their upload), with an imgproxy driver for
  WebP/AVIF. Delivery adds `variants` to assets through `delivery.AssetDecorator` (`Registry.AssetDecorator`); the
  admin gets `media.thumbBase` through `Registry.AdminImages`. SDK: `imageProps` (client), `Image` (react). Docs:
  `docs/images.md`. Contract: `planning/design/images-h9b.md`.
- Search (H9c): `core/search` (Engine, Index, Doc, BuildDocs, the default `Database` engine on the adapters'
  full-text search with `\x02`/`\x03` snippet markers turned into `<mark>`), schema type option `search:`,
  `Registry.SearchEngine`, `Services.Search`, the `search.index` outbox consumer, Start-time Sync/Rebuild with
  catch-up, `search:reindex`, and `Services.HealSearchRows` (reindexes when search rows are missing). Endpoints:
  delivery `GET /search` (the engine; channel collections) and manage `GET /search` (always the database: drafts and
  access scopes). `modules/meilisearch` is the first external engine (net/http only). Docs: `docs/search.md`,
  `docs/meilisearch.md`. Contract: `planning/design/search-h9c.md`.
- Modules in projects (H10a): `hydrogen.yaml` lists a project's modules (catalog names or Go package paths); the CLI
  (`internal/projectfile`, catalog `internal/modcat`) generates `modules_gen.go` (`useModules(h)`) and runs
  `add`/`remove`/`build`/`modules`. At runtime `kernel.ModuleInfo` gives each module's Go module and version from
  build info, `kernel.CoreRequirer` ranges (`internal/semver`) are checked at start, and `modules` (app command) and
  `GET /api/manage/v1/system` (policy `system.read`; admin Settings → System) report them. Docs: `docs/modules.md`,
  `docs/module-versions.md`. Contract: `planning/design/modules-h10a.md`.
- Admin extension points (H10b, completing H5c): block previews (`previews`), block prop widgets, list actions
  (`list_actions`), asset panels (panels typed to an upload type), slots on every list screen, extension views in
  the command palette. Every point is documented in `docs/admin-extensions.md` (public) and typed in
  `@hydrogencms/admin-sdk`. Contract: `planning/design/admin-h10b.md`.
- WebAssembly hooks (H10c): `modules/wasmhooks` (own Go module, wazero) runs hooks compiled to WASI preview-1
  command modules: JSON request on stdin, JSON response (`set`/`errors`/`error`) on stdout, a fresh sandboxed
  instance per call (memory cap, deadline that also cuts sleeps, no FS/env/network). TypeScript hooks:
  `@hydrogencms/hook` (`defineHook`) + `hydrogen hooks build` (esbuild + Javy 9, pinned in `scripts/javy.sh`);
  `make wasm-e2e` (needs the network). Docs: `docs/wasm-hooks.md`. Contract: `planning/design/wasm-h10c.md`.
- GraphQL (H10d): `modules/graphql` (own Go module, graphql-go) builds a read-only schema per channel at start and
  resolves through the delivery API in process (same availability, includes, preview and cache tags). Delivery
  module routes may accept POST (`delivery.Route.Post`, never cached). Docs: `docs/graphql.md`. Contract:
  `planning/design/graphql-h10d.md`.
- Framework adapters (H10e): `@hydrogencms/sveltekit`, `@hydrogencms/nuxt`, `@hydrogencms/astro` with starters in
  `packages/starters/*`, each passing `make starter-<name>-e2e`. Framework-neutral server code lives in
  `@hydrogencms/client/server` and `/richtext`, live merging in `@hydrogencms/bridge/merge`; adapters keep only
  glue. Contract: `planning/design/adapters-h10e.md`.
- Fullstack (H10f): `modules/site` themes (`site.UseTheme`, base chains), custom routes (`site.Handle`), templ-style
  components (`site.Component`, no templ dependency) and the `site.Exporter` API; `modules/siteexport` (static
  export, incremental via the outbox, redirect formats, dir/s3/command publishers; `make site-export-e2e`);
  `modules/siteproxy` (static or supervised SSR frontend behind the binary; `make site-proxy-e2e`). Docs:
  `docs/site.md`, `docs/site-export.md`, `docs/site-proxy.md`. Contract: `planning/design/fullstack-h10f.md`.
- Docs and releases (H10g): `docs/` holds the public docs as Markdown (index in `docs/README.md`). The website
  (hydrogencms/website, GitHub Pages) publishes them from the `release` branch; new pages go into its `sidebar.mjs`. Releases:
  `scripts/release.sh <version>` (checks, never bumps, every go.mod/package.json version, `ReleaseVersion` in
  internal/scaffold and the CHANGELOG section, runs the gates, `--tag` creates the module tags locally, prints the
  push command). Releases are cut on `main`: `--tag` fast-forwards `release` and the release workflow rejects
  tags not on `release`; a GitHub ruleset keeps `release` fast-forward only and requires CI's `ci-ok` check. Hotfixes:
  PR into `release`, run the script there, then merge `release` into `main`. npm packages publish through `scripts/npm-publish.sh` (order in `scripts/npm-packages.sh`; skips
  published versions); the release workflow runs it with `--provenance` when the repository variable `NPM_PUBLISH`
  is `true` (provenance needs a public repo and `repository.directory` in each package.json). Pushing tags and
  publishing are the user's call. Contract:
  `planning/design/release-h10g.md`.
- Static scaffold: `hydrogen new -static` (`internal/scaffold/static`, overlaid on the normal template) creates a
  content-in-git project with an Astro site and a GitHub Pages workflow. Its starter `content/` is generated by
  `make static-content` (`tests/tools/staticsite`, fixed clock and ids; `TestStaticContentIsCurrent` fails when the
  schema changed and it is stale); `make static-e2e` runs the whole flow.
- `tests/`: integration tests of core services on every adapter. `examples/minimal`: the smallest project.
  `examples/multisite`: two sites, five web channels, an app channel. `examples/headless`: no pages module; `web`
  channel preview patterns, `app` token channel, drafts previewed through delivery. `examples/content`: every field
  type, a module schema transform, a typed hook and a content migration, tested through the management API on SQLite
  and Postgres.

## Commands

```sh
make build test vet lint tidy   # all modules
make db-up                      # Postgres 17 on localhost:5436 (hydrogen/hydrogen) for adapter tests
make smoke                      # hydrogen new → config:check → migrate → serve + D0 budget checks
make sdk                        # packages/: install, build, test, size budgets
make next-e2e                   # examples/next-site against examples/multisite (SQLite), curl assertions
make starter-next-e2e           # packages/starters/next on examples/starter (SQLite) + conformance suite
make conformance                # the conformance suite against every frontend (Next starter; site module)
make admin                      # admin/ui: npm ci, typecheck, vitest, build into admin/dist, gzip budgets
make admin-e2e                  # Playwright: admin exit scenarios + axe; page builder live preview on the real
                                # Next starter and the fullstack site module (ADMIN_E2E_ONLY=starter|next|fullstack
                                # runs one phase on the admin/dist already built; E2E_GREP=… filters tests)
make admin-types                # regenerate admin/ui/src/lib/api/schema.d.ts from a running build's OpenAPI
make example-admin              # rebuild examples/content/admin/dist (example-admin-check verifies it in make check)
make check                      # tidy-check vet lint test smoke admin admin-dist-check (dist must be rebuilt)
```

In a project: `hydrogen dev` (live reload; regenerates `gen/` on schema changes), `hydrogen generate`,
`hydrogen migrate`, `hydrogen content:migrate`, `hydrogen content:reindex` (rebuild every projection),
`hydrogen config:check [-db]`, `hydrogen export|import <dir>`, `hydrogen check-frontend -templates <file>
[-channel name] [-db]`, `hydrogen users:create-admin <login>` (password from `HYDROGEN_ADMIN_PASSWORD` or one stdin
line), `hydrogen tokens:create -name n -role r [-agent] [-expires 720h]` (secret printed once, to stdout).
`manage.token` is an optional bearer token with every right (audited as `token:config`; in dev it defaults to
`dev-token`, accepted only from loopback peers without forwarding headers); users sign in with a session (`POST
/auth/login`, CSRF header on writes) and automation uses API tokens. `auth.session_idle`, `auth.session_max`,
`auth.insecure_cookies` (dev only) and `http.trusted_proxies` configure sessions. `delivery.preview_secret` (≥ 32
characters) is required outside dev; dev derives it from `manage.token`.

Postgres tests use `HYDROGEN_TEST_POSTGRES_URL` (default the db-up container) and skip without it unless
`HYDROGEN_TEST_REQUIRE_POSTGRES=1` (CI). Use `modules/postgres/pgtest.Scratch(t)` for a throwaway database.

## Gotchas

- Contract for H1 packages: `planning/design/content-core.md` (Deviations section records every agreed change).
- Generated code (`gen/`, `internal/codegen/testdata/*.golden`) is never edited by hand: `hydrogen generate`,
  `go test ./codegen -update`.
- Core packages never write SQL. Modules may, through `Tx.SQL()`/`DB.Read()`, branching on `Dialect()` where
  syntax differs; byte slices handed out by drivers must be copied before the next row (pgx reuses buffers).
- Structural writes in the pages module take a per-site lock (Postgres advisory xact lock); keep it that way.
  Storage features are an interface in `store`, an implementation in every adapter, and a check in `storetest`.
- Delivery reads published snapshots only. A change that alters a published document's shape (schema change
  without a content migration) needs `RebuildProjections`, or the API serves stale snapshots.
- Frontend webhooks should subscribe to `*`: `item.*` alone misses `page.changed`, which carries the route and
  navigation tags.
- Delivery listings return `{items, hasMore}`; `total` costs a count and is only computed with `?total=true`.
- macOS scans a newly built binary on first launch (~300 ms); measure start-up from the second run.
- Scratch projects inside the repo (`.work/`) need `GOWORK=off`, or the repo's `go.work` claims them.
- After any change under `admin/ui`, run `make admin` and commit `admin/dist` with it (`admin-dist-check` fails
  otherwise); rebuild the Go binary too, because dist is embedded. `hydrogen dev` with `ADMIN_DEV_URL` proxies the
  admin to a Vite dev server. The CSP keeps `script-src 'self'`; inline styles are allowed only because Svelte
  custom elements inject their styles as `<style>` elements. Admin code still uses Svelte's `style:` directive.
- Extension elements run with the signed-in user's rights and get `ctx` as a property (never attributes);
  data in through properties, out through composed `CustomEvent`s. With `ADMIN_ELEMENTS_DEV_URL` (dev only)
  `<prefix>/x/dev/*` is proxied to Vite for HMR. Schema `admin.widget`/`admin.list` `field:cell` names must be
  declared in the manifest, or startup fails.
- The admin's page background is a single-colour gradient on `html` on purpose (styles/base.css): Chrome on
  macOS colour-manages flat solid-colour tiles differently from rasterised ones, so empty stretches showed as a
  darker band. Don't replace it with a plain `background-color`. Admin design rules: `admin/ui/src/lib/README.md`.
- `scripts/npm-ci.sh DIR` (used by the Make targets and e2e) only reinstalls when the lockfile changed, so
  running checks doesn't break a developer's running `npm run dev`.
- Item and singleton writes take `If-Match` (the `ETag` from the read, `updatedAt` at millisecond resolution) and
  answer 412 `precondition_failed` with `current.updatedAt/updatedBy`; the admin shows a conflict dialog.
- Authorization lives in the service layer, never only in handlers: `content.Service` checks `Options.Access` for
  the principal in ctx; route policies (`manage.Route.Policy`, `kernel.Policy("x.y")` for endpoints) are only a
  coarse "holds this policy at all" gate. Every `manage.Route` and `kernel.Endpoint` declares a policy name,
  `manage.Authenticated`/`kernel.Authenticated` or `Public`; a module route without one fails the build.
  New policies and limitations register through `Registry.Policy/Limitation/PolicyLimitation`; code rules through
  `h.AccessRules(typ, rules)` or the typed `content.ArticleType.Access(h, rules)`.
- Take the principal's grants before opening a transaction (`Checker.Prepare`, done by the services), and put the
  open transaction on the context (`store.WithTx`) before calling the checker; limitations query through
  `store.TxFrom(ctx)`. A pooled connection that waits for another while holding one can deadlock.

- The bridge budget is 5 KB gzip and nearly used: it loads only in preview. Anything added to `packages/bridge`
  must pay for itself (run `npm run size` in `packages/`), then `make site-bridge`.
- Canvas: the frame commits inline edits on Enter or blur, so the admin must not move the focus out of the frame while
  an edit is open (`focusToolbar` checks `editing` and where the focus is); canvas model actions run through
  `BlocksModel.quietly` so they don't focus the form. Overlay texts go to a live frame when it reports `rendered`
  (never before the patch, or the mapping misses the new text) and to a reload frame with its next `ready`.
- `DELETE /items/{id}` moves an item to the trash; only `?permanent=true` on a trashed item removes it (the old hard
  delete). Async consumers that cleaned up on delete must also handle `item.unpublished` and `item.trashed`.
- Anything that changes a draft (or item metadata) must go through the content service's write path or call the
  review hook's `DraftChanged` itself, and anything that publishes must go through `publish()`; otherwise review
  approvals go stale or get bypassed. The approval stores a hash of the draft (incl. key and availability).
- Collab sessions trust the item's editors with each other's updates (the server can't read Yjs); saving applies
  the saver's rights. A draft write tells the session it was a session save only through the
  `X-Hydrogen-Collab-Client`/`-Seq` headers, honoured when the client belongs to the writer; anything else resets
  the session. Collab ordering on Postgres relies on the per-item advisory lock (`LockSession` first, always).
- With overlays on, per-locale documents for reading, validating, snapshotting and projecting come from the
  resolved parts (`itemState.publishedParts`/`workingParts`); stored parts are sparse (`raw…Parts`). New code that
  reads a locale's document must use the resolved ones, and writes must stay sparse.
- An external search index is a copy: anything that changes what an item publishes (publish, unpublish, trash,
  restore, purge, `item.updated` with `draft: false`) must reach the `search.index` consumer, and its handlers must
  re-read the item's snapshots (all locales, overlay-derived ones included), never apply event payloads. Manage
  search never uses an external engine (it cannot apply access scopes or see drafts).
- Run e2e scripts one at a time: they share process names and ports, and concurrent runs kill each other's
  servers. Starter lockfiles are gitignored; `packages/.npmrc` needs `legacy-peer-deps=true` for Nuxt's peers.
- Collab lists of groups have no item ids: the Yjs binding pairs form items with `Y.Map`s by identity
  (`Env.lists`) and `ListWidget` keys group rows by object, never by index or value (equal fresh items are
  indistinguishable by value; index keys rebind a row when a peer inserts before it). Known limitation: edits typed
  while a lost seeding claim restarts are merged whole-field and can be dropped.
- GraphQL responses must stay bounded and fresh: depth/field/node/call/item budgets apply to every query
  (introspection included), and a response that looked something up and found nothing is `no-store`.
- Image URLs must stay bounded: `/images` serves only configured presets, allowed widths and focal points that
  some item holding the file really has (checked on a cache miss). Never add a free-form size or crop parameter.
  Anything in a variant's output must be in its URL (preset version, width, focal), or immutable caching breaks.
- A `file` value's `id`, `mime`, `size`, `width` and `height` are the server's: every item write re-reads them by
  sha through `content.Files`, so clients only choose `name` and `focal`. An asset is `null` in delivery unless it is
  published (or a draft in preview), not trashed, and its upload type is in the channel's `collections`. Uploads are
  deduped by hash: e2e fixtures need unique bytes per run (`ADMIN_E2E_ONLY` runs keep the previous database).
- Release publishing goes through `content.Service.PublishTx/UnpublishTx` inside one `store.Tx`: call the service's
  grant preparation before opening the transaction (small pools deadlock otherwise), and change release status only
  through the store's conditional transition, never a blind update (several nodes run the `releases` job and the API).
- Reload-mode preview never autosaves: the admin relays the unsaved fields (`lib/preview/relay.ts`, `PUT
  /preview-relay`) and delivery overlays them on draft views for any preview token covering the item, until the item
  is saved (the overlay loses to a newer item update) or the relay is an hour old. A share token is a bearer secret:
  never log it or keep it client-side after the dialog closes; frontends treat preview tokens as opaque.
- Svelte 5 props passed as expressions (`item={data.id}`) are getters on the parent's whole object: an effect that
  reads one reruns whenever the object is replaced (each autosave). Wrap them in `$derived` before keying effects
  on them (the preview pane does, or every autosave minted a new token and reloaded the frame).
- Channel `links:` (collection → pattern, like `preview:`) gives items without a page route a URL: delivery fills
  `links[id]` and `url` from it, and the pages module prefixes pattern URLs (links and previews) for its web
  channels (`/en/blog/<slug>`). Without `links:`, references to plain-collection items have no URL in frontends.
- Blocks are plain JSON in item fields; a merge patch replaces a blocks field whole. The service assigns UUIDv7
  block ids to blocks without one (`Schema.AssignBlockIDs`, in the draft pipeline). Shared blocks are stored as
  `{type: shared, props: {item}}` and inlined (`blocks`) by the delivery API when a response is built
  (`api/delivery/shared.go`), never in snapshots: the shared item is looked up like any item, so its cache tag is on the
  page response and publishing it purges the pages. Anything that reads snapshots directly must resolve shared
  blocks itself (the `site` module goes through the delivery API, so it doesn't). Block props reuse the field types
  but cannot be `translatable` or `index`; a blocks field is translatable as a whole. Regenerate
  `examples/starter/fixture` after changing its schema (`make starter-fixture`) and commit it with the schema.

## Rules

- Several agents often share this working tree: never `git stash`, `git checkout -- …`, `git reset` or `git clean`
  it to measure a baseline; build the baseline in a separate `git worktree` (or an export of HEAD) instead.
- General-purpose headless: core never renders a public site and has no web assumptions; URLs, the page tree and
  web matchers live in the `pages` module; fullstack rendering lives in the `site` module.
- Core stays small; features that a project can live without are modules registered through the kernel.
- Every new dependency (Go or npm) needs a reason; prefer writing a small piece over importing a large one.
- No `init()` registration and no package-level mutable state.
- Database access only through the storage interfaces. No queries inside JSON documents; filter through indexed
  fields. Every storage change passes the conformance suite on every adapter.
- No cgo in default builds.
- Every mutation: one transaction, validated documents, audit event and outbox event in the same transaction.
  Outbox consumers are idempotent.
- Time and randomness go through injectable interfaces; never let map iteration order reach output.
- Schema lives in project files; never hand-edit generated code.
- No raw CSS or HTML in content; style through design tokens.
- Comments: minimal; only where the implementation is non-obvious.
- Branch `main` for all work; `release` holds only released commits and moves only through
  `scripts/release.sh --tag` (or a hotfix PR into it), never by direct commits. Commit by default once the full gate passes (`make tidy vet lint test smoke`): each milestone,
  and checkpoints within one. Push only when asked. Commits: no "Generated with Claude Code" footer.
