# Admin UI library: conventions and APIs

Read this before adding a screen. The foundation (shell, login, dashboard, router, api wrapper, stores, base
components, styles) lives here and in `src/`; screens live in `src/routes/<area>/`. The API contract is the
OpenAPI document (`src/lib/api/schema.d.ts`, generated). Nothing in the admin uses an endpoint outside
`/api/manage/v1`.

Commands (in `admin/ui`, or `make admin` from the repo root): `npm run dev` (Vite, proxies `/api` to
`ADMIN_API_URL`, default `http://127.0.0.1:8080`), `npm run check` (svelte-check), `npm test` (vitest + jsdom),
`npm run build` (into `../dist`), `npm run size` (budgets: initial JS 120 KB gzip, CSS 30 KB gzip),
`npm run types -- <openapi.json>` (regenerate `schema.d.ts`), `npm run e2e` (Playwright, `BASE_URL`,
`ADMIN_LOGIN`, `ADMIN_PASSWORD`). The Go server embeds `admin/dist`: rebuild the Go binary after `npm run build`.

## Rules

- Svelte 5 runes only (`$state`, `$derived`, `$effect`, `$props`, snippets). Shared state: a `*.svelte.ts` module
  exporting a class instance (see `session`, `types`, `channels`, `toast`). No state library.
- No dependency without a reason; no component kit; no icon library; no CSS-in-JS. Heavy things (editors,
  charts) are imported lazily (`import()`), never from the entry chunk.
- All HTTP goes through `api` (`lib/api/client.ts`). No `fetch` elsewhere.
- Strict CSP (`script-src 'self'; style-src 'self'`): no inline `<script>`, no `style="..."` attribute written
  literally in markup, no `hx-on`-style handlers, no `eval`. Dynamic styles use Svelte's `style:prop={value}`
  directive (applied through the CSSOM, allowed). Static styling goes in CSS classes.
- Every file that is a route is lazy-loaded; only what the shell, login and base components need is initial.
- Strings go through `t('key')` (below). Never hard-code UI text in markup.
- Dates: `formatDateTime(iso)` from `lib/format.ts`.

## Router (`lib/router.svelte.ts`)

Paths are written WITHOUT the admin prefix (`/items/123`); the prefix (`config().prefix`, from
`<prefix>/config.json`, e.g. `/admin`) is added by `href()`/`navigate()`. The built bundle works at any prefix.

### Add a route

1. Create the screen: `src/routes/<area>/Thing.svelte`. It receives `params` (path params) as a prop and may read
   `router.params` / `router.query` reactively. A route component is remounted when the path changes (not when
   only the query changes), so read the query reactively (`$derived(router.query.get('q'))`).
2. Add one line to `src/routes.ts` (first match wins; put `/*` last):
   `{ path: '/things/:id', title: 'things.title', load: () => import('./routes/things/Thing.svelte') }`.
   `title` is an i18n key used for `document.title`. Add `public: true` only for pages that need no session.
   The placeholder routes already registered for the screens (`/collections/:type`, `/collections/:type/new`,
   `/items/:id`, `/singletons/:type`, `/pages/:site`, `/settings/{users,groups,roles,tokens,audit}`, `/account`)
   point at `src/routes/<area>/<Name>.svelte` files that only render "coming soon": replace their content.
3. Link to it with `<a use:link href={href('/things/1', {tab: 'info'})}>`. `link` turns the click into in-app
   navigation (modified clicks, `target`, other origins keep default behaviour). Navigate from code with
   `await router.navigate('/things/1', { replace?: true })`.

Unauthenticated visits to a non-public route go to `/login?next=<path>`; after login the user returns to `next`.
A 401 from any API call clears the session and goes to login the same way.

### API of `router`

| Member | Meaning |
| --- | --- |
| `router.path`, `router.params`, `router.query` | Reactive current path (no prefix), path params, `URLSearchParams` |
| `router.navigate(path, {replace?, force?})` | Navigate (path may include `?query`). Asks registered blockers first unless `force` |
| `router.setQuery({q: 'x', page: 2, status: undefined})` | Patch the query string in place (`replaceState`; `{push: true}` for history entries). `undefined`, `null`, `''`, `false` delete the key. This is how list state (`?q=&sort=&page=&where…`) stays in the URL |
| `router.block(fn)` | Register a "may we leave?" check (returns the unregister function). `Form.guard()` uses it |
| `router.retry()` | Reload the current route (used by the error view) |
| `href(path, query?)` | URL including the prefix, for `href=` attributes |
| `link` | Action for `<a>` |

