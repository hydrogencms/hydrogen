# Hooks and project code

Everything that is behaviour rather than structure is Go code in the project: hooks around content changes, access
rules, HTTP endpoints, scheduled jobs, commands and content migrations. All of it is registered on the
`*hydrogen.Builder` in `hydrogen.go`, in `configure`, after the modules:

```go
func configure(h *hydrogen.Builder) {
	useModules(h) // the modules listed in hydrogen.yaml

	gen.ArticleType.BeforeSave(h, readingTime)
	h.Endpoint("POST", "/import", kernel.Policy("import.run"), importArticles)
	h.Job("articles.digest", "0 6 * * *", sendDigest)
}
```

Code the project does not want to compile can be a [WebAssembly hook](wasm-hooks.md) instead.

## Typed handles

`hydrogen generate` writes `gen/content`: a Go struct per type (`Article`) and a handle (`ArticleType`) that registers
hooks and rules and calls the Local API with that struct, so field names and types are checked by the compiler.
Regenerate after a schema change (`hydrogen dev` does it for you). Fields are pointers when optional, so
`a.Summary == nil` means "not set".

```go
gen.ArticleType.BeforeSave(h, func(c *hooks.Context, a *gen.Article) error {
	if a.Summary != nil {
		s := strings.TrimSpace(*a.Summary)
		a.Summary = &s
	}
	return nil
})
```

The hook gets the document as a struct, and the fields it changes are written back. Fields it leaves alone keep their
stored form.

## Events

| Event | Runs | May |
| --- | --- | --- |
| `BeforeValidate` | On every draft write, before validation. | Normalise the input. |
| `Validate` | On draft writes and on publish. | Add field errors. |
| `BeforeSave` | On draft writes, after validation. | Compute fields. |
| `BeforePublish` | Just before a version is written. | Add field errors; block. |
| `AfterPublish` | After the version is written. | React. In the transaction, or `Async`. |
| `BeforeDelete` | Before an item is trashed or purged. | Block. |
| `AfterDelete` | After it. | React. In the transaction, or `Async`. |
| `BeforeUnpublish`, `AfterUnpublish` | When an item stops being published, by unpublishing or trashing. | Block, and react. Synchronous only. |

The typed handle has methods for the first seven events. For the unpublish events (and hooks for every type, with
`Type: ""`) register a `hooks.Hook` directly:

```go
h.AddHook(hooks.Hook{Type: "article", Event: hooks.BeforeUnpublish, Fn: func(c *hooks.Context) error {
	return fmt.Errorf("articles stay published: %w", content.ErrInvalid)
}})
```

Hooks of one event run in registration order (modules first, then the project) and the first error stops the chain.
`hooks.Context` carries the `Item` (id, type, key, version, main locale), the `Locale`, the `Doc` (the merged
document of the locale; nil on delete and unpublish events), `Errors` and `Tx`.

### Failing

- **Field errors** are the user-facing way to refuse a write. Add one and return `nil`; the write then fails with
  `422 invalid` and the error at its path, exactly like schema validation:

  ```go
  gen.ArticleType.BeforePublish(h, func(c *hooks.Context, a *gen.Article) error {
  	if a.Body == nil {
  		c.Errors.Add("body", "required", "an article needs a body before it is published")
  	}
  	return nil
  })
  ```

- **An error** aborts the operation and rolls it back. Wrap `content.ErrInvalid` (`422`) or `content.ErrBadRequest`
  (`400`) for a refusal the caller should see; any other error is a bug: it is logged and answered `500`.

### Transactions and async hooks

