# Releases

A **release** is a named set of items, each with an action (`publish` or `unpublish`), that goes live together:
now, or at a time you schedule. It is how an editorial team ships a campaign, a redesign or a batch of
translations without half of it being visible for a while.

A release holds no content of its own. Publishing takes each item's draft as it is at that moment, so edits made
after an item was added are included.

## Lifecycle

```
open ──schedule──▶ scheduled ──(due)──▶ publishing ──▶ published
  ▲  ◀──unschedule──┘     │                  │
  │                        │                  └──▶ failed ──(any edit)──▶ open
  └──────────────────  cancel (open or scheduled) ──▶ cancelled
```

| Status | Meaning | Allowed |
| --- | --- | --- |
| `open` | being assembled | edit, schedule, publish now, cancel, delete |
| `scheduled` | will publish at `scheduledAt` | rename, reschedule, unschedule, publish now, cancel |
| `publishing` | a run is in progress | nothing (409 `release_state`) |
| `published` | final | nothing |
| `failed` | the last run published nothing; `errors` says why | edit (reopens it), schedule, publish again, rename |
| `cancelled` | final | delete |

Items change only while a release is `open` or `failed`: unschedule a scheduled release first. Deleting works for
`open` and `cancelled` releases. Any move that the table does not allow answers 409 with code `release_state`.

An item can be in several open releases, and publishing one does not take it out of the others. A release has a
`kind`: `release` (the default) or `item`, which the admin uses for "schedule publish" of a single item (named
"*item name* — publish *date*"). They are the same thing on the server; `kind` only lets listings separate them.

## Publishing is one transaction

Publishing a release (now or when scheduled) runs every item in one database transaction as the principal who
publishes. Each item is checked first: it exists, is not in the trash, has something to publish (for a
`publish` action), validates, has no draft conflict (unless `force`), and the principal holds `content.publish`
for it, with the same item rules and limitations as publishing the item by hand.

If any item fails, **nothing** is published: the transaction rolls back and, in a second transaction, the release
becomes `failed` with one error per problem item:

```json
{ "status": "failed",
  "errors": [ { "item": "01…", "code": "invalid", "message": "required", "path": "fullName" },
              { "item": "02…", "code": "nothing_to_publish", "message": "the item has no draft changes to publish" } ] }
```

Codes: `invalid` (validation; `path` is the first field), `conflict` (published since the draft started; use
`force`), `nothing_to_publish`, `review_required` (the item's type uses [reviews](reviews.md) and the draft is
not approved), `forbidden`, `trashed`, `not_found`, `referenced`, `bad_request` and `internal`
(an unexpected error; details are in the server log). An error without `item` concerns the release (for example
a scheduler who is gone). Fix the problems and publish again: a failed release can be published directly, and any
edit turns it back into `open`.

An `unpublish` action on an item that is not published is a no-op, not an error. Singletons cannot be unpublished.

On success the release is `published` (`publishedAt` set), every item emits its usual `item.published` or
`item.unpublished` event, and one `release.published` event follows:

```json
{ "id": "…", "name": "Autumn launch", "kind": "release", "publishedAt": "…",
  "items": [ { "item": "…", "action": "publish" } ] }
```

Subscribe webhooks to `*` (or `release.*`) to receive it.

## Scheduling

`scheduledAt` must be in the future and at most a year ahead. A job named `releases` runs every minute (the job
runner wakes each second, so a due release starts within about a minute of its time) and publishes the releases
that are due, oldest first. Several servers can run it; a release starts once.

The release is executed **as the principal who scheduled it**. The server stores who that was (`scheduledBy`)
and, at the time of execution, loads that user or token again, as a request would be authenticated: a user must
still exist and be enabled, with their current groups; a token must not be revoked or expired. Grants are read
then, so rights removed after scheduling apply. If the principal is gone, no longer holds `release.manage`, or
lacks `content.publish` for an item, the release becomes `failed` with `forbidden` errors and nothing is
published. Audit entries of the run name the scheduling principal.

A release left in `publishing` for more than 10 minutes (a crashed server) is picked up by the same job and run
again, as the principal who scheduled it (else the one who started it). This is safe because a run is one
transaction: a release is either published completely or not at all, and a run that crashed before committing left
no item changed. (`force` is not remembered across such a retry.)

A scheduled release that can never start, because every item was purged meanwhile, becomes `failed` (error code
`no_items`) instead of being retried every minute. When the server has access control but cannot load principals
(no authenticator), a scheduled release fails with `forbidden` instead of running unchecked; only releases started
by the system run without a principal.

