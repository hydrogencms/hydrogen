# Hydrogen starter for Nuxt

A Nuxt 4 (Vue 3, Nitro node server) site on `@hydrogencms/nuxt`, rendering the page-builder project in
`examples/starter`: pages made of blocks in English and German (`/en`, `/de`), a blog, a navigation from the page
tree, a footer from the `siteSettings` singleton, preview in the admin. It is the template for new Nuxt frontends:
copy this directory and adapt it.

## Run it

```sh
# 1. the CMS: examples/starter with its fixture content
cd examples/starter
export STARTER_FRONTEND=http://127.0.0.1:3000      # where the channels' frontend lives (canonical URLs, preview links)
go run . migrate && go run . import fixture
go run . serve                                     # http://localhost:8080, admin at /admin

# 2. the SDK packages (until they are published) and this app
cd ../../packages && npm install && npm run build
cd starters/nuxt && npm install
export NUXT_HYDROGEN_BASE_URL=http://localhost:8080
npm run dev                                        # or: npm run build && npm start
```

Create a webhook in the CMS config for published changes to show on the site:

```yaml
webhooks:
  nuxt:
    url: http://127.0.0.1:3000/api/hydrogen/revalidate
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}      # the same value as NUXT_HYDROGEN_REVALIDATE_SECRET, 16+ characters
```

| Variable | Default | |
|---|---|---|
| `NUXT_HYDROGEN_BASE_URL` | `http://localhost:8080` | the CMS |
| `NUXT_HYDROGEN_REVALIDATE_SECRET` | | webhook secret |
| `NUXT_HYDROGEN_ORIGIN` | the request's host | fixed public origin, for a deployment whose host is not the channel's |
| `NUXT_HYDROGEN_ADMIN_ORIGIN` | the origin of the CMS | the admin that may embed preview pages and talk to them |

`make starter-nuxt-e2e` (repo root) builds the CMS and this app, imports the fixture and runs the adapter conformance
suite, browser checks included. The install uses `legacy-peer-deps` (see `.npmrc`): npm 11.2 fails on Nuxt's peer set.

## How it is put together

| File | Role |
|---|---|
| `nuxt.config.ts` | the module and its options: CMS, channels, content types with a template |
| `app/pages/[...slug].vue` | pages: the CMS decides which page lives at which URL (`useHydrogenPage()`) |
| `app/pages/[locale]/blog/*.vue` | posts: a collection the CMS does not route (its channels reserve `/blog/*`), found by slug |
| `app/pages/blog/[slug].vue` | the CMS's preview URL of a post (`/blog/{slug}`, no language prefix): finds the post's channel with the token, redirects |
| `app/templates/` | `Page`, `Landing`, `Post`: props `{ item, locale, preview, metadata }` |
| `app/blocks/`, `app/lib/blocks.ts` | block components and the registry: `hero`, `richText`, `cards` + `card`, `callout`, `image`; `shared` is built in |
| `app/composables/` | `useSite()` (navigation and settings of the request's channel), `usePosts()` |
| `app/app.vue`, `app/error.vue` | layout (`data-h-root` around the page) and the 404 page |
| `app/assets/main.css` | the design: schema tokens (`tone`, `space`) are class names |

Pages link with plain `<a href>`: each page is one request, resolved by the CMS. Data your pages need beyond the
resolved item goes through `useHydrogen().client()` inside `useAsyncData`.

**Your own project.** `app/lib/types.ts` points at the types `hydrogen generate` wrote for `examples/starter`; use your
own `gen/types.ts`. Add a block: a component in `app/blocks/`, a line in the registry, and CSS. Channel names double as
locales here (`en`, `de`); adapt `app/lib/channels.ts` and the `[locale]` routes if yours differ.
