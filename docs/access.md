# Access control

Hydrogen decides who may do what with role-based access control, narrowed by limitations, and lets project code add
rules on top. The decision lives in the service layer, not in the HTTP handlers: the management API, the admin, the
Go API, hooks and jobs all go through the same checks, so there is no side door.

## Principals

| Kind | Who | Rights |
| --- | --- | --- |
| `user` | A person who signs in with a login and password. | Those of their roles and groups. |
| `token` | An API token, for scripts and integrations. | Those of its roles. |
| `agent` | An API token created with `-agent` (or `kind: "agent"`). The same, but audit entries record the actor as an agent, with the model and task named in the `X-Hydrogen-Agent-Model` and `X-Hydrogen-Agent-Task` request headers. | Those of its roles. |
| config token | `manage.token` in `app.yaml`. | Everything. Audited as `token:config`. |
| system | Code that runs without a request: hooks, jobs, migrations, the Go API with a bare context. | Everything. |

Every change writes an audit entry with its actor (`GET /api/manage/v1/audit`, or Settings in the admin).

## Users, groups and tokens

```sh
hydrogen users:create-admin alice                 # password from HYDROGEN_ADMIN_PASSWORD or one line of stdin
hydrogen tokens:create -name ci -role editor      # prints the secret once; add -agent, -expires 720h
```

`users:create-admin` creates the user, or resets the password of an existing one (ending their sessions), and gives
the `administrator` role. On a database with no user yet, the server also prints a setup link when it starts
(`no administrator yet` in its log): it opens a form in the admin that creates the first administrator. The link
carries a token that is random at every start (in `dev`, derived from the dev token so it survives live reload) and
stops working once any user exists; `GET /auth/setup` tells whether setup is still open. Passwords are 12 to 256 bytes; there are no composition rules. They are stored as
PBKDF2-HMAC-SHA256 hashes. Logins are throttled per client, per login and per pair, and a flood of password checks is
answered `503` before it can exhaust the server.

