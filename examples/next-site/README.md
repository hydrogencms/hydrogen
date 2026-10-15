# next-site

Next.js 16 App Router site on `@hydrogencms/next`, rendering the web channels of `examples/multisite` through one
catch-all route. `make next-e2e` (repo root) builds it, starts the CMS on SQLite with seeded content and tests it.

**Channel mapping.** One deployment serves all channels (`HYDROGEN_CHANNELS`). The request's host and path prefix
choose the channel with the `match` options the CMS publishes (`www.acme.com/en`, `www.acme.com/de`,
`jobs.acme.com`, `www.acme.sg`). For a deployment on one channel set `HYDROGEN_CHANNELS` to it and, when its host is
not the public one (localhost), `HYDROGEN_ORIGIN=https://www.acme.com`.

| File | Role |
| --- | --- |
| `app/[[...path]]/page.tsx` | the only content route; `generateMetadata` adds canonical and hreflang |
| `proxy.ts` | CMS redirects with their status, real 404s |
| `app/api/hydrogen/revalidate` | webhook receiver (`events: ["*"]`, secret `HYDROGEN_REVALIDATE_SECRET`, 16+ characters) |
| `app/api/hydrogen/preview` | `?token=…&path=…` draft preview |
| `templates/`, `components/nav.tsx` | page, landing, article and fallback templates; navigation from `nav()` |
| `hydrogen-templates.json` | written at build for `hydrogen check-frontend -templates` |

Environment: `HYDROGEN_URL` (the CMS, default `http://localhost:8080`), `HYDROGEN_CHANNELS`, `HYDROGEN_ORIGIN`,
`HYDROGEN_REVALIDATE_SECRET`. The CMS webhook: `url: https://<site>/api/hydrogen/revalidate`, `events: ["*"]`.

**Deployment.** The default Next cache is per instance: a webhook revalidates only the instance that received it.
For several instances or serverless use a shared cache handler or fan the webhook out; otherwise `ttl` bounds
staleness. The front proxy must overwrite `X-Forwarded-Host`/`-Proto`, which the adapter trusts.

Install with `npm install` (`.npmrc` copies the workspace packages in; build them first: `cd ../../packages && npm install && npm run build`).
