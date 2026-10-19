# Hydrogen starter for SvelteKit

A SvelteKit 2 (Svelte 5, `adapter-node`) site on `@hydrogencms/sveltekit`, rendering the page-builder project in
`examples/starter`: pages made of blocks in English and German (`/en`, `/de`), a blog, a navigation from the page
tree, a footer from the `siteSettings` singleton, live preview in the admin. It is the template for new SvelteKit
frontends: copy this directory and adapt it. It looks and behaves like the Next.js starter.

## Run it

```sh
# 1. the CMS: examples/starter with its fixture content
cd examples/starter
export STARTER_FRONTEND=http://127.0.0.1:3000      # where the channels' frontend lives (canonical URLs, preview links)
go run . migrate && go run . import fixture
go run . serve                                     # http://localhost:8080, admin at /admin

# 2. the SDK packages (until they are published) and this app
cd ../../packages && npm install && npm run build
cd starters/sveltekit && npm install               # .npmrc: install-links, the packages are copied, not linked
export HYDROGEN_URL=http://localhost:8080
npm run dev -- --port 3000                       # or: npm run build && PORT=3000 npm start
```

Create a webhook in the CMS config for published changes to show on the site:

```yaml
webhooks:
  sveltekit:
    url: http://127.0.0.1:3000/api/hydrogen/revalidate
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}      # the same value in the frontend's environment, 16+ characters
```

| Variable | Default | |
|---|---|---|
| `HYDROGEN_URL` | `http://localhost:8080` | the CMS |
| `HYDROGEN_REVALIDATE_SECRET` | | webhook secret |
| `HYDROGEN_ORIGIN` | the request's host | fixed public origin, for a deployment whose host is not the channel's |
| `HYDROGEN_ADMIN_ORIGIN` | the origin of `HYDROGEN_URL` | the admin that may embed draft pages and talk to them |

`make starter-sveltekit-e2e` (repo root) builds the CMS and this app, imports the fixture and runs the adapter
conformance suite, browser checks included.

## How it is put together

| File | Role |
|---|---|
| `src/lib/server/hydrogen.ts` | `createHydrogen({ baseUrl, channels, templates, ... })` |
| `src/hooks.server.ts` | `handle`: the preview cookie, the channel, `?preview=` hand-off |
| `src/routes/[...path]/` | pages: the CMS decides which page lives at which URL (`hydrogen.load`, `<Page>`) |
| `src/routes/[locale=locale]/blog/` | posts: a collection the CMS does not route (its channels reserve `/blog/*`), found by slug |
| `src/routes/blog/[slug]/` | the CMS's preview URL of a post (pattern `/blog/{slug}`, no language prefix): finds the post's channel with the token, redirects to its URL |
| `src/routes/api/hydrogen/{preview,revalidate}/` | preview cookie; signed webhook receiver |
| `src/lib/templates/` | `Page`, `Landing`, `Post`, by content type (`templates/index.ts`) |
| `src/lib/blocks/` | the block components and the registry: `hero`, `richText`, `cards` + `card`, `callout`, `image`; `shared` is built in |
| `src/routes/+layout.*`, `src/lib/{Header,Footer}.svelte` | header (navigation of the request's channel), footer (`siteSettings`) |
| `src/app.css` | the design: schema tokens (`tone`, `space`) are class names |

**Preview is live.** In preview `<Page>` renders the template from the admin's working document, so a template and the
blocks it renders are ordinary Svelte components that must work in the browser too.

**Your own project.** `src/lib/types.ts` points at the types `hydrogen generate` wrote for `examples/starter`; use your
own `gen/types.ts`. Add a block: a component in `src/lib/blocks/`, a line in `registry.ts`, and CSS. Channel names
double as locales here (`en`, `de`); adapt `src/lib/channels.ts` and the `[locale=locale]` routes if yours differ.