An API token's secret starts with `hyt_`, is shown once and is stored as a SHA-256 hash. It may expire and can be
revoked at any time. Everything else about users, groups, roles and tokens is managed in the admin or through the
[management API](management-api.md#administration): `/users`, `/groups`, `/roles`, `/assignments`, `/tokens`.

## Roles and policies

A **policy** is one action, named `module.action`. A **role** is a list of policies, each optionally narrowed by
**limitations**. A **role assignment** gives a role to a user, a group (all its members) or a token. A principal's
grants are the union of all of them; one matching grant allows the action.

| Policy | Allows |
| --- | --- |
| `content.read` | Read items, drafts, versions and references. |
| `content.create` | Create items. |
| `content.edit` | Edit and discard drafts. |
| `content.publish` | Publish and unpublish. |
| `content.delete` | Trash, restore and purge items. |
| `content.preview` | Create preview tokens. |
| `content.preview.share` | Create and revoke [share links](share-links.md). |
| `release.manage` | Create, edit, schedule and publish [releases](releases.md). |
| `review.submit`, `review.approve`, `review.bypass` | Submit drafts, approve or request changes, and publish without review ([reviews](reviews.md)). |
| `user.manage`, `role.manage`, `token.manage` | Administer users and groups, roles and assignments, API tokens. |
| `audit.read`, `system.read` | Read the audit log, and the system report. |
| `pages.read`, `pages.edit`, `pages.mount` | With the pages module: read the page tree, edit its nodes, mount collections. |

`*` grants everything. Projects and modules declare more policies (below).

### Limitations

A limitation narrows a grant to part of the content. A grant holds when **all** its limitations match; the values of
one limitation are alternatives.

| Limitation | Values | Applies to |
| --- | --- | --- |
| `collection` | Content types. | The content and review policies. |
| `locale` | Locales, from the language registry. | `content.create`, `content.edit`, `content.publish`. |
| `owner` | `self`: items the principal created. | The content policies. |
| `channel` | Channels: the collections and languages they serve. | The content policies. |
| `pages.site`, `pages.subtree` | A pages site; a node id and everything below it. | The pages policies and the content policies. |

Listings are narrowed the same way: a principal who may read only `article` sees only articles, and one limited to
`owner: self` lists only their own items.

```json
{"name": "news-editor", "policies": [
  {"policy": "content.read"},
  {"policy": "content.edit",    "limitations": {"collection": ["article"], "locale": ["en", "de"]}},
  {"policy": "content.publish", "limitations": {"collection": ["article"], "locale": ["en"]}}
]}
```

An assignment can add limitations of its own (`POST /assignments {"roleId", "principalKind", "principalId",
"limitations": {…}}`), so one role can be handed out to different people for different parts of the site.

The everything policy (`*`) cannot be limited: a role policy `*` with limitations is rejected, and so is an assignment
limitation on a role that contains `*` (use a narrower role instead). A role can only gain `*` once none of its
assignments carries limitations. Limited `*` grants stored by earlier versions stay restricted until you remove their
limitations (the admin shows them and lets you remove them).

### Role templates

On first start Hydrogen creates these roles once. They are ordinary roles afterwards: edit or delete them freely, and
later releases never overwrite them.

| Role | Policies |
| --- | --- |
| `administrator` | Everything. |
| `editor` | Read, create, edit, publish, delete, preview and share previews; releases; submit and approve reviews. |
| `contributor` | Read, create, edit, preview; submit reviews. Writes drafts for others to publish. |
| `ai-contributor` | Read, create, edit. For agent tokens: drafts only, never publish or delete. |
| `viewer` | Read. |

Modules add their policies to the templates (the pages module adds `pages.read` and `pages.edit`). A database seeded
before a module added them keeps its roles: update the role through the API or the admin.

## Rules in code

Roles are data that administrators edit. When a restriction is part of the application, put it in code. A **rule**
is registered for a content type and runs after the role check; it can only narrow, never grant. Rules for a type
AND together, and they never restrict the system or the config token.

```go
func configure(h *hydrogen.Builder) {
	useModules(h)

	gen.ArticleType.Access(h, access.Rules{
		Read: func(ctx context.Context, p *access.Principal) access.Result {
			if p.Kind == access.KindAgent {
				return access.Where("kind", store.OpEq, "news") // agents see news only
			}
			return access.Allow()
		},
		Delete: func(ctx context.Context, p *access.Principal) access.Result {
			return access.Deny()
		},
	})
}
```

`access.Rules` has `Read`, `Create`, `Update`, `Publish` and `Delete` (a nil function allows). `Update` also guards
`review.submit`, and `Publish` also guards `review.approve` and `review.bypass`. A rule returns:

- `access.Allow()` or `access.Deny()`;
- `access.Where(field, op, value)`, which allows only items whose **indexed** field matches. It scopes listings, search
  and counts too, and is checked against the document on every write;
- `access.And(…)` to combine results.

Without the typed handle, `h.AccessRules("article", rules)` does the same.

## Policies in code

Declare a policy for your own endpoints and admin extensions, and roles can grant it:

```go
h.Policy(access.Policy{Name: "import.run", Description: "Run the article import."})
h.Endpoint("POST", "/import", kernel.Policy("import.run"), importHandler)
```

Names are lower-case `word.word`. A `Limitations` list names the limitations that apply to it. Modules register
limitations of their own through the kernel registry (`Registry.Limitation`, `Registry.PolicyLimitation`). Route and
endpoint policies are a coarse gate: "holds this policy at all". Inside the handler, check the policy against what the
request touches with `kernel.ServicesFrom(ctx).Access.Can`.

## Behaviour worth knowing

- A `403` names the missing policy in its message. `release.manage`, the administration policies and `audit.read`
  take no limitations.
- A principal's grants are cached and refreshed on every server when roles, assignments or group membership change.
- A share link or preview token outlives the permissions of the person who made it. Revoke them. See
  [share links](share-links.md).
- Every route and endpoint must declare a policy (or `Authenticated`, or `Public`); a module route without one fails
  startup.
