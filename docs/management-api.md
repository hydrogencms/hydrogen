# The management API

The management API is how content is written and how the project is administered. The admin app is a client of it,
and so are scripts, importers, CI jobs and AI agents. It is served under `/api/manage/v1`. Everything the admin can do
goes through it, with the same permissions, validation and audit trail.

The full contract, generated from your schema (typed `fields`, create and update bodies per type), is the OpenAPI
document at `GET /api/manage/v1/openapi.json`. Like every route here it needs credentials. The pages below are the
overview.

## Authentication

Every route except `POST /auth/login` needs one of:

| Credential | Use |
| --- | --- |
| `Authorization: Bearer hyt_…` | An **API token** made by `hydrogen tokens:create` or `POST /tokens`. For scripts and agents. Its rights are those of its roles. |
| The session cookie `hydrogen_session` and `X-CSRF-Token` | A **user** who signed in with `POST /auth/login {"login", "password"}`. The login answer carries the CSRF token; send it on every write (`POST`, `PUT`, `PATCH`, `DELETE`). The cookie is `HttpOnly`, `Secure` (unless `auth.insecure_cookies` in dev) and `SameSite=Lax`, scoped to `/api`. Sessions end after `auth.session_idle` or `auth.session_max`. |
| `Authorization: Bearer <manage.token>` | The **config token** from `app.yaml`: every right, audited as `token:config`. For bootstrapping. In `dev` it defaults to `dev-token` and works only from loopback addresses. |

```sh
TOKEN=$(hydrogen tokens:create -name ci -role editor)        # printed once
curl -s localhost:8080/api/manage/v1/auth/session -H "Authorization: Bearer $TOKEN"
```

`GET /auth/session` says who is calling and which policies they hold. Failed logins are throttled per client, per
login and per pair (`429`, with `Retry-After`). Users, roles and tokens are covered in [access](access.md).

## Conventions

- Bodies and answers are JSON (`application/json`), bodies at most 4 MiB. Answers are `Cache-Control: no-store`.
- Lists answer `{"items": […], "total": n}` and take `limit` (default 50, at most 500) and `offset`.
- Ids are UUIDv7 strings. Timestamps are RFC 3339, in UTC.
- **Optimistic concurrency.** Reading an item returns an `ETag`. Send it back as `If-Match` on a write; if the item
  changed meanwhile the answer is `412 precondition_failed` with `current.updatedAt` and `current.updatedBy`. Without
  the header nothing is checked.
- **Merge patch.** `PATCH` bodies are JSON merge patches of `fields`: a key replaces that field, `null` removes it,
  absent keys stay. Arrays and blocks are replaced whole.
- **Partial results** (`fields=title,slug`) limit the fields returned for each listed item.

## Errors

Every error is `{"error": {"code", "message", …}}`:

| Status | Code | Meaning |
| --- | --- | --- |
| 400 | `bad_request` | Malformed input, unknown or repeated parameter, bad filter. Invalid input may carry `fields`. |
| 401 | `unauthorized` | Missing or invalid credentials. |
| 403 | `forbidden`, `csrf` | No grant allows it (the message names the policy), or the CSRF header is missing or wrong. |
| 404 | `not_found` | No such item, type, locale or route. |
| 409 | `conflict`, `referenced`, `duplicate_key`, `duplicate_id` | A publish conflict, an item still referenced (the body lists `referrers`), a key in use, or an id in use (trashed items keep theirs). |
| 412 | `precondition_failed` | The `If-Match` check failed. |
| 422 | `invalid` | Schema validation failed. `fields` lists `{path, code, message}`, and `locale` the locale part. |
| 429 | `throttled` | Too many attempts; see `Retry-After`. |
| 500 | `internal` | A bug; it is logged. |

## Content

Content of a type is reached through the collection, and then by id.

