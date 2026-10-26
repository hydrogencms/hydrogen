# Examples

Each example is a Go project you can build and run from its directory (`next-site` is a Next.js app). They share
the same steps: `go build -o .work/<name> .`, `migrate`, `serve`. Run `make admin` once from the repo root first, since
the admin is embedded in the binary and not committed.

| Example | Demonstrates | Start from it when |
| --- | --- | --- |
| [minimal](minimal/README.md) | The core with SQLite and Postgres adapters and no schema: APIs, admin, auth, health checks. | you want the bare server, or a base to add your own schema and modules. |
| [headless](headless/README.md) | No pages module: posts, authors and a settings singleton; a `web` channel with preview patterns, a token-protected `app` channel. | a frontend or app owns its URLs and reads the delivery API. |
| [content](content/README.md) | Every field type, blocks and rich text, a typed hook, a content migration, a schema-transforming module, images, GraphQL and every admin extension point. | you are modelling content or extending the admin and project code. |
| [multisite](multisite/README.md) | Two page sites, five web channels on hosts and path prefixes, a generated market channel, locale inheritance and a token app channel. | you serve several sites, languages or markets from one deployment. |
| [starter](starter/README.md) | A block-based page-builder schema with English and German channels and a committed fixture. | you build a block-based site on any frontend; it is the CMS for `packages/starters/*`. |
| [fullstack](fullstack/README.md) | The starter served by one binary: admin, APIs and a website rendered with Go templates, a theme, custom routes and static export. | you want a website without a separate frontend. |
| [next-site](next-site/README.md) | A Next.js App Router site on `@hydrogencms/next` rendering the `multisite` web channels, with redirects, preview and webhook revalidation. | you build a Next.js frontend for several channels. |
