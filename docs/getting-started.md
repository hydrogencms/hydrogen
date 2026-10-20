# Getting started

In about ten minutes you will create a project, describe a content type, sign in to the admin, publish an article and
read it back from the API.

You need Go 1.26 or later. Nothing else: the default database is a SQLite file, and the admin is built into the binary.

## Install the CLI

```sh
go install github.com/hydrogencms/hydrogen/cmd/hydrogen@latest
hydrogen version
```

The `hydrogen` tool creates projects, manages their modules and runs them with live reload. If you work from a checkout
of the Hydrogen repository instead, build it with `go build -o bin/hydrogen ./cmd/hydrogen` and add `-core <checkout>` to
`hydrogen new` below, which points the new project at your local copy.

## Create a project

```sh
hydrogen new mysite
cd mysite
```

The Go module path defaults to the directory name; pass `-module example.com/mysite` to choose one. (From a checkout:
`hydrogen new -core /path/to/hydrogen mysite`.)

```
mysite/
  main.go            hydrogen.Main(configure)
  hydrogen.go        configure(h): your hooks, endpoints and access rules
  hydrogen.yaml      the modules compiled in (sqlite, postgres)
  modules_gen.go     generated from hydrogen.yaml
  config/app.yaml    server, database and admin settings
  schema/            your content types (article.yaml is an example)
  admin/             a starter for admin extensions (optional)
```

The project is an ordinary Go program. The CLI builds and runs it; you can also run `go build` yourself.

## Describe your content

The project starts with one example type, in `schema/article.yaml`:

```yaml
types:
  article:
    label: Article
    admin: { list: [title, publishedOn] }
    fields:
      title:       { type: string, required: true, max: 120, translatable: true }
      slug:        { type: slug, from: title, index: true }
      summary:     { type: text, max: 300, translatable: true }
      body:        { type: richtext, translatable: true }
      publishedOn: { type: date, index: true }
```

A type is a list of typed fields. `required` is checked at publish, `translatable` makes the value per language, and
`index` makes a field filterable and sortable in the APIs. All field types and options are in [schema](schema.md).
Change it or add types next to it; `hydrogen config:check` validates them without starting the server.

## Run it

```sh
hydrogen dev
```

```
level=INFO msg=serving addr=[::]:8080 env=dev db=sqlite modules="[sqlite postgres project]"
level=WARN msg="no administrator yet: open this link to create the first one" url="http://localhost:8080/admin/login#setup=…"
```

`hydrogen dev` creates the SQLite database in `.work/`, applies the migrations, and rebuilds and restarts when Go code,
configuration or schema change (regenerating the typed code in `gen/`). Open the link it printed and create your
administrator account (passwords are at least 12 characters). Then choose **Article**, write one and publish it.

The link works only while no user exists, and only with the token it carries: outside `dev` the token is new at every
start. On a server you set up from the command line instead, create the administrator with
`hydrogen users:create-admin <login>` (the password comes from `HYDROGEN_ADMIN_PASSWORD` or stdin, never from the
command line).

## Read it from the API

Published content is public, in the delivery API. Replace the key with the one you gave your article, or list them:

```sh
curl -s localhost:8080/api/deliver/v1/collections/article/items
```

```json
{"items":[{"id":"01a15280-8108-77be-b0a0-9ffc272ac898","type":"article","key":"hello","locale":"en","version":1,
"publishedAt":"2026-10-19T04:51:58.755Z","name":"Hello, Hydrogen","fields":{"title":"Hello, Hydrogen",
"slug":"hello-hydrogen","summary":"First article."}}],"hasMore":false}
```

The management API writes. In `dev`, the config token is `dev-token`:

```sh
B=localhost:8080/api/manage/v1
curl -s -X POST $B/collections/article/items -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' \
  -d '{"key":"second","locale":"en","fields":{"title":"A second article"}}'
```

That creates a draft. Publish it from the admin, or with `POST $B/items/{id}/publish`, and it appears in the delivery
API. For real use, create an API token with a role instead of using the config token:

```sh
hydrogen tokens:create -name ci -role editor     # prints the secret once
```

The OpenAPI documents of both APIs, generated from your schema, are at `/api/deliver/v1/openapi.json` and (with a
token) `/api/manage/v1/openapi.json`.

## Typed code

`hydrogen generate` (and `hydrogen dev`) write `gen/content` and `gen/types.ts` from the schema: a Go struct and a
handle per type, and TypeScript types for frontends. Use them to add behaviour in `hydrogen.go`:

```go
func configure(h *hydrogen.Builder) {
	useModules(h)

	gen.ArticleType.BeforeSave(h, func(c *hooks.Context, a *gen.Article) error {
		if a.Summary != nil {
			s := strings.TrimSpace(*a.Summary)
			a.Summary = &s
		}
		return nil
	})
}
```

Hooks, endpoints, jobs and access rules are in [hooks](hooks.md) and [access](access.md).

## Add a frontend

Hydrogen only delivers content; something has to show it. Pick the route that fits.

| You want | Use |
| --- | --- |
| A website without a second program | The `site` module renders with Go templates from the same binary. `hydrogen add site pages`, then a web channel with `render: go`. See [the site module](site.md). |
| A static website on GitHub Pages | `hydrogen new -static mysite`: an Astro site, starter content committed in the repository and a deploy workflow. See [a static site](#a-static-site). |
| React, Svelte, Vue or Astro | A framework adapter and its starter: [Next.js](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/next/README.md), [SvelteKit](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/sveltekit/README.md), [Nuxt](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/nuxt/README.md) or [Astro](https://github.com/hydrogencms/hydrogen/blob/main/packages/starters/astro/README.md). Copy a starter and adapt it. |
| To serve a JavaScript frontend from the CMS binary | [site-proxy](site-proxy.md). |
| GraphQL | `hydrogen add graphql`. See [GraphQL](graphql.md). |
| Another framework or language | The delivery API and the [adapter kit](adapter-kit.md), which lists the four things an adapter does. |

A frontend that wants editors to build pages visually also needs the pages module and blocks in the schema
([schema](schema.md#blocks-and-tokens)), and the [preview bridge](bridge-protocol.md).

## A static site

For a website that GitHub Pages (or any static host) serves, with the content kept in git next to the code:

```sh
hydrogen new -static mysite
cd mysite
hydrogen dev                                # the CMS; open the link it prints to create your administrator
cd site && npm install && npm run dev       # in a second terminal: the site on http://localhost:4321
```

The project has page and block types (`schema/`), starter content (`content/`), an Astro site (`site/`) built with
`@hydrogencms/astro`, and `.github/workflows/deploy.yml`. Edit pages in the admin, run `hydrogen content:save` to write
them to `content/`, commit and push: the workflow starts the CMS on the committed content, builds the site and deploys
it. Set Pages > Source to GitHub Actions once. The generated README has the details; [content in
git](content-in-git.md) explains how the directory and the local database stay in step.

Drafts preview in the admin while `npm run dev` runs: in dev only, the page routes render on demand and the site adds
the preview route and the bridge. `npm run build` stays fully static, with no preview code in `dist`. Share links are
off, because they would point at your local dev server.

## Where to next

- [Concepts](concepts.md): how items, drafts, locales, channels and pages fit together.
- [Configuration](configuration.md) and [modules](modules.md): `app.yaml`, `channels.yaml`, and what else to compile in
  (`hydrogen add pages images`).
- [Deployment](deployment.md): build one binary, run it with Postgres, behind a proxy.
- [Command line](cli.md): every command.
