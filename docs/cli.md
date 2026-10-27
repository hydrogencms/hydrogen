# Command line reference

Hydrogen has two command lines.

- **`hydrogen`** is the developer tool. Install it once (`go install github.com/hydrogencms/hydrogen/cmd/hydrogen@latest`).
  It creates projects and manages their modules, and in a project directory it builds the project and runs one of
  the project's own commands.
- **The project binary** (`bin/<name>`, from `hydrogen build`) is your server. It runs the project commands below and
  needs no Hydrogen installation, so it is what you deploy.

`hydrogen <command>` in a project directory builds the project into `.work/bin/app`, then runs `<command>` there, so
the two are interchangeable: `hydrogen migrate` and `bin/mysite migrate` do the same. Both read `config/` of the
current directory (`CONFIG_DIR`) and the environment (`APP_ENV`, [configuration](configuration.md)).

## Developer commands

| Command | Does |
| --- | --- |
| `hydrogen new [-static] [-module path] [-core dir] [-no-tidy] <dir>` | Creates a project: `go.mod`, `main.go`, `hydrogen.go`, `hydrogen.yaml`, `config/app.yaml`, `schema/`, and an `admin/` starter for admin extensions. `-module` is the Go module path (default: the directory name). `-core <dir>` uses a local Hydrogen checkout through `replace` directives and `file:` npm dependencies instead of published versions. `-no-tidy` skips `go mod tidy`. `-static` creates a static website with its content in git instead: SQLite and the pages module, [content in git](content-in-git.md) with starter content in `content/`, page and block types, an Astro site in `site/` and a GitHub Pages workflow (it also runs `generate`, so `-no-tidy` skips that too, and there is no `admin/`). |
| `hydrogen dev [command args]` | Runs the project with live reload (default command `serve`). A change to Go code, config, schema or SQL rebuilds, regenerates `gen/` and restarts; a failed build keeps the old server running. Go code includes the packages the project compiles from local directories (a `replace` to a local checkout, such as `new -core` writes, or another module of the `go.work` workspace), so editing a Hydrogen checkout restarts the project too. Dev builds link without debug information (`-ldflags=-w`, a faster link) unless `GOFLAGS` sets `-ldflags`; set it to attach a debugger. |
| `hydrogen generate [-go dir] [-package name] [-ts file]` | Writes typed Go (`gen/content`) and TypeScript (`gen/types.ts`) from the schema, and `modules_gen.go` from `hydrogen.yaml`. `-ts ""` skips TypeScript. |
| `hydrogen add <module\|path[@version]>…` | Adds modules to `hydrogen.yaml` and `go.mod`. |
| `hydrogen remove <module\|path>…` | Removes them. |
| `hydrogen modules` | Lists the catalog; `*` marks the project's modules. |
| `hydrogen modules -versions` | Builds the project and lists the modules really compiled in, with versions. |
| `hydrogen modules init <module\|path>…` | Creates `hydrogen.yaml` for a project that registers modules by hand. |
| `hydrogen build [-o path] [go build flags]` | Compiles the project into `bin/<name>` with `-trimpath` and `CGO_ENABLED=0`. Other flags go to `go build`. |
| `hydrogen hooks build [files…]` | Compiles TypeScript hooks (default `hooks/*.ts`) to `hooks/<name>.wasm` with esbuild and Javy. See [WebAssembly hooks](wasm-hooks.md). |
| `hydrogen version` | Prints the version. |

Module commands are described in [modules](modules.md).

`hydrogen dev` reads `ADMIN_DEV_URL` (proxy the admin app to a Vite dev server) and `ADMIN_ELEMENTS_DEV_URL` (proxy
your [admin extensions](admin-extensions.md)). Both work in the `dev` environment only.

## Project commands

These are built into every project binary. Run them as `hydrogen <command>` or `bin/<name> <command>`; with no
command (or `help`) the binary lists everything it has, module commands included.

| Command | Does |
| --- | --- |
| `serve` | Runs the server. In `dev` it applies pending migrations first; elsewhere it refuses to start with any pending. |
| `migrate` | Applies pending database migrations of the core and the modules, and prints each. Safe to run on every deploy. |
| `config:check [-db]` | Validates configuration, schema, channels and module wiring without serving, and prints warnings. `-db` also connects, reports pending migrations and refuses unaccounted breaking schema changes. |
| `content:migrate` | Applies pending [content migrations](hooks.md#content-migrations), then records the schema revision. |
| `content:reindex` | Rebuilds every item's projections: indexes, search rows, references, snapshots, working copies. Use after a change that alters what is published. |
| `generate [-go dir] [-package name] [-ts file]` | As above. |
| `job:run <name>` | Runs a scheduled job now. It fails when another server holds the job's lock; without a name it prints the job names. |
| `check-frontend -templates <file> [-channel name] [-db]` | Checks that a frontend has a template for every page type and mounted collection its channels can serve. `<file>` is JSON: `{"templates": ["page", "article"]}`. |
| `users:create-admin <login>` | Creates a user, or resets the password of one, with the `administrator` role. The password comes from `HYDROGEN_ADMIN_PASSWORD` or one line of stdin. |
| `tokens:create -name <n> -role <role> [-agent] [-expires 720h]` | Creates an API token with the named roles and prints its secret to stdout, once. The id goes to stderr. |
| `export [-published-only] [-layout ndjson\|items] <dir>` | Writes content and module data to a directory. `-layout items` writes one pretty-printed `items/<type>/<id>.json` per item instead of one `items.ndjson` (what `content:save` uses). See [Backup and portability](deployment.md#backups-and-export). |
| `import [-allow-schema-change] <dir>` | Restores an export into an empty, migrated database. |
| `content:save` | With [`content.files`](content-in-git.md): writes the database's content to the directory, leaving it untouched when nothing changed. |
| `content:load [-force]` | With `content.files`: replaces the database with the directory's content, keeping the old database in a backup. `-force` also discards unsaved changes. |
| `modules` | Lists the modules in this build with their versions. |
| `search:reindex` | Rebuilds the external search index from published content ([search](search.md)). |
| `version` | Prints the Hydrogen version. |

Module commands appear here when the module is in the build. For example `site:check` (the [site module](site.md))
and `site:export` (static export).

Every command exits with `0` on success and `1` on error, printing `error: …` to stderr. `content:migrate`,
`content:reindex`, `import`, `users:create-admin` and `tokens:create` refuse to run while database migrations are
pending: run `migrate` first.

## Environment

| Variable | Used by | Meaning |
| --- | --- | --- |
| `APP_ENV` | all | The environment, default `dev`. Selects `config/env/<APP_ENV>.yaml` and the rules for secrets. |
| `CONFIG_DIR` | all | Configuration directory, default `config`. |
| `HYDROGEN__A__B` | all | Overrides the config key `a.b`. |
| `HYDROGEN_ADMIN_PASSWORD` | `users:create-admin` | The password; else one line of stdin. |
| `LISTEN_ADDR`, `DATABASE_URL`, `MANAGE_TOKEN`, `PREVIEW_SECRET` | `serve` | The variables the starter `config/app.yaml` reads. |
| `ADMIN_DEV_URL`, `ADMIN_ELEMENTS_DEV_URL` | `dev` | Vite dev servers for the admin and for extensions. |
| `JAVY` | `hooks build` | Path to the Javy executable, when it is not on `PATH`. |
