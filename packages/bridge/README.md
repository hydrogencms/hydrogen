# @hydrogencms/bridge

The frontend side of the Hydrogen preview bridge: the admin shows your site in an iframe, and this package receives the
working document, applies edits live, and reports back what it rendered. The wire format is in
[docs/bridge-protocol.md](../../docs/bridge-protocol.md); [docs/adapter-kit.md](../../docs/adapter-kit.md) covers
wiring it into a frontend. Framework adapters wrap it (`useLivePreview` and equivalents).

```sh
npm i @hydrogencms/bridge
```

## Use

```ts
import { createBridge } from '@hydrogencms/bridge'

const bridge = createBridge({
  adminOrigin: 'https://admin.example.com',
  mode: 'live',                           // 'live': re-render from patches; 'reload': refetch the page
  onInit: ({ doc }) => render(doc),
  onPatch: (doc) => render(doc),
})
bridge.connect()                          // does nothing outside a preview; after rendering: bridge.rendered(rev)
```

`isPreview()` tells whether the page runs in a preview. `applyPatch(doc, ops)` applies the protocol's JSON patch
operations. In `reload` mode `reloadSwap()` refetches the page and swaps the `[data-h-root]` element, keeping the scroll
position.

## Exports

| Import | Holds |
| --- | --- |
| `@hydrogencms/bridge` | `createBridge`, `isPreview`, `applyPatch`, `reloadSwap`, `autoStart`, types. |
| `@hydrogencms/bridge/merge` | `mergeDoc`, `keepShared`, `liveDoc(initial, doc)`: the live document laid over the server-rendered one, keeping delivery-only keys such as `links` and the inlined blocks of shared blocks. |
| `@hydrogencms/bridge/iife` | `dist/bridge.iife.js` for server-rendered pages: `<script src="/bridge.js" data-auto data-admin-origin="https://admin.example.com" data-mode="reload"></script>`. |

Mark up the DOM with `data-h-block`, `data-h-path` and `data-h-root` as described in the protocol to enable canvas
editing.
