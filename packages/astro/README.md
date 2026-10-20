# @hydrogencms/astro

Astro adapter for Hydrogen (Astro 6 and 7, SSR with an adapter such as `@astrojs/node`, or a fully static build). The CMS decides which page
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

`Astro.locals.hydrogen`: `page`, `href(url)` (a CMS URL as a path of this site, with Astro's `base`), `channel` (matched by host and prefix), `draft`, `client(channel?)` (a client of the
CMS: cached and tagged normally, uncached with the preview token in preview), `nav()`, `exitUrl(path)`, `unavailable`.
Add `src/pages/404.astro` (the middleware rewrites unknown URLs to it with status 404).

## Static sites

With `output: 'static'` and no Astro adapter, `astro build` stays fully static: the integration installs the
middleware only (no preview route, no bridge, no revalidation webhook; rebuild when content changes). While Astro
prerenders, the middleware never reads cookies, strips Astro's `base` from the path, and asks the CMS about
`origin` (the option) or else Astro's `site` or else the request's origin: set one of them to the origin the
channels' `frontend` uses (a channel that matches by host needs it). The channel's `frontend` may include the base
(then canonical and hreflang URLs carry it): CMS URLs that hold the base are recognised and never get it twice. Pass
`base: import.meta.env.BASE_URL` to `createHydrogen` so `staticPaths()` and `href()` know it before the middleware
loads.

```js
// astro.config.mjs
export default defineConfig({ output: 'static', site: 'https://acme.github.io', base: '/docs', integrations: [hydrogen()] })
```

```astro
---
// src/pages/[...path].astro: one page for every URL the CMS routes
import hydrogen from '../hydrogen'
export function getStaticPaths() {
  return hydrogen.staticPaths()
}
const p = Astro.locals.hydrogen.page!
---
```

`hydrogen.staticPaths({ param = 'path' })` returns `{ params: { path } }` for every URL the configured channels route:
it lists each collection a channel allows in each of its languages (500 items per request, four lists at a time),
keeps the item `url`s that belong to that channel and are not reserved, removes duplicates and sorts them. `path`
has no base and no leading slash (`undefined` for the root, as Astro's rest parameters expect). A collection that
cannot be listed (a singleton) is skipped; any other failure fails the build. Use `export function getStaticPaths`,
not `export const …`: Astro keeps a whole frontmatter at module level for the latter and `Astro.locals` is unavailable
there.

**Draft preview in `astro dev`.** A prerendered page sees no cookies and no query string, so a static site could not
show drafts. In `astro dev` (and only there) the integration therefore adds `/api/hydrogen/preview` and
`/__hydrogen/bridge.js` and renders the CMS page routes on demand, through Astro's `astro:route:setup` hook. The
admin's Preview then works like on an SSR site: `?preview=<token>` sets the cookie, the page shows the draft, and the
bridge reloads it on save. Editors of a content-in-git project run the admin locally next to `npm run dev`, which is
where this matters. The routes that render on demand are `src/pages/[...path].astro` and `src/pages/404.astro` (a
rewrite to the 404 page needs it on demand too); list others with `hydrogen({ devRoutes: [...] })` (paths relative
to the project root, replacing the default) when your pages live elsewhere. Do not list routes that rely on
`getStaticPaths` props (Starlight and other injected routes). The layout must emit the preview markers when
`Astro.locals.hydrogen.draft`: use `<Head />`, or copy what `hydrogen new -static`'s `Layout.astro` does (robots,
`hydrogen-preview` meta, the bridge script via `h.href('/__hydrogen/bridge.js')`, and `data-h-root` on `<main>`).
The CMS's share links point at the dev server, so turn them off (`delivery.share_links: false`). Serve the admin and
the dev site from the same host name (`hydrogen dev` uses `127.0.0.1`, so `server: { host: '127.0.0.1' }` and a
channel `frontend` on `127.0.0.1`): `localhost` and `127.0.0.1` are different sites, and the browser drops the preview
cookie inside the admin's frame. With `trailingSlash: 'always'` the dev server adds the slash to preview URLs, which
the CMS builds without one.

Unknown paths are rewritten to your `404` page, which is built once as `404.html` and not resolved itself. Reserved
paths (`reserved` in the channel, relative to its prefix) pass through: the middleware answers 404 for every other
path it does not know, so list the pages your frontend owns (`/blog/*`) as reserved and give them their own
routes. A CMS redirect becomes Astro's meta-refresh page at the requested path, pointing to the target with the
base. Directory pages are resolved without their trailing slash. Header matchers cannot work in a build (there are no
request headers).

The channel's `frontend` is this site by definition: URLs on its origin are internal even when Astro's `site`
differs, and a build warns once when the two origins differ (canonical and hreflang URLs from the CMS name the
frontend's). Align them, for example one `SITE_URL` for both. `href()` follows Astro's `trailingSlash` for page paths
(`'always'` adds `/` unless the path has a file extension, `'never'` removes it except for the root; `?` and `#` stay).

Links: wrap CMS URLs (`item.links`, `item.url`, tree nodes) in `Astro.locals.hydrogen.href(url)` (also
`hydrogen.href`, after the middleware ran): own-origin and absolute-path URLs get the base, other origins stay.
Pass `resolveLink` to `RichText` to do the same for rich text links. A static build asks the CMS once per list page
and once per page rendered (the answers of `client()` calls in layouts are cached for `ttl`).

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

`dist/*.js` is held to an 8 KB gzip budget (`npm run size`); the dev preview of static sites is most of what it adds
over the other adapters. The components are source files that Astro compiles in the consumer's build; they are thin
wrappers over `dist` helpers and are not counted.
