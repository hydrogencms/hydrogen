# minimal

The smallest Hydrogen project: `main.go` registers the SQLite and Postgres adapters and nothing else. There is no
schema and no channels file, so it has no content types. It shows what the core gives every project: the management
and delivery APIs, the admin, sign-in and API tokens, migrations, health checks and `config:check`. Start here to see
the bare server, or as the base for a project that adds its own schema and modules.

## Prerequisites

- Go 1.26 or later.
- `make admin` once from the repo root (needs Node). The admin is embedded in the binary and `admin/dist` is not
  committed; without it the server serves an "admin not built" page. Rebuild the Go binary after `make admin`.
- Postgres is optional: run `make db-up`, then set
  `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable`. The default is a SQLite file.

## Run it

From the repo root:

```sh
cd examples/minimal
mkdir -p .work
go build -o .work/minimal .
.work/minimal config:check
.work/minimal migrate
.work/minimal serve
```

It listens on `:8080` (`LISTEN_ADDR`) and stores data in `.work/hydrogen.db` (`DATABASE_URL`). The example runs in the
dev environment: the manage token is `dev-token` (`MANAGE_TOKEN`), accepted only from loopback, and the preview secret
is derived from it.

## Open and try

- `http://localhost:8080/healthz` and `/readyz` answer `ok`.
- `http://localhost:8080/admin`: on first start the server logs a `…/admin/login#setup=<token>` link that creates the
  first administrator. Or create one from the shell:
  `HYDROGEN_ADMIN_PASSWORD=change-me-please .work/minimal users:create-admin admin`.
- `curl -H 'Authorization: Bearer dev-token' localhost:8080/api/manage/v1/system` lists the core version and the
  compiled-in modules. `/api/manage/v1/openapi.json` (same header) is the OpenAPI document.
- `curl -H 'Authorization: Bearer dev-token' localhost:8080/api/manage/v1/types` returns `[]`. To give the project
  types, add a `schema/` directory and `content.schema_dir: schema` to `config/app.yaml` (see `examples/headless`).

## Test and reset

`go test ./...` in this directory boots the server on SQLite. The Postgres run is skipped unless a database is up
(`make db-up`, or `HYDROGEN_TEST_POSTGRES_URL`). To reset, delete `.work/hydrogen.db` (and the `-shm` and `-wal`
files next to it, if present).

## Gotchas

- Binaries and `.work/` are gitignored.
- Outside dev, `delivery.preview_secret` (32+ characters, `PREVIEW_SECRET`) is required.
