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
  tokens), `cachetag/` (tags and the `cache.purge` consumer), `webhook/` (signed `Hydrogen-Signature`), `jobs/`
  (cron jobs under a lock), `kernel/endpoints.go` (`/api/x`), export/import (`exportfmt/`, `store/export.go`,
  `hydrogen_export.go`), `client/` (Go client), `hydrogen_frontend.go` (`check-frontend`). Pages adds `/route`
  and `/tree`. Contract: `planning/design/delivery-h4.md`.
- `tests/`: integration tests of core services on every adapter. `examples/minimal`: the smallest project.
  `examples/multisite`: two sites, five web channels, an app channel. `examples/content`: every field type, a module schema transform, a typed hook and a content migration, tested
  through the management API on SQLite and Postgres.

## Commands

```sh
make build test vet lint tidy   # all modules
make db-up                      # Postgres 17 on localhost:5436 (hydrogen/hydrogen) for adapter tests
make smoke                      # hydrogen new → config:check → migrate → serve + D0 budget checks
```

In a project: `hydrogen dev` (live reload; regenerates `gen/` on schema changes), `hydrogen generate`,
`hydrogen migrate`, `hydrogen content:migrate`, `hydrogen config:check [-db]`, `hydrogen export|import <dir>`,
`hydrogen check-frontend -templates <file> [-channel name] [-db]`. The management API token is `manage.token`
(`dev-token` in dev when unset; required elsewhere once the schema has types). `delivery.preview_secret` (≥ 32
characters) is required outside dev; dev derives it from `manage.token`.

Postgres tests use `HYDROGEN_TEST_POSTGRES_URL` (default the db-up container) and skip without it unless
`HYDROGEN_TEST_REQUIRE_POSTGRES=1` (CI). Use `modules/postgres/pgtest.Scratch(t)` for a throwaway database.

## Gotchas

- Contract for H1 packages: `planning/design/content-core.md` (Deviations section records every agreed change).
- Generated code (`gen/`, `codegen/testdata/*.golden`) is never edited by hand: `hydrogen generate`,
  `go test ./codegen -update`.
- Core packages never write SQL. Modules may, through `Tx.SQL()`/`DB.Read()`, branching on `Dialect()` where
  syntax differs; byte slices handed out by drivers must be copied before the next row (pgx reuses buffers).
- Structural writes in the pages module take a per-site lock (Postgres advisory xact lock); keep it that way. Storage features are an interface in `store`, an implementation in every
  adapter, and a check in `storetest`.
- Delivery reads published snapshots only. A change that alters a published document's shape (schema change
  without a content migration) needs `RebuildProjections`, or the API serves stale snapshots.
- Delivery listings return `{items, hasMore}`; `total` costs a count and is only computed with `?total=true`.
- macOS scans a newly built binary on first launch (~300 ms); measure start-up from the second run.
- Scratch projects inside the repo (`.work/`) need `GOWORK=off`, or the repo's `go.work` claims them.

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
