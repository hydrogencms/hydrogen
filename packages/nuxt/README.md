# @hydrogencms/nuxt

Nuxt module for Hydrogen (Nuxt 4, Nitro node server, Vue 3). The CMS decides which page lives at which URL; the app has
one catch-all page, a template per content type, and block components. Not for static generation: pages render per
request. See `packages/starters/nuxt`, and `docs/adapter-kit.md` for the contract.

```ts
// nuxt.config.ts
export default defineNuxtConfig({
  modules: ['@hydrogencms/nuxt'],
  hydrogen: { baseUrl: 'https://cms.acme.com', channels: ['en', 'de'], templates: ['page', 'landing'] },
})
```

```vue
<!-- app/pages/[...slug].vue -->
<script setup lang="ts">
const templates = { page: Page, landing: Landing }
const page = useHydrogenPage()            // { template, item, locale, metadata, preview }; sets the head; 404/503 otherwise
</script>
<template><component :is="templates[page.template]" :item="page.item" :locale="page.locale" :preview="page.preview" :metadata="page.metadata" /></template>
```

Install: `npm i @hydrogencms/nuxt @hydrogencms/client @hydrogencms/router @hydrogencms/bridge`. Put `data-h-root` on the
element around `<NuxtPage />` (the preview bridge swaps it), and link pages with plain `<a href>` (full page loads: the
CMS client lives on the server, and a page's data comes from the request's resolution).

## Options (`hydrogen`)

| Option | Default | |
|---|---|---|
| `baseUrl` | `http://localhost:8080` | the CMS |
| `channel` / `channels` | required | one channel, or several told apart by their `match` options (host and prefix) |
| `templates`, `fallback` | `[]`, `false` | content types that have a template; a fallback template exists (template `*`) |
| `token` | | token of a token-protected channel (server side only) |
| `origin`, `trustForwarded` | the request's host, `false` | fixed public origin; use `X-Forwarded-Host/-Proto` (off by default; only behind a front proxy that overwrites them) |
| `previewPath` | `/preview` | `/preview/<collection>/<id>`: the preview URL of items without a route |
| `ttl`, `timeoutMs` | 3600 s, 3000 ms | freshness of a cached CMS response without a webhook; time the CMS may take |
| `revalidateSecret` | | webhook secret, 16+ characters |
| `adminOrigin` | the origin of `baseUrl` | the admin that may embed preview pages and talk to them |

Everything is runtime config (`runtimeConfig.hydrogen`), so `NUXT_HYDROGEN_BASE_URL`, `NUXT_HYDROGEN_ORIGIN`,
`NUXT_HYDROGEN_REVALIDATE_SECRET`, `NUXT_HYDROGEN_TOKEN` and `NUXT_HYDROGEN_ADMIN_ORIGIN` override it at start.

## What the module adds

- **A server middleware** that asks the CMS (`/route`, one cached request per page view) before Nuxt renders:
  redirects answer with the CMS's status (301 included, query kept), the channel is matched from host and path
  prefix, reserved paths (`/blog/*`, `/api/*`) are left to your own routes. The rest is passed to the page in
  `event.context.hydrogen`; `useHydrogenPage()` turns "not found" into a real 404 and an unreachable CMS (down or
  slower than `timeoutMs`) into a 503, unless the answer is still cached.
- **Composables**: `useHydrogenPage()`, `useHydrogen()` (`client(channel?)`, `channel`, `channels`, `preview`; call it in
  `setup` and use the client inside `useAsyncData` handlers), `useHydrogenPreview()` (for routes of your own).
- **Components**: `<HydrogenBlocks>`, `<HydrogenField>`, `<HydrogenRichText>`, `<HydrogenImage>`; `createRegistry`,
  `useLinks`.
- **Server routes** `/api/hydrogen/preview` and `/api/hydrogen/revalidate`, and `/__hydrogen/bridge.js`.

## Blocks

```ts
const registry = createRegistry({ hero: Hero, cards: Cards, card: Card })     // `shared` is built in
```
```vue
<HydrogenBlocks :blocks="item.fields.body" :registry="registry" path="fields/body" :preview="preview" :links="item.links" />
```

A block component takes the block's props as its prop `props` and renders the block's slots as Vue slots of the same
name (`<slot name="items" />`). `data-h-block` (and `data-h-path` in preview) fall through to its root element, so a
component needs a single root (otherwise bind `$attrs` itself). `useLinks()` is the item's `links` (item id → URL, for
`ref` props); `<HydrogenRichText :doc :links>` turns `item:<id>` links into those URLs and drops unsafe ones. Unknown
block types render nothing (and warn in development).

## Caching and revalidation

CMS responses are cached in memory per server instance: a successful GET is kept with the tags of its `Surrogate-Key`
(plus `*`) until `ttl` passes. `POST /api/hydrogen/revalidate` receives the CMS webhook (`events: ["*"]`, the same
secret), verifies `Hydrogen-Signature` and drops the entries that carry one of the event's `tags`. Preview requests are
never cached. With several instances, fan the webhook out to each (or rely on `ttl`).

## Preview

A preview URL from the CMS is the normal URL plus `?preview=<token>`. The middleware redirects it to
`/api/hydrogen/preview`, which keeps the token in an HttpOnly cookie (`?exit=1` leaves) and returns to the page. With
the cookie the middleware and `useHydrogen().client()` carry the token uncached; a refused token leaves preview.
`useHydrogenPreview()` (called by `useHydrogenPage`) adds `<meta name="hydrogen-preview">` with the SHA-256 of the token
and `<meta name="robots" content="noindex">`, and the Nitro plugin appends the bridge script to the page, in **reload
mode**: after a saved draft the admin tells the bridge to re-fetch the page and swap `[data-h-root]`.
