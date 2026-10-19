# @hydrogencms/next

Next.js App Router adapter for Hydrogen (Next 16, React 19). The CMS decides which page lives at which URL; the
app has one catch-all route, templates per content type, and a few route handlers. See `examples/next-site`.

```ts
// lib/hydrogen.ts
export const hydrogen = createHydrogen({ baseUrl, channels: ['corporate_en', 'careers_en'], templates: { page: Page }, fallback: Generic,
  revalidateSecret: process.env.HYDROGEN_REVALIDATE_SECRET })
// app/[[...path]]/page.tsx        export const instant = false; export default hydrogen.page(); export const generateMetadata = hydrogen.metadata()
// app/api/hydrogen/revalidate/route.ts   export const POST = hydrogen.revalidate()
// app/api/hydrogen/preview/route.ts      export const GET = hydrogen.previewRoute()
// proxy.ts                        export const proxy = createProxy({...})      // from '@hydrogencms/next/proxy'
```

## Caching

Needs `cacheComponents: true` in `next.config.ts`. CMS responses are cached with `"use cache"`, and the entry is
tagged with `cacheTag(...)` **after** the fetch, from the response's `Surrogate-Key`. That is what `fetch(url, { next: { tags } })`
(tags fixed before the request) and `unstable_cache` (tags fixed at definition) cannot do. Hydrogen's purge tags
(`i:<id>`, `c:<type>`, `s:<singleton>`, `r:<site>`, `nav:<site>`) are used unchanged, plus `*` on every entry, so
the webhook's `tags` go straight to `revalidateTag(tag, { expire: 0 })` and exactly the entries that carry them
are refetched on their next use. `ttl` (default 1 h) is only the safety net for a missed webhook. Beyond Next's 128
tags per entry the item tags are dropped first and the entry gets a short life.

Pages themselves render per request from those entries. Redirects and 404 statuses cannot be changed once a page
streams (cache components make every page dynamic data streaming), so `createProxy` decides them with one uncached
`/route` request per page view: the CMS's redirect status (301 and the query string kept) and a real 404. If the CMS
is down, slow (`timeoutMs`, default 3 s) or fails, the request passes and the page renders from its cache; a 400 from
the CMS is a 404. A preview cookie is only added to that call as the token; a refused token is ignored.

**Multiple instances.** The default cache is per instance, so a webhook revalidates only the instance that received
it. Serverless or multi-instance deployments need a shared cache handler (`cacheHandlers` in `next.config.ts`) or the
webhook fanned out to every instance; otherwise `ttl` bounds staleness.

## Channels

`channel: 'x'` serves one channel. `channels: [...]` serves several from one deployment: the request's host (or
`x-forwarded-host` with `trustForwarded: true`) and path prefix are matched with the channels' `match` options (`matchSite`), exactly as the
CMS does. `origin` overrides the public origin used to locate URLs.

## Preview

A preview URL from the CMS (`POST /preview-tokens` returns `url`) is the normal URL plus `?preview=<token>`. The proxy
redirects any request with that parameter to the preview route (`token`, and `path` = the URL without `preview`), so
routes and pattern URLs (`/preview/<collection>/<id>`) both work. The token is a bearer secret and is never logged.

`GET /api/hydrogen/preview?token=<preview token>&path=/…` enables draft mode, stores the token in an httpOnly cookie
and redirects (`?exit=1` leaves). In draft mode the page and its templates' `client` are uncached and carry the token;
`/preview/<collection>/<id>` (the channels' preview pattern) shows an item that has no route yet.

## Live preview

In draft mode the page renders a client wrapper (`@hydrogencms/next/live`, on `@hydrogencms/react/live`'s
`useLivePreview`) around a **client template** (a `'use client'` module), so the page updates while the editor types in
the admin: the bridge receives the working document and patches, the template re-renders from them. Client templates
get `{ item, locale, metadata, preview }` (`LiveTemplateProps`); server templates render as before (with `client`,
`blocks` and `preview`) and only show saved drafts. Shared blocks, which the admin's working document holds without
their content, keep the content the page was delivered with.

In draft mode the page also renders `<meta name="hydrogen-preview" content="1">`: the preview cookie is HttpOnly, so
the bridge cannot read it. Pass `path="fields/body"` and `preview` to `<Blocks>` in templates (`@hydrogencms/react`).

`createHydrogen({ ..., blocks: registry, adminOrigin })`: `blocks` (a `createRegistry` result) is handed to server
templates as `blocks`; `adminOrigin` is the origin of the admin that embeds draft pages (default: the origin of
`baseUrl`) and the only one the bridge talks to. It must be the origin the editor's browser reaches the admin at: if
`baseUrl` is an internal address (`http://cms:8080`) set `adminOrigin` to the public one. In draft mode a non-https
origin (other than localhost) or an internal-looking host logs one warning.

For routes of your own (a collection the CMS does not route): `hydrogen.clientFor(channel)` (cached and tagged, or
uncached with the token in draft mode), `hydrogen.previewToken()`, `hydrogen.render(Template, { item, locale, metadata,
draft })` (applies the live wrapper), `hydrogen.channel()` (the request's channel, for layouts) and `hydrogen.adminOrigin`.
See `packages/starters/next` and `docs/adapter-kit.md`.

## Notes

- Templates are server components and get `{ item, locale, metadata, client }`; their `client` is cached: pass only
  serializable options.
- `X-Forwarded-Host`/`Host` must be trustworthy (set by your proxy), as for any host-based routing.
- The page never trusts the proxy: it derives the channel from Host, path and the channels' matchers itself. The
  proxy's `x-hydrogen-path` header (always overwritten) only picks the navigation's channel for that response.
- `x-forwarded-host` and `x-forwarded-proto` are ignored unless `trustForwarded: true`; set it only behind a front
  proxy that overwrites them (a fixed `origin` makes them irrelevant).
- Channel tokens are kept out of cache keys (`token` is registered once per process).
- Reserved paths (the channels' `reserved`) pass through the proxy to your own routes.