| Method and path | Policy | Does |
| --- | --- | --- |
| `GET /types`, `GET /types/{type}` | any | Compiled types, and one type with its JSON Schema. |
| `GET /blocks` | any | Block types and token sets. |
| `GET /collections/{type}/items` | `content.read` | List. Params: `locale`, `status` (`any`, `published`, `draft`), `q`, `sort`, `where[field][op]`, `fields`, `limit`, `offset`. |
| `POST /collections/{type}/items` | `content.create` | Create a draft: `{"id"?, "key"?, "locale"?, "alwaysAvailable"?, "fields"}`. `id` (any non-nil UUID) chooses the new item's id, e.g. to keep ids stable across re-imports; without it one is generated. |
| `GET /collections/{type}/facets` | `content.read` | Counts per value of up to five indexed text fields. |
| `GET /items/{id}` | `content.read` | Read the working view. `locale`, `draft`, `trashed`. |
| `PATCH /items/{id}` | `content.edit` | Merge-patch the draft: `{"fields", "locale"?, "key"?}`. |
| `DELETE /items/{id}/draft` | `content.edit` | Discard draft parts (`parts=` shared or locale codes). |
| `POST /items/{id}/publish` | `content.publish` | Publish draft parts: `{"locales"?, "force"?}`. |
| `POST /items/{id}/unpublish` | `content.publish` | Stop publishing. |
| `DELETE /items/{id}` | `content.delete` | Move to the trash. With `?permanent=true`, purge an item already in the trash. |
| `POST /items/{id}/restore` | `content.delete` | Bring an item back from the trash, unpublished. |
| `GET /trash` | `content.read` | Trashed items. |
| `GET /items/{id}/versions`, `…/versions/{n}` | `content.read` | Version history and one version's documents. |
| `POST /items/{id}/versions/{n}/restore` | `content.edit` | Write a version into the draft. |
| `GET /items/{id}/references` | `content.read` | Items that point at this one. |
| `GET`, `PATCH /singletons/{type}`, `POST /singletons/{type}/publish` | read, edit, publish | The same for a singleton. |
| `GET /search` | `content.read` | Search across types; includes drafts. |
| `GET /channels` | any | The configured channels. |

An item has one **draft** and any number of published **versions**. A write goes to the draft, split into *parts*
(the shared fields, and one part per locale). `status` is `draft` (never published), `published`, `changed`
(published with draft parts) or `unpublished`. Publishing validates the draft in full (required fields included),
runs the hooks, writes a version and queues the events; the delivery API sees it at once.

A first call, end to end:

```sh
B=localhost:8080/api/manage/v1; A="Authorization: Bearer $TOKEN"
ID=$(curl -s -X POST $B/collections/article/items -H "$A" -H 'Content-Type: application/json' \
  -d '{"key":"hello","locale":"en","fields":{"title":"Hello"}}' | jq -r .id)
curl -s -X PATCH $B/items/$ID -H "$A" -H 'Content-Type: application/json' -d '{"fields":{"summary":"First."}}'
curl -s -X POST  $B/items/$ID/publish -H "$A" -H 'Content-Type: application/json' -d '{}'
```

## Editorial features

