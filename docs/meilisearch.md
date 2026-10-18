# Meilisearch

The Meilisearch module answers searches from a [Meilisearch](https://www.meilisearch.com) server instead of the
database's full-text search: typo tolerance, prefix matching and relevance ranking come from the engine. It is
optional. Without it searches use the database (see [search.md](search.md)). It needs Meilisearch 1.2 or later
(deleting by filter) and has no other dependency: it talks to the HTTP API with the standard library.

```go
h.Use(sqlite.Module(), meilisearch.Module())
```

```yaml
modules:
  meilisearch:
    url: http://127.0.0.1:7700
    key: ${env:MEILI_KEY}
    index: hydrogen
    timeout: 10s
    task_timeout: 10m
```

| Key | Default | Meaning |
| --- | --- | --- |
| `url` | required | Base URL of the server, `http://` or `https://`. |
| `key` | none | API key (the master key works). Must come from `${env:NAME}`. Required outside the dev environment. |
| `index` | `hydrogen` | Index uid. The module also uses `<index>_meta` (the settings fingerprint) and `<index>_next` (while rebuilding). |
| `timeout` | `10s` | Bounds every HTTP request to the server. |
| `task_timeout` | `10m` | Bounds the wait for one Meilisearch task (a document batch, a settings change, a swap). |

A key needs these actions: `search`, `documents.add`, `documents.get`, `documents.delete`, `indexes.create`,
`indexes.update`, `indexes.delete`, `indexes.swap`, `settings.update` and `tasks.get`. Its index scope must cover
`<index>`, `<index>_meta` and `<index>_next`, for example the pattern `hydrogen*`. The module does not read the
index list, so `indexes.get` is not needed. A key limited to some indexes cannot read the task of an index swap
(Meilisearch attaches it to no index), so after a rebuild the module confirms the swap by a marker document instead.

## Running one locally

```sh
docker compose -f scripts/docker-compose.yml -p hydrogen --profile search up -d meilisearch
```

This starts `getmeili/meilisearch` on `127.0.0.1:7701` with the master key from `MEILI_MASTER_KEY`
(default `dev-master-key-0123456789`); use it as `MEILI_KEY`. `make db-up` is unchanged: the service only starts with
the `search` profile. Point the config at it with `url: http://127.0.0.1:7701`.

## How it works

- **Index.** One index holds a document per item and locale, with primary key `<item>_<locale>`. Searchable
  attributes are the item name and its search text; the indexed fields of the schema are filterable, and the scalar
  ones sortable.
- **Keeping it current.** On start the module applies its settings. When the index is new, or the schema's indexed
  fields changed since the last complete rebuild, the kernel rebuilds it in the background. Publishing, unpublishing,
  trashing, restoring and purging update the documents of the item. `hydrogen search:reindex` forces a rebuild.
- **Rebuild without downtime.** A rebuild fills `<index>_next`, swaps it with the live index in one step and deletes
  the old one, so searches never see a partial index. The fingerprint is stored last: an interrupted rebuild is run
  again at the next start. Items published while a rebuild runs are caught up by the kernel; deletes are applied to both
  indexes.
- **Waiting.** Every write waits for its Meilisearch task and fails with the server's message when the task fails or
  does not finish within `task_timeout`.
- **Deleting.** Items are removed with one delete-by-filter (`item IN [...]`) per batch, which covers every locale.
  A delete during a rebuild also applies to `<index>_next`, so a purged item does not return with the swap.
  `Upsert` replaces all documents of the items it is given: it adds the new documents, then deletes the item's
  documents with other ids (a locale that is no longer published), so the item is never missing from the index.

## Behaviour that differs from the database engine

- **Relevance.** `score` is Meilisearch's ranking score (0 to 1). Requests with a `sort` get score 0 and the sort
  decides the order.
- **Totals.** When `offset` is a multiple of `limit` the total is exact (up to 10,000 hits, then it is marked
  `estimated`). Otherwise Meilisearch only estimates it and the result says so.
- **Highlights.** Matches are wrapped in `<mark>`; the rest of the text is HTML-escaped. `text` is cropped to about
  20 words around the match.
- **Facets.** Values are counted over all matches, most frequent first, ties by value (100 values at most).
- **Filters.** `eq`, `ne`, `lt`, `lte`, `gt`, `gte` (numbers and times), `in`, `contains` and `exists` are
  supported. `prefix` is not (Meilisearch has it only as an experimental feature) and is rejected as an invalid
  request. Times are indexed as Unix seconds. String values are quoted and escaped, so they cannot change the
  meaning of a filter.
- **Words.** Meilisearch matches whole words and a prefix of the last word, tolerates typos, and ignores accents
  and case; the database engine does less of this, so result sets differ.
- **Case and length.** Meilisearch compares string filters and facet values case-insensitively and trimmed, where the
  database engine matches exactly: `kind = "News"` also finds `news`, and facet values that differ only by case are
  merged into one. Strings longer than about 468 bytes are truncated for filtering, so filter on short values.
