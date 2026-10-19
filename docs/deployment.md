# Deployment

A Hydrogen project compiles to one static binary that serves the admin, both APIs and the media files. Around it you
need a database, the project's `config/` and `schema/` directories, and a place for uploaded files. There is nothing
else to install: no Node.js at runtime, no cgo, no web server in front of it unless you want TLS and a CDN.

## Build

```sh
hydrogen build                                  # bin/<name>, CGO_ENABLED=0, -trimpath
GOOS=linux GOARCH=amd64 hydrogen build         # cross-compile for the server
```

`hydrogen build` is `go build` with the right flags (and it regenerates `modules_gen.go` first). A plain
`go build -trimpath .` gives the same program, because `modules_gen.go` and `gen/` are committed. With the SQLite and
Postgres adapters the binary is about 30 MB; the admin app is embedded in it.

Ship these next to the binary, and run it from that directory (relative paths resolve there, or set `CONFIG_DIR`):

| What | Why |
| --- | --- |
| `config/` | `app.yaml`, `channels.yaml`, `env/<APP_ENV>.yaml`. |
| `schema/` | The content model, read when the server starts. |
| `site/` | Templates and assets, when the [site module](site.md) is used. |
| `hooks/*.wasm` | Compiled [WebAssembly hooks](wasm-hooks.md), if any. |
| `admin/dist/` | Built [admin extensions](admin-extensions.md), if any. |

## Configure

Run with `APP_ENV=prod` (any name but `dev`). Secrets must then come from the environment; there are no development
fallbacks. The starter `config/app.yaml` reads these:

| Variable | Meaning |
| --- | --- |
| `DATABASE_URL` | `postgres://user:pass@host:5432/db?sslmode=require`, or `sqlite:/data/hydrogen.db`. |
| `PREVIEW_SECRET` | At least 32 random characters; signs preview tokens. The same on every server. |
| `MANAGE_TOKEN` | Optional bearer token with every right, for bootstrapping and automation. Use a long random value, or remove `manage.token` so only users and API tokens sign in. |
| `LISTEN_ADDR` | Default `:8080`. |

`hydrogen config:check` with the production environment catches a missing secret before you deploy. See
[configuration](configuration.md) for everything else.

## Database

- **Postgres** (the production choice): any recent version. Several servers can share one database. The URL is a
  standard Postgres URL; connection pool settings are the pgx ones (`?pool_max_conns=20`). Each server keeps a few
  connections for notifications and locks besides its pool, so size `max_connections` for servers times pool.
- **SQLite**: one file, one node. Writes go through one connection, reads through a separate pool, in WAL mode. It
  suits small sites and single-server deployments. Keep the file on a local disk (not a network file system) and
  never run two servers on it.

## Migrations

Outside `dev` the server does not change the database by itself. The deploy order is:

```sh
bin/mysite migrate          # database schema of the core and modules: idempotent, prints what it applied
bin/mysite content:migrate  # only when you changed the schema in a breaking way (see hooks: content migrations)
bin/mysite serve
```

`serve` refuses to start while database migrations or content migrations are pending, and `GET /readyz` answers `503`
until the database is migrated, so a load balancer holds traffic back from a node that is not ready. Run `migrate`
once per release, from a release step or an init container, before starting the new version. After a schema change
that alters what is published and needs no migration, run `content:reindex` once.

The first administrator is created with a command, not an HTTP call:

```sh
echo "a long passphrase" | bin/mysite users:create-admin alice
```

## Docker

There is no official image: the binary is the image. A multi-stage build keeps it small. This runs as a non-root user
and keeps SQLite (or uploads) in `/data`:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/mysite . && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=65532:65532 /data /data
COPY --from=build /out/mysite /app/mysite
COPY config /app/config
COPY schema /app/schema
ENV APP_ENV=prod LISTEN_ADDR=:8080 DATABASE_URL=sqlite:/data/hydrogen.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/app/mysite"]
CMD ["serve"]
```

```sh
docker run --rm -v hydrogen-data:/data -e MANAGE_TOKEN=… -e PREVIEW_SECRET=… mysite migrate
docker run -d  -v hydrogen-data:/data -e MANAGE_TOKEN=… -e PREVIEW_SECRET=… -p 8080:8080 mysite
```

With Postgres, drop the volume and pass `DATABASE_URL`. A compose file with a database next to it:

```yaml
services:
  db:
    image: postgres:17
    environment: { POSTGRES_USER: hydrogen, POSTGRES_PASSWORD: ${DB_PASSWORD}, POSTGRES_DB: hydrogen }
    volumes: [db:/var/lib/postgresql/data]
  app:
    build: .
    environment:
      DATABASE_URL: postgres://hydrogen:${DB_PASSWORD}@db:5432/hydrogen?sslmode=disable
      PREVIEW_SECRET: ${PREVIEW_SECRET}
      MANAGE_TOKEN: ${MANAGE_TOKEN}
    ports: ["8080:8080"]
    depends_on: [db]