List screens: read state from `router.query`, write with `router.setQuery`, and derive the API query from it,
so a copied URL reproduces the view. Use `{push: true}` for page changes and replace for typing.

## API wrapper (`lib/api/client.ts`)

```ts
import { api, ApiError } from '../../lib/api/client';

const page = await api.get('/users', { query: { limit: 20, offset: 0 } });       // typed response
const item = await api.get('/items/{id}', { params: { id } });
const made = await api.post('/collections/{type}/items', { params: { type }, body: { /* typed */ } });
await api.patch('/items/{id}/draft', { params: { id }, body: {...} });
await api.del('/tokens/{id}', { params: { id } });
```

- `api.get|post|put|patch|del(path, {params?, query?, body?, signal?, noRedirect?})`. `path` must be a key of the
  generated `paths` (the compiler checks the method, `query` and `body`; the result type is the 200/201 JSON
  body, `void` otherwise). `params` fills `{name}` segments (URL-encoded). `query` drops `undefined`, `null` and
  `''`; arrays repeat the key. `signal` is an `AbortSignal`.
- Typed collection paths: the OpenAPI lists a path per type (`/collections/article/items`) plus the generic
  `/collections/{type}/items`; use the generic one. Item bodies are typed per type, so cast the body when you
  build it dynamically (`body: values as never`).
- `credentials: 'same-origin'`; `X-CSRF-Token` is added on every non-GET from `session.csrf`.
- Errors are `ApiError`: `status` (0 = network failure), `code` (`invalid`, `forbidden`, `not_found`, `conflict`,
  `referenced`, `throttled`, ...), `message`, `fields?: {path, code, message}[]`, `retryAfter?` (seconds, from
  `Retry-After`), `body` (the raw envelope: `referrers`, `hidden`, `locale`, `parts` for `referenced` etc.).
- Side effects: 401 -> session cleared and routed to login (skipped with `noRedirect: true`; login and the
  session probe use it). 5xx and network errors -> an error toast (the error is still thrown). 4xx -> you handle
  it. A 403 shows "You can't do this" with the policy (`e.message` is the policy name): use
  `toast.error(t('app.forbidden') + ': ' + t('app.forbiddenPolicy', {policy: e.message}))`.
- Responses of `/types` and `/types/{type}` are untyped in OpenAPI: use `types` (below), which is typed by hand.
- Regenerate types when the API changes: `make admin-types` (or `npm run types -- openapi.json`) and commit
  `schema.d.ts`.

## Session (`lib/auth/session.svelte.ts`)

`import { session } from '../lib/auth/session.svelte'`

| Member | Meaning |
| --- | --- |
| `session.principal` | `{id?, kind, name}` or `null` |
| `session.grants` | `[{policy, role, limits?: [{name, values}]}]` |
| `session.csrf` | token sent on unsafe requests |
| `session.can(policy, {type?, channel?, locale?})` | Coarse client-side permission check |
| `session.load()`, `login(l, p)`, `logout()`, `clear()` | Used by the shell and login |
| `session.onClear(fn)` | Reset per-user caches on sign out or expiry |
| `session.principalKey` | Stable id for per-user localStorage keys (`h.something.<key>`) |

`can()` is true when a grant for the policy or `*` exists. With `{type}` it also evaluates `collection` limits
(type listed) and `channel` limits (some listed channel delivers the type); `{channel}` and `{locale}` check
those limits. `owner`, `pages.site`, `pages.subtree` limits are not evaluated. **It is advisory: use it to
hide navigation and buttons; the API decides** (it answers 403 and the UI shows the message). Items carry
`actions: {edit, publish, delete}` for the caller: prefer those over `can()` for per-item buttons.

## Types and channels

`import { types } from '../lib/types.svelte'` (loaded by the shell before routes render; `await types.load()` is
cheap and cached):

- `types.all`, `types.get(name)`, `await types.detail(name)` (the compiled type from `GET /types/{type}` with `jsonSchema` attached; cached; summaries from `types.all` already carry fields, `admin`, `namePattern`, `options`).
- `types.listable` (every non-singleton type, page types included: what the Content nav lists),
  `types.collections` (non-singleton, non-page), `types.pageTypes`, `types.singletons`.
