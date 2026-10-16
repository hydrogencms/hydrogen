# Share links and relayed previews

A **share link** opens the preview of one item for anyone who holds it: a colleague, a client, a phone. It is
view only, expires, and can be revoked. A **relayed draft** is what lets the viewer see edits as they are typed,
before anything is saved.

## What a share link is

An editor creates a link for one item in one channel (and locale) with a lifetime. The link is the item's preview
URL with `?preview=hps_…` appended. The token (`hps_` and 32 random bytes) is shown once, when the link is
created. Delivery accepts it wherever it accepts a preview token (`?preview=` or the `X-Hydrogen-Preview`
header), for that item in that channel only; it shows the item's draft, never cached, with `X-Robots-Tag:
noindex`. The site module and the frontend SDKs accept it like any preview token.

The server stores only a double SHA-256 of the token. A database dump gives neither the token nor the proof a
frontend publishes (`SHA-256(token)`).

## Exposure

Anyone with the link sees the item's draft, including unpublished text and the items it references (`include`),
until the link expires or is revoked. Treat the URL like a password:

- keep lifetimes short (the default is 24 hours);
- revoke links you no longer need: requests with the token fail within a few seconds on every server, and open
  live streams end;
- sharing needs the `content.preview.share` policy, which the built-in `editor` and `administrator` roles have
  and `contributor` does not. Creating a link also needs the same item access as minting a preview token;
- a link outlives the permissions of the person who created it: removing someone's role or disabling their
  account does not end their links. Revoke them (anyone with `content.preview.share` on the item can);
- listing links needs `content.preview`; revoking needs `content.preview.share`;
- creating and revoking are audited (`preview.share.create`, `preview.share.revoke`).

## Configuration

In `app.yaml`:

```yaml
delivery:
  share_links: true          # false: creating is refused and existing links stop working
  share_default_ttl: 24h
  share_max_ttl: 168h        # at most 720h
```

## Management API

| Route | Needs | Body / result |
| --- | --- | --- |
| `POST /preview-shares` | `content.preview.share` | `{item, channel, locale?, ttl?}` → `201 {id, url?, token, expiresAt, channel, locale}` |
| `GET /preview-shares?item=` | `content.preview` | `{shares: [{id, channel, locale, createdBy, createdByName, createdAt, expiresAt, revokedAt?, viewers}]}` (no tokens) |
| `DELETE /preview-shares/{id}` | `content.preview.share` | `204`, idempotent |
| `PUT /preview-relay/{item}?locale=` | `content.edit` | `{fields}` → `{rev}` |
| `DELETE /preview-relay/{item}?locale=` | `content.edit` | `204` |

`ttl` is seconds or a duration such as `24h`. `url` is absent when no frontend URL resolves for the item. The Go
client has `CreateShare`, `Shares`, `RevokeShare`, `RelayDraft` and `DropRelay`.

`createdByName` is the creator's display name (a user's name, else login; an API token's name), resolved by the
server; it is empty when the creator is unknown or gone.

## Relayed drafts

`PUT /preview-relay/{item}` stores the editor's unsaved working fields for an item and locale (one per pair, the
newest wins). It is validated like a draft save: types and shapes are checked, required fields may be missing.
Nothing is saved to the item.

What is stored is the validated, normalized document, not the request body.

While a relayed draft is newer than the item's saved draft and less than an hour old, every preview of that
item shows it instead of the saved draft: share-link viewers, and the admin's own preview frame. Details:

- The relay is compared with the item's update time, not the locale's draft alone: any save of the item (in any
  locale) ends the overlay. Dropping the relay (`DELETE`) ends it too.
- A relay not updated for an hour (`RelayTTL`) is ignored silently; the preview falls back to the saved draft.
  A job deletes stale rows every ten minutes and tells open streams to rebuild.
- Strings containing U+0000 are refused with `422`.

## Live updates

`GET /api/deliver/v1/preview/events?proof=<SHA-256 of the share token, hex>` is a Server-Sent Events stream:

- `event: draft`, data `{"rev": n, "v": "<16 hex>", "item": {…}}`, where `item` is exactly what `GET /items/{id}`
  returns for the link's token. `rev` is the revision of the relay applied in `item` (0 when the saved draft is
  shown) and `v` is a short hash of the `item` JSON sent: a viewer skips an event whose `v` it already shows.
  Sent on connect and after every change of the item's relay, including dropping it (at most four per second per
  stream). The item is built once per item, channel and locale and shared by the streams that show it;
- `event: error`, data `{"message"}`: the item could not be built this time. The stream stays open and the next
  change sends a new `draft`;
- `event: end`, data `{"reason": "expired" | "revoked" | "unavailable"}`, then the stream closes. `unavailable`
  means the item no longer exists or is not available in the channel;
- a `: ping` comment every 25 seconds.

An unknown, expired or revoked link answers `401` before streaming. A server keeps at most 20 open streams per
link and 1000 in all; beyond that a stream request answers `429` (JSON error, `Retry-After`). The stream works across servers (a relay
written on one reaches viewers connected to another). Cross-origin reads are allowed for the channels'
frontends and the request's own origin (scheme, host and port must match; hosts compare case-insensitively),
never for every origin.

Frontends do not need to call this: the preview bridge opens the stream when a preview page outside the admin
has a proof, and repaints on each `draft` event (see [bridge-protocol.md](bridge-protocol.md)).
