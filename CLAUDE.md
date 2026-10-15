# CLAUDE.md

hydrogencms is a headless, composable CMS in Go (SQLite or Postgres through swappable adapters, Svelte 5 admin,
TypeScript frontend SDK).

**Core principle: lightweight and powerful.** Judge every change by both: does it keep the binary, memory, startup,
admin bundle and dependency list small, and does it add real capability? Prefer the standard library, no cgo,
no web framework or ORM, lazy-load heavy admin features, and put anything most projects don't need in a
module.

If `planning/` exists, read [`planning/README.md`](planning/README.md) first. It is private and gitignored:
never commit it, quote it in commit messages or public docs, or move its content into tracked files unless asked.

## Layout

- Core module `github.com/hydrogencms/hydrogen` at the repo root: `hydrogen.go` (project entry point and
  commands), `kernel/`, `config/`, `store/` (interfaces; `store/storetest` is the adapter conformance suite),
  `outbox/`, `audit/`, `clock/`, `ids/`, `cmd/hydrogen` (developer CLI), `internal/scaffold`, `internal/devwatch`.
- `modules/<name>`: optional modules. Those with third-party dependencies have their own `go.mod`
  (`modules/sqlite`: modernc.org/sqlite; `modules/postgres`: pgx). `go.work` joins everything for development;
  each module also builds alone (`GOWORK=off`).
