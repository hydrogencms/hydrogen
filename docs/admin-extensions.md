# Admin extensions

The admin is an API client. You extend it with custom elements that it loads lazily from your project or from a
module, and mounts in named places. Nothing is rebuilt: you declare the elements in a manifest, ship them as an ES
module, and the admin imports that module the first time an element is needed.

Elements are project code. They run in the admin page with the signed-in user's session rights, like Go modules do;
only ship elements you trust. The server still enforces every API call.

TypeScript types and three helpers live in `@hydrogencms/admin-sdk` (`defineElement`, `emit`, `useContext`).

## The manifest

The project declares it under `admin:` in `config/app.yaml`; `hydrogen config:check` validates it. File paths are
paths on disk, relative to the working directory. The admin receives the composed result as `extensions` in
`<prefix>/config.json` and the files are served under `<prefix>/x/`.

```yaml
admin:
  elements: [./admin/dist/elements.js]      # ES modules that define the custom elements
  theme: { name: Acme CMS, logo: ./admin/logo.svg, accent: "#0b6", css: ./admin/theme.css }
  strings: ./admin/strings.json             # {"key": "text"}: overrides or adds admin strings
  widgets: { brand-color: x-brand-color }   # name -> element; fields and block props use `admin: { widget: name }`
  cells: { swatch: x-color-swatch }         # name -> element; `admin.list` entry "field:swatch"
  panels:  [{ tag: x-seo-panel, title: SEO, types: [article] }]
  actions: [{ tag: x-translate, title: Translate, types: [article], policy: content.edit }]
  list_actions: [{ tag: x-export-csv, title: Export CSV, types: [article], policy: content.read }]
  dashboard: [{ tag: x-recent, title: Recently edited }]
  views:   [{ path: /tools/import, tag: x-import, menu: Tools, title: Import, policy: import.run }]
  slots:   [{ tag: x-banner, at: editor.before }]
  previews: { hero: x-hero-preview }        # block type -> element
```

Rules `config:check` enforces: element tags are custom element names (lower case, with a dash); `widgets`, `cells`
and `previews` keys are names (letters, digits, `_`, `-`); every `types` entry is a content type; every `policy` is a
declared policy; view paths start with `/`, may use `:param` segments and one final `*`, and may not equal or sit
under a built-in route; slot positions are from the list below; every file exists; `previews` keys are block types;
and every `admin.widget` a field or a block prop names, and every `admin.list` cell, is declared by the project or a
module. `title` is required for panels, actions, list actions and views.

`policy` hides an entry from principals who lack it (the admin evaluates `can(policy)`; the API still enforces what
the element calls). `types` limits an entry to content types; empty means every type. Dashboard entries take no
`types`.

## Element contract

The admin creates the element, sets a `ctx` property and the props below as **properties** (never attributes)
before it connects, and may set them again later. Data goes out through bubbling, composed `CustomEvent`s
(`emit(el, name, detail)`). Only `CustomEvent`s count: a native `change` bubbling from an inner input is ignored,
and `change` and `navigate` need an object `detail`. Do not keep state across property changes you did not cause.

`ctx` (`AdminContext`):

| Member | Meaning |
| --- | --- |
| `api.get/post/patch/del(path, init?)` | the management API with the user's session; resolves parsed JSON, rejects with `{ status, code, message, fields? }` |
| `t(key, vars?)` | admin strings, including yours from `strings` |
| `navigate(path)` | admin-relative navigation; absolute and protocol URLs are refused |
| `toast(kind, text)` | `info`, `success` or `error` |
| `principal` | `{ kind, id?, name }` (`anonymous` on login) |
| `can(policy, { type? })` | whether the principal holds the policy |
| `locale`, `prefix` | UI locale and the admin mount prefix |

