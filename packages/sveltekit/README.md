# @hydrogencms/sveltekit

SvelteKit adapter for Hydrogen (SvelteKit 2, Svelte 5). The CMS decides which page lives at which URL; the app has
one catch-all route, a template per content type, and two small endpoints. `packages/starters/sveltekit` is a
complete site on it.

```ts
// src/lib/server/hydrogen.ts
export const hydrogen = createHydrogen({ baseUrl, channels: ['en', 'de'], templates: ['page', 'landing'],
  revalidateSecret: env.HYDROGEN_REVALIDATE_SECRET })
// src/hooks.server.ts                                  export const handle = hydrogen.handle
// src/routes/[...path]/+page.server.ts                 export const load = (event) => hydrogen.load(event)
// src/routes/[...path]/+page.svelte                    <Page {data} {templates} />
// src/routes/api/hydrogen/preview/+server.ts           export const GET = hydrogen.previewRoute
// src/routes/api/hydrogen/revalidate/+server.ts        export const POST = hydrogen.revalidate
```

`@hydrogencms/sveltekit` is the server side (no Svelte); `@hydrogencms/sveltekit/components` holds the Svelte 5
components and the registry. Use `adapter-node` (or any adapter with a long-lived process: the cache is in memory).

## Routing and statuses

`handle` puts `event.locals.hydrogen` on every request. `load` asks the CMS (`@hydrogencms/router`) once per request,
before anything renders, so the status is right: a page renders, a redirect answers the CMS's status (301 included)
with the query kept, `not_found`, an unknown prefix and a `reserved` path (`/blog/*`, `/api/*`: yours) are real 404s.
A CMS that errors is a 502, never a 404. With `channels: [...]` the request's host and path prefix are matched against
the channels' `match` options (`matchSite`), as the CMS does; `origin` overrides the public origin used to locate URLs
(`trustForwarded` reads `X-Forwarded-Host`/`-Proto`, default false: set it only behind a front proxy that overwrites them).

Your own routes (a blog the CMS does not route) use `hydrogen.client(event, channel)`, `hydrogen.channel(event)`,
`hydrogen.nav(event)` and `hydrogen.render(event, { template, item, locale, metadata })`, which returns what `<Page>`
renders.

## Templates and blocks

`<Page {data} {templates} fallback>` writes the head (`<title>`, canonical, one `hreflang` alternate per entry;
`noindex` and the token proof in preview) and renders the component for the item's type with `{ item, locale,
metadata, preview }`. Blocks go through a registry:

```svelte
<Blocks blocks={item.fields.body} {registry} path="fields/body" {preview} />
```

```ts
export const registry = createRegistry({ hero: Hero, cards: Cards })   // `shared` is built in; unknown types render nothing
```

A block component gets `{ block, props, attrs, path, renderSlot }`: spread `attrs` on its root element (`data-h-block`
always, `data-h-path` in preview) and render a slot with `{@render renderSlot('items')}`. `<RichText doc links />`
renders rich text (`item:<id>` links come from `item.links`; unsafe URLs are dropped), `<Image asset preset />` an
asset with its variant and srcset, `<Field path value {preview} />` marks a text field for the canvas (pass the text as
`value`: Svelte's own comment nodes would otherwise break inline editing).

## Preview

The CMS's preview URL is the page's URL plus `?preview=<token>`. `handle` redirects it to the preview route, which
keeps the token in an httpOnly cookie (`SameSite=None; Secure` over https, one week) and redirects to the page; `?exit=1`
leaves preview, and a token the CMS refuses leaves it by itself. While the cookie is there, everything is fetched
uncached with the token, responses are `Cache-Control: private, no-store`, pages carry `data-h-path` markers and
`<meta name="hydrogen-preview">` (the token's SHA-256) and the page renders from the admin's working document through
`@hydrogencms/bridge` in live mode (`<Live>`): it updates while the editor types. An item without a route is shown at
`/preview/<collection>/<id>` (`previewPath`). The token is never logged.

## Revalidation

Every CMS response is kept in a small in-memory cache keyed by request and tagged with its `Surrogate-Key`
(`ttl`, default 1 hour, only bounds a missed webhook; 500 entries). Subscribe a webhook with `events: ["*"]` to
`/api/hydrogen/revalidate`: the signed event (`Hydrogen-Signature`, secret of 16+ characters) purges exactly the entries
carrying its `tags`. The cache is per process: run one instance, or deliver the webhook to every instance.
