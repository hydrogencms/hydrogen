# Hydrogen starter for Next.js

A Next.js 16 (App Router, React 19) site on `@hydrogencms/next`, rendering the page-builder project in
`examples/starter`: pages made of blocks in English and German (`/en`, `/de`), a blog, a navigation from the page
tree, a footer from the `siteSettings` singleton, live preview in the admin. It is the template for new Next.js
frontends: copy this directory and adapt it.

## Run it

```sh
# 1. the CMS: examples/starter with its fixture content
cd examples/starter
export STARTER_FRONTEND=http://127.0.0.1:3000      # where the channels' frontend lives (canonical URLs, preview links)
go run . migrate && go run . import fixture
go run . serve                                     # http://localhost:8080, admin at /admin

# 2. the SDK packages (until they are published) and this app
cd ../../packages && npm install && npm run build
cd starters/next && npm install
export HYDROGEN_URL=http://localhost:8080
npm run dev                                        # or: npm run build && npm start
```

Sign in to the admin (`go run . users:create-admin <login>`), open a page and its preview pane: the page updates as you
type. Create a webhook in the CMS config for published changes to show on the site:

```yaml
webhooks:
  next:
    url: http://127.0.0.1:3000/api/hydrogen/revalidate
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}      # the same value in the frontend's environment, 16+ characters
```

| Variable | Default | |
|---|---|---|
| `HYDROGEN_URL` | `http://localhost:8080` | the CMS |
| `HYDROGEN_REVALIDATE_SECRET` | | webhook secret |
| `HYDROGEN_ORIGIN` | the request's host | fixed public origin, for a deployment whose host is not the channel's |
| `HYDROGEN_ADMIN_ORIGIN` | the origin of `HYDROGEN_URL` | the admin that may embed draft pages and talk to them |

`make starter-next-e2e` (repo root) builds the CMS and this app, imports the fixture and runs the adapter conformance
suite, browser checks included.

## How it is put together

| File | Role |
|---|---|
| `lib/hydrogen.ts` | `createHydrogen({ baseUrl, channels, templates, ... })` |
| `app/[[...path]]/page.tsx` | pages: the CMS decides which page lives at which URL (`hydrogen.page()`, `generateMetadata`) |
| `proxy.ts` | CMS redirects with their status, real 404s, also for blog slugs; the preview hand-off |
| `app/[locale]/blog/[slug]/page.tsx`, `app/[locale]/blog/page.tsx` | posts: a collection the CMS does not route (its channels reserve `/blog/*`), found by slug with the client |
| `app/blog/[slug]/page.tsx` | the CMS's preview URL of a post (pattern `/blog/{slug}`, no language prefix): finds the post's channel with the token, redirects to its URL |
| `app/api/hydrogen/{revalidate,preview}` | signed webhook receiver; draft-mode entry |
| `templates/` | `page`, `landing`, `post`: `'use client'` components, see below |
| `components/blocks.tsx` | the block components and the registry: `hero`, `richText`, `cards` + `card`, `callout`, `image`; `shared` is built in |
| `components/site.tsx`, `app/layout.tsx` | header (navigation of the request's channel), footer (`siteSettings`) |
| `app/globals.css` | the design: schema tokens (`tone`, `space`) are class names |

**Templates are client components.** In draft mode `@hydrogencms/next` renders a client template from the admin's working
document (`useLivePreview`), so the page updates without saving. A server component cannot do that: its output is
final when the server renders it. Client templates still render on the server for visitors. They receive
`{ item, locale, metadata, preview }` (no `client`: functions cannot cross to the browser; fetch data in the route and
pass it as `item`, or keep the data fetching in server components around the template). A template and the blocks it
renders must not import server-only code.

**Blocks.** Every component spreads `attrs` on its root element (`data-h-block` always, `data-h-path` in preview) and
gets its slots rendered. Pass `path="fields/body"` and `preview` to `<Blocks>` so paths are emitted in preview. Links
inside block props and rich text come from the item's `links` (provided to the blocks through the `Links` context).

**Your own project.** `lib/types.ts` points at the types `hydrogen generate` wrote for `examples/starter`; use your own
`gen/types.ts`. Add a block: a component in `components/blocks.tsx`, a line in the registry, and CSS. Map the schema's
tokens to classes in `app/globals.css`. Channel names double as locales here (`en`, `de`); adapt `lib/channels.ts` and the
`[locale]` routes if yours differ.
