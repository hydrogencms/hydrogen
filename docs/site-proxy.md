# Serving a JavaScript frontend from the Hydrogen binary (site-proxy)

The `site-proxy` module puts a frontend behind the same port as the CMS: one binary, one origin, no CORS, no
second deployment. It has two modes.

- **static**: serve a directory (or an embedded file system) holding a static export of the frontend.
- **ssr**: start a server-rendering process (Next.js standalone, Nuxt, Astro, SvelteKit's node adapter, ...),
  supervise it, and reverse-proxy to it.

Use the `site` module instead when Go templates are enough. The two cannot be combined: both claim the website's
URLs, and the build fails with a message naming the channel (`render: go`).

```sh
hydrogen add site-proxy
```

## Routing

The kernel's routes always win: the admin (`admin.prefix`), `/api/manage/v1`, `/api/deliver/v1`, `/api/x/*`,
`/assets/*`, `/images/*`, `/healthz`, `/readyz` and the routes other modules register. The frontend receives every
other request, every method. That includes the rest of `/api`, so a frontend's own `/api/...` routes (a Next.js
revalidate webhook, say) work unchanged. A path the CMS owns and the frontend also defines is the CMS's.

## Static mode

```yaml
modules:
  site-proxy:
    static:
      dir: out                # relative to the working directory
      spa_fallback: false     # unmatched page paths answer index.html (client-routed apps)
      immutable:              # request paths cached for a year (default shown)
        - /_next/static/**
        - /_astro/**
        - /_nuxt/**
```

- `/about` finds `about` (a file), `about/index.html` or `about.html`; `/` finds `index.html`.
- A missing path answers `404.html` with status 404 when the export has one, else a plain 404. With
  `spa_fallback`, a missing path without a file extension answers `index.html` with status 200 instead; missing
  files (`/app.js`) still 404.
- Patterns in `immutable` are `path.Match` patterns (`*` stays inside one segment) or end in `/**` for a whole
  subtree. They get `Cache-Control: public, max-age=31536000, immutable`; everything else gets `no-cache` with
  `ETag`/`Last-Modified` (when the file system has modification times), so browsers revalidate.
- Dotfiles (`.env`, `.git/...`) are never served, except `/.well-known/`. Only `GET` and `HEAD` are answered.

To embed the export in the binary, pass the file system in code (`dir` must then be empty):

```go
//go:embed all:out
var site embed.FS

func configure(h *hydrogen.Builder) {
	out, _ := fs.Sub(site, "out")
	h.Use(siteproxy.Module(siteproxy.WithFS(out)))
}
```

`spa_fallback` and `immutable` still come from the configuration.

## SSR mode

```yaml
modules:
  site-proxy:
    ssr:
      command: [node, server.js]       # argv, no shell; {port} and {host} are replaced
      dir: .next/standalone            # working directory
      host: localhost                  # loopback only (default 127.0.0.1)
      inherit_env: false               # true: pass Hydrogen's whole environment
      env: { PORT: "{port}", HOSTNAME: "{host}", HYDROGEN_URL: "http://localhost:8080" }
      health: /                        # polled until it answers with a status below 500
      start_timeout: 30s
      restart_backoff: 1s              # doubles per consecutive failure up to restart_backoff_max
      restart_backoff_max: 30s
      stop_timeout: 10s                # SIGTERM, then SIGKILL
    timeout: 60s                       # wait for the response headers
```

Hydrogen picks a free port on the loopback host for every start. The process gets a small part of Hydrogen's
environment (`PATH`, `HOME`, `USER`, `TMPDIR`, `TZ`, `LANG`, `LC_*`, `NODE_*`, `NPM_CONFIG_*`) plus `env`, so the
CMS's secrets (database URL, storage keys, tokens) never reach frontend code. Set `inherit_env: true` to pass
everything. Put what the frontend needs (`HYDROGEN_URL`, a revalidate secret, ...) in `env`; `${env:NAME}` works there.
The admin session cookie (`hydrogen_session`) is removed from proxied requests; preview cookies pass through. It is not routed to until the health check passes; until then (and while it restarts) visitors get
`503` with `Retry-After`. If it exits, Hydrogen restarts it after the backoff (the backoff resets once a process
stayed healthy for 10 s). Its stdout and stderr go to the log, one line each, as `frontend: <line>`. On shutdown
new requests get `503`, requests in flight finish (up to `stop_timeout`), then the whole process group gets
`SIGTERM` and, after `stop_timeout`, `SIGKILL`; leftovers of a crashed child are killed too. On Linux the child
also dies with Hydrogen when Hydrogen is killed outright (`Pdeathsig`); elsewhere only a normal shutdown stops it.
On Windows the process is killed without a grace period and its children are not grouped. A started process counts
as healthy only if it answers the health check and is still running 150 ms later, which catches the rare case of
another process taking the chosen port first.

The proxy streams responses as they arrive (React Server Components, SSE), supports upgrades, keeps the client's
`Host` header and sets `X-Forwarded-For`, `-Proto` and `-Host`. Inbound `X-Forwarded-*` headers are taken over
only from `http.trusted_proxies` peers; from anyone else they are replaced, as for sign-in client addresses.

### Next.js

Build with `output: 'standalone'`, copy `.next/static` to `<standalone>/.next/static` and `public` to
`<standalone>/public`, and run its `server.js`. Give it `host: localhost` and `HOSTNAME: "{host}"`: Next.js
resolves its own middleware rewrites against the host it listens on, and with an IP address in `HOSTNAME` a
rewrite (the starter's 404 pages) loops. The frontend's `HYDROGEN_URL` and `HYDROGEN_ORIGIN` are the CMS's public
origin, which is now also the website's. The channel's `frontend` setting is that same origin.

`scripts/site-proxy-e2e.sh` (`make site-proxy-e2e`) builds the Next starter this way, runs it behind the module and
passes the adapter conformance suite against the CMS port alone.

## Configuration check

`config:check` and start-up reject: both `static` and `ssr`, neither, a non-loopback `ssr.host`, bad
`immutable` patterns, and any channel with `render: go`. A `static.dir` that is not a directory fails at start.
