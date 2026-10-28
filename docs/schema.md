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
| `search` | `true` or `false`: whether the type is searchable. Default: collections yes, singletons and upload types no. An object (`enabled`, `weights`, `exclude`) sets relevance weights and an exclusion field. See [search](search.md). |
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
| `unique` | No other item of the type (not in the trash) may have the same value, per locale for a `translatable` field. Checked on every draft write that changes the value, on publish and on restoring from the trash; a clash is a validation error with code `not_unique`. Implies `index`. Top-level `string`, `slug` and `email` fields only. |
| `default` | The value a new item starts with. It must be valid for the field. |
| `when` | Shows the field only when a sibling declared before it matches: `{ field: kind, eq: video }`. One of `eq`, `ne`, `in` (a list) or `empty` (`true` or `false`). |
| `admin` | `widget` (a name declared in the admin manifest), `placeholder`, `hidden`, `readonly`. |
| `ai` | `false` keeps the field away from models and retrieval; `{ describe: "…" }` explains it to them. |

Items of a `list`'s `of:` cannot use `required`, `translatable`, `index`, `unique`, `when` or `default`.

### Field types

| Type | Stores | Options |
| --- | --- | --- |
| `string` | One line of text. | `min`, `max` (characters), `pattern` (a regular expression). |
| `text` | Multi-line text. | `min`, `max`. |
| `richtext` | A ProseMirror document. | `marks` (any of `bold`, `code`, `italic`, `link`, `strike`, `subscript`, `superscript`, `underline`), `nodes` (`blockquote`, `bulletList`, `codeBlock`, `hardBreak`, `heading`, `horizontalRule`, `image`, `orderedList`, `paragraph`, `table`). `table` allows rows and cells (`tableRow`, `tableCell`, `tableHeader` with `colspan`, `rowspan`, `colwidth`); `image` is a block with `src` (an http(s) URL or a root-relative path such as `/assets/…`), `alt`, `title`, `width` and `height`. Without the options everything is allowed. See [rich text](#rich-text) for attributes and custom nodes. |
| `number` | A JSON number. | `integer`, `min`, `max`. |
| `boolean` | `true` or `false`. | none |
| `enum` | One of a fixed set of strings. | `values` (required, unique). |
| `date` | `"2026-10-14"`. | `min`, `max` (dates). |
| `datetime` | RFC 3339, `"2026-10-14T09:30:00Z"`. | `min`, `max`. |
| `slug` | Lower-case letters, digits and single dashes (non-ASCII letters allowed). | `max` (default 120), `from` (a sibling string or text field to derive it from), `allow` (`"."`, `","` or `".,"`: those characters may join the parts of a slug, alone or followed by a dash, as in `benelux-b.v` or `acme,-inc`; never first, last or doubled). |
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
`min`, `max`, `min_length`, `max_length`, `min_items`, `max_items`, `not_unique`, `unknown_field` and, for blocks, `block_type`.
At most 100 problems are reported. Hooks can add their own;
see [hooks](hooks.md).

### Slugs and unique values

A `slug` holds lower-case letters, digits and single dashes. Migrated URLs sometimes carry dots and commas; set
`allow: ".,"` on the field to keep them (`benelux-b.v`, `acme,-inc`). They join parts like a dash does, so a slug
cannot start or end with one or double it (`a..b` is refused), and `..` never becomes a path segment. A page or mounted
item whose slug allows them is served at the same path (`/acme,-inc`); the pages module accepts the same characters
in vanity paths. A route pattern fills `{slug}` with the value as it is when the field allows them, and slugifies
other fields as before.

`unique: true` keeps two items from claiming one slug:

```yaml
slug: { type: slug, from: title, allow: ".,", unique: true, translatable: true }
```

The check looks at the other items of the same type that are not in the trash, in the same locale when the field is
`translatable`, and takes a lock per field until the transaction ends, so concurrent writers cannot both pass (a
database lock on Postgres; SQLite has one writer). A write that does not change the value skips it, so duplicates that
already exist do not block other edits; they are reported again when the item is published. Trashing an item frees its
values.

### Indexed fields

`index: true` stores the value in a real, indexed column. Only indexed fields (plus `name`, `key`, `createdAt`,
`updatedAt` and `publishedAt` for sorting) can be used in `where[field][op]` filters and `sort`. Filters are `eq` (the default), `ne`,
`lt`, `lte`, `gt`, `gte`, `in`, `contains`, `exists`, `prefix` and `under`, combined with `where[or]` and `where[not]` groups. Nothing queries inside JSON documents.

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
In the admin, an item with a `blocks` field also opens in the **Page builder** (`?view=build` on the item's URL): its
blocks fields beside the live preview on the whole window, without the sidebar and the other fields.

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

## Rich text

A `richtext` field stores a ProseMirror document (`{type, attrs, content, text, marks}`). The built-in vocabulary is
the list under [field types](#field-types); `marks` and `nodes` limit it per field.

Built-in attributes:

| Node or mark | Attribute | Values |
| --- | --- | --- |
| `paragraph`, `heading` | `align` | `left`, `center`, `right`, `justify`. |
| `paragraph`, `heading` | `id` | An anchor id: a letter, then letters, digits, `-` or `_` (64 characters at most), unique in the document. |
| `orderedList` | `start` | An integer from -1000000 to 1000000. |
| `bulletList`, `orderedList` | `tight` | `true` renders list items without paragraph wrappers. |
| `link` | `target` | `_blank` (renderers add `rel="noopener noreferrer"`). |
| `link` | `href` | Also `#<id>` (an anchor in the same document) and `item:<uuid>#<id>` (an anchor on another item). |

A link to `item:<uuid>#<id>` counts as a reference to the item like `item:<uuid>`; the fragment is not part of the
reference, and delivery's `links` map is keyed by the bare id.

### Custom nodes and marks

The top-level `richtext:` key declares nodes and marks beyond the built-in ones. One declaration reaches the
validation of stored documents, the admin editor, the site module and the TypeScript renderers; nothing HTML or CSS
is stored, only typed attributes.

```yaml
richtext:
  nodes:
    callout:
      label: Callout          # shown in the admin
      tag: aside              # rendered element; default div (span for inline nodes)
      content: blocks         # blocks | inline | none | [part, part, ...]
      attrs:
        variant: { type: enum, values: [info, warning, tip], default: info }
    details:
      label: Accordion
      tag: details
      content: [detailsSummary, detailsBody]   # a fixed sequence of part nodes, each once, in order
    detailsSummary: { tag: summary, content: inline }
    detailsBody: { content: blocks }
    embed:
      label: Embed
      tag: figure
      content: none           # a leaf
      attrs:
        url: { type: url, required: true }
        caption: { type: string, max: 300 }
    footnoteRef:
      inline: true            # an inline atom inside paragraph and heading text; needs content: none
      content: none
      attrs:
        n: { type: int, min: 1, max: 999, required: true }
  marks:
    highlight:
      label: Highlight
      tag: mark               # default span
      attrs:
        tone: { type: enum, values: [yellow, green] }
```

Rules, checked when the schema loads:

- Names match `^[a-z][a-zA-Z0-9]{0,39}$` and may not be a built-in node or mark. At most 50 nodes and 20 marks.
- Tags: block nodes take `div section aside figure figcaption details summary header footer dl dt dd`; inline nodes
  and marks take `span mark small abbr cite q kbd dfn ins del var samp time`. Whether a combination is good HTML
  (a `summary` inside `details`) is the project's call.
- `content` is required. `blocks` holds one or more block nodes (built-in and custom) the field allows; `inline` holds
  text, `hardBreak` and inline custom nodes; `none` is a leaf; a list names **part** nodes that must appear exactly
  once each, in that order. A part is valid only inside its parent, is named by one parent, cannot be inline and
  cannot contain its own ancestor as a part. An `inline: true` node needs `content: none`.
- Attributes (20 per node or mark, names as above): `string` (`max` up to 10000, default 500), `enum` (`values`: 1 to
  100 names matching `^[a-z0-9][a-z0-9-]*$`), `int` (`min`, `max`, within one billion either way), `bool` and `url`
  (an absolute http(s) URL or a root-relative path). `required: true` makes an attribute mandatory; `default` must
  itself be valid, is written by the editor when it inserts the node and is used by renderers when the stored
  attribute is absent.
- A field's `nodes` and `marks` lists accept custom names (not part nodes: a part is allowed wherever its parent is).
  Without the lists every built-in and custom node and mark is allowed.

Frontends read the declaration from `GET /api/manage/v1/richtext`, and `hydrogen generate` writes it to
`gen/richtext.ts` (`richTextSchema`, only when `richtext:` is declared) for the TypeScript renderers.

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
  by themselves when the server starts in dev, and with `hydrogen content:migrate` elsewhere. A migration rewrites
  one type, so a change to a block or token set needs a migration (same `Changes`) for each type whose blocks can
  hold it, e.g. one for `landing` and one for `page` when both allow `hero`.
- After a change that alters what a published document looks like, `hydrogen content:reindex` rebuilds every
  projection, so delivery does not serve stale snapshots.

Schemas are also compiled into JSON Schema (draft 2020-12): `GET /api/manage/v1/types/{type}` returns a type and its
JSON Schema.
