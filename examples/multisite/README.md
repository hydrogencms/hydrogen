# multisite

One deployment serving several websites: two page sites (`corporate`, `careers`), five web channels and a token
channel, defined in `config/channels.yaml`. `corporate_en`, `corporate_de` and `corporate_dech` share the host
`www.acme.com` on the path prefixes `/en`, `/de` and `/de-ch`; `careers_en` owns `jobs.acme.com`; a `templates:` entry
generates one channel per market (`www.acme.sg`); and `ios_app` is a token-protected channel with no URLs. Languages
inherit (`de-CH` from `de`, `en-SG` from `en`) through the `overlays` module. It exercises the `pages`, `overlays` and
`images` modules, channel matching and settings cascade, reserved paths, hreflang and canonical URLs, and token
channels. It is also the CMS behind `examples/next-site`. Start here for multi-site, multi-language or multi-market
projects.

## Prerequisites

- Go 1.26 or later.
- `make admin` once from the repo root (needs Node): the admin is embedded and `admin/dist` is not committed.
- `jq` for the commands below (only to pick ids out of responses).
- Postgres is optional: `make db-up`, then
  `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable`.

## Run it

From the repo root:

```sh
cd examples/multisite
mkdir -p .work
go build -o .work/multisite .
.work/multisite migrate
.work/multisite serve
```

Defaults: `LISTEN_ADDR=:8080`, `DATABASE_URL=sqlite:.work/hydrogen.db`, `SCHEMA_DIR=schema`,
`MANAGE_TOKEN=dev-token`, `IOS_TOKEN=dev-ios-token-0001`. It runs in the dev environment: the manage token is accepted
from loopback only and the preview secret is derived from it. There is no fixture. Sign in at
`http://localhost:8080/admin` with the `#setup=` link the server logs, or run
`HYDROGEN_ADMIN_PASSWORD=change-me-please .work/multisite users:create-admin admin`.

## Try it

The channels match by host and path prefix, but the CMS serves everything on one port, so you select a channel with
`?channel=` on the delivery API and give the public URL you want resolved. No `/etc/hosts` entries or `Host` headers
are needed. Build a small corporate site:

```sh
M=localhost:8080/api/manage/v1
D=localhost:8080/api/deliver/v1
post() { curl -fsS -X POST -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' "$M$1" -d "$2"; }

home=$(post /collections/page/items '{"locale":"en","fields":{"title":"Home"}}' | jq -r .id)
post /items/$home/publish '{}' >/dev/null
homeNode=$(post /pages/sites/corporate/nodes "{\"kind\":\"page\",\"item\":\"$home\"}" | jq -r .id)

about=$(post /collections/page/items '{"locale":"en","fields":{"title":"About us","intro":"Who we are."}}' | jq -r .id)
post /items/$about/publish '{}' >/dev/null
post /pages/sites/corporate/nodes "{\"kind\":\"page\",\"item\":\"$about\",\"parent\":\"$homeNode\"}" >/dev/null
```

Then:

```sh
# resolve a public URL on a channel: the item, breadcrumbs, canonical URL and hreflang alternates
curl -s "$D/route?channel=corporate_en&url=https%3A%2F%2Fwww.acme.com%2Fen%2Fabout-us"

# the navigation tree of a channel
curl -s "$D/tree?channel=corporate_en&depth=2"

# which channel serves a URL (management API): host and prefix decide
curl -s -H 'Authorization: Bearer dev-token' "$M/pages/locate?url=https%3A%2F%2Fwww.acme.com%2Fen%2Fabout-us"
curl -s -H 'Authorization: Bearer dev-token' "$M/pages/locate?url=https%3A%2F%2Fwww.acme.sg%2Fabout-us"

# the app channel needs its token
curl -s -o /dev/null -w '%{http_code}\n' "$D/collections/product/items?channel=ios_app"
curl -s -H 'Authorization: Bearer dev-ios-token-0001' "$D/collections/product/items"
```

The alternates in the first response list `www.acme.com/en/about-us` and `www.acme.sg/about-us`. Add a German
translation (`PATCH $M/items/$about` with `{"locale":"de","fields":{"title":"Uber uns"}}`), publish that locale
(`POST $M/items/$about/publish` with `{"locales":["de"]}`) and `corporate_de` resolves
`https://www.acme.com/de/uber-uns`. To serve these hosts for real, point a frontend at the CMS (`examples/next-site` does).

## Test and reset

`go test ./...` runs the management, delivery, access, export and client scenarios on SQLite and, when a database is up
(`make db-up`), on Postgres. `make next-e2e` (repo root) runs `examples/next-site` against this project. To reset,
delete `.work/hydrogen.db` (and its `-shm` and `-wal` files, if present).

## Gotchas

- `reserved:` paths (`/cart`, `/account/*`) belong to the frontend; pages cannot take them.
- A request must name a channel (`?channel=`) or resolve through `/pages/locate`; `ios_app` is the only token channel.
- `gen/` is generated code (`hydrogen generate`); never edit it by hand.