- `groupTypes(list)` groups by `admin.group` (ungrouped first, then alphabetical; labels sorted).
- Interfaces `CompiledType`, `CompiledField` (hand-typed from the `/types` JSON: `shape.base`, `admin.widget`,
  `when`, `of`, `fields`, `options`...).

`import { channels } from '../lib/channels.svelte'`: `channels.items`, `channels.current` ('' = all),
`channels.select(name)` (persisted in localStorage per principal), `channels.active`, `channels.collections`
(null = no filter), `channels.languages`, `channels.site`, `channels.showsType(type)`. Lists and navigation
apply the active channel by filtering to `channels.collections` / `channels.languages`.

## Forms (`lib/form.svelte.ts`)

```svelte
<script lang="ts">
  import { Form } from '../../lib/form.svelte';
  const form = new Form({ name: '', email: '' }, { prefix: '' });  // prefix 'fields.' strips it from API error paths
  form.guard();                                // call during init: beforeunload + in-app leave confirmation while dirty
  async function save() {
    const ok = await form.submit((v) => api.post('/users', { body: v }));   // v is a plain snapshot
    if (ok) toast.success(t('users.saved'));
  }
</script>
<form onsubmit={(e) => { e.preventDefault(); void save(); }}>
  <Field label={t('users.name')} error={form.error('name')} required>
    {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
  </Field>
  {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
  <Button type="submit" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>
</form>
```

- `form.values` (deep reactive), `form.dirty` (compares with the last saved state), `form.pending`,
  `form.errors` (`Record<path, message>`), `form.formError` (non-field message).
- Error paths follow the API: `title`, `body[2].text`, `fields.title` (use `prefix` to drop `fields.`).
  `form.error(path)`, `form.errorsUnder(path)` (for groups and lists), `form.clearError(path)`,
  `form.setErrors(fields)`.
- `form.submit(fn)`: no-op while pending; maps a 4xx `ApiError` with `fields` into `errors` and sets `formError`;
  returns `true` (and marks saved) or `false`. 5xx errors are rethrown (the wrapper already toasted).
- `form.reset(values)` after loading or reloading from the server; `form.markSaved()`.

## Shared helpers

- `lib/errors.ts`: `errorMessage(e)` (403 reads "You can't do this: <policy>"), `reportError(e)` (toast a 4xx; rethrows non-API errors), `copyText`, `generatePassword`. Use these instead of local copies.
- `api` options `ifMatch` (sent as `If-Match`) and `onHeaders` (response headers, e.g. the new `ETag`); `ApiError.etag` is the ETag of a failed response (a 412 carries the current one). The item editor stores the ETag per loaded item and shows a Reload / Overwrite dialog on `412`.
- A 401 while a `Form` with `guard()` is dirty opens a re-login dialog instead of navigating (`router.anyDirty`).

## i18n (`lib/i18n.ts`)

`t('login.failed')`, `t('table.sortBy', { column })` (`{name}` placeholders). One English table; a missing key
renders as the key. Add keys to your own file so parallel work does not collide: `lib/messages/collections.ts`
(prefix keys `collections.`), `lib/messages/pages.ts` (`pages.`), `lib/messages/settings.ts` (`settings.`, also
account); area catalogs register themselves when their route chunk loads (`import '../../lib/messages/settings'` at the top of an area's route components); shared strings stay in `core.ts` (`common.*`, `nav.*`, `table.*`). Keys are stable API for
project overrides (`register({...})`).

## Styling

- `src/styles/tokens.css` (colours with `light-dark()`, spacing `--h-space-1..7`, type `--h-text-xs..xl`, radius,
  shadow, `--h-focus`), `base.css`, `components.css` (all component rules, class names
  `h-<component>__<part>--<modifier>`). Use tokens only; never hard-code colours or pixel spacing in a screen.
- Dark mode follows `prefers-color-scheme`; `data-theme="light|dark"` on `<html>` overrides (principal menu).
- Utility classes: `h-stack` (column gap), `h-row` (flex, centre, gap), `h-spacer`, `h-muted`, `h-sr` (visually
  hidden), `h-page` (screen container: `h-page__head` for title + actions), `h-card` (`h-card__title`),
  `h-grid`, `h-list`/`h-list__item`, `h-alert` (inline error).
- Screen-specific CSS goes in a `<style>` block of the screen component using `h-<area>__<part>` classes and
  tokens (Svelte scopes it). Keep the total CSS under 30 KB gzip (`npm run size`).
- Icons: `<Icon name="trash" />` (24x24 stroke sprite; names in `lib/ui/icons.ts`: home file folder folder-open
  layers page settings user users key shield search plus x check chevron-right/down/left/up more edit trash copy
  external eye globe clock alert info menu logout sun moon grip arrow-up arrow-down filter link image list
  calendar tag history star). An unknown name renders `file`. Add an icon by adding its path data to `icons.ts`.

## Accessibility rules

- Every interactive element is a real `<button>`/`<a>`/input, reachable by keyboard, with the visible focus ring
  (do not remove `:focus-visible` styles). Icon-only controls use `IconButton` with a `label`.
- Every input has a label (`Field`, or `aria-label`). Errors use `Field`'s `error` (role=alert, linked with
  `aria-describedby`). Required fields are marked and `required`.