### Several servers

Every status change is a conditional update on the status that was read, in one statement (`TransitionRelease` in
the store), so servers sharing one database cannot both win a transition: of two simultaneous publishes of the
same release one runs and the other gets 409 `release_state`; a run re-checks `publishing` under the row's lock
before it touches an item; and a failure is recorded only while the release is still `publishing`. A release that
one server published is never marked `failed` because another server lost the race.

## Access

| Policy | Allows |
| --- | --- |
| `release.manage` | create, edit, schedule, unschedule, cancel, publish and delete releases. Granted to the `editor` and `administrator` templates. |
| `content.read` | read releases (`GET /releases`, `GET /releases/{id}`, `GET /items/{id}/releases`). |
| `content.publish` | needed for each item when a release runs, now or scheduled. `release.manage` alone cannot publish. |

Existing databases keep the roles they were seeded with: **add `release.manage` to the stored editor role** (and to
any custom role that should manage releases) in the admin, or editors get 403 on every release route.

Release views and item adds use the same read check as reading the item (grants, limitations and access rules):
items the caller cannot read are shown without name or type (`state: hidden`) and cannot be added, and
`GET /items/{id}/releases` answers 404 for an item the caller cannot read.

Everything is audited in the transaction that changes it: `release.create`, `release.update` (rename, items,
unschedule), `release.schedule`, `release.cancel`, `release.delete`, `release.publish`, `release.fail`.

## Upgrading to a version with releases and the trash

Migration `0007_editorial` adds the trash columns to `item`, replaces the unique index on the item key with one
that ignores trashed items (a trashed item's key is free again), and creates the release tables. The index change
means the old and the new code must not write items at the same time: either stop all servers, migrate (`hydrogen
migrate`), and start the new version, or deploy in order (migrate first, then roll the servers; during the roll the
old servers still read and write items but know nothing of the trash). Also add `release.manage` to stored roles as
described under Access.

## Export, import and async consumers

Releases are operational state and are not exported or imported: an import starts with none, and an export
leaves out trashed items (and, with the pages module, their nodes, slugs and routes). Webhooks and other async
consumers that react to an item going offline must subscribe to `item.unpublished` and `item.trashed`; the
`AfterDelete` hook (in the transaction or async) fires on a purge only (`item.deleted`).

## Management API

All under `/api/manage/v1`; the OpenAPI document describes each operation.

| Route | Does |
| --- | --- |
| `GET /releases?status=&kind=&item=&limit=&offset=` | list, newest first: `{items, total, hasMore}` (rows have `itemCount`, no items) |
| `POST /releases` | `{name, description?, kind?, items?: [{item, action?, locales?}], scheduledAt?}` |
| `GET /releases/{id}` | the release with `items` (name, type, action, locales, `state`: `changes`, `current`, `trashed`, `missing`, `hidden`) |
| `PATCH /releases/{id}` | `{name?, description?}` |
| `DELETE /releases/{id}` | open or cancelled only |
| `PUT /releases/{id}/items/{item}` | `{action?, locales?}`: add or replace |
| `DELETE /releases/{id}/items/{item}` | remove (idempotent) |
| `POST /releases/{id}/schedule` | `{at}` (RFC 3339) |
| `POST /releases/{id}/unschedule`, `/cancel` | no body |
| `POST /releases/{id}/publish` | `{force?}` (body optional) |
| `GET /items/{id}/releases` | open and scheduled releases that contain the item, with its `action` and `locales` |

A publish that could not publish answers **422** with code `release_failed` and the release in `release`:

```json
{ "error": { "code": "release_failed", "message": "nothing was published: 2 error(s); see the release" },
  "release": { "status": "failed", "errors": [ … ], … } }
```

## Go client

```go
m := client.NewManage(url, token)
rel, _ := m.CreateRelease(ctx, client.CreateReleaseInput{Name: "Autumn launch", Items: []client.ReleaseItemInput{
    {Item: a}, {Item: b, Action: "unpublish"}}})
_, _ = m.ScheduleRelease(ctx, rel.ID, time.Now().Add(48*time.Hour))
rel, err := m.PublishRelease(ctx, rel.ID, false)
var e *client.Error
if errors.As(err, &e) && e.Code == "release_failed" {
    for _, x := range rel.Errors { log.Println(x.Item, x.Code, x.Message) }
}
```

In Go code inside the server, `kernel.Services.Releases` is the same service (`release.Service`); methods act as the
principal in the context (none means the system).
