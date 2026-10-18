# Search

Hydrogen searches published content through one interface, `core/search`, with two kinds of engine:

- **The database engine** (the default) uses the database adapter's own full-text search: SQLite FTS5 or Postgres
  `tsvector`. It needs no setup and is always in step with the content, because it reads the same transaction-safe
  projections the delivery API reads.
- **External engines** are modules that register themselves with `Registry.SearchEngine`. Hydrogen ships a
  Meilisearch module; see [meilisearch.md](meilisearch.md).

Everything else on this page (searchable types, filters, facets, sorting, highlights, the endpoints) works the same
with either engine.

## Searchable types

A type takes part in search when its `search` option is true. The default depends on the kind of type:

| Type | Default |
| --- | --- |
| collection | `search: true` |
| singleton | `search: false` |
| upload type (`upload:`) | `search: false` |

```yaml
types:
  article:
    fields:
      title: { type: string, translatable: true }
      topic: { type: string, index: true }
  settings:
    singleton: true            # not searchable by default
    fields: { site: { type: string } }
  press:
    search: false              # a collection that stays out of search
    fields: { title: { type: string } }
  legal:
    singleton: true
    search: true               # a singleton that is searchable
    fields: { body: { type: richtext } }
```

Only searchable types are indexed by external engines and accepted by the search endpoints.

## What is searched

For every published locale of an item, the engine searches the item's **name** and its **search text**: string, text
and slug fields, rich text, blocks, and list and group members of those. Enum, url, email, date, number and boolean
fields, and any field marked `ai: false`, are left out. Matching is by word prefix and ignores case, so `sol pan`
finds "Solar panels". Results are ranked by relevance unless the request sorts.

Only **published** content is found. Drafts, unpublished and trashed items never are; unpublishing, trashing or
purging an item removes it from the results at once (the database engine) or when the outbox delivers the event
(external engines, usually within a second).

### Locales

A search is always for one locale. An item is a hit when it has a document in that locale, which includes locales
that inherit from another through [overlays](locales.md). An item that is **always available** but has no document
in the locale is available in its main locale; the hit then reports the main locale as its `locale`.

Every engine applies this to text searches too. A delivery search asks for one locale: the request's `locale`, else the
channel's first. With overlays, a derived locale (such as `de-CH` inheriting from `de`) has its own documents, so
searching it finds text inherited from the parent.

## Filters, facets, sorting

Filters and sorts use **indexed** fields (`index: true`), the same fields and operators as collection lists:
`eq`, `ne`, `lt`, `lte`, `gt`, `gte`, `in`, `contains` (list fields), `exists` and `prefix` (text).

```
GET /api/deliver/v1/search?q=solar&where[topic][eq]=energy&where[rank][gte]=3&sort=-rank
```

- **Sort**: an indexed scalar field or `publishedAt`, `-` for descending. Without `sort` the order is relevance.
  With a sort, hits have `score` 0.
- **Facets**: `facets=topic,tags` counts the hits per value of indexed text fields (at most five). A list field
  counts an item once per value. Facets count the filtered hits and respect `where`.
  At most 50 values are returned per facet, most hits first.
- **Highlights**: `highlight=true` (the default on the delivery API) adds, per hit, an HTML-escaped snippet of the
  name and of the matching text with each match in `<mark>…</mark>`. Everything else is escaped, so the snippets are
  safe to insert as HTML.

Facets and `where` filters on translatable fields use the documents of the searched locale only; they do not use
the main-locale fallback documents of always-available items, so facet counts can be lower than `total`.

An indexed field name shared by several searchable types should have the same kind (text, number, date, boolean,
reference) in each; filtering or sorting by it applies to the types that have it.

## Endpoints

### Delivery: `GET /api/deliver/v1/search`

Channel selection, tokens and preview behave as for collection lists.

| Parameter | Meaning |
| --- | --- |
| `q` | required, 1 to 200 characters |
| `type` | comma list of types; they must be in the channel's collections and searchable; default all of them |
| `locale` | the channel's locale selection applies, as for lists |
| `where[field][op]` | filters on indexed fields |
| `facets` | comma list, at most five |
| `sort` | an indexed scalar field or `publishedAt`; `-` prefix for descending |
| `limit`, `offset` | 1 to 50 (default 10), offset at most 1000 |
| `include` | expand references, as for lists |
| `highlight` | default true |