- Dialogs use `Dialog`; lists of data use `Table` (real `<table>`); trees use `Tree` (ARIA tree); status is never
  colour only (Badge has text). Contrast comes from the tokens.
- A route sets its `<h1>`; the shell sets `document.title` from the route's `title`.
- Async regions that change announce through `role="status"`/`aria-live` (Toast does).
- E2E runs axe on login, list, editor, tree and roles: no serious violations.

## Base components (`lib/ui`)

Import by path: `import Button from '../../lib/ui/Button.svelte'`. All pass extra attributes (`id`, `name`,
`aria-*`, `class`, event handlers such as `onclick`/`oninput`) to the underlying element unless noted.

| Component | Props |
| --- | --- |
| `Button` | `variant: 'primary'\|'secondary'(default)\|'ghost'\|'danger'`, `size: 'sm'\|'md'`, `icon?`, `loading?` (spinner, disabled), `href?` (renders `<a>`; add `use:link` yourself for in-app navigation), `type` (default `button`), `disabled`, children |
| `IconButton` | `icon`, `label` (required: aria-label and tooltip), `variant: 'ghost'\|'secondary'\|'danger'`, `size` |
| `Input` | `value` (bindable), `type`, `invalid?`, `el?` (bindable element), HTML input attributes |
| `Textarea` | `value` (bindable), `rows` (4), `invalid?` |
| `Select` | `value` (bindable), `options: {value, label, disabled?}[]`, `placeholder?` (adds an empty first option), `invalid?` |
| `Checkbox` | `checked` (bindable), `indeterminate?`, `label?` or children |
| `Switch` | `checked` (bindable), `label?` or children (role=switch) |
| `Field` | `label`, `description?`, `error?`, `required?`, `id?`, children snippet receiving `{id, 'aria-describedby', 'aria-invalid', invalid}`: spread it on the control: `{#snippet children(c)}<Input {...c} bind:value />{/snippet}` |
| `DatePicker` | `value` (bindable ISO text), `type: 'date'\|'datetime-local'\|'time'`, `min`, `max`, `invalid?` (native input) |
| `Dialog` | `open` (bindable), `title`, `onclose?`, `size: 'sm'\|'md'\|'lg'`, `dismissible` (true), children, `footer?` snippet. Moves focus in, traps Tab, Esc closes, restores focus; rendered in `<body>` |
| `Menu` | `items: {label, icon?, href?, onselect?, danger?, disabled?, separator?, checked?}[]`, `label` (a11y name), `align: 'end'\|'start'`, `trigger?` snippet (default "more" icon). Arrow keys, Home/End, Esc; `href` is an in-app path |
| `Tabs` | `tabs: {id, label, count?, disabled?}[]`, `value` (bindable id), `label`, children snippet `(activeId)` rendering the panel. Arrow keys, Home/End |
| `Table` | `columns: {key, label, sortable?, align?, width?, cell?: Snippet<[row]>}[]`, `rows`, `rowKey(row)`, `caption` (a11y), `sort?: {key, dir}`, `onsort(key)`, `selectable`, `selected` (bindable `Set<string>`), `sticky` (true), `loading` (skeleton rows), `empty?` snippet, `onrowclick?(row)` (mouse only: also put a link in a cell). Types `Column`, `Sort` from `lib/ui/table.ts` |
| `Pagination` | `page` (1-based), `pageSize`, `total?`, `hasMore?` (when total unknown), `onpage(page)` |
| `Badge` | `tone: 'neutral'\|'success'\|'warning'\|'danger'\|'info'`, children |
| `Skeleton` | `lines`, `height`, `width` |
| `EmptyState` | `title`, `description?`, `icon?`, `actions?` snippet |
| `Breadcrumbs` | `items: {label, href?}[]` (`href` is an in-app path), `label?` |
| `Tree` | see below |
| `Combobox` | see below |
| `Icon` | `name`, `size` (18), `label?` (makes it an image with a name) |
| `ToastHost` | mounted once by `App`; use the `toast` store |

