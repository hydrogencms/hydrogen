# Admin UI library: conventions and APIs

Read this before adding a screen. The foundation (shell, login, dashboard, router, api wrapper, stores, base
components, styles) lives here and in `src/`; screens live in `src/routes/<area>/`. The API contract is the
OpenAPI document (`src/lib/api/schema.d.ts`, generated). Nothing in the admin uses an endpoint outside
`/api/manage/v1`.

Commands (in `admin/ui`, or `make admin` from the repo root): `npm run dev` (Vite, proxies `/api` to
`ADMIN_API_URL`, default `http://127.0.0.1:8080`), `npm run check` (svelte-check), `npm test` (vitest + jsdom),
`npm run build` (into `../dist`), `npm run size` (budgets: initial JS 120 KB gzip, CSS 30 KB gzip; the design aims at 45 KB and 16 KB),
`npm run types -- <openapi.json>` (regenerate `schema.d.ts`), `npm run e2e` (Playwright, `BASE_URL`,
`ADMIN_LOGIN`, `ADMIN_PASSWORD`). The Go server embeds `admin/dist` (built, not committed): rebuild the Go binary after `npm run build`.

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
- Dates: `formatDateTime(iso)` and `relativeTime(iso)` from `lib/format.ts` (relative in tables and feeds, exact in `title`; `<Time at>` does both).

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

## Styling and the design system

Look: calm, neutral and monochrome-first. Soft canvas, white cards with a hairline border (no shadow), a black
(white in dark mode) pill for the one primary action, Material Symbols icons. Colour means something (status, errors,
focus) or is the project's accent. Light and dark must both look right: always use tokens, never literal colours.

Files: `src/styles/tokens.css` (all variables), `base.css` (reset, type, `h-sr`, `h-muted`, `h-stack`, `h-row`,
`h-spacer`, `h-num`), `components.css` (every base component, classes `h-<component>__<part>--<modifier>`),
`layout.css` (page primitives, shell, command palette, login, dashboard). Total CSS stays under 16 KB gzip.

### Tokens

| Group | Tokens |
| --- | --- |
| Neutrals (`light-dark()`) | `--h-bg` (canvas), `--h-surface` (cards), `--h-surface-2` (subtle fill, hover rows), `--h-surface-3` (pressed, selected, segmented track), `--h-hover` (translucent hover for any background), `--h-border`, `--h-border-strong`, `--h-text`, `--h-text-muted` (4.5:1), `--h-text-faint` (hints only) |
| Accent | `--h-accent` (the single theme override point: black light, white dark), `--h-accent-contrast`, derived `--h-accent-strong`, `--h-accent-soft`, `--h-selected` (follow an override) |
| Status | `--h-success/-warning/-danger/-info` (text) and `-soft` (background); `--h-danger-solid` (danger button) |
| Type | `--h-font` (Inter if installed, else the system UI font; nothing is shipped), `--h-mono`, `--h-text-xs..4xl` = 12 13 14(body) 16 20 24 30 40 px (`--h-text-dialog` 18), `--h-leading`. Tables, counts and times use tabular figures (`h-num`) |
| Spacing | `--h-space-1..8` = 4 8 12 16 24 32 48 64 px, plus `--h-space-20`, `--h-space-40`; `--h-page-pad` 32 / 24 (< 1280) / 16 (< 768) |
| Shape | `--h-radius-sm` 6, `--h-radius` 10 (controls, nav items), `--h-radius-md` 12 (menus), `--h-radius-lg` 16 (cards), `--h-radius-xl` 20 (dialogs), `--h-radius-pill` |
| Elevation | `--h-shadow-sm`/`--h-shadow` (raised: active nav item, menus, scrolled header), `--h-shadow-dialog`, `--h-focus` (ring, plus accent border), `--h-overlay` |
| Controls | `--h-control-height-sm/-/-lg` = 32 / 36 / 44 px |
| Layout | `--h-nav-width` 256, `--h-rail-width` 64, `--h-topbar-height` 64 |
| Motion | `--h-dur-fast` 120 ms, `--h-dur` 180 ms, `--h-ease`; both are 0 under `prefers-reduced-motion` |
| Avatars | `--h-tint-1..6` |

Dark mode follows `prefers-color-scheme`; `data-theme="light|dark"` on `<html>` overrides it (sidebar theme toggle).
Project themes (H5c) set `--h-accent` (and may override any token); everything derived follows. Old names that
extensions use are kept: `--h-bg`, `--h-surface-hover`, `--h-radius`, `--h-accent-soft`, `--h-accent-strong`.

