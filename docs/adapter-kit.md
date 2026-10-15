# Adapter kit: connecting a frontend to Hydrogen

An adapter is the small piece of glue between a frontend framework and the CMS. `@hydrogencms/next` is the reference
implementation (`packages/next`, used by `starters/next`); the `site` module is the same contract implemented in Go
templates. This guide is what you need to write another one, and `@hydrogencms/adapter-test` tells you when it works.

Your frontend only renders. The CMS decides which page lives at which URL, the admin performs every edit, and the
preview bridge shows the frontend what the editor is changing.

## The four wiring points

### 1. One catch-all route that asks the CMS

For every page request, ask the delivery API what the URL is: `@hydrogencms/router`'s `resolve(url)`, or
`GET /api/deliver/v1/route?channel=<name>&url=<absolute public URL>` from a stack without the SDK. The answer's
`located.kind` is one of:

| `kind` | Do |
|---|---|
| `page`, `item` | render `item` with the template for `item.type` |
| `redirect` | answer `located.resolution.status` (301, 302, ...) with `located.resolution.location`, keeping the query |
| `not_found`, `no_channel` | answer a real 404 status with your not-found page |
| `reserved` | the path belongs to your own routes (see below); never answer it from the CMS |

Statuses matter: a redirect must be a redirect and a missing page a 404, not a 200 with a message. Frameworks that
stream pages decide the status before rendering; `@hydrogencms/next` does it in the proxy (middleware) with one cheap
uncached `/route` call and renders from cached data afterwards.

Several channels (languages, markets, hosts) can share one deployment: match the request's host and path prefix
against the channels' `match` options (`matchSite`) the way the CMS does, and pass the channel on.

**Reserved paths.** A channel's `reserved` list (the starter's: `/api/*`, `/blog/*`) are paths the frontend owns. The CMS
never routes them, so you can serve anything there: an API, or a collection such as a blog that is not placed in the
page tree. Collections without the pages module are queried by slug through the delivery API (`GET
/collections/<type>/items?channel=<name>`) and routed by the frontend itself.

### 2. Render the template and the blocks

Pick the template (a component, a Go template, ...) by `item.type`. Fields come from `item.fields`; `item.links`
maps item ids to site paths, which is how rich text links (`item:<id>`) and `ref` props become URLs.

Block fields hold a tree of `{ id, type, props, slots? }`. Render each block through a registry from type to
component, pass it its props and its rendered slots, and wrap it so that **its root element carries
`data-h-block="<type>"`**. A `shared` block arrives with the shared item's blocks inlined as `blocks`; render them
in place. Unknown block types should render nothing (and warn in development) rather than fail the page.
`@hydrogencms/react` has `createRegistry`, `<Blocks>` and `<RichText>` for React; the Go `site` module has the
`blocks`/`block`/`richtext` template functions.

### 3. Put the metadata in the head

`located.canonical` is the page's canonical URL and `located.alternates` the same page in other channels or
languages (`{ channel, locale, url }`). Render `<title>`, `<link rel="canonical">` and one `<link rel="alternate"
hreflang="<locale>" href="<url>">` per alternate. In preview add `<meta name="robots" content="noindex">`.

### 4. Preview, and optionally revalidation