Toasts: `import { toast } from '../lib/ui/toast.svelte'`: `toast.success(text)`, `toast.error(text)`,
`toast.info(text)`, `toast.push(kind, text, timeoutMs)`.

### Tree

```svelte
<Tree
  nodes={roots}                 // TreeNode[]: {id, label, icon?, hasChildren?, children?, disabled?, data?}
  label="Corporate pages"
  bind:expanded                 // Set<string> of open ids (persist it yourself)
  bind:selected                 // selected id
  loadChildren={async (n) => fetchChildren(n.id)}   // called when a node with hasChildren and no children opens; cached
  onactivate={(n) => open(n)}   // Enter / double click
  onselect={(n) => ...}         // click / Space
  onmove={({ node, target, position }) => ...}       // pointer drag; position 'before'|'after'|'inside'; enables dragging
  canDrop={(node, target, position) => boolean}
  onmoverequest={(n) => openMoveDialog(n)}           // keyboard alternative: key "m" or the grip button
  {row}                         // optional snippet (node) for custom row content (badges, dots)
  bind:this={tree} />           // tree.reload(id?) drops cached children of a node (or all) and reloads open ones
```

Keys: Up/Down, Home/End, Right (open, then first child), Left (close, then parent), Enter, Space, `m`.
One tab stop (roving tabindex); `aria-level/setsize/posinset/expanded/selected`. Dragging is ignored on
buttons/links inside a row; the drop never targets the node itself or its descendants. The component never
mutates your data: `onmove` is a request; update your tree (or `reload`) after the API call.

### Combobox

```svelte
<Combobox
  bind:value                    // ComboOption | null: {value, label, hint?}
  search={(q, signal) => api.get('/collections/{type}/items', { params: { type }, query: { q, limit: 10 }, signal }).then(r => r.items.map(toOption))}
  placeholder="Search…" debounce={200} minChars={0} clearable
  onselect={(o) => ...}
  {...fieldControl} />          // id, aria-describedby from Field
```

Search runs on focus (empty query) and on typing, debounced; the previous request is aborted and stale
responses are ignored. Keys: Down/Up, Enter selects, Esc closes. `role=combobox` with `aria-activedescendant`.

## Shell contract

