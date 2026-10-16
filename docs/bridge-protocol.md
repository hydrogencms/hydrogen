# Preview bridge protocol, version 1

The admin shows a page of your frontend in an iframe. The two sides talk with `window.postMessage`. This document is
the wire contract; `@hydrogencms/bridge` implements the frontend side and `@hydrogencms/react` builds on it.

Every message is a JSON object with `h: 1` (the protocol version) and a string `type`. Receivers ignore anything else.

## Messages

| From | `type` | Fields | When |
|---|---|---|---|
| frontend | `ready` | `mode` (`"live"` or `"reload"`), `sdk` (`name@version`), `url`, `preview` (optional, see [Proof of the preview token](#proof-of-the-preview-token)), `edit` (`true` when the bridge supports [canvas editing](#canvas-editing)) | the bridge connected, and again after every reload swap or reload |
| admin | `init` | `item`, `itemType` (the content type), `locale`, `rev`, `doc` | answer to `ready` or `resync`: the full working document of the item in `locale` |
| admin | `patch` | `rev`, `base`, `ops` | a change: JSON Patch operations to apply to the document at revision `base`, producing `rev` |
| frontend | `resync` | `rev` | the frontend cannot apply a patch (wrong base, no document yet, or a failing operation); `rev` is the revision it holds, or `-1` |
| admin | `reload` | `rev` | reload mode: the draft was saved, fetch the page again |
| frontend | `rendered` | `rev`, `ms` | the DOM shows revision `rev`; `ms` is the time since the message that caused it |
| frontend | `error` | `message` | a render, patch or reload failure to display in the admin |
| admin | `overlay` | `on` (bool), `texts` (`[{path, text, scope}]`) | canvas editing: switch the overlay on or off and say which plain-text values can be edited; sent after every verified `ready` and whenever `on` or `texts` change |
| frontend | `layout` | `nodes` (`[{path, rect: [x, y, w, h], block?, text?}]`), `vw`, `vh` | while the overlay is on: the measured nodes, after `overlay`, each render or swap, and on scroll, resize and DOM changes (at most one per animation frame) |
| frontend | `hover` | `path` (string or `null`) | while on: the node under the pointer changed |
| frontend | `select` | `path` (string or `null`), `edit` (bool) | while on: a click (`edit: false`) or double click (`edit: true`) on a node; `null` on empty space |
| admin | `scrollTo` | `path` or `by` (px) | bring a node into view / scroll the page by a delta |
| admin | `editText` | `path`, `multiline` (bool) | start editing a text node in place |
| frontend | `inlineEdit` | `path`, `value` (string) or `cancel: true` | inline editing ended: the committed value, or cancelled or refused |

`rev` is an integer owned by the admin. The frontend never invents revisions.

## Origins

- The frontend accepts a message only when `event.origin` equals the configured admin origin exactly and
  `event.source` is `window.parent`. The origin is a bare `scheme://host[:port]`; `*` and paths are rejected at
  start-up.
- The frontend posts only to that origin (never `*`).
- The admin posts only to the origin of the preview URL it loaded and accepts messages only from that origin and
  from its own iframe.
- A message failing a check is dropped without an answer.

### Locked frames

When the preview URL is on the admin's own origin, the admin locks the frame: its sandbox omits `allow-same-origin`,
so the page runs with an opaque origin (`window.origin` is `"null"`) and has no handle on the admin, no cookies and no
storage. Two things change on the admin side:

- it accepts a message when `e.source` is the frame's `contentWindow` (the origin of a locked frame reads `"null"`,
  so it cannot be compared);
- it posts to a locked frame with target `*`. Only `init` and `patch` carry the draft, and the admin sends `init`
  only after the proof below.

The frontend side is unchanged: it still checks the admin origin and posts to it. Cross-origin frontends are not
locked; the browser already isolates them, and the admin posts to the preview origin.

## Proof of the preview token

A frame can navigate away (a link in content) to a page that carries a bridge or a look-alike. So `ready` carries
`preview`: the SHA-256 of the preview token, as 64 hex digits. The bridge reads it from

```html
<meta name="hydrogen-preview" content="<sha256 hex of the preview token>">
```

A server that knows the token (it came in `?preview=` or a cookie) renders the hash, never the token. `preview` is
omitted when the meta is missing, holds the old value `1`, or is not 64 hex digits. The admin sends `init` only when
`ready.preview` equals the hash of the token it minted for the URL; otherwise it ignores the `ready` and shows "not
the previewed page". An adapter whose `ready` has no matching hash fails the conformance check "frontend doesn't
prove the preview token".

## Activation

The bridge is inert (no listener, no `ready`) unless the page is inside a frame **and** was opened for preview.
Preview is detected by any of:

- a non-empty `preview` query parameter (the token the CMS adds to the preview URL);
- a cookie named `hydrogen-preview` that JavaScript can read (a server that keeps it HttpOnly cannot use this one);
- `<meta name="hydrogen-preview" content="…">`, which a server renders when it knows the request is a preview. The
  content is the token's hash (see above); the old `content="1"` still activates the bridge but cannot pass the
  admin's proof.

Code that knows better can pass `preview: true` or `false` to `createBridge`. Nothing changes for visitors: outside
preview the bridge does nothing.

## Viewer mode (share links)

A share link opens a preview page **outside a frame**: no admin, no `postMessage`. The bridge then follows the
editor's relayed (unsaved) draft over Server-Sent Events. It starts when the page is in preview (see Activation), is
not framed, carries a proof (`<meta name="hydrogen-preview" content="<sha256 hex>">`) and the browser has `EventSource`.
The `ready`/`init`/`patch` messages and the canvas are not used.

The bridge opens `${adminOrigin}/api/deliver/v1/preview/events?proof=<proof>` (the CMS serves the admin and delivery
on one origin; the `relay` option replaces the URL up to the query, and `relay: false` turns viewer mode off). The
proof is the SHA-256 of the share token; the token itself never leaves the page's server. The stream sends:

| Event | Data | The bridge |
|---|---|---|
| `draft` | `{"rev": n, "item": <the item as delivery returns it for this link>}` | live mode: `onInit({item: item.id, itemType: item.type, locale: item.locale, rev, doc: item})`, so the page renders the new document (`rendered` is a no-op without a parent). Reload mode: `reloadSwap()`, falling back to `location.reload()`; the first `draft` after connecting, and a repeat of the same `v` (the content hash; `rev` when the event has none: a reconnect), are skipped because the page already shows it. Swaps run one at a time with at most one queued, so the latest draft always wins. |
| `end` | `{"reason": "expired" \| "revoked"}` | closes the stream (no reconnect), then `location.reload()`; the server then answers 401 |

Known race: the page cannot tell which draft it rendered, so the first `draft` is always skipped. If the relay
changes between the server rendering the page and the stream connecting, that change is not shown until the next
one (the stream does not replay). Reloading the page fixes it.

`disconnect()` closes the stream. `error()` posts nothing outside a frame. An unknown, expired or revoked proof is refused before streaming (401) and
`EventSource` gives up; the page is not changed.

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

The bridge falls back to `location.reload()` when the swap cannot work: the window has an opaque origin (a locked
frame: `window.origin === "null"`, where the fetch would be cross-origin), or the fetch itself fails (network or CORS).
Browsers restore the scroll position. There is no `rendered` afterwards; the reloaded page starts a new bridge that
sends `ready`, and the admin answers as for any `ready`. An HTTP error or an expired preview is still reported with
`error`, not reloaded.

## Links in a frame

In a frame, in preview, when the current URL carries `?preview=<token>`, the bridge appends that parameter to a
same-origin `<a href>` when it is clicked (capturing `click` and `auxclick` listeners; the DOM is not rewritten on
load). A frame without cookies (a locked frame) so keeps preview while navigating. Links that already have a
`preview` parameter, other origins and other schemes are left alone, and so is everything when the URL has no token.

## Canvas editing

A bridge that sends `ready` with `edit: true` supports the messages below, which let the admin draw an overlay over
the frame and edit text on the page itself. They are additive: the protocol stays version 1, and a bridge without
`edit` simply does not answer them. The origin rules above apply unchanged, in locked frames too.

The admin draws everything (outlines, labels, toolbars, drag indicators). The frame only measures, reports pointer
events and edits text.

### Overlay and layout

`overlay {on: true, texts}` switches the overlay on; `on: false` (or `disconnect`) switches it off. Off, the bridge
holds no listeners or observers and sends no `layout`, `hover` or `select`; the page is fully interactive. The
preview-link rewriting described above does not depend on the overlay. After a `location.reload()` the new bridge
sends `ready` and the admin sends `overlay` again; after a reload swap the overlay stays on and the bridge measures
again.

While on, the bridge sends `layout` after `overlay`, after `rendered` and each reload swap, and when the page
scrolls, resizes or changes (a `MutationObserver`, and a `ResizeObserver` where available). Measurements are
coalesced to at most one `layout` per animation frame. A `layout` identical to the last one sent is skipped, except
that `overlay`, `rendered` and each reload swap always get one. The observer watches child lists, text and the
attributes `data-h-path`, `data-h-block`, `class`, `style` and `hidden`.

- A node is `{path, rect, block?, text?}`. `rect` is `[x, y, width, height]` in CSS pixels of the frame's viewport
  (`getBoundingClientRect`, rounded). Nodes with zero width or height are left out. Nodes come in document order.
- `block` is the element's `data-h-block`. `text` is `true` when the element's only child is one non-empty text node
  (a candidate for inline editing).
- `vw` and `vh` are the viewport size.

Nodes are every `[data-h-path]` element inside `<body>` plus the elements found by text mapping.

### Text mapping

`texts` lists plain-text values the admin could edit: `{path, text, scope}`, where `scope` is the `data-h-path` of
the block that holds the value, or `""` for a top-level field. For each entry the bridge finds the element whose only
content is that text:

1. The scope element is the one with `data-h-path` equal to `scope`; for `""` it is `[data-h-root]`, else `<body>`.
2. It walks the scope's text nodes (skipping `script`, `style`, `noscript` and `template`). A text node qualifies
   when its closest `data-h-path` ancestor is the scope element itself (for `""`: it has none, so text inside blocks
   never matches a top-level field) and its data, with whitespace runs collapsed and trimmed, equals the entry's
   `text` collapsed and trimmed.
3. The parent element of the first qualifying text node becomes the node for `path`, unless that element already
   carries `data-h-path`. An element is mapped to one entry only.

The mapping is recomputed on `overlay`, after renders and swaps, and on DOM changes. No markers are needed for it, but
a frontend that wants exact results can mark an element with `data-h-path` itself.

### Pointer events

While on, `click`, `dblclick`, `auxclick` and form `submit` are captured and prevented (links, buttons and submits do
nothing), except inside the element being edited inline. A `submit` sends no `select`. The node for an event is the
nearest ancestor of the target (or the target) that is a node in the layout: elements with zero width or height
(`display: contents`, say) are skipped.

- `click` and `dblclick` send `select` with that node's `path`, or `null` on empty space; `edit` is `true` for the
  double click.
- Pointer movement (listened to in the capture phase, so pages that stop propagation do not silence it) sends `hover` only when the node under the pointer changes; leaving the document sends
  `hover` with `null`.

The bridge changes no styles on the page. The only attribute it sets is `contenteditable`, during inline editing.

### Scrolling

`scrollTo {path}` scrolls the node into view (`block: "nearest"`, smooth unless the user prefers reduced motion).
`scrollTo {by}` scrolls the page by `by` pixels with `behavior: "instant"`, whatever the page's CSS (the admin uses it to
scroll while dragging).