| Area | Paths | Page |
| --- | --- | --- |
| Releases | `/releases`, `/releases/{id}/…`, `/items/{id}/releases` | [releases](releases.md) |
| Reviews | `/reviews`, `/items/{id}/review/…` | [reviews](reviews.md) |
| Previews | `POST /preview-tokens`, `/preview-shares`, `/preview-relay/{item}` | [share links](share-links.md) |
| Collaboration | `/collab/items/{id}/…` | [collaboration](collaboration.md) |
| Media | `POST /uploads` | [media](media.md) |
| Pages | the page tree, mounts, routes and redirects, when the module is on | [below](#pages) |

`POST /preview-tokens {"channel"?, "item"?, "locale"?, "ttl"?}` mints a delivery preview token (at most an hour) and,
for an item, the frontend URL to show it at. `channel` may be left out when the project has one channel (or, with an
item, one channel that serves its type); otherwise the answer is `400` listing the channels to choose from.

### Pages

With the pages module, the page tree of each site is managed here (a node is a `page` placing an item, a `mount`
of a collection, or a `link`). Reads need `pages.read`, writes `pages.edit`; a mount also needs `pages.mount`.

| Route | Policy | Body or query | Does |
| --- | --- | --- | --- |
| `GET /pages/sites` | `pages.read` | | The sites the caller can see: `{items: [name]}`. |
| `GET /pages/sites/{site}/tree` | `pages.read` | `locale`, `parent` (node id), `depth` (1 to 10, default 1) | The children of `parent`, or of the root with `root` in the answer. |
| `POST /pages/sites/{site}/nodes` | `pages.edit` | `kind` `page`: `item`, `parent`, `after`. `mount`: `parent`, `segment`, `collection`, `route`, `filter`, `canonical`, `label`, `redirectField`, `redirectStatus`, `after`. `link`: `parent`, `label`, one of `item`, `node` or `url`, `after` | Add a node; `201` with the node. |
| `GET /pages/nodes/{id}` | `pages.read` | | One node. |
| `PATCH /pages/nodes/{id}` | `pages.edit` | `parent`, `after` (a sibling id, or `null` for first), `hidden`, `mount` or `link` (a new spec) | Move, reorder or update a node. |
| `DELETE /pages/nodes/{id}` | `pages.edit` | | Delete a mount or link node (`204`). |
| `GET /pages/sites/{site}/resolve` | `pages.read` | `path` (required), `locale` | What a path resolves to: a page, a redirect or nothing. |
| `GET /pages/items/{id}/paths` | `pages.read` | | Every route of an item, with its state. |
| `GET /pages/locate` | `pages.read` | `url` (required), `header.<Name>` to simulate request headers | Find the site, locale and page a URL belongs to through the web channels. |

A mount's `redirectField` names a top-level `string` or `url` field of its collection. An item whose value there is an
absolute http(s) URL or a site path starting with `/` answers a redirect to it (`redirectStatus` 301, 302, 307 or 308;
default 302) instead of showing itself, and `url` and `links` in delivery point at the target. Items with an empty or
invalid value show themselves; the option applies when the item is published and when the mount changes.

Manual redirects are `redirect` items (`from`, `to`, `statusCode`, `site`, `forLocale`, `force`). A redirect is refused
when a page or mounted item holds `from`, unless `force` is set: a forced redirect is answered before any page, mount or
ordinary redirect at that path (the page keeps its own route, so links to it still point there; anyone requesting the
path is redirected). One forced redirect per site, locale and path. `from` may be `/`, the only way to redirect a
site's root while the tree has a root page. Routes of forced redirects show as `manual` with `force: true`.

The routes are in the OpenAPI document with their schemas.

## Administration

| Paths | Policy | Does |
| --- | --- | --- |
| `/users`, `/users/{id}`, `/users/{id}/groups` | `user.manage` | Users. A `password` sets or resets it and ends the user's sessions. |
| `/groups`, `/groups/{id}`, `/groups/{id}/members/{user}` | `user.manage` | Groups and membership. |
| `/roles`, `/roles/{id}`, `/roles/{id}/assignments` | `role.manage` | Roles. |
| `/assignments` | `role.manage` | Give a role to a user, group or token, optionally with limitations. |
| `/tokens`, `/tokens/{id}` | `token.manage` | API tokens. The secret is in the create answer only. |
| `/policies` | any | The policies and limitation types roles are built from. |
| `/audit` | `audit.read` | The audit log, newest first. Filters: `actor`, `action`, `subject`, `since`, `before`. |
| `/system` | `system.read` | Versions and compiled-in modules. |
| `/auth/login`, `/auth/logout`, `/auth/session`, `/auth/password` | public / any | Sessions. |

Modules add routes under the same prefix and the same rules: every route declares the policy it needs.

## Extending

Project code adds HTTP endpoints under `/api/x` with the same authentication; see [hooks](hooks.md#endpoints). In Go,
`api/client` is a typed client for this API, and `hydrogen generate` writes Go and TypeScript types for your content.
