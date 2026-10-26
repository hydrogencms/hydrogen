# content

The content-modelling example: `article` with every field type, `person`, `image` (an upload type), `page` and
`landing` page types with blocks, custom rich-text nodes and marks (`schema/richtext.yaml`), and a `settings` singleton. On top of the schema it shows what a project adds in Go
(`main.go`): a typed `BeforeSave` hook (`article.readingMinutes` from the summary), a content migration
(`person.fullName` becomes `firstName` and `lastName`), a custom policy (`import.run`) and the `seo` module in `seo/`,
which adds an `seo` group to every page type through a schema transform. It registers the `pages`, `images` and
`graphql` modules, and `admin/` holds one custom element for every admin extension point (wired in the `admin:` block
of `config/app.yaml`). Start here to see what the schema language can express and how project code and the admin are
extended.

## Prerequisites

- Go 1.26 or later.
- `make admin` once from the repo root (needs Node): the admin is embedded and `admin/dist` is not committed.
- The extension bundle `admin/dist/elements.js` is committed; rebuild it only after changing `admin/src` with
  `make example-admin`.
- Postgres is optional: `make db-up`, then
  `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable`.

## Run it

From the repo root:

```sh
cd examples/content
mkdir -p .work
go build -o .work/content .
.work/content config:check
.work/content migrate
.work/content serve
```

Defaults: `LISTEN_ADDR=:8080`, `DATABASE_URL=sqlite:.work/hydrogen.db`, `SCHEMA_DIR=schema`,
`MANAGE_TOKEN=dev-token`. The example runs in the dev environment: the manage token is accepted from loopback only and
the preview secret is derived from it. There is no fixture; create content in the admin or over the API.

Sign in at `http://localhost:8080/admin` with the `#setup=` link the server logs, or create an administrator:
`HYDROGEN_ADMIN_PASSWORD=change-me-please .work/content users:create-admin admin`.

## Try it

- Open the admin, create an article and watch the extensions: the brand-colour widget on `color`, the swatch column
  in the list, the SEO panel and the "Send to translation" action in the editor, the dashboard widget and the
  Tools / Import view.
- Create and publish an article over the API. Replace `<id>` with the id from the first response:

  ```sh
  M=localhost:8080/api/manage/v1
  H='Authorization: Bearer dev-token'
  curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/collections/article/items \
    -d '{"locale":"en","fields":{"title":"Launch day","summary":"We shipped. It went well.","kind":"news"}}'
  curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/items/<id>/publish -d '{}'
  curl -s localhost:8080/api/deliver/v1/collections/article/items
  ```

  The response already has `readingMinutes`, set by the hook.
- Read the same data as GraphQL:
  `curl -sG localhost:8080/api/deliver/v1/graphql --data-urlencode 'query={ articleList { items { id title readingMinutes } } }'`.
  The schema is at `/api/deliver/v1/graphql/schema.graphql`.
- Open `/api/manage/v1/openapi.json` (with the bearer header) for the generated management API.
- The `gen/` directory is generated code (`hydrogen generate`; Go in `gen/content`, TypeScript in `gen/types.ts`);
  never edit it by hand.

## Test and reset

`go test ./...` runs the management, delivery, client, access and pages scenarios on SQLite and, when a database is
up (`make db-up`), on Postgres. `schema_v1/` is the earlier schema that the content migration test starts from. To
reset, delete `.work/hydrogen.db` (and its `-shm` and `-wal` files, if present).

## Gotchas

- The project has no channels file, so delivery has one default channel with every language and collection and no web
  matcher. Item URLs are path-only. `examples/multisite` and `examples/starter` show channels.
- `hydrogen config:check` validates the `admin:` manifest: an element tag or `field:cell` name used in the schema
  but not declared there fails startup.
