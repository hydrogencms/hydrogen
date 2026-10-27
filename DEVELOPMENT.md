# Developing Hydrogen locally

How to run your working copy of Hydrogen with content, the admin and a frontend, so you can try a change the way a
project would see it. For the checks to run before committing, see [Checks](#checks).

Run `make admin` once after cloning (and again after changing `admin/ui`): the built admin is embedded in the
binary but not committed. Without it the admin URL shows an "admin not built" page and the APIs work as usual.

The projects in `examples/` are listed in `go.work`, so `go run .` in any of them builds against this checkout:
change `core/`, `api/`, `features/` or `modules/`, restart, and the example runs your code.

## Pick a loop

| You are working on | Run |
| --- | --- |
| Core, APIs, pages, blocks, preview, visual editing, the site module | [Fullstack](#fullstack-one-process): one process, no Node |
| The TypeScript SDK, the bridge, a framework adapter | [CMS + Next.js starter](#cms--a-javascript-frontend) |
| The admin app (`admin/ui`) | Either of the above plus [the admin dev server](#the-admin-with-hot-reload) |
| A project of your own | [Point it at this checkout](#your-own-project-on-this-checkout) |

## Fullstack: one process

`examples/fullstack` serves the admin, the APIs and a website rendered by the site module from one port. It uses the
starter's schema and fixture content.

```sh
cd examples/fullstack
go run . migrate
go run . import ../starter/fixture
go run . serve                      # website http://localhost:8080/en, admin http://localhost:8080/admin
```

The first start without a user logs a `…/admin/login#setup=<token>` link: open it to create the first administrator.
Or create one from the command line; the argument is the login and the password is read from stdin (one line, no
prompt) or from `HYDROGEN_ADMIN_PASSWORD`:

```sh
HYDROGEN_ADMIN_PASSWORD='a long password' go run . users:create-admin admin
```

Open a page in the admin and its preview pane shows the site rendered from your working copy.

## CMS + a JavaScript frontend

`examples/starter` is the same content without a renderer; `packages/starters/next` renders it with Next.js. Two
terminals:

```sh
# 1. the CMS
cd examples/starter
go run . migrate && go run . import fixture
go run . serve                      # http://localhost:8080, admin at /admin

# 2. the SDK packages from this checkout, then the starter
cd packages && npm install && npm run build
cd starters/next && npm install
HYDROGEN_URL=http://localhost:8080 npm run dev     # http://localhost:3000/en
```

The starter's channels expect the frontend at `http://localhost:3000` (`STARTER_FRONTEND` changes it). For published
changes to show without a restart, add the revalidation webhook from
[`packages/starters/next/README.md`](packages/starters/next/README.md). The SvelteKit, Nuxt and Astro starters in
`packages/starters/` run the same way; each has a README.

After changing anything in `packages/`, run `npm run build` in `packages/` again; the starters use the built `dist/`.

## The admin with hot reload

```sh
cd admin/ui && npm ci && npm run dev                          # Vite on http://localhost:5173
ADMIN_DEV_URL=http://localhost:5173 go run . serve            # in the example, instead of plain serve
```

The CMS proxies `/admin` to Vite (dev environment only). `admin/dist` is not committed: the binary embeds whatever
`make admin` last built there, and a binary built without it shows an "admin not built" page.

## Your own project on this checkout

To try a change in a project outside the repository before it is released, point its Go modules and npm packages at
this checkout:

```sh
go mod edit -replace github.com/hydrogencms/hydrogen=/path/to/hydrogencms
go mod edit -replace github.com/hydrogencms/hydrogen/modules/sqlite=/path/to/hydrogencms/modules/sqlite
# … one line per module with its own go.mod that the project uses (sqlite, postgres, graphql, wasmhooks)
go mod tidy

npm install /path/to/hydrogencms/packages/client     # and the other @hydrogencms/* packages the frontend uses
```

The project embeds the admin from the checkout, so run `make admin` there first. Only released versions carry the
built admin: a project that requires an unreleased commit (`go get github.com/hydrogencms/hydrogen@main`) gets the
"admin not built" page. Drop the `replace` lines before committing the project. A scratch project inside this repository (`.work/`) needs
`GOWORK=off`, or `go.work` claims it.

## Good to know

- `go run . serve` builds once. After changing Go code, stop it and run it again. `hydrogen dev` (build it with
  `go build -o bin/hydrogen ./cmd/hydrogen`, run `../../bin/hydrogen dev` in the example) restarts on changes to the
  example's own files (schema, config, Go code) and to the packages of this checkout it compiles, so editing
  `core/` or a module restarts it too. A restart takes under a second after a project edit and one to two after an
  edit deep in `core/` (more packages recompile).
- Open the admin and the frontend on the same host name (`localhost` for both, or `127.0.0.1` for both). Otherwise
  the browser treats the preview frame as another site and drops its preview cookie.
- Start over with an empty database: delete the example's `.work/hydrogen.db`, then `migrate` and `import` again.
- Postgres instead of SQLite: `make db-up`, then `DATABASE_URL=postgres://hydrogen:hydrogen@localhost:5436/hydrogen`
  for the example.
- In dev, the management API also accepts the token `dev-token` from loopback, handy for `curl`:
  `curl -H 'Authorization: Bearer dev-token' localhost:8080/api/manage/v1/types`.

## Checks

```sh
go test ./core/content/...          # one package
make test                           # every Go module (Postgres tests skip without make db-up)
make sdk                            # npm packages: build, test, size budgets
make admin                          # admin: typecheck, vitest, build into admin/dist
make check                          # the full gate
```

End-to-end runs exercise the same setups as above:

| Target | What it runs |
| --- | --- |
| `make site-e2e` | the conformance suite against `examples/fullstack` |
| `make starter-next-e2e` | `examples/starter` + the Next.js starter + the conformance suite |
| `make starter-sveltekit-e2e`, `starter-nuxt-e2e`, `starter-astro-e2e` | the other starters |
| `make admin-e2e` | Playwright scenarios in the admin, live preview on the Next starter and the fullstack site |

Run e2e targets one at a time: they share ports and process names.
