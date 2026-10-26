# headless

A project without the pages module, for a frontend that owns its URLs: `post`, `author` and a `settings` singleton,
two languages (`en`, `de`), a `web` channel that turns an item into a preview link from its `preview` patterns, and an
`app` channel protected by a token. It exercises the schema, the management and delivery APIs, channel selection,
preview tokens (drafts read through delivery) and token channels. Start here for a Next.js, mobile or other app that
routes by itself.

## Prerequisites

- Go 1.26 or later.
- `make admin` once from the repo root (needs Node): the admin is embedded and `admin/dist` is not committed.
- Postgres is optional: `make db-up`, then
  `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable`.

## Run it

From the repo root:

```sh
cd examples/headless
mkdir -p .work
go build -o .work/headless .
.work/headless migrate
.work/headless serve
```

Defaults: `LISTEN_ADDR=:8080`, `DATABASE_URL=sqlite:.work/hydrogen.db`, `SCHEMA_DIR=schema`,
`MANAGE_TOKEN=dev-token`, and `APP_TOKEN=dev-app-token-0001` (the `app` channel's token). It runs in the dev
environment: the manage token is accepted from loopback only, and the preview secret is derived from it.

Sign in to the admin at `http://localhost:8080/admin` with the `#setup=` link the server logs, or create an
administrator: `HYDROGEN_ADMIN_PASSWORD=change-me-please .work/headless users:create-admin admin`.

## Try it

Create a post, publish it and read it as the frontend would. Replace `<id>` with the id the first call returns.

```sh
M=localhost:8080/api/manage/v1
H='Authorization: Bearer dev-token'
curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/collections/post/items \
  -d '{"locale":"en","fields":{"title":"Hello","slug":"hello-world","body":"Hi"}}'
curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/items/<id>/publish -d '{}'

curl -s 'localhost:8080/api/deliver/v1/collections/post/items?channel=web'
curl -s -H 'Authorization: Bearer dev-app-token-0001' localhost:8080/api/deliver/v1/collections/post/items
curl -s 'localhost:8080/api/deliver/v1/collections/post/items?channel=app'    # 401: the app channel needs its token
```

A draft is invisible to delivery. Mint a preview token for it; the response carries the frontend URL built from the
channel's `preview` pattern (`post` becomes `/blog/{slug}`, other collections `/preview/{collection}/{id}`):

```sh
curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/preview-tokens \
  -d '{"channel":"web","item":"<id>","locale":"en"}'
```

The `url` is `http://localhost:3000/blog/hello-world?preview=<token>` (`frontend` in `config/channels.yaml`). Pass the
token to delivery as `?preview=<token>` or the `X-Hydrogen-Preview` header to read the draft. A token covers only its
item.

## Test and reset

`go test ./...` runs the scenario on SQLite and, when a database is up (`make db-up`), on Postgres. To reset, delete
`.work/hydrogen.db` (and its `-shm` and `-wal` files, if present).

## Gotchas

- There is no pages module: no `/route` or `/tree`, and no URLs except the preview patterns.
- The `web` channel has no `match`, so select it with `?channel=web`.