`Shell.svelte` renders navigation from `types` (Content grouped by `admin.group`; label and `admin.icon` from the
type; singletons under "Settings content"), `/pages/sites` (Pages; one entry per site when `config().pages` and
`pages.read` is held; narrowed to the active channel's site), and the Settings entries gated by `can()`
(`user.manage` -> Users, Groups; `role.manage` -> Roles; `token.manage` -> API tokens; `audit.read` -> Audit).
The channel switcher is a `<select>` over the channels the principal may read; routes react to `channels.current`.
The global search box navigates to `/search?q=`; the results route is a placeholder to be filled in.
Routes render inside `<main class="h-main">`; give them a root `<div class="h-page">`.

## Runtime config

`config()` from `lib/config.ts` returns `{apiPrefix, prefix, pages, deliveryPrefix}` read once from
`<prefix>/config.json` (located relative to the entry script, so any `admin.prefix` works).

## Extension slots (H5c)

`config().extensions` (from `<prefix>/config.json`, always present, empty lists when unconfigured) is parsed by
`lib/extensions.svelte.ts` (`ext`). `main.ts` calls `ext.boot()` before the first render: it applies the theme
(`theme.name` overrides the i18n key `app.name`, so the shell, login and title follow; `theme.logo` replaces the nav
icon; `theme.accent` sets `--h-accent` on `:root`; `theme.css` is appended as a `<link rel=stylesheet>`), fetches every
`strings` URL and merges the JSON into i18n (listed order, later wins; a failed or missing file is skipped with one
console warning), and registers the `views` as routes.

Element modules (`elements`) are ES modules that define custom elements as a side effect of loading. Nothing is
imported until a slot first renders: `ext.ensureElements()` runs `import(url)` once per URL. A URL that fails shows
one error toast ("Admin extension failed to load: <url>") and the built-in UI stays; the next slot use retries it
(no second toast). The mount code (`lib/ext/ExtensionHost.svelte`, `mount.ts`) is itself a dynamic import behind
`<Extension>`, so it is only fetched when a slot has entries; the initial chunk holds just the manifest parsing,
theme/strings boot and the menu/policy filters. `<Extension tag props
onEvents />` (`lib/ext/Extension.svelte`) waits for it, then `mountElement(container, tag, props, {ctx, on})`
creates the element, assigns `ctx` and then every prop as a **property** (never an attribute) before it is
connected, and listens for the events on the element. Prop changes call `update(props)` (unchanged values are not
re-assigned); a new session (principal or grants change) re-assigns `ctx`. `ctx` is the SDK's `AdminContext`: `api`
(`get(path, init?)`, `post(path, body?, init?)`, `patch`, `del`: paths relative to the API prefix, the admin's client
with CSRF and error handling), `t`, `navigate`, `toast`, `principal`, `can`, `locale`, `prefix`.

| Slot | Where | Props (besides `ctx`) | Events |
| --- | --- | --- | --- |
| widget | `lib/fields/FieldRow`, `admin.widget` = a manifest widget name | `value`, `field`, `locale`, `readonly`, `error?` | `change` `{value}` sets the form value |
| cell | list column, `admin.list` entry `field:cell` | `value`, `item`, `field` | none |
| panel | editor tabs after the built-ins (disabled on new items), filtered by `types` | `item`, `type`, `locale`, `draft` | `refresh` reloads the item |
| action | editor header after Publish, filtered by `types` and `can(policy)` | `item`, `type`, `locale` | `refresh`, `navigate` `{path}` |
| dashboard | a card per widget, in manifest order, titled with `title` | none | none |
| view | route from `views` (`ViewHost`), menu entry under its `menu` heading when `can(policy)` | `params`, `query` (plain object) | `navigate` `{path}` |
| before/after | `login`, `list`, `editor`, `dashboard` | `at`; `type` (list); `item`, `type` (editor) | none |

Rules: only real `CustomEvent`s count (a native `change` bubbling out of an inner input of a light-DOM widget is
ignored), and `change`/`navigate` need an object `detail`. `navigate` and `ctx.navigate` accept admin-relative paths
only (a leading `/` is added; absolute and protocol URLs are refused with a warning). `policy` is enforced for
panels, actions, slots, dashboard widgets and views with `can()`. A `refresh` in the editor asks to confirm when
the form has unsaved edits. Custom widgets render their own error: the `Field` row around a custom widget shows
only the label and description, and the widget gets `error`. Manifest maps and lists are sanitised (own string
entries only; `__proto__`, `constructor`, `prototype` keys and non-string values are dropped).

An unknown widget or cell name falls back to the built-in control with one console warning. Hooks for tests:
wrappers carry `data-extension="<tag>"`; slots add `data-slot="<at>"`, actions `data-action="<tag>"`, dashboard cards
`data-dashboard-widget="<tag>"`.

### Dev entry and HMR

When the server is started with `ADMIN_ELEMENTS_DEV_URL` (e.g. `http://localhost:5174`, the project's Vite dev
server; dev only), `config.json` lists `<prefix>/x/dev/src/elements.ts` instead of the built files and the server
proxies `<prefix>/x/dev/*` to Vite, so the strict CSP (`'self'`) still holds. The loader needs nothing special: it
`import()`s that URL like any other. Vite's HMR can replace a custom element's module, but `customElements.define`
cannot redefine a tag, so a redefined element only updates after the page is reloaded (SDK `defineElement` warns);
Svelte custom elements that update props reactively refresh in place. Modules are imported on the first slot use,
so open a screen that renders one to load it.
