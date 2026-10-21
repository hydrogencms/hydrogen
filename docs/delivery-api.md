# The delivery API

The delivery API is how frontends and apps read **published** content. It is read-only, served under
`/api/deliver/v1`, and cached: every response carries an `ETag` and cache headers, and an in-process cache is purged
the moment content changes. It is on whenever the schema has at least one type.

```sh
curl -s localhost:8080/api/deliver/v1/collections/article/items
curl -s localhost:8080/api/deliver/v1/items/by-key/hello
```

The full contract, generated from your schema, is the OpenAPI document at `GET /api/deliver/v1/openapi.json`. It is
public, and lists the types, fields and filters of your project. The pages below are the overview.

## Endpoints

| Method and path | Returns |
| --- | --- |
| `GET /collections/{type}/items` | A page of a collection's items. |
| `GET /items/{id}` | One item by id. |
| `GET /items/by-key/{key}` | One item by its key (lower-case letters, digits, `-` and `_`). |
| `GET /singletons/{type}` | A singleton's item. |
| `GET /search` | Full-text search over the channel's searchable collections. See [search](search.md). |
| `GET /channels/{name}` | A channel's public information: name, languages, collections and public options. |
| `GET /openapi.json` | This API's OpenAPI document. |
| `GET /route`, `GET /tree` | With the pages module: locate a URL, and read a site's navigation. See [Pages](#pages). |
| `GET /graphql` and `POST /graphql` | With the [graphql](graphql.md) module. |
| `GET /preview/events` | The live stream of a share-link preview. See [share links](share-links.md). |

Modules add routes under the same prefix; they follow every rule below.

## Channels

