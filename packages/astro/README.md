# @hydrogencms/astro

Astro adapter for Hydrogen (Astro 6 and 7, SSR with an adapter such as `@astrojs/node`). The CMS decides which page
lives at which URL; the site has one catch-all page, a template component per content type, block components and a
config module. See `packages/starters/astro` for a complete site.

```js
// astro.config.mjs
import hydrogen from '@hydrogencms/astro'
export default defineConfig({ output: 'server', adapter: node({ mode: 'standalone' }), integrations: [hydrogen()] })
```

```ts
// src/hydrogen.ts  (the integration reads the default export; `hydrogen({ config: 'src/other.ts' })` moves it)
import { createHydrogen } from '@hydrogencms/astro/runtime'
export default createHydrogen({
  baseUrl: process.env.HYDROGEN_URL!, channels: ['en', 'de'], templates: ['page', 'landing'],
  revalidateSecret: process.env.HYDROGEN_REVALIDATE_SECRET,
})
```

```ts
// src/env.d.ts: types Astro.locals.hydrogen
/// <reference types="@hydrogencms/astro/env" />
```

The integration installs, from that one instance:

| Piece | What it does |
|---|---|
| middleware | asks the CMS about every page request and decides what a page can no longer change: redirects with the CMS's status (301 and the query string kept) and a real 404 through your `/404` page (also for unknown language prefixes and bad paths). Reserved paths (`/blog/*`, `/api/*`) pass to your own pages. Result in `Astro.locals.hydrogen` |
| `/api/hydrogen/preview` | `?token=…&path=/…` keeps the preview token in an HttpOnly cookie and redirects; `?exit=1` clears it. The CMS's `?preview=<token>` URLs are handed here by the middleware |
| `/api/hydrogen/revalidate` | the signed webhook (`Hydrogen-Signature`), purging the cache by the event's tags |
| `/__hydrogen/bridge.js` | the preview bridge (`@hydrogencms/bridge/iife`), included in preview pages by `<Head />` |

## The catch-all page

```astro
---
// src/pages/[...path].astro
const h = Astro.locals.hydrogen
const p = h.page            // { template, item, locale, metadata } resolved by the middleware
const T = p && ({ page: Page, landing: Landing } as const)[p.template as 'page']
if (!T) return new Response('Not found', { status: h.unavailable ? 503 : 404 })
---
<Layout><T item={p.item} locale={p.locale} metadata={p.metadata} preview={h.draft} /></Layout>
```

`Astro.locals.hydrogen`: `page`, `channel` (matched by host and prefix), `draft`, `client(channel?)` (a client of the
CMS: cached and tagged normally, uncached with the preview token in preview), `nav()`, `exitUrl(path)`, `unavailable`.
Add `src/pages/404.astro` (the middleware rewrites unknown URLs to it with status 404).

## Components

Published as `.astro` source (Astro compiles them in your project): `@hydrogencms/astro/components/<Name>.astro`.

| Component | |
|---|---|
| `Head` | in `<head>`: title, canonical, hreflang alternates, `noindex` and the token proof (`<meta name="hydrogen-preview">`) plus the bridge script in preview. `<Head title description />` for your own routes |
| `Blocks` | `<Blocks blocks={item.fields.body} registry={registry} path="fields/body" preview={preview} links={item.links} />` |
| `RichText` | `<RichText doc={…} links={item.links} />`: escaped HTML, `item:<id>` links through `links`, unsafe URLs dropped; `render` overrides a node type |
| `Image` | `<Image asset={…} preset="card" />` (client's `imageProps`: variant, srcset, focal point) |
| `Field` | `<Field path="fields/title" preview={preview}>…</Field>`: wraps editable inline content in `data-h-path` in preview only |

**Blocks.** `createRegistry({ hero: Hero, … })` maps block types to `.astro` components. A component gets `{ block,
props, attrs, path, preview, links, registry, slots }`, spreads `attrs` on its root element (`data-h-block` always,
`data-h-path` in preview) and renders a slot with `<Blocks {...slots.items} />`. `shared` is built in (the inlined
blocks, read-only). An unknown type renders nothing (a labelled box in preview) and warns once in development.

**Reload mode.** Astro renders on the server, so the bridge runs in reload mode: when the editor saves, the page is
fetched again and the element marked `data-h-root` is swapped. Put `data-h-root` on the element around everything the
editor can change (`<main data-h-root>`); the bridge script stays outside it.

## Caching

CMS responses are cached in memory, keyed by request and tagged with the responses' `Surrogate-Key` (`i:<id>`,
`c:<type>`, `s:<singleton>`, `r:<site>`, `nav:<site>`, plus `*`). The webhook purges exactly the entries that carry
the event's tags; `ttl` (default 1 h) is the safety net for a missed webhook, a 404 lives 30 s. A CMS that is down
is answered from expired entries when there are any. Preview requests (the token in the cookie) are never cached,
and their responses are `private, no-store`. The cache lives in one server process: with several instances, fan the
webhook out to each, or rely on `ttl`.

## Size

`dist/*.js` is held to a 7 KB gzip budget like the other adapters (`npm run size`). The components are source files
that Astro compiles in the consumer's build; they are thin wrappers over `dist` helpers and are not counted.