### Page anatomy

Every screen is `<div class="h-page">` with, in order: a page header, an optional toolbar row, then content.
The top bar (shell) shows the breadcrumbs; the page header shows title, badge, meta and actions.

```svelte
<div class="h-page">
  <PageHeader title={type.label} breadcrumbs={[{ label: 'Content' }, { label: type.label }]}
    meta="Description" overflow={[{ label: 'Delete', icon: 'trash', danger: true, onselect }]}>
    {#snippet badge()}<Badge>{total}</Badge>{/snippet}
    {#snippet actions()}<Button>Columns</Button>{/snippet}
    {#snippet primary()}<Button variant="primary" icon="plus">New article</Button>{/snippet}
  </PageHeader>
  <Tabs variant="underline" tabs={views} value={view} label="Views" />   <!-- toolbar: tabs, search, chips -->
  <Table ... />                                                         <!-- a card spanning the full width -->
</div>
```

- `.h-page`: flex column, gap 24, padding from `--h-page-pad`, **no max-width**. `.h-page__head` is the old header
  row (title + actions); new screens use `PageHeader`.
- `.h-page-grid`: 12 columns, gap 20; children take `h-span-3|4|6|8` (and `h-md-span-N` from 768 px,
  `h-lg-span-N` from 1280 px); children are full width below their first breakpoint. Cards in the grid:
  `<Card class="h-lg-span-8">`.
- `.h-split` + `--h-split-left` (px) + `.h-split__sep`: resizable two-pane layout (tree and editor); stacks below 1024.
- `.h-form`: a column of Fields with 20 px between them. `.h-pill` / `.h-pill--primary`: pill look for menu triggers.
- Breadcrumbs: `<PageHeader breadcrumbs>` or `<Breadcrumbs items>` set the trail (`lib/crumbs.svelte.ts`) that the
  top bar renders; without either, the top bar shows the route's title. `<Breadcrumbs inline>` renders in place.

### Do / don't

- One primary action per view: the black pill, top right. Secondary actions are light pills. Destructive and
  rare actions go in the overflow menu (⋯) and confirm in a dialog (the red button only appears there).
- No underlined links (links are text colour, medium weight, underline on hover only). No blue links.
- No `max-width` on working screens (lists, trees, editors). Cap only reading text and form inputs.
- Cards: border, no shadow, radius 16. Shadows only for raised things (menus, active nav, dialogs, bulk bar).
- Show times relative with the exact time in `title`: `<Time at={iso} />` or `relativeTime(iso)` + `formatDateTime`.
  Never show raw ids or audit codes where a person reads.
- Icons: only the Material Symbols set in `lib/ui/icons.ts` (Outlined, weight 400, fill 0, grade 0; Apache-2.0; path
  data on a `0 -960 960 960` viewBox, filled with `currentColor`, no icon font), drawn at 14/16 (dense), 18 (nav),
  28-32 (empty states). No duotone or emoji icons; add a new icon by copying its path from
  `@material-symbols/svg-400/outlined/<name>.svg` into `icons.ts` (keep our name; aliases share one string).
- Loading is a `Skeleton` in the shape of the content, never a blank area or a lone spinner; empty is an
  `EmptyState` that says what to do next with a button; completed actions toast.
- Status is never colour only: `Badge` carries text; language dots carry the code next to them.
- Keep focus rings visible; every control has a name (`Field` label, `aria-label`, or `IconButton label`).
- Utility classes: `h-stack`, `h-row`, `h-spacer`, `h-muted`, `h-sr`, `h-num`, `h-alert`, `h-list`, `h-grid`.
  Screen-specific CSS goes in the screen's `<style>` block with tokens (Svelte scopes it).

## Rich text editor (`lib/richtext`)

`RichTextEditor.svelte` is the one editor for every richtext field and block prop. It is a thin shell that
dynamic-imports `Editor.svelte` (ProseMirror, toolbar, link popover, "Edit as JSON" dialog) and shows a skeleton
meanwhile; nothing under `lib/richtext` except `config.ts`, `types.ts` and the shell may be imported from the
entry chunk.

```svelte
<RichTextEditor label={field.label} value={doc.body} config={configOf(field.options)} locale={ctx.locale}
  {readonly} error={ctx.error(path)} id={control.id} aria-describedby={control['aria-describedby']}
  onchange={(v) => (doc.body = v)} />
```