Every request is for one [channel](configuration.md#channels), which decides the languages, the collections and who
may read. The channel is chosen, in this order, by:

1. a channel token in `Authorization: Bearer …` (the token belongs to one channel);
2. `?channel=<name>` for a public channel;
3. the channel the preview token was minted for;
4. the only channel, when the project has one.

Otherwise the answer is `400`. A token channel read without its token is `401`; a public channel does not need a
token. A type the channel does not list is `404`, like any item that is not available.

## Items

```json
{
  "item": {
    "id": "01a15280-8108-77be-b0a0-9ffc272ac898",
    "type": "article",
    "key": "hello",
    "locale": "en",
    "version": 1,
    "publishedAt": "2026-10-19T04:51:58.755Z",
    "name": "Hello, Hydrogen",
    "fields": { "title": "Hello, Hydrogen", "slug": "hello-hydrogen", "summary": "First article." }
  },
  "included": { }
}
```

A listing answers `{"items": […], "hasMore": false}`. `hasMore` tells whether another page follows without counting;
add `?total=true` to also get an exact `total`, which costs a count.

- **Unavailable is 404.** An item that is a draft, unpublished, trashed, in a locale the channel does not serve, or
  outside the channel's collections answers the same `404 not_found`, so a response never reveals what exists.
- **Locale.** An item is shown in the first of the channel's `languages` it is available in. `?locale=de` moves that
  language to the front (it must be one of the channel's). `"fallback": true` marks an item shown in a locale other
  than the requested one, or the channel's first.
- **References.** `ref` fields are ids. `?include=1` (up to `3`) adds the referenced items to `included`, keyed by
  id, at most 100 of them. `asset` fields are expanded in place into `{id, url, name, mime, size, fields,
  variants?}`, or `null` when the asset is not published.
- **Links.** `links` maps item ids found in rich text and link fields to site paths, and `url` is the item's own
  path, from the pages module or the channel's `links` patterns. Items with neither have no URL.
- **Shared blocks.** A `shared` block arrives with the shared item's blocks inlined.
- **Fields.** `?fields=title,seo.title` shows only those fields (a dot path keeps one member of a group); the
  envelope (`id`, `type`, `key`, `locale`, `version`, `publishedAt`, `name`, `url`, `links`, `fallback`) stays. Use
  it for menus and cards: fields left out cost no transfer, no decoding, no inlined shared blocks and no expanded
  assets. `include` and `links` follow the references in the selected fields only; items in `included` are whole.
  Every path must name a field of the type (of a type the channel shows, for `/items/{id}`, `by-key` and
  `/search`); empty means every field. Works on `/items/{id}`, `/items/by-key/{key}`, `/singletons/{type}`,
  listings, `/search` and the pages module's `/tree`.

### Lists

| Parameter | Meaning |
| --- | --- |
| `limit`, `offset` | Page size (default 50, at most 500) and start (at most 1,000,000). |
| `sort` | Comma-separated fields, `-` for descending: `-publishedOn,name`. Sortable: indexed fields, `name`, `key`, `createdAt`, `updatedAt`, `publishedAt` (the latest publication; never published items last). At most 4. |
| `where[field][op]=value` | Filter on an indexed field. Ops: `eq` (default), `ne`, `lt`, `lte`, `gt`, `gte`, `in` (comma-separated), `contains`, `exists` (`true` or `false`), `prefix`, `under`. Repeated filters are ANDed, at most 32. |
| `q` | Full-text words (at most 16). |
| `include`, `fields`, `total`, `locale`, `channel`, `preview` | As above. |

```sh
curl -s 'localhost:8080/api/deliver/v1/collections/article/items?where[publishedOn][gte]=2026-01-01&sort=-publishedOn&limit=10&include=1'
```

An unknown parameter is `400`, as is a repeated single-valued one. Filters on a field that is not `index: true` are
refused with a message naming the field.

## Caching

| Header | Meaning |
| --- | --- |
| `ETag` | Send it back as `If-None-Match` to get `304`. |
| `Cache-Control` | `public, max-age=0, s-maxage=<ttl>, stale-while-revalidate=<ttl>`. `ttl` is the channel setting `delivery.cache_ttl` (default 60 seconds); token channels say `private`. |
| `Surrogate-Key`, `Cache-Tag` | The tags of everything the response used, for CDN purges. |
| `Vary` | `Authorization`, plus headers a module says the answer depends on. |

Tags are `i:<item id>` (an item), `c:<type>` (a collection's listings), `s:<type>` (a singleton), `r:<site>` and
`nav:<site>` (routes and navigation of a pages site) and `ch:<channel>`. A response that used more than 64 items is
tagged with the collections instead. The in-process cache (`delivery.cache_bytes`) is purged by the same tags, on
every server, through the database's notifications. Point a CDN at `s-maxage` and purge by `Surrogate-Key`, or let
the frontend's webhook do it (below).

Preview requests are never cached.

## Preview

Editors see drafts through a **preview token**. A token is minted for a channel (and optionally one item) with
`POST /api/manage/v1/preview-tokens`, lives at most an hour, and goes in `X-Hydrogen-Preview` or `?preview=`. With it,
delivery answers from the draft, uncached and with `X-Robots-Tag: noindex`. The admin does this for its preview
frame; a frontend only has to pass the token on. [Share links](share-links.md) give the same view to people without an
account.

## Pages

With the pages module, a channel with `match` serves a page tree:

- `GET /route?channel=<name>&url=<absolute public URL>` answers `{"located": {"kind": …}, "item": …}`. `kind` is `page`
  or `item` (render `item`), `redirect` (answer `located.resolution.status` with `location`), `not_found`,
  `no_channel` or `reserved` (a path the frontend owns).
- `GET /tree?depth=2` returns the visible navigation below the site root (`depth` 1 to 10, default 1; `site` picks
  another site than the channel's; `fields=title,seo.title` puts those fields of each page's item on its node as
  `fields`, in one batch).

[The adapter kit](adapter-kit.md) shows how frontends use them.

## Webhooks

Frontends that cache learn about changes through a webhook, configured under `webhooks` in `app.yaml`
([configuration](configuration.md#webhooks)). Each delivery is a `POST` of

```json
{"id": "42", "topic": "item.published", "data": {"id": "…", "type": "article", "version": 2, "locales": ["en"]},
 "tags": ["i:…", "c:article"], "createdAt": "2026-10-19T04:51:58Z"}
```

with the headers `Hydrogen-Event` (the topic), `Hydrogen-Delivery` (the event id, the same on every retry: use it to
deduplicate) and `Hydrogen-Signature: t=<unix>,v1=<hex>`, where `v1` is the HMAC-SHA256 of `t + "." + body` with the
webhook's secret. Reject signatures older than a few minutes. A non-2xx answer is retried with backoff. Subscribe to
`*`: `item.*` alone misses `page.changed`, which carries the route and navigation tags. The JavaScript adapters
verify the signature and purge their caches for you (`@hydrogencms/client/server`).

## Errors

Errors share one envelope with the [management API](management-api.md#errors):

```json
{"error": {"code": "not_found", "message": "not available"}}
```

| Status | Code | When |
| --- | --- | --- |
| 400 | `bad_request` | Unknown or repeated parameter, bad `include`, unknown field in `fields`, unknown channel or locale, no channel chosen. |
| 401 | `unauthorized` | A token channel without a valid token, or an invalid or expired preview token. |
| 404 | `not_found` | Not available, or no such endpoint. |
| 500 | `internal` | A bug; it is logged. |

## Clients

- `@hydrogencms/client` is a typed client for any JavaScript runtime. See
  [its README](https://github.com/hydrogencms/hydrogen/blob/main/packages/client/README.md).
- `api/client` is a Go client for the same API.
- `hydrogen generate` writes TypeScript types for your content to `gen/types.ts`.
