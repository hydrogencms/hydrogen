# Content in git

Some sites keep their content in the repository next to the code. The directory written by `export` is the source of
truth and is committed. A local SQLite database is the working copy that editors use in the admin. A build in CI
imports the directory into a fresh database and builds the site from it.

Use it when the content is small and edited by few people, when you want content changes reviewed like code, or when
a static site is built from the repository. It is not for several servers or many simultaneous editors: use Postgres
and a normal deployment for those.

## Set up

Point `content.files` at a directory in the project and use SQLite:

```yaml
# config/app.yaml
content:
  files: content
database:
  url: ${env:DATABASE_URL:-sqlite:.work/hydrogen.db}
```

`content.files` is relative to the project directory. Any other database makes `config:check` and `serve` fail:
the directory is loaded into a SQLite file the project owns. Add `.work/` to `.gitignore`; the database, its backups
and the sync marker live there.

Run `hydrogen dev`. With no database yet and no directory, nothing is loaded. Edit content in the admin, then save
it.

`hydrogen new -static` creates a project set up this way, with an Astro site and a GitHub Pages workflow (see
[getting started](getting-started.md#a-static-site)).

## The loop

```sh
hydrogen dev                 # edit in the admin
hydrogen content:save        # write the database's content to content/
git add content && git commit
```

`content:save` exports to a temporary directory next to `content/` and swaps it in, so a failure never leaves a
half-written directory. When the content did not change it leaves the files alone (the export's timestamp does not
make a diff). It works while the server runs.

After `git pull` or a checkout that changes `content/`, restart `hydrogen dev` (or run `serve`): the start loads the
new content. Users and sessions are not part of an export, so loading a changed directory also resets them; the
first-run link creates the administrator again.

## What each start does

A marker beside the database (`<database>.content-sync`) records the content the database and the directory last
agreed on. `serve` compares both with it before it accepts requests:

| Database | Directory | The start |
| --- | --- | --- |
| none, or empty | has content | migrates, imports the directory, records the marker |
| same content as the directory | | records the marker and starts |
| unchanged since the marker | changed (a pull) | moves the database to a backup, rebuilds it from the directory, logs it |
| changed since the marker | unchanged | keeps the database and logs a reminder to run `content:save` |
| changed | changed, or never synced and different | refuses to start |

The last row prints the two ways out:

- keep the database: `hydrogen content:save`, then review the change with `git diff content`;
- keep the directory: `hydrogen content:load -force`, which moves the database to a backup first.

The check is cheap. The marker also holds the newest audit event it saw, so a restart that finds no newer content
event does not export anything. Sign-ins and changes to users, roles and tokens do not count. Only when a content
event is newer does the start export the database to a scratch directory and compare fingerprints, and an edit that
was reverted by hand counts as unchanged. The fingerprint is a hash of the export's manifest, which lists the
SHA-256 of every file, without the export time, the Hydrogen version and the database dialect. The directory is
fingerprinted from its manifest and the sizes of its files; the checksums are verified when it is imported.

## Commands

| Command | Does |
| --- | --- |
| `content:save` | Writes the database's content to `content.files` and records the marker. |
| `content:load [-force]` | Replaces the database with the directory's content, after moving the old one to a backup. Refuses when the database holds changes that are not saved, unless `-force`. Refuses while a server answers on `http.addr`; stop the server first (a server on another address is not detected, so stop it yourself). |

## Backups

A database is never deleted. Before it is replaced the server (or `content:load`) closes it and moves the database
file and its `-wal` and `-shm` files to `backups/<UTC timestamp>/` next to it (by default `.work/backups/…`), and
logs the path. Delete old backups yourself.

## CI

Check out the repository and start the server on a fresh database. The start migrates and imports `content/`, even
with `APP_ENV=production`:

```sh
export APP_ENV=production DATABASE_URL=sqlite:.work/hydrogen.db
export MANAGE_TOKEN=… PREVIEW_SECRET=…          # required outside dev
bin/mysite serve &                                # imports content/ on start
until curl -sf localhost:8080/healthz; do sleep 0.2; done
bin/mysite site:export out                        # or fetch the delivery API from your frontend build
```

Anything built from the running server then sees exactly the committed content. In a fresh checkout there is no
database or marker, so the directory is loaded as is.