| Kind | Props (besides `ctx`) | Events |
| --- | --- | --- |
| widget | `value`, `field`, `locale`, `readonly`, `error?` | `change` `{ value }`; optional `blur` |
| cell | `value`, `item`, `field` | none; renders inline |
| panel | `item`, `type`, `locale`, `draft` | `refresh` (reload the item) |
| action | `item`, `type`, `locale` | `refresh`, `navigate` `{ path }` |
| list action | `type`, `selected` (item ids, empty when nothing is selected), `query` (the list's current query parameters) | `refresh` (reload the list), `navigate` `{ path }` |
| dashboard widget | none | none |
| view | `params`, `query` | `navigate` `{ path }` |
| slot | `at`, plus the subject below | none |
| block preview | `type`, `props`, `context` (`"palette"` or `"card"`) | none |

## Extension points

### Field widgets and block prop widgets

`admin: { widget: brand-color }` on a field replaces its input with the declared element. Block props reuse the
field types, so the same key works on a prop in the `blocks:` section of the schema and the element gets the same
props and events. `config:check` resolves widget names against the merged manifest of the project and all modules.
An unknown name falls back to the built-in input with one console warning.

### Cells

`admin.list: [title, "color:swatch"]` renders that column of the collection list with the `swatch` cell.

### Panels, actions and list actions

Panels are sections beside the editor; actions sit in the editor toolbar and draw their own buttons. List actions sit
in the collection list's toolbar (always visible) and in its bulk bar (when items are selected).

A panel whose `types` name an upload type also appears as a section of the asset details panel, with the same props.
A panel with no `types` appears in the editor only.

### Dashboard and views

Dashboard entries are widgets on the home screen. A view is a full screen at an admin-relative path, with a menu
entry grouped under `menu`; the views the principal may open are also listed in the command palette (group
"views"). Views with `:param` or `*` paths are not listed there.

### Slots

`slots: [{ tag, at, policy? }]` places an element before or after a built-in screen. Positions: `login`, `list`,
`editor`, `dashboard`, `pages`, `assets`, `search`, `releases`, `reviews`, `trash` and `settings`, each with
`.before` and `.after`. Props besides `at`: the editor adds `item` and `type`, the list `type`, pages `site`,
search `q`, and settings `section` (`users`, `groups`, `roles`, `tokens`, `audit` or `system`).

### Block previews

`previews: { hero: x-hero-preview }` shows the element as the thumbnail of that block type in the block palette and
as the collapsed summary of its card in a blocks field. In the palette `props` are the type's defaults. The admin
fixes the box: a palette tile of about 160 by 90 px and a one-line summary on cards, clipped, non-interactive and
hidden from assistive technology, so keep it purely visual and cheap. A missing, slow or throwing element leaves the
built-in rendering in place.

### Theme and strings

The theme (project only) sets the app name, logo, accent colour and an extra stylesheet. `strings` is a JSON object of
key to text; the project's file wins over modules'. `theme` values may not contain `url(`, `;` or `}`.

## Loading and failures

The element modules load on first use, once per URL. If one fails to load the admin shows one error toast per URL and
retries on the next use. Entries whose element is not defined render nothing; unknown widget and cell names warn once
and use the built-in rendering. A throwing preview falls back to the built-in one.

## Building elements

Svelte 5 custom elements are the recommended way (`<svelte:options customElement="x-banner" />`), Lit works equally
well, and a plain `class extends HTMLElement` is enough for small ones. Bundle everything into the files listed under
`elements`. Register with `defineElement(tag, ctor)` so hot reloads do not throw. `hydrogen new` ships an `admin/`
starter.

In development, set `ADMIN_ELEMENTS_DEV_URL` to your Vite dev server; the admin then lists
`<prefix>/x/dev<elements_dev_entry>` (default `/src/elements.ts`) in place of the built files, proxied with hot module
replacement, and `config:check` skips the existence check of the element files. This is honoured only when
`APP_ENV` is `dev`.

## Modules

A module registers a manifest with `Registry.AdminExtension(kernel.AdminExtension{Files: fsys, Manifest: m})`. File
names in the manifest (`elements`, `strings`) are names inside `Files` (an `fs.FS`, typically embedded) and are
served under `<prefix>/x/m/<module>/`. A module cannot set the theme. Lists from modules follow the project's;
`widgets`, `cells`, `previews` and view paths must be unique across the project and all modules, and `config:check`
reports clashes. A module manifest is checked like the project's, including policies it declares with
`Registry.Policy`.
