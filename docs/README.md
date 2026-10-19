# Hydrogen documentation

Hydrogen is a headless, composable CMS written in Go: one small binary plus a database (SQLite or Postgres), a
built-in admin with live visual editing, and APIs that any frontend can read. These pages cover what you need to build,
run and extend a site with it. New here? Read [Getting started](getting-started.md), then [Concepts](concepts.md).

## Start

| Page | What it covers |
| --- | --- |
| [Getting started](getting-started.md) | Install the CLI, create a project, describe a type, sign in to the admin, publish and read content over the API. |
| [Concepts](concepts.md) | Types, items, drafts and versions, locales, channels, pages, blocks, assets, modules and the outbox. |

## Guides

| Page | What it covers |
| --- | --- |
| [Hooks and project code](hooks.md) | Go hooks, typed handles, endpoints, jobs, commands, content migrations, the Local API. |
| [Access control](access.md) | Users, groups and API tokens, roles, policies, limitations, role templates, rules in code. |
| [Deployment](deployment.md) | Building the binary, Docker, Postgres, migrations, proxies and CDNs, files, scaling, backups. |
| [Media](media.md) | Upload types, files, the media library, S3-compatible storage. |
| [Search](search.md) | Searchable types, the database engine, filters, facets and highlights. |
| [Releases](releases.md) | Publishing groups of items together, now or on a schedule. |
| [Reviews](reviews.md) | Requiring approval before a type is published. |
| [Collaboration](collaboration.md) | Presence and live co-editing in the admin. |
| [Share links](share-links.md) | Preview links for people without an account, and relayed drafts. |
| [Admin extensions](admin-extensions.md) | Custom elements for fields, lists, panels, dashboards and views. |

## Reference

| Page | What it covers |
| --- | --- |
| [Schema](schema.md) | Every field type and option, validation, blocks and tokens, upload types, changing a schema. |
| [Configuration](configuration.md) | `app.yaml`, `channels.yaml`, environment variables, secrets and environments. |
| [Command line](cli.md) | The `hydrogen` tool and the commands of a project binary. |
| [Delivery API](delivery-api.md) | Reading published content: channels, items, lists, caching, previews, webhooks. |
| [Management API](management-api.md) | Writing content and administering the CMS: authentication, endpoints, errors. |

## Modules

Optional parts of the binary, compiled in per project.

| Page | What it covers |
| --- | --- |
| [Choosing modules](modules.md) | `hydrogen.yaml`, `hydrogen add` and `remove`, the catalog, third-party modules. |
| [Module versions](module-versions.md) | Compatibility checks and reading the versions of a running server. |
| [Languages and overlays](locales.md) | Channel languages, fallbacks and the overlays module. |
| [Images](images.md) | Presets, focal points and resized variants. |
| [Content in git](content-in-git.md) | Keeping the content in the repository, with SQLite as the working copy. |
| [Meilisearch](meilisearch.md) | An external search engine. |
| [WebAssembly hooks](wasm-hooks.md) | Hooks in any language, sandboxed, and TypeScript hooks. |

## Frontends

| Page | What it covers |
| --- | --- |
| [Adapter kit](adapter-kit.md) | What a frontend adapter does, the adapters that exist, and how to write another. |
| [Preview bridge protocol](bridge-protocol.md) | How the admin and a frontend talk in the preview frame. |
| [The site module](site.md) | Rendering a website from the binary with Go templates, themes and custom routes. |
| [Static export](site-export.md) | Rendering the site module's pages to files for a CDN. |
| [Serving a JavaScript frontend](site-proxy.md) | Running a static export or an SSR process behind the CMS port. |
| [GraphQL](graphql.md) | A read-only GraphQL endpoint generated from the schema. |

Framework adapters and starters, which live in the repository:

| Framework | Package | Starter |
| --- | --- | --- |
| Next.js | [`@hydrogencms/next`](https://github.com/hydrogencms/hydrogen/blob/main/packages/next/README.md) | [starter](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/next/README.md) |
| SvelteKit | [`@hydrogencms/sveltekit`](https://github.com/hydrogencms/hydrogen/blob/main/packages/sveltekit/README.md) | [starter](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/sveltekit/README.md) |
| Nuxt | [`@hydrogencms/nuxt`](https://github.com/hydrogencms/hydrogen/blob/main/packages/nuxt/README.md) | [starter](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/nuxt/README.md) |
| Astro | [`@hydrogencms/astro`](https://github.com/hydrogencms/hydrogen/blob/main/packages/astro/README.md) | [starter](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/astro/README.md) |

Shared packages: [`@hydrogencms/client`](https://github.com/hydrogencms/hydrogen/blob/main/packages/client/README.md)
(typed delivery client and server helpers),
[`@hydrogencms/router`](https://github.com/hydrogencms/hydrogen/blob/main/packages/router/README.md) (URL resolution),
[`@hydrogencms/react`](https://github.com/hydrogencms/hydrogen/blob/main/packages/react/README.md) (blocks and rich
text),
[`@hydrogencms/admin-sdk`](https://github.com/hydrogencms/hydrogen/blob/main/packages/admin-sdk/README.md) (admin
extensions).
