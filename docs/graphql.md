# GraphQL

The graphql module serves the delivery API as GraphQL. It is read-only: a second shape of what
`/api/deliver/v1` already returns, with no mutations and no subscriptions. The schema is generated from your content
schema when the server starts, so there is no code generation step. It is optional and its own Go module:

```sh
hydrogen add graphql
```

```go
h.Use(sqlite.Module(), graphql.Module())
```

Resolvers call the delivery API in process, so everything that holds for REST holds here: only published content,
channel and locale rules, token channels, preview tokens, asset expansion (and image variants when the images
module is on), links, shared blocks and cache tags.

## Endpoint

```
GET  /api/deliver/v1/graphql?query=…&variables=…&operationName=…
POST /api/deliver/v1/graphql            {"query": "…", "variables": {…}, "operationName": "…"}
GET  /api/deliver/v1/graphql/schema.graphql
```

The channel is selected like on every delivery route: a channel token in `Authorization: Bearer …`, else
`?channel=<name>` (public channels), else the preview token's channel, else the only channel. Preview tokens go in
`X-Hydrogen-Preview` or `?preview=`. A token channel without its token answers 401.

A POST body is at most 64 KB and its content type `application/json` (or `application/graphql` with the query as
the body). Browsers on a configured origin (the request's own origin and the channels' frontends) may POST
cross-origin; the preflight answers for them.

```sh
curl -s localhost:8080/api/deliver/v1/graphql?channel=web \
  -H 'Content-Type: application/json' \
  -d '{"query": "{ article(key: \"hello\") { title author { fullName } } }"}'
```

## Schema

A channel's schema holds only the content types of its collections. Each type becomes an object type named in
PascalCase (`blog_post` becomes `BlogPost`; a clash gets a number) implementing the interface `Item`
(`id`, `type`, `name`, `key`, `locale`, `url`, `publishedAt`). The type's fields are flattened onto it.

| Schema field type | GraphQL |
| --- | --- |
| string, text, slug, email, url | `String` |
| enum | an enum `<Type>_<field>` when every value is a valid name, else `String` |
| number | `Int` when `integer: true` and the field declares `min` and `max` within 32 bits; `Long` for other integers; `Float` otherwise |
| boolean | `Boolean` |
| date, datetime | `String` (ISO 8601) |
| json, rich text, blocks | `JSON` |
| group | an object type `<Type>_<field>` |
| list | `[T]` |
| ref | the referenced type, a union of them, or `Item` for a ref to any type |
| asset | `Asset { id url name mime size width height focal alt fields variants }` (`size`, `width` and `height` are `Long`) |
| file | `File { id name mime size width height }` |

A field a type shares a name with (`id`, `type`, `key`, …) is renamed. Dump the exact schema of a channel with the
SDL endpoint below.

Root fields, per collection `article` (the type's name in camelCase):

```graphql
type Query {
  article(id: ID, key: String, locale: String): Article
  articleList(where: JSON, sort: String, limit: Int, offset: Int, locale: String): ArticleList
  settings(locale: String): Settings            # a singleton
  item(id: ID!, locale: String): Item
  search(q: String!, types: [String!], where: JSON, facets: [String!], sort: String, limit: Int, offset: Int, locale: String): SearchResult
}
```

`route(url: String!)` (the pages module is on) and `search` (a search engine is configured) appear only when their
delivery routes exist. A list answers `{ items, hasMore, total }`; `total` is counted only when you select it.
An item that is not published or not visible on the channel is `null`, like a 404 in REST.

`where` is the REST `where[field][op]` filter as a JSON object over indexed fields, and `sort` is the REST sort
(`-rank,title`):

```graphql
{
  articleList(where: { rank: { gte: 2 }, kind: { eq: "news" } }, sort: "-rank", limit: 10) {
    total
    items {
      title
      rank
      author { fullName }
      hero { url alt width height variants }
      related { ... on Article { title } }
    }
  }
}
```

Only the fields a query selects are fetched: the module asks the delivery API for those fields (`?fields=`), so large
fields such as rich text and blocks that are not selected are not encoded, cached or decoded. A query it cannot
analyse (a fragment on a union, say) and the `search` field fetch whole items.

Variables work as usual:

```graphql
query ($key: String) { article(key: $key) { title } }
```

## Limits

`modules.graphql` in `config/app.yaml`:

| Key | Default | Meaning |
| --- | --- | --- |
| `max_depth` | 10 | Deepest field nesting, fragments expanded. |
| `max_fields` | 500 | Fields selected, fragments expanded. |
| `max_query_bytes` | 16384 | Longest query text (at most 65536, the POST body limit). |
| `max_calls` | 50 | Internal delivery requests one GraphQL request may make. |
| `max_nodes` | 20000 | Objects (items, groups, list elements) one response may hold; over it, `QUERY_TOO_EXPENSIVE`. Cyclic references multiply fast, so this bounds the response size. |
| `max_items` | 5000 | Items one request may ask the delivery API for: each call costs its page size (default 50) times include depth plus one. |
| `introspection` | true | `false` hides `__schema`, `__type` and the SDL endpoint (404). |

Introspection queries count too: at most 13 levels deep, `max_fields` fields, and no field except `ofType` may nest
inside itself (the standard introspection queries pass). `Long` is a 64-bit integer sent as a JSON number;
JavaScript clients read it exactly only up to 2^53.

A request over a limit, or one that does not parse or validate, answers 400 with a GraphQL `errors` list. A resolver
error carries the delivery status in `errors[].extensions.status`.

## Caching

GET responses are cached like other delivery responses: `Cache-Control`, `ETag` (so `If-None-Match` answers 304),
and `Surrogate-Key` / `Cache-Tag` holding the union of the tags of every item the query touched, so a CDN purge or
the in-process cache drops the answer when any of them is republished. Use GET with a stable
query string for what a CDN should cache. POST responses are always `Cache-Control: no-store`, as are preview
responses and responses with errors. An answer in which something looked up was missing (an unpublished item, an
unknown key, an empty singleton) is also `no-store`: it carries no tag that would purge it once the item exists.

## Preview and tokens

Send the same headers as for REST: `Authorization: Bearer <channel token>` for a token channel and
`X-Hydrogen-Preview: <token>` (or `?preview=`) to see drafts. With a preview token the response is uncached and
drafts appear exactly where REST would show them.

## SDL for tooling

`GET /api/deliver/v1/graphql/schema.graphql?channel=<name>` returns the channel's schema as SDL (plain text). Feed
it to GraphQL Code Generator, an editor plugin or a mock server. Introspection answers the same schema. With
`introspection: false` the endpoint answers 404 as well.

## Not supported

Mutations (use the management API), subscriptions (use preview events), persisted queries, query batching and
`@defer`/`@stream`.