volumes:
  db:
```

(Run `migrate` once with `docker compose run --rm app migrate` before the first `up`.)

## Behind a proxy or CDN

- **TLS** ends at your proxy. Session cookies are `Secure`, so the admin works only over HTTPS (in `dev`,
  `auth.insecure_cookies` relaxes that). Set `http.trusted_proxies` to the proxy's address range, so login throttling
  and session records see client addresses instead of the proxy's.
- **Streams.** Collaboration and live previews use Server-Sent Events. The server sends `X-Accel-Buffering: no`; make
  sure the proxy neither buffers nor times out idle streams too early.
- **Uploads** go through the proxy: raise its body limit to at least `media.max_upload` (default 50 MB).
- **Caching.** Delivery responses are `public` with `s-maxage` and carry `Surrogate-Key`. A CDN can cache
  `/api/deliver/v1/*` and be purged by tag from the webhook ([delivery API](delivery-api.md#caching)). Token channels
  are `private`; do not cache them shared. Do not cache `/api/manage/v1` or the admin.
- **Separate hosts.** Serve the admin and API from their own host, away from the public site, when the site module or
  site-proxy would otherwise share an origin with editors' sessions (the server warns at start-up).
- **Health.** Use `GET /healthz` (the process is up) for liveness and `GET /readyz` (database reachable, no pending
  migrations) for readiness.

## Files

Uploads go to `media.storage`. The `local` driver writes to a directory (`data/media`), which must be persistent and,
with several servers, shared. For anything beyond one server use the `s3` driver (any S3-compatible service, with
`path_style: true` for MinIO) and set `media.public_url` to the CDN in front of the bucket. See [media](media.md). Files
are named by the SHA-256 of their content, so they cache forever.

## Scaling

One server handles a lot: reads come from published snapshots and an in-process cache. When you need more, or
rolling deploys without downtime, use Postgres and run several identical servers behind a load balancer. Everything
that must happen once is coordinated through the database:

- The **outbox** hands each event to one worker at a time (`FOR UPDATE SKIP LOCKED`, with a lease), so webhooks, cache
  purges and indexing run once per event across servers, at least once overall. Handlers are idempotent.
- **Scheduled jobs** take a lock per tick: one server runs each job per tick. `jobs.enabled: false` keeps a node out
  of it (for example web-only nodes next to one worker).
- **Cache purges** travel over `LISTEN/NOTIFY`, so every server drops stale responses at once. Sessions and
  permissions live in the database, and a role change reaches all servers.
- **Structural writes** (the page tree, collaboration sessions) use advisory locks.

SQLite cannot do any of this and is for one node only. Run each server with the same `PREVIEW_SECRET`, `manage.token`
and configuration files.

## Backups and export

Back up the database like any other: `pg_dump` or continuous archiving for Postgres, and for SQLite use
`sqlite3 hydrogen.db ".backup out.db"` (a plain copy of a live WAL database can be inconsistent). Back up the media
directory or bucket too. That restores everything, accounts included.

`export` is the portable, database-independent copy of the *content*:

```sh
bin/mysite export /backups/2026-10-19              # items, every version and draft, schema revisions, uploads
bin/mysite export -published-only /tmp/site        # only what is published
bin/mysite import /backups/2026-10-19              # into an empty, migrated database
```

An export is a directory of NDJSON files, a manifest with SHA-256 checksums (verified before an import writes
anything) and the uploaded files. It also holds each module's data (the page tree, redirects). Users, roles, tokens,
sessions and the audit log are not exported: create administrators again with `users:create-admin`. Import refuses a
non-empty database, and an export made under a different schema unless you pass `-allow-schema-change`. An export can
move a site between SQLite and Postgres. Exports never hold trashed items.

## Operating

- **Logs** go to stderr, as JSON outside `dev` (`log.format`, `log.level`). Every request failure with status 500 is
  logged with its cause; the API answers `{"error": {"code": "internal"}}` only.
- **Shutdown** on `SIGTERM` or `SIGINT` drains requests for up to `http.shutdown_timeout` (15 seconds) and stops the
  modules.
- **Upgrades.** Update the Hydrogen version in `go.mod`, rebuild, run `migrate`, then restart. `hydrogen modules
  -versions` shows what is compiled in, and `GET /api/manage/v1/system` (policy `system.read`) shows it on a running
  server. Read the changelog for steps a release needs, such as `content:reindex`.
- **Security.** Keep `manage.token` out of the repository and rotate it by changing the variable. Prefer API tokens with
  narrow roles for automation. Set `admin.enabled: false` on servers that should not expose the admin.