### Inline editing

`editText {path, multiline}` starts editing the node for `path`. The bridge refuses with `inlineEdit {path, cancel:
true}` unless the node exists and its only child is one text node. Otherwise it sets `contenteditable` on the element
(`plaintext-only`, or `true` with pasted text inserted as plain text where that value is unsupported), focuses it, and
puts the caret at the end unless a selection already lies inside it.

- Single line: Enter (and Shift+Enter) commits. Multiline: Enter inserts a newline and Ctrl or Cmd+Enter commits.
- Leaving the element (blur) commits. Escape cancels. Turning the overlay off (or `disconnect`), also when it was
  already off, cancels. So does the element leaving the document (a reload swap or a framework re-render replaced it).
  The Escape or Enter that ends an edit does not propagate, so page hotkeys never see it.
- The committed value of a single line is the element's `textContent` (CSS `text-transform` never leaks into it):
  whitespace runs (newlines included) become one space and the ends are trimmed. A multiline value is its `innerText`. An unchanged value is still sent.
- When editing ends the bridge removes `contenteditable`, puts back the element's original child node (the
  framework's own text node) and sets its data to the committed value, or to the original on cancel. This keeps
  frameworks that own that node working. Then it sends `inlineEdit {path, value}` or `{path, cancel: true}`.

Nothing is sent while the user types: the admin updates the document from the committed value, and the page
re-renders from it as for any other change.

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
- `data-h-root` marks the element reload mode swaps. It is also the scope for top-level text mapping.

Explicit markers and mapped text differ in who decides. `data-h-path` is the frontend saying "this element is that
path" and always wins; use it for blocks, and for text that appears more than once or is not the only content of its
element. Text that is the only content of an element needs no marker: the bridge matches it by value (see
[Text mapping](#text-mapping)). Rich text is not mapped and not edited inline.

The admin uses these nodes to map clicks in the preview back to the form.

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
