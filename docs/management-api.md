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
| 409 | `conflict`, `referenced`, `duplicate_key` | A publish conflict, an item still referenced (the body lists `referrers`), or a key in use. |
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
| `POST /collections/{type}/items` | `content.create` | Create a draft: `{"key"?, "locale"?, "alwaysAvailable"?, "fields"}`. |
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
| Pages | the page tree, mounts, routes and redirects, when the module is on | |

`POST /preview-tokens {"channel", "item"?, "locale"?, "ttl"?}` mints a delivery preview token (at most an hour) and,
for an item, the frontend URL to show it at.

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
