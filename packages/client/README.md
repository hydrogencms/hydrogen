# @hydrogencms/client

Typed client for the Hydrogen delivery API (`/api/deliver/v1`). It has no dependencies and runs in Node 20+, browsers
and edge runtimes (it only needs `fetch`). The framework adapters are built on it; see
[docs/delivery-api.md](../../docs/delivery-api.md) for the API itself.

```sh
npm i @hydrogencms/client
```

## Use

```ts
import { createClient } from '@hydrogencms/client'

const cms = createClient<{ post: { title: string } }>({ baseUrl: 'https://cms.acme.com', channel: 'web_en' })

const { data: list, tags } = await cms.list('post', { where: { title: { prefix: 'H' } }, sort: '-publishedAt', limit: 10 })
const { data: item } = await cms.item(list.items[0].id, { include: 1 })
const { data: card } = await cms.item(list.items[0].id, { fields: ['title', 'seo.title'] })   // only these fields
const { data: route } = await cms.route('/en/about')   // what lives at a URL: page, item, redirect, not found
```

Every call resolves to `{ data, tags, etag, status }`; `tags` are the response's `Surrogate-Key` cache tags. Errors
throw `HydrogenError` (`status`, `code`, `fields`). The client has `item`, `itemByKey`, `singleton`, `list`, `route`,
`tree`, `channel` and `withPreview(token)`. Options: `token` (channel token), `preview` (preview token),
`headers`, `fetch`, and `init`, a per-request hook for framework fetch options such as cache settings.

Also exported: `buildQuery`, `parseTags`, `linkTo` and `imageProps(asset, preset)`, which gives the `<img>`
attributes (`src`, `srcSet`, `width`, `height`, `alt`) of an asset's preset variant.

## Subpath exports

| Import | Holds |
| --- | --- |
| `@hydrogencms/client` | `createClient`, types, `HydrogenError`, `imageProps` and the helpers above. |
| `@hydrogencms/client/server` | Helpers for server-side adapters: webhook signature verification (`verifySignature`), the preview cookie rules (`parsePreviewRequest`, `previewCookieOptions`), public URLs, and `createTagCache`, a response cache purged by webhook tags. Uses WebCrypto only. |
| `@hydrogencms/client/richtext` | Framework-neutral rich text: `richTextNodes(doc, { links })` walks a ProseMirror document into plain element nodes, `safeHref` and `linkHref` check and resolve links. |

See [docs/adapter-kit.md](../../docs/adapter-kit.md) for how adapters use these.