Synchronous hooks run inside the transaction that changes the item, so what they write through `c.Tx` commits or
rolls back with it. Keep them fast and free of network calls. `AfterPublish` and `AfterDelete` can run after the
commit instead, from the [outbox](#outbox-and-events):

```go
gen.ArticleType.AfterPublish(h, notify, content.Async("notify-slack"))
```

An async hook has no `Tx`, a stable name (it becomes an outbox consumer; progress is kept under it), and is delivered
**at least once**, retried with backoff on failure: make it idempotent. It runs once per published locale.

## Local API

The content service is the Go API of the same operations the HTTP APIs offer, with the same validation, hooks, audit
and events. Reach it from endpoints, jobs and commands through `kernel.Services`:

```go
func sendDigest(ctx context.Context, s *kernel.Services) error {
	res, err := gen.ArticleType.List(ctx, s.Content, content.ListOptions{Status: store.StatusPublished, Limit: 20})
	…
}
```

A context without a principal is the system and is not restricted. To act as someone, put their principal in the
context with `access.WithPrincipal(ctx, p)`; the checks then apply. `Handle` offers `Get`, `Create`, `Update`,
`Publish` and `List`; `s.Content` has the rest (unpublish, trash, versions, references, search).

## Endpoints

```go
h.Policy(access.Policy{Name: "import.run", Description: "Run the article import."})

h.Endpoint("POST", "/import", kernel.Policy("import.run"), func(w http.ResponseWriter, r *http.Request) {
	s := kernel.ServicesFrom(r.Context())
	var in struct{ URL string `json:"url"` }
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	// …use s.Content, s.DB, s.Log
	manage.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
})
```

Endpoints are served under `/api/x` (here `POST /api/x/import`). The path is relative and uses `net/http` pattern
syntax (`/sync/{id}`). The auth argument is required: `kernel.Public`, `kernel.Authenticated` (any session, token or
the config token) or `kernel.Policy("name")` for a principal that holds a registered policy. Non-public endpoints
authenticate like the [management API](management-api.md#authentication) (bearer token, or session with CSRF) and
carry the principal in the context. The policy check is coarse, so check it against the item you touch with
`s.Access.Can`. Panics are recovered as `500`. Registering the same method and path twice, a bad path or an unknown
policy fails at start-up and in `hydrogen config:check`.

## Jobs and commands

```go
h.Job("articles.digest", "0 6 * * *", sendDigest)         // 5-field cron, in UTC
h.Job("articles.sync", "@every 15m", syncArticles)

h.Command(kernel.Command{
	Name: "articles:count", Summary: "print the number of articles",
	Run: func(ctx context.Context, app *kernel.App, args []string) error { … },
})
```

A job runs on every server that shares the database but **once per tick**: the runner takes a lock, so you need no
leader election. `hydrogen job:run <name>` runs one now (it fails if another server holds the lock), and
`jobs.enabled: false` keeps a node from running jobs. Job names are unique across modules. Built in:
`auth.cleanup` (expired sessions), `content.trash` (purges old trash), `collab.cleanup`, `preview.cleanup` and
`releases` (scheduled [releases](releases.md)), plus those of modules (`media.gc`).

A command is listed by the project binary's `help` and runs as `hydrogen articles:count` in the project directory (or
`bin/mysite articles:count`).

## Outbox and events

Every change writes an event to the **outbox** in the same transaction, and consumers receive it after the commit, at
least once. Topics include `item.created`, `item.updated`, `item.published`,
`item.unpublished`, `item.trashed`, `item.deleted`, `page.changed` and `release.*`. Webhooks, cache purges, search
indexing and async hooks are all consumers. Subscribe your own with an async hook, a webhook
([delivery API](delivery-api.md#webhooks)) or, in a module, `Registry.Consumer`.

To purge your own cache tags when events arrive, register a tag mapper: it turns a topic and payload into the tags
that event invalidates.

```go
h.TagMapper(func(topic string, payload json.RawMessage) []string {
	if topic == "item.published" { return []string{"home"} }
	return nil
})
```

## Schema in code

`h.Schema(func(b *schema.Builder) error { … })` runs after the schema files load and before they compile, so code can
add types, fields, groups, blocks and token sets (`b.AddType(name, schema.Spec{…})`), or change what modules added.
Prefer YAML for structure the team edits.

## Content migrations

A breaking [schema change](schema.md#changing-a-schema) (a removed field, a changed type) refuses to start until a
migration accounts for it. A migration rewrites the stored documents of one type:

```go
h.Migration(content.Migration{
	Name:    "0001_person_split_name", // ordered by name, applied once
	Type:    "person",
	Changes: []string{"person.fullName"}, // the breaking changes it accounts for
	Up: func(d *content.MigrationDoc) error {
		full, _ := d.Fields["fullName"].(string)
		delete(d.Fields, "fullName")
		first, last, _ := strings.Cut(full, " ")
		d.Fields["firstName"], d.Fields["lastName"] = first, last
		return nil
	},
})
```

`Up` runs for every item and locale, on the merged document; the result is written back split by the new schema.
Apply with `hydrogen content:migrate` (automatic on start in `dev`), which also rebuilds the projections and records
the new schema revision.

## Access rules

Code rules and policies are in [access](access.md#rules-in-code).

## Modules

A module is a Go package with `func Module() kernel.Module`. `Register(r *kernel.Registry)` declares what it adds
(field types and type options, channel options, management and delivery routes, migrations, hooks, jobs, commands,
exporters, admin extensions); optional `Boot`, `Start` and `Stop` methods receive the services and run background
work. A module reads its configuration from `modules.<name>`. List it in `hydrogen.yaml` (see [modules](modules.md)),
and declare the core versions it supports for [compatibility checks](module-versions.md).
