# Collaboration: presence and co-editing

Editors who open the same item see each other and edit it together. The admin joins a session for the item when
the editor opens (config `collab.enabled`, default `true`) and shows who else is there, which field each one has
focused, and merges their typing live. Saving stays manual: any participant's Save stores the merged document as the
draft, and the others are told.

The server never decodes the document. It authenticates, orders, stores and relays opaque [Yjs](https://yjs.dev)
updates; the saving client sends the merged fields through the normal draft API, so validation, access rules, hooks,
reviews and versions apply as always. Moves of blocks are a delete plus an insert in Yjs: a block moved while
someone else edits inside it can lose that edit.

Without collaboration (disabled, or the editor could not join) the admin shows a banner and edits as before, with
`If-Match` conflicts (412).

## Configuration

```yaml
collab:
  enabled: true   # default; false: no routes, no job, the admin does not join sessions
```

The admin reads the flag as `collab` in `<admin prefix>/config.json`. A server with several nodes needs a database
adapter with multi-node support (Postgres): sessions live in the database and nodes relay through its notifications.

## Protocol

All routes are under `/api/manage/v1/collab/items/{id}/` and need `content.read` on the item. Update payloads are
base64 in JSON. Errors use the management API envelope.

- `POST join {locale, edit, client?}` returns `{client, session, seq, updates, more, init, pending, presence, etag}`.
  `edit: true` needs `content.edit` on the item and locale (403). `client` names a client that re-joins (same
  principal); otherwise the server picks one. `etag` has the format of the item's `ETag` header. At most 20 clients
  per item (429 `session_full`).
  - A session's document has parts `shared` and `l:<locale>`. For each part nobody has seeded, the server claims it
    for exactly one editing client, listed in `init`: that client builds the part from the draft it loaded and posts
    it as an update with `init`. Parts another client is seeding are in `pending`: do not edit them until their init
    update arrives. A claim not fulfilled within 15 seconds of the first claim is released (joining again does not
    extend it); a client with pending parts re-joins (same `client`) to claim it.
  - `updates` is the log to apply, one page of it: at most 200 updates and about 2 MB. `seq` is the highest sequence
    number in the page. With `more: true` the log goes on: read the rest with `GET updates?client=&since=<seq>`
    until `more` is false, applying each page, before opening the event stream from the last `seq` (`since`).
  - A session is seeded from the draft the first joiner loaded. A join that finds the draft newer than the session
    (a write whose reset has not been delivered yet) drops the session first: its clients get `reset {external}` and
    the joiner starts a new one.
- `GET updates?client=&since=` returns `{updates, seq, more}`: the log after `since` as one page (same limits).
  `seq` is the cursor for the next call; the last page has `more: false` and the log's head as `seq`.
- `GET events?client=&since=` is a Server-Sent Events stream for a joined client (the browser's session cookie
  authenticates it). Updates after `Last-Event-ID` (what a reconnecting `EventSource` sends), else `since`, else the
  head of the log are sent first, so a reconnect loses nothing. A client has one stream: opening another for the
  same client ends the older one (without an event). Events:
  - `update {seq, data, client, init?}` (the SSE `id` is `seq`; a client's own updates are not echoed)
  - `presence {clients: [{client, principal, name, color, locale, focus, idle, edit}]}`
  - `saved {etag, client, name, seq}`: a participant saved the draft. `etag` is the draft's ETag right after that
    save, `seq` the `X-Hydrogen-Collab-Seq` the saver sent (0 without it). A receiving client takes the etag and
    marks its form clean only when it has no unsynced local changes and has applied no update beyond `seq` (its
    highest applied seq <= `seq`); otherwise the save lacks something it holds, and it stays dirty with the new etag.
  - `reset {reason}` with `external` (the draft changed outside the session: an API write, publish, unpublish,
    discard, version restore, a save by a tab without collaboration, or a save naming a client of another
    principal; or the session was reset with `POST reset`), `expired` (this client was dropped, also when it lost
    read access) or `trashed`. The stream ends and the log is dropped: reload the draft and join again.
  - `compact {upto}`: post a compacted state (only the client named by the server receives it)
  - a `: ping` comment every 25 seconds
  At most 20 streams per item, 6 per principal and item, and 2000 per server (429 `too_many_streams`).
- `POST updates {client, data, init?}` returns `{seq}`. At most 512 KB. Viewers (`edit: false`, also a client that
  lost edit access) get 403 `read_only`. With `init`, 409 `claim_lost` when the claim expired or went elsewhere:
  join again. A client may post a burst of 50 updates and 20 a second on average (429 `rate_limited` with
  `Retry-After`: batch typing into fewer updates and retry). A log of 2000 updates or 32 MB takes no more: 409
  `compact_required`, a `compact` event goes to the most recently active editor, and the post may be retried after
  the compaction.
- `POST compact {client, data, upto}` replaces the updates with `seq <= upto` by one state that includes them all
  (at most 2.5 MB). It is accepted only from the client the last `compact {upto}` event named and for an `upto` no
  higher than that event's, within two minutes (409 `compact_not_requested` otherwise; 400 when `upto` is beyond
  the log). The server asks the most recently active editor once the log passes 300 updates or 4 MB.
- `POST reset {client}` (editing clients) drops the whole session, log and clients, and sends `reset {external}` to
  every stream, the caller's included. A client that cannot apply the log (a Yjs error) calls it and joins again.
- `POST presence {client, locale, focus?, idle?}` is the heartbeat, every 15 seconds and on focus change (`focus` is
  the field path, e.g. `body[2].props.heading`). A client silent for 45 seconds is dropped; its next call answers 404
  `unknown_client`: join again. A new `locale` must be one the client may read, and edit when it joined as an
  editor (403 otherwise: join again for that locale).
- `POST leave {client}` is idempotent.

A save sends the headers `X-Hydrogen-Collab-Client: <client>` and `X-Hydrogen-Collab-Seq: <seq>` with its draft
`PATCH` (item or singleton); `seq` is the highest update seq the client has applied, its own acknowledged updates
included. The resulting `item.updated` event carries them, and the session gets `saved` instead of `reset` when the
client is in the session and belongs to the principal that wrote (any other client id makes it a `reset
external`). The same headers on a publish or discard are recorded in the event, but those change the draft parts,
so the session resets (`external`) and the clients reload. Events carry the draft state the write produced, and a
reset for a write the session already includes (it was seeded from, or last saved, that state or a later one: a late
or repeated delivery) does nothing. Unpublishing an item turns its published content into draft parts, so it resets the session
too; restoring a trashed item needs no reset, because trashing dropped the session and a trashed item cannot be
joined.

When the last client leaves (`POST leave`, which the admin sends when an editor closes), the session ends with
its log: saving is manual, so changes nobody saved go with the people who left them, as when a single editor
closes without saving. A session whose clients vanished without leaving (a crashed tab) is dropped two minutes
after they go stale, with its log (job `collab.cleanup`, every 30 seconds, which
also drops silent clients and releases expired claims).

## Access and trust

Read access to the item is checked when a client joins and when its stream connects. During a session the rights
are checked again on a client's heartbeats and, when the last check is more than 10 seconds old, on its updates,
compactions and resets: a client that lost read access to the item is dropped (its stream gets `reset {expired}`,
its calls 404 `unknown_client`), one that lost edit access to its locale becomes a viewer (`edit: false`, updates
403 `read_only`). Rights changes therefore take effect within about 10 seconds. A client that is refused at the API
gate (`content.read` withdrawn altogether) cannot heartbeat and is dropped after 45 seconds without one.

The relay is opaque. An editor's updates are not constrained to the fields or blocks they may edit, and the server
cannot validate them: editors of the same item trust each other's updates, as they would trust each other's saves
of the same draft. A malformed update cannot be applied by clients (they call `POST reset`); a well-formed but
unwanted one is a Yjs edit like any other. What protects the draft is the save: it goes through the normal draft
API with the saver's rights (per locale), so a participant without rights on a locale cannot save it, and whatever
the shared document holds for that locale is not stored until someone who may save it does. The limits above (update
size, rate, log size, clients and streams) bound what one client can cost.

Memory: a node keeps the newest updates of items with open streams in feeds, within one byte budget (32 MB) for all
of them; a stream that falls behind the cache reads from the database.

## Go client

`client.Manage` has `JoinCollab`, `PostCollabUpdate`, `CollabUpdates`, `CompactCollab`, `ResetCollab`,
`CollabPresence` and `LeaveCollab`. The event stream is for browsers.
