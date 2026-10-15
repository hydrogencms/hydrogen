# Preview bridge protocol, version 1

The admin shows a page of your frontend in an iframe. The two sides talk with `window.postMessage`. This document is
the wire contract; `@hydrogencms/bridge` implements the frontend side and `@hydrogencms/react` builds on it.

Every message is a JSON object with `h: 1` (the protocol version) and a string `type`. Receivers ignore anything else.

## Messages

| From | `type` | Fields | When |
|---|---|---|---|
| frontend | `ready` | `mode` (`"live"` or `"reload"`), `sdk` (`name@version`), `url` | the bridge connected, and again after every reload swap |
| admin | `init` | `item`, `itemType` (the content type), `locale`, `rev`, `doc` | answer to `ready` or `resync`: the full working document of the item in `locale` |
| admin | `patch` | `rev`, `base`, `ops` | a change: JSON Patch operations to apply to the document at revision `base`, producing `rev` |
| frontend | `resync` | `rev` | the frontend cannot apply a patch (wrong base, no document yet, or a failing operation); `rev` is the revision it holds, or `-1` |
| admin | `reload` | `rev` | reload mode: the draft was saved, fetch the page again |
| frontend | `rendered` | `rev`, `ms` | the DOM shows revision `rev`; `ms` is the time since the message that caused it |
| frontend | `error` | `message` | a render, patch or reload failure to display in the admin |

`rev` is an integer owned by the admin. The frontend never invents revisions.

## Origins

- The frontend accepts a message only when `event.origin` equals the configured admin origin exactly and
  `event.source` is `window.parent`. The origin is a bare `scheme://host[:port]`; `*` and paths are rejected at
  start-up.
- The frontend posts only to that origin (never `*`).
- The admin posts only to the origin of the preview URL it loaded and accepts messages only from that origin and
  from its own iframe.
- A message failing a check is dropped without an answer.

## Activation

The bridge is inert (no listener, no `ready`) unless the page is inside a frame **and** was opened for preview.
Preview is detected by any of:

- a non-empty `preview` query parameter (the token the CMS adds to the preview URL);
- a cookie named `hydrogen-preview` that JavaScript can read (a server that keeps it HttpOnly cannot use this one);
- `<meta name="hydrogen-preview" content="1">`, which a server renders when it knows the request is a preview.

Code that knows better can pass `preview: true` or `false` to `createBridge`. Nothing changes for visitors: outside
preview the bridge does nothing.

## Live mode

For frontends that can render from data in the browser. After `ready` the admin sends `init`; the frontend renders
from `doc`. On each edit the admin sends a `patch`.

1. If `base` equals the revision the frontend holds, it applies `ops`, stores `rev` and re-renders.
2. Otherwise, or if an operation fails, it sends `resync` (a failing operation also sends `error` first) and keeps its
   document. The admin answers with a fresh `init`.
3. After the DOM is updated (two animation frames later) the frontend sends `rendered` with the revision shown.

A patch that arrives before any `init` is answered with `resync`.

## Reload mode

For server-rendered pages. The admin saves the draft, then sends `reload`. The frontend fetches `location.href`
(credentials included, so the preview token or cookie travels), parses the HTML and replaces the element marked
`data-h-root` with the new one; without such an element it replaces the children of `<body>`. It restores the scroll
position, updates `document.title`, sends `ready` again and then `rendered` with the time taken.
Scripts in swapped markup do not run; keep long-lived scripts (including the bridge) outside `data-h-root`.

## Documents and paths

`doc` has the shape of the item's delivery representation: the item's `fields` in the stored form, so a block tree is
`doc.fields.body` and a block is `{ id, type, props, slots? }`. Operations use RFC 6902 `add`, `remove` and `replace`
with RFC 6901 JSON Pointer paths against `doc`:

- `/fields/body/2/props/heading` addresses a property; `~1` stands for `/` and `~0` for `~`.
- In an array, `add` accepts an index from `0` to the length (inserting before that position) or `-` (append);
  `remove` and `replace` need an existing index. Indexes have no leading zeros.
- `add` on an object member sets it, creating or overwriting. `remove` and `replace` need the member to exist.
- The empty path `""` addresses the whole document (`replace`/`add` swap it; `remove` is an error).
- Other operations (`move`, `copy`, `test`) are not part of v1. Applying is atomic: one failing operation rejects the
  whole patch.

Frontends built on `@hydrogencms/react` lay the received `doc` over their initial document key by key, so keys the
admin does not send (for example `links`) stay.

## Markers in the DOM

- `data-h-block="<type>"` is on the root element of every rendered block, in preview and in production.
- `data-h-path="fields/body/2"` is added in preview only: the document path of the block (or field) without the leading
  slash. Blocks in a slot use `<block path>/slots/<slot>/<index>`. Blocks inlined from a shared block are read-only and
  carry no path.
- `data-h-root` marks the element reload mode swaps.

The admin can use `data-h-path` to map clicks in the preview back to the form.

## Using the packages

```ts
import { createBridge } from '@hydrogencms/bridge'
const bridge = createBridge({ adminOrigin: 'https://admin.example.com', mode: 'live',
  onInit: ({ doc }) => render(doc), onPatch: (doc) => render(doc) })
bridge.connect()          // after rendering, tell the admin: bridge.rendered(rev)
```

React: `useLivePreview(initial, { adminOrigin })` returns the latest document and reports `rendered` after commit.
For server-rendered pages add the script tag
`<script src="/bridge.js" data-auto data-admin-origin="https://admin.example.com" data-mode="reload"></script>`
(the IIFE build, `dist/bridge.iife.js`).
