# Hydrogen starter for Astro

An Astro 6 (SSR, `@astrojs/node`) site on `@hydrogencms/astro`, rendering the page-builder project in
`examples/starter`: pages made of blocks in English and German (`/en`, `/de`), a blog, a navigation from the page
tree, a footer from the `siteSettings` singleton, preview in the admin. It is the template for new Astro frontends:
copy this directory and adapt it.

## Run it

```sh
# 1. the CMS: examples/starter with its fixture content
cd examples/starter
export STARTER_FRONTEND=http://127.0.0.1:4321      # where the channels' frontend lives (canonical URLs, preview links)
go run . migrate && go run . import fixture
go run . serve                                     # http://localhost:8080, admin at /admin

# 2. the SDK packages (until they are published) and this app
cd ../../packages && npm install && npm run build
cd starters/astro && npm install
export HYDROGEN_URL=http://localhost:8080
npm run dev                                        # or: npm run build && npm start
```

Create a webhook in the CMS config for published changes to show on the site:

```yaml
webhooks:
  astro:
    url: http://127.0.0.1:4321/api/hydrogen/revalidate
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}      # the same value in the frontend's environment, 16+ characters
```

| Variable | Default | |
|---|---|---|
| `HYDROGEN_URL` | `http://localhost:8080` | the CMS |
| `HYDROGEN_REVALIDATE_SECRET` | | webhook secret |
| `HYDROGEN_ORIGIN` | the request's host | fixed public origin, for a deployment whose host is not the channel's |
| `HYDROGEN_ADMIN_ORIGIN` | the origin of `HYDROGEN_URL` | the admin that may embed draft pages and talk to them |

`make starter-astro-e2e` (repo root) builds the CMS and this app, imports the fixture and runs the adapter conformance
suite, browser checks included.

## How it is put together

| File | Role |
|---|---|
| `astro.config.mjs`, `src/hydrogen.ts` | the integration and its `createHydrogen({ baseUrl, channels, templates, ... })` |
| `src/pages/[...path].astro` | pages: the middleware asked the CMS; this picks the template by content type |
| `src/pages/404.astro` | the not-found page (status 404 is set by the middleware and the page) |
| `src/pages/[locale]/blog/*.astro` | posts: a collection the CMS does not route (its channels reserve `/blog/*`), found by slug with the client |
| `src/pages/blog/[slug].astro` | the CMS's preview URL of a post (`/blog/{slug}`, no language prefix): finds the post's channel with the token, redirects to its URL |
| `src/templates/` | `Page`, `Landing`, `Post` |
| `src/components/registry.ts`, `blocks/` | the block components and the registry: `hero`, `richText`, `cards` + `card`, `callout`, `image`; `shared` is built in |
| `src/layouts/Layout.astro` | `<Head />`, header (navigation of the request's channel), `<main data-h-root>`, footer |
| `src/styles/globals.css` | the design: schema tokens (`tone`, `space`) are class names |

**Your own project.** `src/lib/types.ts` points at the types `hydrogen generate` wrote for `examples/starter`; use your
own `gen/types.ts`. Add a block: a component in `src/components/blocks/`, a line in the registry, and CSS. Channel names
double as locales here (`en`, `de`); adapt `src/lib/channels.ts` and the `[locale]` routes if yours differ.