The admin opens the URL returned by `POST /api/manage/v1/preview-tokens` (`{ channel, item, locale }` answers
`{ token, url }`). It is the page's normal URL plus `?preview=<token>`: the frontend's route for the item, or the
channel's preview pattern (for the starter `/blog/{slug}`, `/preview/{collection}/{id}`) for items without a route, such
as a collection item or a page that has never been published. The pattern is used as written: it does not get the
channel prefix, so a frontend with language prefixes has to find the right language itself (the starter tries each
channel with the token, then redirects to the post's real URL).

1. Accept the token. Keep it server side where you can (the Next adapter stores it in an HttpOnly cookie on
   `/api/hydrogen/preview` and enables the framework's draft mode); never log it.
2. Fetch with the token (`X-Hydrogen-Preview: <token>` or `?preview=` on delivery requests): drafts, uncached. A page
   that was never published renders from its draft; a token that is refused (401) means "not in preview".
3. Include the bridge so the admin can talk to the page (next section). Without it the page still previews, but does
   not update while the editor types.
4. Never cache preview responses, and never serve them to visitors without the token.

**Revalidation** (optional for the conformance suite's last check, essential for a cached frontend): subscribe a
webhook (`events: ["*"]`) and purge what the event's `tags` name. Delivery responses carry their cache tags in
`Surrogate-Key`; tag your cached entries with them and a publish purges exactly the pages that show the item.
`@hydrogencms/next` exposes `hydrogen.revalidate()` for the signed webhook.

## The preview bridge

The admin shows your page in an iframe and talks to it with `postMessage`. The wire contract is in
[bridge-protocol.md](bridge-protocol.md). In short:

- The frontend sends `ready` when its bridge starts; the admin answers with `init` (the working document).
- **Live mode**: the page re-renders from the document; each edit arrives as a JSON Patch (`patch`). Use
  `@hydrogencms/bridge` directly, or `@hydrogencms/react`'s `useLivePreview` / `<LivePreview>`.
- **Reload mode**: for server-rendered pages. The admin saves the draft and sends `reload`; the bridge re-fetches the
  page (token or cookie included) and swaps the element marked `data-h-root`. The IIFE build of the bridge
  (`@hydrogencms/bridge/iife`, a `<script data-auto data-admin-origin data-mode="reload">`) needs no JavaScript
  framework.
- The bridge is inert unless the page is in a frame **and** in preview: a `?preview=` parameter, a readable
  `hydrogen-preview` cookie, or `<meta name="hydrogen-preview" content="1">`. Servers that keep the token in an
  HttpOnly cookie render the meta tag in preview.
- It talks only to the configured **admin origin** (default: the CMS's origin); everything else is ignored.

### Markers in the markup

| Marker | Where | Meaning |
|---|---|---|
| `data-h-block="<type>"` | root element of every rendered block, always | the block's type; the conformance suite counts them |
| `data-h-path="fields/body/2"` | blocks and editable fields, **in preview only** | the document path (`<block path>/slots/<slot>/<i>` inside slots); blocks inlined from a `shared` block carry none (read-only) |
| `data-h-root` | the element reload mode swaps | everything the admin may change lives inside it |

## Checking your adapter

`@hydrogencms/adapter-test` is the definition of "works with Hydrogen". It expects a CMS loaded with the starter
fixture (`examples/starter`: pages in English and German on `/en` and `/de`, three posts, a shared block, a redirect
and a never-published page) whose channels point at your frontend:

```sh
cd examples/starter
STARTER_FRONTEND=http://127.0.0.1:3000 hydrogen migrate && hydrogen import fixture && hydrogen serve   # the CMS on :8080

npx hydrogen-adapter-test --cms http://127.0.0.1:8080 --token <management token> --frontend http://127.0.0.1:3000 --browser
```

The CMS needs a webhook to your frontend's revalidation endpoint (or whatever purges your cache). Options: `--admin-origin`
(the origin your frontend accepts bridge messages from, default the CMS's), `--channels en,de`, `--only pages,preview` (groups: pages links seo notfound redirects reserved blog preview bridge revalidate),
`--revalidate-timeout 20`, `--json`; `HYDROGEN_URL` and `HYDROGEN_TOKEN` can stand in for `--cms` and `--token`. The
suite edits and publishes one page and one post and puts them back, so run it against a scratch database.

| Group | Checks |
|---|---|
| `pages` | every page of every channel answers 200, shows its title in an `<h1>`, renders exactly the blocks of the document (`data-h-block` counts, shared blocks inlined), and emits no `data-h-path` outside preview |
| `links` | links to other items inside rich text render as `<a href>` with the URL delivery puts in `links` (a post needs a channel `links:` pattern) |
| `seo` | `<link rel="canonical">` and the `hreflang` alternates match the CMS's `/route` answer |
| `notfound` | unknown pages, deep paths, unknown prefixes and unknown blog slugs answer 404 |
| `redirects` | the CMS's manual redirects answer with the CMS's status and location |
| `reserved` | the CMS reports `/blog/*` and `/api/*` as reserved |
| `blog` | every post renders at `<prefix>/blog/<slug>` with its blocks |
| `preview` | a never-published page renders with the token and is a 404 without it; a draft edit of a page and of a post (through the `/blog/{slug}` pattern) shows with the token only; `data-h-path` is present |
| `bridge` | `--browser`: with the suite playing the admin in a frame, a page outside preview stays silent; `ready` arrives from the frontend origin; live mode (on a page and on a post's preview URL): `init`, a replace, a whole-document replace (path `""`), add at an index and with `-` (with `data-h-path`) and remove patches update the DOM and answer `rendered`, a stale `base` is answered with `resync`; reload mode: `reload` after a saved draft re-renders and answers `ready` and `rendered` |
| `revalidate` | a published change shows on the frontend within the timeout, and so does the revert |

The browser checks use **your** Playwright (`npm i -D playwright && npx playwright install chromium`; or
`--playwright-from <dir>` to resolve it elsewhere); without it they are skipped with a message. Give the CMS and the
frontend the same host name (`127.0.0.1` or `localhost` for both) so that the preview cookie reaches the frame.

The suite is also a library: `import { runSuite, format } from '@hydrogencms/adapter-test'`. In CI, `make
conformance` runs it against the Next.js starter and the `site` module.
