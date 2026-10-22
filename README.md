# Hydrogen

A headless, composable CMS written in Go. One small binary and a database (SQLite or Postgres) give you a content
backend, an admin with live visual editing, and APIs that any website or app can read. Lightweight and powerful: a
small core, with everything a project can live without in optional modules.

- **Lightweight.** About 30 MB with both database adapters and the admin inside, no cgo, no runtime dependencies,
  fast start-up, a small admin. SQLite needs no setup; Postgres scales to several servers.
- **Headless first.** The core delivers content over APIs and never renders your public site. Or serve a website from
  the same binary with Go templates, or put a JavaScript frontend behind it. Content does not change when you switch.
- **Live visual editing.** Editors build pages from typed blocks in a live preview of the real frontend, with
  outlines, drag and drop, and inline text editing, and several editors can work on one item together.
- **Typed from the schema.** You describe content in YAML. Hydrogen validates every write against it and generates
  typed Go and TypeScript, an OpenAPI document and, optionally, a GraphQL schema.
- **Channels.** Each consumer, a website in one language or a mobile app, is a channel in config with its own
  languages, fallbacks, collections and access.
- **Editorial workflow.** Drafts and versions, a trash, scheduled releases, reviews, share links for previews, locale
  overlays for markets, a media library with image variants, and full-text search.
- **Access control.** Roles, policies and limitations, API tokens, rules in code, and an audit log. AI agents use the
  same tokens, validation and review as people.
- **Customisable in Go.** Typed hooks, endpoints, jobs, commands and access rules in your project, WebAssembly hooks
  in any language, admin extensions as custom elements, and modules for the rest.
- **No lock-in.** Open source, documented storage, portable export and import, open API specs.

Hydrogen is young (0.x): expect the APIs to change before 1.0.

## Quick start

You need Go 1.26 or later.

```sh
go install github.com/hydrogencms/hydrogen/cmd/hydrogen@latest
hydrogen new mysite && cd mysite
hydrogen dev                    # SQLite in .work/, live reload; prints a link to create your admin account
```

Open the link, create your account, then write and publish an article (the project starts with an example `article`
type in `schema/article.yaml`) and read it:

```sh
curl -s localhost:8080/api/deliver/v1/collections/article/items
```

[Getting started](docs/getting-started.md) walks through this in full. To work from a checkout of this repository,
build the tool with `go build -o bin/hydrogen ./cmd/hydrogen` and create projects with `hydrogen new -core <checkout>`.

## Modules

The core is the content model, the APIs, the admin, access control, the media library, search and the editorial
features. These are added per project with `hydrogen add <name>` (listed in `hydrogen.yaml`):

| Module | Adds |
| --- | --- |
| `sqlite`, `postgres` | The database adapters. |
| `pages` | A page tree per site: URLs, mounts, navigation, redirects and route history. |
| `site` | A website rendered by the binary with Go templates, themes and live preview. |
| `site-export` | Static export of the `site` module for a CDN or static host. |
| `site-proxy` | A JavaScript frontend (static export or SSR process) served behind the CMS port. |
| `overlays` | Locale overlays: `de-CH` inherits from `de` and stores only what differs. |
| `images` | Presets, focal points and resized variants of uploaded images. |
| `meilisearch` | Meilisearch as the search engine. |
| `graphql` | A read-only GraphQL endpoint generated from the schema. |
| `wasmhooks` | Content hooks as sandboxed WebAssembly programs, written in Go, Rust or TypeScript. |

Third-party modules are Go packages with `func Module() kernel.Module`. See [Choosing modules](docs/modules.md).

## Frontends

| Frontend | Use |
| --- | --- |
| Next.js | [`@hydrogencms/next`](packages/next/README.md) and a [starter](packages/starters/next/README.md). |
| SvelteKit | [`@hydrogencms/sveltekit`](packages/sveltekit/README.md) and a [starter](packages/starters/sveltekit/README.md). |
| Nuxt | [`@hydrogencms/nuxt`](packages/nuxt/README.md) and a [starter](packages/starters/nuxt/README.md). |
| Astro | [`@hydrogencms/astro`](packages/astro/README.md) and a [starter](packages/starters/astro/README.md). |
| Go templates | The [`site` module](docs/site.md), in the same binary. |
| GraphQL | The [`graphql` module](docs/graphql.md). |
| Anything else | The [delivery API](docs/delivery-api.md) and the [adapter kit](docs/adapter-kit.md). |

The adapters share [`@hydrogencms/client`](packages/client) (typed delivery client) and
[`@hydrogencms/router`](packages/router) (URL resolution), and a conformance suite checks each against the same
content.

## Documentation

Start at the [documentation index](docs/README.md).

- [Getting started](docs/getting-started.md) and [concepts](docs/concepts.md)
- [Schema](docs/schema.md), [configuration](docs/configuration.md) and the [command line](docs/cli.md)
- The [delivery API](docs/delivery-api.md) and the [management API](docs/management-api.md)
- [Access control](docs/access.md), [hooks](docs/hooks.md) and [deployment](docs/deployment.md)

## Development

To run your working copy with content, the admin and a frontend, see [DEVELOPMENT.md](DEVELOPMENT.md). How
changes are proposed and assessed (one pull request per issue, against the core principles): [CONTRIBUTING.md](CONTRIBUTING.md).

```sh
make build test vet lint   # every Go module in the repository
make db-up                 # Postgres 17 on localhost:5436 for the Postgres adapter tests
make smoke                 # new project, migrate, serve, with binary size, start-up and memory budgets
make sdk                   # the npm packages: install, build, test, size budgets
make admin                 # the admin app: typecheck, tests, build into admin/dist (run once after cloning)
make check                 # the full gate: tidy, vet, lint, test, smoke, admin
```

The repository holds the Go core (`core/`, `api/`, `features/`, `admin/`), the optional modules (`modules/`), the
npm packages (`packages/`), and examples (`examples/`).

Go module: `github.com/hydrogencms/hydrogen`. License: [MIT](LICENSE).
