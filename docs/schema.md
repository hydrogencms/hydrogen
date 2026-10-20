# Schema reference

A project's content model lives in YAML files in `schema/` (`content.schema_dir`, default `schema`). Every `.yaml` or
`.yml` file in the directory, at any depth, is read and merged; the file split is yours. A file has four optional
top-level keys: `types`, `groups`, `blocks` and `tokens`. Unknown keys fail with the file and line, and all problems
are reported together by `hydrogen config:check`.

```yaml
types:
  article:
    label: Article
    name: "{title}"
    admin: { list: [title, publishedOn], group: Editorial, icon: newspaper }
    fields:
      title:       { type: string, required: true, max: 120, translatable: true }
      slug:        { type: slug, from: title, index: true }
      body:        { type: richtext, translatable: true }
      author:      { type: ref, to: [person], on_delete: block, index: true }
      publishedOn: { type: date, index: true }
```

`hydrogen generate` turns the compiled schema into typed Go (`gen/content`) and TypeScript (`gen/types.ts`);
`hydrogen dev` does it on every change. Never edit generated files.

## Names

Type, field, group, block, slot and token-set names match `^[a-z][a-zA-Z0-9]*$` (camel case) and are at most 64
characters. These top-level field names are reserved because every item has them: `id`, `type`, `key`, `locale`,
`status`, `version`, `name`, `createdAt`, `updatedAt`, `publishedAt`.

## Types