```json
{ "items": [ { "id": "01…", "type": "article", "name": "Solar panels", "fields": { "…": "…" } } ],
  "hits":  [ { "id": "01…", "type": "article", "locale": "en", "score": 0.83,
               "highlights": { "name": "<mark>Solar</mark> panels", "text": "…cheap <mark>solar</mark> power…" } } ],
  "total": 12, "estimated": false,
  "facets": { "tags": [ { "value": "energy", "count": 4 } ] } }
```

`items[i]` is the item view the collection list would return, and `hits[i]` describes the same item. A hit whose
item the channel cannot show is dropped from both (`total` stays the engine's count). Responses are cached like
lists and purged by the cache tags of the searched types. A preview token searches the published index only.
`estimated` is true when the engine only estimates `total` (Meilisearch with an offset that is not a multiple of
the limit).

### Management: `GET /api/manage/v1/search`

For editors and the admin's command palette. It always uses the database, so drafts are found and the caller's
access scopes apply.

| Parameter | Meaning |
| --- | --- |
| `q` | the text |
| `type` | comma list; default every type the caller can read |
| `locale` | the locale to search |
| `limit` | 1 to 50, default 20 |

The answer is `{ "items": [ …item summaries as the list endpoint… ], "snippets": { "<id>": "<html>" } }`, merged
across types by relevance (ties by `updatedAt`, newest first).

## External engines and reindexing

When the registered engine has an index of its own (`search.Index`), the kernel keeps it current:

- **At start** the engine is told the settings (searchable types, indexed fields, a fingerprint of both and of the
  document format). If the index is new, or the fingerprint changed because the schema or an upgrade changed what is
  indexed, the kernel rebuilds the index in the background while the old one keeps answering. Only one node rebuilds
  at a time. If the engine is down at start, the server still starts and retries every 30 seconds.
- **After every change** the `search.index` consumer (it follows publishes, unpublishes, trash, restore, purge and
  changes to an item's key or availability; draft edits are ignored) re-reads the item's published content, including
  locales that inherit through overlays, and replaces its documents, or removes them when the item is unpublished, trashed, purged or not searchable. Overlay locales rebuilt
  by a parent's publish are covered, because the whole item is re-read. Events are delivered at least once and the
  consumer is idempotent; a delivery that keeps failing is retried with back-off (up to five minutes) for about a day before it
  is given up on, so a restart of the engine costs nothing; run `search:reindex` after a longer outage. One node
  handles an item at a time; a busy item is retried. After each index write the consumer purges the delivery cache of
  the item's collection, so a cached search never outlives the index update.

```sh
hydrogen search:reindex
```

`search:reindex` rebuilds the whole index from the published content now, whether or not the settings changed.
With the database engine it does nothing: there is no separate index.

Purges and edits made while a rebuild runs are not lost (engines also remove purged items from the index being
built): when the rebuild finishes, the kernel re-indexes every item of a
searchable type that changed since a minute before it started.

The consumer only upserts: `Index.Upsert` replaces all documents of the items it is given (every locale), so a locale
that disappeared from an item does not linger.

### Self-healing database search

If items are published but the database's published search rows are missing altogether (for example after a migration
that dropped them), the server notices at start and runs the same rebuild as `content:reindex` in the background,
logging a warning and then an info line when it is done. It also rebuilds the working projection, so searches (and
the admin's palette) find nothing until healing finishes. One node does it; the others log that it is under way.

## Writing an engine

An engine implements `search.Engine` (`Name`, `Search`) and, for its own index, `search.Index` (`Sync`, `Upsert`,
`Delete`, `Rebuild`), and registers itself in `Register`:

```go
func (m *Module) Register(r *kernel.Registry) error { return r.SearchEngine(m.engine) }
```

At most one module may register an engine. `search.BuildDocs` and `search.NewSettings` are what the kernel uses to
produce the documents and settings an `Index` receives; `search.Marked` and `search.MarkTerms` turn raw snippets
(matches wrapped in `\x02` and `\x03`) into the escaped `<mark>` form every engine returns. Problems with a request's
values should be reported with an error that wraps `search.ErrInvalid`, which the APIs answer with 400.

An engine's `Sync` must keep answering "rebuild" until a `Rebuild` with the same settings has completed, so it stores
the fingerprint last. Search text is capped like the database's own search rows, and a request may contain at most 16
words (`search.ErrInvalid` otherwise).