- Props (`types.ts`): `value` (stored doc; undefined/empty shows an empty editor), `config` (`{marks?, nodes?}` allowlists,
  read from `field.options` with `configOf`; undefined allows everything), `locale`, `readonly`, `error`, `label`
  (accessible name), `id`, `aria-describedby`, `placeholder`, `rows` (min height, default 8), `json` (show "Edit as
  JSON", default true), `onchange(value)`.
- `onchange` gets the stored ProseMirror JSON (`{type:'doc', content}`), or `undefined` when the editor is empty
  (the server's `IsEmpty`). Null attributes are never stored (`toStored`); mark order follows the server's.
  Outside changes to `value` (locale switch) replace the document; echoes of the editor's own output do not.
- The schema (`schema.ts`) is built from the allowlists with the server's node/mark names and attributes
  (headings `level`, code blocks `language`, links `href`: http(s), `mailto:` or `item:<uuid>`). A value the schema
  cannot hold (a mark the field disallows) shows an error with "Edit as JSON" instead of the editor.
- Keys: Mod-B/I/E bold/italic/code, Mod-K link (popover: URL or item search over readable collections),
  Mod-Alt-0/2/3/4 paragraph/headings, Shift-Ctrl-8/9 lists, Shift-Enter hard break, Mod-Z/Shift-Mod-Z. Input rules:
  `## `..`#### `, `- `, `* `, `1. `, `> `, ``` ```. Paste goes through the schema's DOM parser (unknown marks and
  nodes are stripped, unsafe links dropped, h1-h6 clamp to 2-4).
- The component instance exports `getView()` (the ProseMirror `EditorView`) for tests and embedding.
- Tests: `schema.test.ts` (round trips, allowlists, links, input rules), `editor.test.ts` (component), e2e
  `richtext.spec.ts`.

## Block editor (`lib/blocks`)

`FieldRow` renders a `blocks` field with `blocks/BlocksField.svelte`, a lazy chunk (`{#await import()}`, so nothing
under `lib/blocks` is in the entry chunk except the pure `tree.ts` that `fields/logic.ts` uses to clean and hydrate
block values). The block types and token sets come from `GET /blocks` through `blocksCatalog`
(`catalog.svelte.ts`: `load()` once per session, `get`, `label`, `icon`, `allowed(rule)`, `token(set)`, `sharedTo`,
`schema(rule)`).

- Value: the stored array `[{id, type, props, slots?}]`. `tree.ts` holds the pure operations (insert, remove, move,
  reorder, clone with new ids, `canInsert`/`canRemove`/`canMove` with their refusal reasons, move `targets`,
  `summary`, `cleanBlocks`/`hydrateBlocks`, `routeErrors`, `uuidv7`); all return new trees and share untouched
  subtrees. `BlocksModel` (`model.svelte.ts`) is the per-field state: open cards, selection, pointer drag, the
  in-memory clipboard (`clipboard`), undo of deletes (a toast with an action), shared block helpers.
- Cards (`BlockCard`) edit `props` with the generic field widgets (`Fields` over the block type's props, paths
  `body[2].props.heading`; `token` props are a select of the set's values; richtext props use `RichTextEditor`) and
  render slots as nested `BlockList`s. Palette (`Palette`): a popover with search and arrow keys, a second step to
  pick a blockset item for a shared block. Disabled actions say why (`blocks.reason.*`).
- Keyboard: Alt+Up/Down on a card header reorders, Delete removes (undo toast), "Move to..." opens `MoveDialog`.
  Pointer: the grip drags (4 px threshold, Escape cancels, edge auto-scroll); drop targets are `[data-block-id]` and
  `[data-drop-list]` elements, and a target the rules refuse shows no indicator.
- Ids: new blocks get a client UUIDv7; duplicates, pastes and detached shared blocks get new ids.
- Shared blocks (`SharedBody`): shown read-only (name, status, the block types it holds) with "Edit shared" and
  "Detach"; "Save as shared" (`SaveSharedDialog`) creates a draft item of a blockset type and swaps the block.
- API errors: `FieldCtx.errors()` lets the field route `body[1].slots.items[0].props.title` to the property,
  slot-level and block-level messages to the slot or card, and anything it cannot place to a list above the field;
  cards with errors open and show a count.

## Preview pane (`lib/preview`)

The item editor's "Preview" button (Ctrl/Cmd+Shift+P) opens `PreviewPane` (lazy) in the inspector column, with the
inspector one tab away; the column resizes (min 360 px, width remembered in `h.preview.width`). Below the inline
breakpoint it is the same slide-over as the inspector, so the pages split view gets it too.

- The pane mints a token (`POST /preview-tokens {channel, item, locale, ttl: 3600}`), renews it at 50 minutes, and
  loads the returned `url` in `<iframe sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
  referrerpolicy="no-referrer">`. When the preview URL has the admin's own origin the frame is locked: the sandbox
  drops `allow-same-origin` (opaque origin; messages are accepted by source window alone and posted with target
  `*`). The admin's CSP needs the frontend origin in `frame-src` (the server adds each
  channel's `frontend` origin).
- `host.ts` (`PreviewHost`) is the admin side of the bridge protocol (`docs/bridge-protocol.md`): it accepts messages
  only from the frame window and the preview URL's origin, verifies each `ready` (`ready.preview` must be the SHA-256
  hex of the minted token, else status "Not the previewed page" and the message is ignored), answers `ready` (live) with `init` `{item, itemType,
  locale, rev, doc: {fields}}`, sends a debounced (50 ms) `patch` per change (`patch.ts` diffs plain JSON into
  add/remove/replace operations: key by key for objects, per index for same-length arrays, adds or removes at the
  position of a pure insertion or removal, otherwise a whole replace), answers `resync` with `init`, and in reload
  mode (`ready.mode === 'reload'`) asks the editor to save the draft after 600 ms of quiet, then sends `reload`.
- The document is `{fields}` of the edited locale as the form holds it (cleaned like a save). Shared blocks get
  their `blocks` inlined from the shared item's draft, as delivery does.
- Autosave (`Editor.svelte`, `autosave`) is a normal draft `PATCH` with `If-Match`: it replaces the draft in place,
  writes an audit event and an `item.updated` outbox event, and never creates a version. It does not reset the form,
  so typing is not interrupted; edits made while it runs stay dirty (`Form.markSavedAs`).
- Status: connecting, live, reload mode (with "Saving...", "Saved" or "Not saved"), error with the frontend's message;
  a render time badge shows `rendered.ms`. No preview address, no channel, or a missing right each explain themselves.

## Canvas (`lib/canvas`)

Visual editing over the preview frame (H7). It lives in the lazy `PreviewPane` chunk and is mounted only when the
toggle "Edit on canvas" is on (remembered in `h.preview.canvas.<principal>`, default on) and the last verified
`ready` said `edit: true`; an older bridge disables the toggle with an explanation. The frame only measures and
reports (`layout`, `hover`, `select`, `inlineEdit`); everything drawn is admin DOM.

- `PreviewPane` sends `overlay {on, texts}` (`texts.ts` `textRefs` of the working document) and passes
  `fields`, `setField` and `readonly` from the editor. `Canvas.svelte` sits in `.h-preview__deviceframe` and
  multiplies the frame's CSS-pixel rects by `scale`.
- Selection is `{field, id?, prop?}` (block id, so it survives reorders); the frame path is derived with
  `paths.ts`. Frame click -> `BlocksModel.reveal` and toolbar focus; focus inside `.h-item__form` ->
  selection and `scrollTo`; double click on text -> `editText`, and the committed `inlineEdit` value goes through
  `model.commit` or `setField`.
- `Toolbar.svelte` is `role="toolbar"` with a roving tabindex (Left/Right/Home/End). Keys: Alt+arrows move, Up/Down
  select the previous/next block, Enter shows the form, Delete removes (undo toast), Mod+D duplicates, Escape clears.
  Every action reuses the `BlocksModel` operations, so rules, refusals (as `title`), live announcements and undo
  are the form's own. Insert uses `Palette` with `variant="canvas"` (`anchor` for a menu-opened palette).
- Drag: the grip starts a pointer drag over `.h-canvas__glass`; `drop.ts` `dropAt` picks the target from the
  reported rects, restricted to the same blocks field and `canMove`; the edges autoscroll the frame.
- Empty blocks fields get an "Add block to {field}" button (`EmptyAdd.svelte`); strings are in `messages/canvas.ts`.

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
| `Button` | `variant: 'primary'\|'secondary'(default)\|'ghost'\|'danger'`, `size: 'sm'(32)\|'md'(36)\|'lg'(44)`, `icon?`, `loading?` (spinner replaces the icon, disabled), `block?` (full width), `href?` (renders `<a>`; add `use:link` yourself for in-app navigation), `type` (default `button`), `disabled` (45% opacity), children |
| `IconButton` | `icon`, `label` (required: aria-label and tooltip via `title`), `variant: 'ghost'\|'secondary'\|'danger'`, `size: 'sm'\|'md'\|'lg'`; a 36 px circle |
| `Input` | `value` (bindable), `type`, `invalid?`, `icon?` (leading icon, `search` for search boxes), `large?` (44 px), `el?` (bindable element), HTML input attributes |
| `Textarea` | `value` (bindable), `rows` (4), `invalid?` |
| `Select` | `value` (bindable), `options: {value, label, disabled?}[]`, `placeholder?` (adds an empty first option), `invalid?`; `class` goes on the wrapper (width), other attributes on the `<select>` |
| `Checkbox` | `checked` (bindable), `indeterminate?`, `label?` or children |
| `Switch` | `checked` (bindable), `label?` or children (role=switch) |
| `Field` | `label`, `description?` (shown under the label), `error?` (icon + message, role=alert), `required?` (muted `*`), `id?`, children snippet receiving `{id, 'aria-describedby', 'aria-invalid', invalid}`: spread it on the control: `{#snippet children(c)}<Input {...c} bind:value />{/snippet}` |
| `DatePicker` | `value` (bindable ISO text), `type: 'date'\|'datetime-local'\|'time'`, `min`, `max`, `invalid?` (native input) |
| `Combobox` | see below |
| `Badge` | `tone: 'neutral'\|'success'\|'warning'\|'danger'\|'info'`, `dot?` (6 px leading dot), children |
| `Chip` | `label`, `variant: 'neutral'(default)\|'applied'(accent-filled)\|'ghost'(dashed "+ Add filter")`, `icon?`, `menu?` (chevron), `onremove?` (adds the × button), `removeLabel?`; other attributes (onclick) go on the main button |
| `Avatar` | `name` (initials; the tint is hashed from it), `size: 24\|28\|32\|40` |
| `Segmented` | `options: {value, label, icon?, dot?: 'filled'\|'half'\|'hollow', disabled?, iconOnly?}[]`, `value` (bindable), `label` (group name), `size: 'sm'\|'md'`, `onchange?`. Buttons with `aria-pressed`; labels never wrap |
| `Tabs` | `tabs: {id, label, count?, icon?, disabled?}[]`, `value` (bindable id), `label`, `variant: 'underline'(page-level view tabs)\|'segmented'(inside panels)`, `children?` snippet `(activeId)` rendering the panel (omit for tabs that only drive the page), `trailing?` snippet (a "+ Save view" ghost tab), `onchange?`. Arrow keys, Home/End; the row scrolls instead of wrapping |
| `Table` | `columns: {key, label, sortable?, align?, num?, width?, mobile?: 'hide', cell?: Snippet<[row]>}[]`, `rows`, `rowKey(row)`, `caption` (a11y), `sort?: {key, dir}`, `onsort(key)`, `selectable`, `selected` (bindable `Set<string>`), `sticky` (true), `maxHeight?` (scroll card; makes the sticky header work), `loading` (skeleton rows), `empty?` snippet, `footer?` snippet (inside the card: range, page size, `Pagination`), `onrowclick?(row)` (mouse only: also put a link in a cell). A card with 40 px header and 52 px rows; below 768 px rows become stacked cards (first column = title, others show their label). Types `Column`, `Sort` from `lib/ui/table.ts` |
| `NameCell` | the primary cell: `name`, `sub?` (muted second line), `icon?` (32 px tile) or `avatar?` (a name), `href?` (a real link) |
| `Time` | `at` (ISO or Date): `<time>` with the relative text and the exact time in `title` |
| `Pagination` | `page` (1-based), `pageSize`, `total?`, `hasMore?` (when total unknown), `onpage(page)` |
| `BulkBar` | `count`, `onclear`, children (the actions, usually ghost `Button size="sm"`): the floating black pill "3 selected · Publish · Delete · ×"; renders nothing at 0 |
| `Card` | `title?`, `padding: 'md'(24)\|'sm'(20)\|'none'`, `action?` snippet (header right), `level?` (heading 2), children; other attributes (`data-*`, `class`, grid spans) go on the `<section>` |
| `PageHeader` | `title`, `breadcrumbs?`, `badge?` snippet, `meta?` (string or snippet), `actions?` snippet (secondary pills), `overflow?: MenuItem[]` (the ⋯ menu), `primary?` snippet (the one black pill) |
| `Menu` | `items: {label, icon?, href?, onselect?, danger?, disabled?, separator?, checked?, hint?, heading?}[]`, `label` (a11y name), `align: 'end'\|'start'`, `side: 'bottom'\|'top'`, `trigger?` snippet (default a ⋯ icon button), `triggerClass?`. Arrow keys, Home/End, Esc; `href` is an in-app path; `heading: true` entries are group labels |
| `Popover` | `label`, `trigger` snippet, children snippet `(close)`, `align`, `triggerClass?`, `open` (bindable). A free-form panel (filter editors); Esc and click-away close, focus returns |
| `Dialog` | `open` (bindable), `title`, `onclose?`, `size: 'sm'(400)\|'md'(480)\|'lg'(640)`, `dismissible` (true), `bare?` (no header/padding; title stays the a11y name), children, `footer?` snippet (right-aligned: secondary then primary/danger). Moves focus in, traps Tab, Esc closes, restores focus; rendered in `<body>` |
| `EmptyState` | `title`, `description?`, `icon?` (32 px line icon in a 56 px circle), `actions?` snippet |
| `Skeleton` | `lines`, `height`, `width`, `shape: 'line'\|'circle'\|'block'` (shimmer; off under reduced motion) |
| `Breadcrumbs` | `items: {label, href?}[]` (`href` is an in-app path), `label?`, `inline?` (render here instead of declaring the top bar trail) |
| `Logo` | `size` (28): the H mark; it follows `--h-accent`. A theme `logo` replaces it |
| `Tree` | see below |
| `Icon` | `name`, `size` (18; use 14/16 dense, 28 empty states), `label?` (makes it an image with a name) |
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

`Shell.svelte` (glue), `Sidebar.svelte`, `Topbar.svelte`, `CommandPalette.svelte` (lazy chunk, first ⌘K).

- **Sidebar**: 256 px on the canvas, no border. Logo + theme name (`app.name`) + collapse button. The collapse
  state (64 px rail with tooltips) is remembered per user in `localStorage` (`h.nav.<principal>`); without a choice
  it starts as a rail below 1024 px. Below 768 px it is a drawer (menu button, scrim, Esc). Rail items keep their
  accessible names. Navigation comes from `types` (Content grouped by `admin.group`; label and `admin.icon` from
  the type; singletons in a collapsible "Settings content" group), `/pages/sites` (Pages; one entry per site,
  displayed capitalised, when `config().pages` and `pages.read` is held; narrowed to the active channel's site),
  extension views under their menu heading, and Settings (Users, Groups, Roles, API tokens, Audit gated by
  `can()`), collapsible and closed until the current page is inside it (the rail shows everything). Active item =
  raised white pill. **Saved views** nest under their collection, read from `localStorage` (`h.views.<principal>.<type>`,
  written by the list's views dialog, which dispatches `window` event `h:views` after saving). Bottom: the theme
  toggle (system/light/dark; one cycle button in the rail) and the account button (avatar + name, accessible name
  "Account menu: <name>") with Account and Sign out.
- **Top bar** (64 px, sticky, transparent until scrolled, then surface + hairline): breadcrumbs (from the
  `crumbs` store), search pill (opens the palette; ⌘K / Ctrl+K anywhere), channel pill (globe, `channels.select`),
  black "Create" pill (menu of creatable types: `content.create`, channel-visible), and on narrow screens the
  menu button and a profile avatar. The palette searches items by name with one list request per readable
  collection (`q`, 4 each), and offers types, pages sites and actions ("New <type>", "Go to Settings"); Enter on
  the first row runs `/search?q=`. Recent items are the ones opened from the palette.
- **Main**: `<main class="h-main">` has no padding and no width cap; the screen's root `.h-page` brings the padding.
- Channel filtering: lists and navigation apply `channels.collections` / `channels.languages` as before.

### Dashboard and login

Login: split layout from 1024 px (form column max 400 px; right: a CSS-only pattern panel with the product
name); below that the form only. Labels "Login" and "Password", button "Sign in", slots `login.before/after` stay
in the form column. Dashboard: greeting by time of day, stats (drafts awaiting publish = one `status=draft`
count request per readable collection; published this week from `/audit?action=item.publish&since=` (hidden
without `audit.read`); collections; locales), recent edits as sentences (`/audit?action=item.*`; users from
`/users`, items from `GET /items/{id}?draft=true`, each degrading to "Someone" / "an item"; without `audit.read`
the principal's newest drafts), quick create, then extension dashboard widgets as cards
(`section[data-dashboard-widget]`).

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