| Key | Meaning |
| --- | --- |
| `label`, `description` | Shown in the admin. The label defaults to the name split into words. |
| `fields` | The type's fields, a mapping of name to field spec, in the order the admin shows them. |
| `singleton` | `true`: exactly one item (site settings). It is read by type, not listed. |
| `page` | `true`: items can be placed in the page tree. Needs the pages module. |
| `children` | Pages module: the page types allowed below this one. Page types only. |
| `name` | A pattern for the item's display name, such as `"{firstName} {lastName}"`. It may use scalar fields. Default: the first string field. |
| `upload` | Makes the type a collection of uploaded files. See [Upload types](#upload-types). |
| `blockset` | `true`: the type's single `blocks` field can be reused with the built-in `shared` block. |
| `review` | `true`: publishing needs an approved review. See [reviews](reviews.md). |
| `search` | `true` or `false`: whether the type is searchable. Default: collections yes, singletons and upload types no. See [search](search.md). |
| `admin` | `list`: field names shown as columns (`field:cell` picks a cell renderer from an [admin extension](admin-extensions.md)); `group`: the sidebar group; `icon`. |
| `ai` | `instructions`: guidance for generated text; `generate`: fields a model may draft. |

A type without `singleton` is a **collection**. Modules add type keys of their own (`children` and `search` above);
an unknown key fails with the list of known ones.

## Fields

Every field is `name: { type: <type>, … }`. These keys work on every field type:

| Key | Meaning |
| --- | --- |
| `type` | The field type (below). Required. |
| `label`, `description` | Admin label and help text. |
| `required` | Needed to publish. Drafts may leave it out. |
| `translatable` | The value is per locale. Top-level fields only; others are shared by all locales. |
| `index` | Makes the field filterable and sortable in the APIs. Top-level scalar, `ref`, `file` and list-of-scalar fields only. |
| `default` | The value a new item starts with. It must be valid for the field. |
| `when` | Shows the field only when a sibling declared before it matches: `{ field: kind, eq: video }`. One of `eq`, `ne`, `in` (a list) or `empty` (`true` or `false`). |
| `admin` | `widget` (a name declared in the admin manifest), `placeholder`, `hidden`, `readonly`. |
| `ai` | `false` keeps the field away from models and retrieval; `{ describe: "…" }` explains it to them. |

Items of a `list`'s `of:` cannot use `required`, `translatable`, `index`, `when` or `default`.

### Field types

| Type | Stores | Options |
| --- | --- | --- |
| `string` | One line of text. | `min`, `max` (characters), `pattern` (a regular expression). |
| `text` | Multi-line text. | `min`, `max`. |
| `richtext` | A ProseMirror document. | `marks` (any of `bold`, `code`, `italic`, `link`), `nodes` (`blockquote`, `bulletList`, `codeBlock`, `hardBreak`, `heading`, `horizontalRule`, `orderedList`, `paragraph`). Without them everything is allowed. |
| `number` | A JSON number. | `integer`, `min`, `max`. |
| `boolean` | `true` or `false`. | none |
| `enum` | One of a fixed set of strings. | `values` (required, unique). |
| `date` | `"2026-10-14"`. | `min`, `max` (dates). |
| `datetime` | RFC 3339, `"2026-10-14T09:30:00Z"`. | `min`, `max`. |
| `slug` | Lower-case letters, digits and single dashes (non-ASCII letters allowed). | `max` (default 120), `from` (a sibling string or text field to derive it from). |
| `email` | An email address. | none |
| `url` | An absolute `http(s)` URL, or `item:<id>` to link another item. | `relative: true` also accepts a root-relative path (`/docs/start?a=1#top`: one leading slash, no spaces or backslashes), for a site's own links. |
| `ref` | The id of another item. | `to` (required: the types allowed), `on_delete`: `block` (default), `nullify` or `warn`. |
| `list` | An ordered list. | `of` (required: a field spec for the items), `min`, `max`. |
| `group` | A nested object. | `fields` (inline) or `use` (the name of a reusable group); not both. |
| `json` | Any JSON value, not validated. | none |
| `file` | A stored file: `{id, name, mime, size, width?, height?, focal?}`. | none. See [media](media.md). |
| `asset` | A reference to an item of an upload type. | `to` (required: upload types), `on_delete`. |
| `blocks` | A tree of page-builder blocks. | `allow` (required: block type names), `min`, `max`. |
| `token` | One value of a design-token set. | `set` (required: a token set name). |

Modules can register further field types. An option a type does not take fails with the list it does.

`ref` fields are checked on write and tracked: deleting an item that others point at is refused (`block`), clears the
references (`nullify`) or just warns (`warn`). Delivery can resolve references with `include`
(see [the delivery API](delivery-api.md)).

```yaml
related:  { type: list, of: { type: ref, to: [article], on_delete: nullify }, max: 5 }
tags:     { type: list, of: { type: string, max: 30 }, max: 10, index: true }
address:  { type: group, fields: { street: { type: string }, city: { type: string } } }
video:    { type: url, when: { field: kind, eq: video } }
```

### Validation

Every document is validated against the schema when a draft is written and again, with required fields and list
minimums, when it is published. A failing write answers `422` with one entry per problem:

```json
{"error": {"code": "invalid", "message": "validation failed", "locale": "en",
  "fields": [{"path": "publishedOn", "code": "format", "message": "must be a date (YYYY-MM-DD)"}]}}
```

Paths address the value (`tags[2]`, `address.city`). Codes include `required`, `type`, `format`, `pattern`, `enum`,
`min`, `max`, `min_length`, `max_length`, `min_items`, `max_items`, `unknown_field` and, for blocks, `block_type`.
At most 100 problems are reported. Hooks can add their own;
see [hooks](hooks.md).

### Indexed fields

`index: true` stores the value in a real, indexed column. Only indexed fields (plus `name`, `key`, `createdAt` and
`updatedAt` for sorting) can be used in `where[field][op]` filters and `sort`. Filters are `eq` (the default), `ne`,
`lt`, `lte`, `gt`, `gte`, `in`, `contains`, `exists`, `prefix` and `under`. Nothing queries inside JSON documents.

## Groups

A group is a named, reusable set of fields. Declare it once and pull it in with `use`:

```yaml
groups:
  seo:
    description: Search engine metadata.
    fields:
      metaTitle: { type: string, max: 60 }
      metaText:  { type: text, max: 160 }

types:
  article:
    fields:
      seo: { type: group, use: seo }
```

A group that uses itself, directly or not, is an error.

## Blocks and tokens

The page builder composes pages from typed **blocks**. A block type has `props` (fields, with the same specs as
above, but never `translatable` or `index`) and optional `slots` that hold child blocks. A `blocks` field on a type
lists the block types an editor may add.

```yaml
tokens:
  tone:  [light, dark, brand]
  space: [none, sm, md, lg]

blocks:
  hero:
    label: Hero
    icon: layout
    props:
      heading: { type: string, required: true, max: 120 }
      tone:    { type: token, set: tone, default: light }
  cards:
    label: Cards
    props:
      columns: { type: enum, values: ["2", "3", "4"], default: "3" }
    slots:
      items: { label: Cards, allow: [card], min: 1, max: 12 }
  card:
    props:
      title: { type: string, required: true }
      link:  { type: ref, to: [page], on_delete: nullify }

types:
  page:
    page: true
    fields:
      title:  { type: string, required: true, translatable: true }
      blocks: { type: blocks, allow: [hero, cards], max: 20, translatable: true }
```

- A **token set** is a list of names (letters, digits, `_`, `-`). Style comes from tokens, never from raw CSS in
  content. A `token` field holds one value of its set.
- A block value is `{ "id": "…", "type": "hero", "props": {…}, "slots": {"items": [ … ]} }`. The service assigns a
  UUID to blocks that have no `id`.
- A slot has `allow` (block types), `min` and `max` (`max` at least 1), `label` and `description`.
- A `blocks` field must be a top-level field of a type: not in a list, a group or block props. It is translatable as a
  whole. A merge patch replaces it whole.
- The built-in **`shared`** block embeds the blocks of another item. Mark a type `blockset: true` (it needs exactly one
  `blocks` field) and add `shared` to a `blocks` field's `allow`. The delivery API inlines the shared item's blocks.
- `GET /api/manage/v1/blocks` lists block types and token sets for editors and tools.

## Upload types

`upload:` turns a type into a collection of files. Each item is one file plus the fields you declare.

```yaml
types:
  image:
    upload: { accept: [image/*, application/pdf], max_size: 20MB }
    admin: { group: Media, icon: image }
    fields:
      alt:     { type: string, translatable: true, max: 300 }
      caption: { type: text, translatable: true }
```

- `accept` lists MIME types or patterns (`image/*`); empty accepts anything. `max_size` is a number of bytes or a
  string such as `20MB`; it can only lower `media.max_upload`.
- The type gets three built-in fields, which you must not declare: `file` (required), `folder` (default `/`) and
  `tags`.
- An upload type cannot also be a `singleton`, `page` or `blockset`.
- Other types point at the assets with `asset` fields: `hero: { type: asset, to: [image] }`.

Upload, storage, hashing and delivery of files are in [media](media.md); resized renditions in [images](images.md).

## Changing a schema

Delivery serves published snapshots, so a schema change that alters the stored shape needs care.

- **Additive changes** (a new optional field, a new type, a wider `allow`) need nothing.
- **Breaking changes** are a removed type or field, a changed field type, a changed `translatable`, a narrowed
  `blocks` `allow` and a token field moved to another set. `hydrogen serve` and `hydrogen config:check -db` refuse
  them until a **content migration** accounts for them (see [hooks](hooks.md#content-migrations)). Migrations apply
  by themselves when the server starts in dev, and with `hydrogen content:migrate` elsewhere.
- After a change that alters what a published document looks like, `hydrogen content:reindex` rebuilds every
  projection, so delivery does not serve stale snapshots.

Schemas are also compiled into JSON Schema (draft 2020-12): `GET /api/manage/v1/types/{type}` returns a type and its
JSON Schema.
