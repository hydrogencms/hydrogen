# Concepts

The ideas the rest of the documentation builds on. Each section ends with where to read more.

## Headless, with a built-in website if you want one

Hydrogen stores and edits content and delivers it over HTTP. The core never renders your public site and knows
nothing about URLs: that is the job of a frontend, which asks the delivery API for content. If you would rather not
run a second program, the `site` module renders a website with Go templates from the same binary, from the same API.
Content does not change when you switch between the two.

```
editors ── admin ──┐                      ┌── frontend (Next, SvelteKit, Nuxt, Astro, …)
scripts, agents ───┴─ management API ─┐   ├── site module (Go templates)
                                      ├── content ──► delivery API ──┤
 your Go code ── hooks, Local API ────┘   └── apps, anything over HTTP
```

## Content types

The model is declared in YAML files in `schema/`. A **type** is a list of typed fields. There are four kinds:

- A **collection** has many items: articles, products, people. It is the default.
- A **singleton** has exactly one: site settings, a footer.
- An **upload type** is a collection of files (images, PDFs) with fields of its own (alt text, credits).
- A **page type** (`page: true`, with the pages module) is a collection whose items can sit in the page tree.

Field types cover text, rich text, numbers, dates, enums, references to other items, lists, groups, files and blocks.
Fields marked `translatable` are per language; `index` fields can be filtered and sorted. Schema files and the
generated typed Go and TypeScript are in [schema](schema.md).

## Items, drafts and versions

An **item** is one piece of content of a type. It has an id (UUIDv7), an optional unique **key** (`hello`, readable in
URLs and code), a display name computed from its fields, and one document per locale.

Writes never touch what is live. An item has one **draft** and any number of published **versions**:

```
create ─► draft ─► publish ─► version 1 ─► edit ─► draft ─► publish ─► version 2
                                  ▲ live                                 ▲ live
```

Its **status** is `draft` (never published), `published`, `changed` (published, with unpublished edits) or
`unpublished`. Publishing validates the whole document, runs the hooks, writes an immutable version and queues the
events that purge caches. Old versions can be read and restored into the draft. Deleting moves an item to the **trash**
for a retention period (`content.trash_retention`), where it can be restored; only then is it purged. References are
tracked: an item that others point at cannot be deleted until they let go (or the reference says `nullify`).

The management API and the admin edit drafts; the delivery API shows published versions. Drafts are previewed with
tokens (below). Editors can also group publishing into [releases](releases.md), require [reviews](reviews.md), edit
together live ([collaboration](collaboration.md)) and share previews ([share links](share-links.md)).

## Locales

An item is stored in one or more locales. Fields without `translatable` are shared by all of them; translatable ones
hold one value per locale. The item has a **main locale** and may be `alwaysAvailable`, which means it is served in its
main locale where it has no translation. A draft is split into **parts**: the shared fields, and one part per locale, so
translators publish their language independently.

**Market overlays** let a locale such as `de-CH` inherit from `de` and store only what differs
([locales](locales.md)).

## Channels

A **channel** is one consumer of content: a website in one language, a mobile app, an integration. In
`config/channels.yaml` a channel lists its ordered **languages** (the first an item is available in wins), the
**collections** it may read, free-form **settings**, and who may read it: public, or with a secret token. Several
channels can share a group's settings. Every delivery request is for one channel, and an item that is not available in
it simply is not found. With no `channels.yaml` there is one channel, `default`. See
[configuration](configuration.md#channels).

## Delivery and management

Two HTTP APIs, with different jobs:

- The **delivery API** (`/api/deliver/v1`) is read-only and public by default. It serves **published snapshots**, so
  reads never query a document in place, are cacheable and are tagged for purging. See
  [the delivery API](delivery-api.md).
- The **management API** (`/api/manage/v1`) writes drafts, publishes and administers everything. It needs
  credentials. The admin is its client. See [the management API](management-api.md).

## Pages

With the `pages` module, a **site** has a tree of pages that decides URLs. Pages are items of page types; a page's path
comes from its slug and its parents'. A **mount** exposes a whole collection at a path (`/news/{slug}`) without placing
each item. Changing a slug keeps the old path as a redirect, and editors can add manual redirects. A web channel says
which site and which host and path prefix it serves (`match`), and the delivery API's `/route` answers "what is at
this URL?" with a page, a redirect or a 404. Without the module you route in the frontend; collections and `links`
patterns still give items URLs.

Redirects are items of the built-in `redirect` type. An ordinary redirect never takes a path a page or mounted item
holds; tick **force** and it wins instead (the page stays in the tree but is unreachable at that path), which is also how
the site root `/` is redirected. A mount can name a field (`redirectField`) whose value, an absolute URL or a site path,
makes that item answer a redirect, and links to it then point there.

## Blocks

A **block** is a typed piece of a page: a hero, a card grid, a quote. Its props are fields, and its **slots** hold child
blocks. A `blocks` field stores a tree of them as plain JSON. Style comes from **design tokens** (named sets such as
`tone: [light, dark]`), never from raw CSS or HTML in content. Frontends render blocks through a registry from block
type to component; the admin edits them in a live preview of the real frontend. See [schema](schema.md#blocks-and-tokens)
and the [adapter kit](adapter-kit.md).

## Assets

Files are stored once, by the SHA-256 of their bytes, and managed as items of an upload type. Content points at them
with `asset` fields, which the delivery API expands into URLs and metadata; the `images` module adds resized
variants. Storage is a local directory or any S3-compatible bucket. See [media](media.md) and [images](images.md).

## Preview

An editor's preview shows the frontend with drafts. The admin mints a short-lived **preview token** and the frontend
sends it back to the delivery API, which then answers from the draft, uncached. The preview frame and the frontend
talk through the [bridge protocol](bridge-protocol.md), which makes outlines, selection and inline text edits possible
on the real page.

## Modules

The core is small on purpose. Everything a project may live without is a **module**, compiled in: the database adapter,
pages, the website renderer, images, GraphQL, search engines, WebAssembly hooks, your own. A module declares what it
adds when the server starts: field types, schema options, routes, jobs, hooks, admin extensions. A project lists its
modules in `hydrogen.yaml`; the catalog is in [modules](modules.md).

## Events and the outbox

Every mutation runs in one transaction that also records an audit entry and an **outbox** event. After the commit,
consumers receive the event at least once: the delivery cache is purged, the search index updated, webhooks sent, async
hooks run. The same mechanism serves your code ([hooks](hooks.md)). Because events are stored with the change, nothing
is lost when a server dies between the two.

## Access

Who may do what is role-based: **policies** (`content.edit`) grouped into **roles**, narrowed by **limitations**
(only this collection, only English, only own items) and assigned to users, groups and API tokens. Code can add rules
on top. AI agents use the same tokens, roles, validation and review as people. See [access](access.md).

## Storage

Each item version and locale is one validated JSON document in a real table; everything that is queried, joined or must
stay consistent (indexes, references, search rows, routes, users) lives in ordinary tables. There is no
entity-attribute-value layer and no queries inside JSON. SQLite and Postgres are adapters over the same storage
interfaces, tested by one conformance suite, and [export and import](deployment.md#backups-and-export) move a site
between them.
