# Configuration

A project is configured by YAML files in `config/` and by environment variables. The files describe the project; the
environment carries secrets and what differs between machines. `hydrogen config:check` validates everything without
starting the server.

| File | Holds |
| --- | --- |
| `config/app.yaml` | The server: `http`, `database`, `content`, `manage`, `auth`, `delivery`, `admin`, `media`, `webhooks` and one `modules.<name>` section per module. |
| `config/channels.yaml` | Languages and delivery channels. Read as the `channels` section. See [Channels](#channels). |
| `config/env/<APP_ENV>.yaml` | Optional overlay merged over the two files above for one environment. |
| `schema/*.yaml` | The content model. See [schema](schema.md). |

`CONFIG_DIR` changes the directory (default `config`). A missing file is skipped; a missing `channels.yaml` means one
channel called `default`.

## Environment variables in files

A scalar may contain `${env:NAME}` or `${env:NAME:-default}`. Without a default, an unset variable fails with the file
and line. Quote a value to keep it a string; an unquoted one takes the type of the substituted text (a number, a
boolean).

```yaml
http:
  addr: ${env:LISTEN_ADDR:-:8080}
database:
  url: ${env:DATABASE_URL:-sqlite:.work/hydrogen.db}
```

Any key can also be overridden from the process environment: `HYDROGEN__A__B=v` sets `a.b`, with segments separated
by two underscores and lower-cased (`HYDROGEN__LOG__LEVEL=debug`). An override replaces one scalar. It cannot set a
list, and it fails when it names a key that does not exist.

Decoding is strict: an unknown key, a wrong type or a value that is not a duration (`30s`, `5m`, `720h`) or a size
(`512KB`, `50MB`, `2GB`) fails with `file:line`. A top-level key or `modules.<name>` section that nothing reads, such as
the configuration of a module this build does not include, fails too.

## Secrets and environments

`APP_ENV` names the environment (default `dev`; lower-case letters, digits, `_` and `-`). It picks the overlay file
`config/env/<APP_ENV>.yaml`, and it changes what is allowed:

- A **secret** key must get its value from `${env:NAME}`; a literal in the file is rejected. The secret keys are
  `database.url`, `manage.token`, `delivery.preview_secret`, `media.storage.access_key`, `secret_key` and
  `session_token`, every webhook `secret`, and channel `tokens`.
- A default (`${env:NAME:-default}`) on a secret only applies in `dev`. In any other environment the variable must be
  set, or startup and `config:check` fail naming it.
- Outside `dev`: `database.url` is required, `delivery.preview_secret` must be at least 32 characters, webhook URLs
  must be `https`, `auth.insecure_cookies` is refused, and the server will not start with pending migrations or
  content migrations.
- In `dev`: the database defaults to `sqlite:.work/hydrogen.db`, `manage.token` to `dev-token` (accepted only from
  loopback peers, and the server warns), the preview secret is derived from the token, logs are text, and migrations
  apply when the server starts.

```yaml
# config/env/prod.yaml
log: { level: info, format: json }
http: { trusted_proxies: [10.0.0.0/8] }
admin: { enabled: false }
```

## app.yaml reference

Defaults are shown where there is one.

### `http`

| Key | Default | Meaning |
| --- | --- | --- |
| `addr` | `:8080` | Listen address. |
| `read_header_timeout` | `10s` | Time allowed to read request headers. |
| `shutdown_timeout` | `15s` | Time allowed for in-flight requests and modules to stop. |
| `trusted_proxies` | none | CIDRs or addresses of reverse proxies whose `X-Forwarded-For` is believed, for login throttling and session records. |

### `database`

| Key | Meaning |
| --- | --- |
| `url` | `sqlite:<path>` (relative to the working directory, or absolute) or a Postgres URL (`postgres://user:pass@host:5432/db`). The scheme must belong to a module in the build (`sqlite`, `postgres`). Secret. |

### `log`

`level` is `debug`, `info` (default), `warn` or `error`. `format` is `text` or `json`; the default is `text` in `dev` and
`json` elsewhere. Logs go to stderr.

### `outbox` and `jobs`

| Key | Default | Meaning |
| --- | --- | --- |
| `outbox.poll` | `1s` | Fallback polling interval of the outbox workers (they are also woken by commits). |
| `outbox.lease` | `1m` | How long a claimed delivery stays hidden from other workers. |
| `jobs.enabled` | `true` | `false` stops this node running scheduled jobs. `hydrogen job:run` still works. |

### `content`

| Key | Default | Meaning |
| --- | --- | --- |
| `schema_dir` | `schema` | Directory of schema files. |
| `default_locale` | `en` | Main locale of new items. |
| `trash_retention` | `720h` | How long trashed items are kept before the `content.trash` job purges them. `0` keeps them until someone purges them. |
| `files` | none | A directory (relative to the project) that holds the content as an export, kept in git. Needs SQLite; `serve` loads it and `content:save` writes it. See [content in git](content-in-git.md). |
| `review_four_eyes` | `true` | The person who submitted or last edited a draft cannot approve it. See [reviews](reviews.md). |

### `manage` and `auth`

| Key | Default | Meaning |
| --- | --- | --- |
| `manage.token` | none | A bearer token with every right, for automation and bootstrapping, audited as `token:config`. Optional: without it only users and API tokens sign in. Secret. |
| `auth.session_idle` | `12h` | Sessions end after this long without a request. |
| `auth.session_max` | `168h` | Sessions end this long after login, however active. |
| `auth.insecure_cookies` | `false` | Drops the `Secure` flag of the session cookie. `dev` only. |

See [access](access.md) for users, roles and tokens.

### `delivery`

| Key | Default | Meaning |
| --- | --- | --- |
| `preview_secret` | none | Signs preview tokens. At least 32 characters; required outside `dev`. Secret. |
| `cache_bytes` | `33554432` (32 MiB) | Size of the in-process response cache, in bytes (an integer, no unit). `0` turns it off. |
| `share_links` | `true` | Lets editors create [share links](share-links.md). |
| `share_default_ttl`, `share_max_ttl` | `24h`, `168h` | Lifetime of a new share link and the longest one allowed (at most `720h`). |

### `admin`

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `true` | Serve the admin app. |
| `prefix` | `/admin` | Where it is mounted: a leading slash, no trailing one, not under `/api`. |
| `elements`, `theme`, `strings`, `widgets`, `cells`, `panels`, `actions`, `dashboard`, `views`, `slots`, `previews`, `list_actions` | none | The extension manifest. See [admin extensions](admin-extensions.md). |
| `elements_dev_entry` | `/src/elements.ts` | Vite entry used with `ADMIN_ELEMENTS_DEV_URL` in `dev`. |

`ADMIN_DEV_URL` and `ADMIN_ELEMENTS_DEV_URL` (dev only) proxy the admin, or your extension elements, to Vite dev servers.

### `collab`

`collab.enabled` (default `true`) turns on [co-editing](collaboration.md).

### `media`

Used when the schema has an upload type or a `file` field. See [media](media.md).

| Key | Default | Meaning |
| --- | --- | --- |
| `max_upload` | `50MB` | Largest upload; a type's `upload.max_size` can only lower it. |
| `public_url` | none | Base URL of file links in delivery responses, when a CDN serves `/assets`. |
| `storage.driver` | `local` | `local` or `s3`. |
| `storage.dir` | `data/media` | Directory of the local driver. |
| `storage.bucket`, `region`, `endpoint`, `path_style`, `prefix` | none | The `s3` driver: any S3-compatible service. `path_style: true` for MinIO and most self-hosted ones. |
| `storage.access_key`, `secret_key`, `session_token` | none | S3 credentials. Secrets. |

### `webhooks`

Each entry delivers outbox events to an HTTPS endpoint, signed, with retries. The name is lower-case letters, digits,
`-` and `_`.

```yaml
webhooks:
  frontend:
    url: https://www.example.com/api/hydrogen-webhook
    events: ["*"]                       # topics: item.published, item.*, page.changed, or *
    secret: ${env:WEBHOOK_SECRET}
    timeout: 10s                        # default 10s, at most 2m
```

See [the delivery API](delivery-api.md#webhooks) for the payload and signature.

### `modules`

Each module reads `modules.<name>` (for example `modules.pages`, `modules.site`); the section is described on the
module's page. The catalog is in [modules](modules.md). A section for a module the build does not include fails.

```yaml
modules:
  pages:
    sites: [main]
```

## Channels

A **channel** is one consumer of content: a website, a language of it, an app, an integration. `config/channels.yaml`
defines them. The file has five top-level keys, all optional:

```yaml
languages:                    # the registry; channels may only use these codes
  en: {}
  de: {}
  de-CH: { inherits: de }     # a market overlay; needs the overlays module (see locales)

defaults:                     # inherited by every channel
  settings: { delivery: { cache_ttl: 300 } }

groups:                       # shared settings of several channels
  web:
    site: main
    frontend: ${env:SITE_URL:-http://localhost:3000}
    reserved: [/api/*, /blog/*]
    links: { post: "/blog/{slug}" }
    preview: { "*": "/preview/{collection}/{id}" }

channels:
  en: { group: web, languages: [en],     match: { prefix: /en } }
  de: { group: web, languages: [de, en], match: { prefix: /de } }
  ios:
    languages: [en, de]
    collections: [article]
    access: token
    tokens: { v1: "${env:IOS_TOKEN}" }

templates:                    # one channel per combination of the `for` values
  - for: { market: [sg] }
    name: "web_{market}"
    group: web
    languages: ["en-{MARKET}", en]
```

A channel takes each key from its own entry, then its group, then `defaults`; the first that sets it wins, except
`settings`, which are merged key by key (the channel's value wins). `group` and `tokens` belong to the channel itself.
Channel names are lower-case letters, digits and `_`. Once `channels.yaml` defines channels, each needs `languages`
(on itself, its group or `defaults`).

| Key | Meaning |
| --- | --- |
| `group` | The group whose settings this channel inherits. |
| `languages` | Ordered fallback: the first language an item is available in is served. Codes like `en` or `de-CH`; must be in the registry when one exists. |
| `collections` | Content types the channel may read. Omit for all. |
| `settings` | Free-form values. `delivery.cache_ttl` (seconds, default 60) sets the `s-maxage` and `stale-while-revalidate` of responses. Frontends read the rest from the channel's public info. |
| `frontend` | The frontend's `http(s)` URL, without credentials or query. Used for preview links, the admin's preview frame and CORS. |
| `preview` | Collection to path pattern for previews, with `{id}`, `{locale}`, `{collection}` and any field of the item (`{slug}`). `"*"` is a fallback. |
| `links` | Collection to path pattern for items that have no page route. Like `preview`, without the fallback. They also serve as preview patterns. |
| `access` | `public` (default) or `token`. A token channel is read only with its bearer token. |
| `tokens` | For `access: token`: a mapping of name to secret. Secrets must come from `${env:…}`, have at least 16 characters, no whitespace, and cannot be shared by two channels. |

Modules add keys. The pages module adds `site` (one of `modules.pages.sites`), `match` (`host`, `prefix` and/or a
`header: {name, value}`; two channels cannot match the same way; `prefix: /` serves every path, so a site with one web
channel needs only `match: { prefix: / }`, and a more specific channel still wins where it fits) and `reserved` (paths the frontend owns, such as
`/cart` or `/account/*`). The site module adds `render: go`; the images module adds `image_presets`. A key no module
registered fails with the list of known ones.

With no `channels.yaml` there is one channel, `default`, serving every collection in the default locale to the public.
Languages, fallbacks and overlays are in [locales](locales.md); how a frontend picks a channel is in
[the delivery API](delivery-api.md#channels).

## Checking

```sh
hydrogen config:check          # files, schema, channels, module wiring; prints warnings
hydrogen config:check -db      # also connects, reports pending migrations and unaccounted breaking schema changes
```

It prints `config ok (env <name>)` on success and the first error with its file and line otherwise.
