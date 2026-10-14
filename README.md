# hydrogencms

A general-purpose headless, composable CMS written in Go: one content backend for websites and applications. Lightweight and powerful: the lightest element, and the one everything
else is built from.

- **Lightweight.** One small binary plus a database (SQLite or Postgres), fast startup, a small admin, few dependencies, sensible defaults.
- **Powerful through composition.** Capabilities come from small modules, not a heavy core.
- **Headless first.** Content is delivered over APIs to any frontend; the CMS never renders the public site.
- **Composable.** A small core plus modules compiled in at build time. Projects include only what they need.
- **Live visual editing.** Editors build pages from typed blocks in a live preview of the real frontend.
- **Headless or fullstack.** Serve your website from the same binary with Go templates (or host a JS frontend),
  or run fully headless; switch later without touching content.
- **Any frontend.** A framework-neutral router and preview bridge, a Next.js adapter, and a documented path for
  custom frameworks in any language. With the pages module, one catch-all route lets the CMS control pages and
  routing.
- **Collections and pages.** Every content type is a collection with lists, filters and search; singletons hold
  one-off content like site settings. An optional pages module adds a site tree that owns navigation, URLs and
  redirects, and can mount collections (`/news/{slug}`). Websites work with or without it.
- **Channels.** Each consumer, website or app, is a channel defined in config, with its own languages, fallback
  and collections.
- **Document-relational storage.** One validated JSON document per item version and locale, plus real tables for
  everything that is queried, joined or must stay consistent. No EAV. Swappable database adapters.
- **Easy to customise.** Schema files for structure; typed hooks, access rules, endpoints and admin elements for
  behaviour; plugins use the same mechanisms.
- **AI-ready.** Retrieval (RAG) over your content, and an MCP server and agent API that let models create and
  update content under the same permissions, validation and review as people.
- **No lock-in.** Open source, documented storage, portable export and import, open API specs.

Status: early development. The foundations (kernel, configuration, SQLite and Postgres adapters, outbox, audit,
`hydrogen new` and `hydrogen dev`) work; content features are next.

## Try it (from a checkout, until releases exist)

```sh
go build -o bin/hydrogen ./cmd/hydrogen
bin/hydrogen new -module example.com/mysite -core . ../mysite
cd ../mysite && ../hydrogen/bin/hydrogen dev    # http://localhost:8080/healthz, SQLite in .work/
```

## Development

```sh
make build test vet lint   # every module in the repo
make db-up                 # Postgres 17 on localhost:5436 for the Postgres adapter tests
make smoke                 # new project → migrate → serve, with binary size, start-up and memory budgets
```

Go module: `github.com/hydrogencms/hydrogen`. License: [MIT](LICENSE).
