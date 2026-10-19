# Changelog

All notable changes to Hydrogen are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/). Hydrogen is 0.x: APIs may change between minor versions.

## [Unreleased]

### Added

- **First-run setup.** While no user exists, the server prints a link at start that opens a form in the admin to
  create the first administrator (`GET`/`POST /auth/setup`). The link's token is random per start (stable across
  `hydrogen dev` restarts) and stops working once a user exists. Without the link, the sign-in page says where to
  find it. `users:create-admin` still works.
- New projects start with an example `article` type in `schema/article.yaml`, so the admin has something to edit.
- A web channel can serve every URL with `match: { prefix: / }`; more specific channels still win where they fit.
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
