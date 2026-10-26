# fullstack

The starter project served by one binary: the admin (`/admin`), the APIs (`/api`) and the website, rendered by the `site`
module with Go `html/template`. It uses the schema and fixture of `examples/starter` (`SCHEMA_DIR` defaults to
`../starter/schema`) and the same two web channels (`/en`, `/de`), now with `render: go`. It exercises the `site`
module: a project theme in `theme/` that builds on the embedded starter theme, template overrides in
`site/templates`, custom routes (`/blog` and `/blog/{slug}` from `config/app.yaml`; `/search`, `/latest` and `/colophon`
in `routes.go`), a templ-style component (`colophon.templ`), live preview and visual editing in the admin, and the
`site-export` module for static output. Start here for a website that needs no separate frontend.

## Prerequisites

- Go 1.26 or later.
- `make admin` once from the repo root (needs Node): the admin is embedded and `admin/dist` is not committed. Rebuild
  the Go binary after it.
- Postgres is optional: `make db-up`, then
  `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable`.

## Run it

From the repo root:

```sh
cd examples/fullstack
mkdir -p .work
go build -o .work/fullstack .
.work/fullstack migrate
.work/fullstack import ../starter/fixture
.work/fullstack site:check -db
.work/fullstack serve
```

`site:check -db` renders every template against the imported content and schema-generated data (about 45 renders).
Defaults: `LISTEN_ADDR=:8080`, `DATABASE_URL=sqlite:.work/hydrogen.db`, `SCHEMA_DIR=../starter/schema`,
`MANAGE_TOKEN=dev-token`, `SITE_URL=http://localhost:8080` (the website's public origin: canonical and hreflang URLs
and where the admin opens previews), `ADMIN_URL` (the admin's origin; required outside dev). It runs in the dev
environment: the manage token is accepted from loopback only and the preview secret is derived from it. For another
port set both: `LISTEN_ADDR=127.0.0.1:9000 SITE_URL=http://127.0.0.1:9000`.

Sign in to the admin with the `#setup=` link the server logs, or run
`HYDROGEN_ADMIN_PASSWORD=change-me-please .work/fullstack users:create-admin admin`.

## Open and try

- Website: `http://localhost:8080/en/`, `/en/about`, `/de/`, `/en/blog`, `/en/blog/building-with-blocks`,
  `/en/search?q=blocks`, `/en/latest`, `/en/colophon`. `/` redirects to `/en`. Unknown paths answer a 404 page.
- Admin: `http://localhost:8080/admin`. Open a page, edit a block and use the preview pane: the site is the preview
  frame, and the canvas lets you select, drag and edit text on the page itself.
- Publish a post and watch the website change (the page cache is purged by the same notifications as the delivery
  cache):

  ```sh
  M=localhost:8080/api/manage/v1
  H='Authorization: Bearer dev-token'
  curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/collections/post/items \
    -d '{"locale":"en","fields":{"title":"Fresh from curl","slug":"fresh-from-curl","excerpt":"Hello."}}'
  curl -s -X POST -H "$H" -H 'Content-Type: application/json' $M/items/<id>/publish -d '{}'
  curl -s localhost:8080/en/blog | grep 'Fresh from curl'
  ```
- The delivery API is on the same port, for example `curl -s 'localhost:8080/api/deliver/v1/tree?channel=en'`.
- Change a template: copy a file from the embedded theme into `site/templates` (for example `pages/default.html`) or edit
  `site/templates/routes/latest.html`, restart and reload. `docs/site.md` lists the template functions and helpers.
- `.work/fullstack site:export` renders the whole site to static files in `site-export/` (not gitignored: delete it
  afterwards, or see `docs/site-export.md` for `-out` and the publishers).

## Test and reset

`go test ./...` runs the project on SQLite and, when a database is up (`make db-up`), on Postgres. `make site-e2e` (repo
root) builds this project, imports the fixture and runs the adapter conformance suite, including browser checks; run
e2e scripts one at a time. To reset, delete `.work/hydrogen.db` (and its `-shm` and `-wal` files, if present) and
import again (`import` needs an empty database).

## Gotchas

- Use one hostname for the admin and the site. Open both as `localhost`, or both as `127.0.0.1`, and make `SITE_URL`
  match: with `localhost` for one and `127.0.0.1` for the other the browser drops the preview cookie in the frame.
- The website shares the admin's origin here, so the server warns at start: the website's scripts are trusted like
  project code. For production serve the website from a separate host or port and set `ADMIN_URL`.
- The schema belongs to `examples/starter`: change it there (and run `make starter-fixture`), or copy the directory
  into this project and set `SCHEMA_DIR`.
- `colophon_templ.go` is generated from `colophon.templ`; never edit it by hand.