- Content core (H1): `schema/` (YAML schema → compiled schema, field types, validation), `content/` (the content
  service = Local API: drafts, publish, projections, delete policies, hooks, content migrations), `hooks/`,
  `manage/` (management REST API + OpenAPI at `/api/manage/v1`), `codegen/` (typed Go and TypeScript),
  `store/storeutil` (dialect-neutral helpers shared by the adapters' query compilers).
- Module extension points (H2): `Tx.SQL()` / `DB.Read()` (portable SQL for modules' own tables: `?`
  placeholders, ids.UUID/time conversion), schema type options (`Registry.TypeOption`), module management routes
  (`Registry.ManageRoute`, `manage.WriteJSON/DecodeJSON/Fail`).
- `modules/pages`: page tree per site, mounts, links, routes with history, manual redirects, Resolve; its SQL lives
  in `modules/pages/migrations/{sqlite,postgres}` and its contract in `planning/design/pages-h2.md`.
- Channels (H3): `channel/` (channels.yaml → compiled set: languages, collections, settings cascade, templates,
  tokens, preview patterns, module options), `config.Node` (position-aware config for modules), web channels in
  `modules/pages/web*.go` (match/site/reserved options, Match, Locate, canonical, hreflang, URL/LinkURL).
  Contract: `planning/design/channels-h3.md`.
- Delivery (H4): published snapshots (`content/snapshot.go`, `store.Reader`, `RebuildProjections`), `delivery/`
  (read API at `/api/deliver/v1`: channel/locale selection, `include`, LRU cache purged by tag, ETag, preview
  tokens; `POST /preview-tokens` with an `item` also returns the frontend `url`: a module's `Registry.PreviewURL`
  resolver (pages: the item's route), else the channel's `preview` pattern, plus `?preview=<token>`), `cachetag/`
  (tags and the `cache.purge` consumer), `webhook/` (signed `Hydrogen-Signature`), `jobs/` (cron jobs under a lock),
  `kernel/endpoints.go` (`/api/x`), export/import (`exportfmt/`, `store/export.go`, `hydrogen_export.go`), `client/`
  (Go client), `hydrogen_frontend.go` (`check-frontend`). Pages adds `/route` and `/tree`. Contract:
  `planning/design/delivery-h4.md`.
- Access (H5a): `access/` (principals, RBAC policies and limitations, code access rules, passwords, sessions, API
  tokens, `Authenticator`, `Checker`, `Admin` for users/groups/roles/tokens/audit), `store.AuthStore` (migration
  0004), working projections (0005). `content.Service` checks the principal in the context (HTTP entry points
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
- Page builder (H6a): `schema/blocks.go` (top-level `tokens` and `blocks` schema keys, the `blocks` and `token`
  field types, `blockset` types and the built-in `shared` block), block ids/validation/projections in the same
  package, `manage` `GET /blocks` (block types + token sets), codegen `Block`/`Blocks<…>`/`DecodeProps`, and
  `examples/starter` (schema, channels and the committed `fixture/` export; `make starter-fixture` rebuilds it
  deterministically). Contract: `planning/design/pagebuilder-h6.md`.
- Frontend starters (H6b): `starters/next` (Next.js starter on `examples/starter`), `packages/adapter-test`
  (adapter conformance suite), `docs/adapter-kit.md` and `docs/bridge-protocol.md` (adapters; preview bridge).
- Site module (H6c): `modules/site` renders a website with `html/template` for web channels that set `render: go`
  (channel option; needs `match`, public access). It calls the delivery API in process (so shared blocks, links,
  preview drafts and cache tags are delivery's), keeps its own page cache purged by the same `cache.purge`
  notifications, and serves `/__hydrogen/bridge.js` (embedded copy of `packages/bridge`'s IIFE: `make site-bridge`,
  verified by `site-bridge-check` in `make check`) and fingerprinted assets. Templates: project `site/templates`
  over the embedded `themes/starter` by file name (`layout.html`, `pages/<type>.html` or `pages/default.html`,
  `collections/<template>.html`, `blocks/<type>.html`, `404.html`; block roots carry `{{.Attrs}}`). Config
  `modules.site` (`dir`, `admin_url` (required outside dev), `preview_csp`, `cache_bytes`, `root`, `routes`).
  Preview pages send a strict CSP and `Referrer-Policy: no-referrer`; serve the website from a different host than
  the admin in production (on one origin a previewed page can reach the admin; startup warns). `hydrogen site:check
  [-db]` renders everything against schema-generated data. `examples/fullstack` (schema and fixture of
  `examples/starter`); `make site-e2e` runs the conformance suite against it.
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
make starter-next-e2e           # starters/next on examples/starter (SQLite) + conformance suite
make conformance                # the conformance suite against every frontend (Next starter; site module)
make admin                      # admin/ui: npm ci, typecheck, vitest, build into admin/dist, gzip budgets
make admin-e2e                  # Playwright: admin exit scenarios + axe; page builder live preview on the real
                                # Next starter and the fullstack site module (ADMIN_E2E_ONLY=next|fullstack)
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
- Generated code (`gen/`, `codegen/testdata/*.golden`) is never edited by hand: `hydrogen generate`,
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

- Channel `links:` (collection → pattern, like `preview:`) gives items without a page route a URL: delivery fills
  `links[id]` and `url` from it, and the pages module prefixes pattern URLs (links and previews) for its web
  channels (`/en/blog/<slug>`). Without `links:`, references to plain-collection items have no URL in frontends.
- Blocks are plain JSON in item fields; a merge patch replaces a blocks field whole. The service assigns UUIDv7
  block ids to blocks without one (`Schema.AssignBlockIDs`, in the draft pipeline). Shared blocks are stored as
  `{type: shared, props: {item}}` and inlined (`blocks`) by the delivery API when a response is built
  (`delivery/shared.go`), never in snapshots: the shared item is looked up like any item, so its cache tag is on the
  page response and publishing it purges the pages. Anything that reads snapshots directly must resolve shared
  blocks itself (the `site` module goes through the delivery API, so it doesn't). Block props reuse the field types
  but cannot be `translatable` or `index`; a blocks field is translatable as a whole. Regenerate
  `examples/starter/fixture` after changing its schema (`make starter-fixture`) and commit it with the schema.

## Rules

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
- Branch `main`. Commit by default once the full gate passes (`make tidy vet lint test smoke`): each milestone,
  and checkpoints within one. Push only when asked. Commits: no "Generated with Claude Code" footer.
