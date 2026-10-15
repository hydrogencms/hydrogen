# @hydrogencms/admin-sdk

Types and three helpers for building admin extension elements: custom elements that the Hydrogen admin loads from
your project (or a module) and mounts in its widget, cell, panel, action, dashboard, view and slot positions. ESM,
no runtime dependencies, under 5 KB gzip.

Elements are project code. They run in the admin page with the signed-in user's session rights, like Go modules do;
only install elements you trust. The server still enforces every API call.

## Manifest

Declared in `config/app.yaml` under `admin:` and sent to the admin as `extensions` in `<prefix>/config.json`
(typed as `ExtensionManifest`).

```yaml
admin:
  elements: [./admin/dist/elements.js]   # ES modules that define the custom elements
  theme: { name: Acme CMS, logo: ./admin/logo.svg, accent: "#0b6", css: ./admin/theme.css }
  strings: ./admin/strings.json          # {"key": "text"} overriding or adding i18n keys
  widgets: { brand-color: x-brand-color }   # field `admin: { widget: brand-color }`
  cells: { swatch: x-color-swatch }         # `admin.list` entry "field:swatch"
  panels:  [{ tag: x-seo-panel, title: SEO, types: [article] }]
  actions: [{ tag: x-translate-action, title: Translate, types: [article], policy: content.edit }]
  dashboard: [{ tag: x-recent-widget, title: Recently edited }]
  views:   [{ path: /tools/import, tag: x-import-tool, menu: Tools, title: Import, policy: import.run }]
  slots:   [{ tag: x-banner, at: editor.before }]
```

In `config.json` the URLs are prefix-relative. `strings` is always an array of URLs: the project YAML takes one path,
modules add theirs, and later entries win. Panels, dashboard entries and slots also accept an optional `policy`;
the admin hides the entry when `can(policy)` is false.

## Contract

The admin sets a `ctx` **property** (an `AdminContext`, not an attribute) and the slot's props as properties before
the element connects, and may set them again later. Data goes in through properties and out through bubbling,
composed `CustomEvent`s. Do not keep state across property changes you did not cause.

`AdminContext`: `api.get/post/patch/del(path, init?)` (parsed JSON; rejects with `{ status, code, message, fields? }`),
`t(key, vars?)`, `navigate(path)` (admin-relative), `toast(kind, text)`, `principal`, `can(policy, { type? })`,
`locale` (the UI locale, `en` for now), `prefix`. In login slots there is no session, so `principal` is
`{ kind: 'anonymous', name: '' }`. Rejections carry `fields` as `{ path, code, message }[]` when validation fails.

| Slot | Props (besides `ctx`) | Events |
| --- | --- | --- |
| widget (`WidgetProps`) | `value`, `field`, `locale`, `readonly`, `error?` | `change` `{ value }` (`WidgetChangeEvent`); optional `blur` |
| cell (`CellProps`) | `value`, `item`, `field` | none (renders inline) |
| panel (`PanelProps`) | `item`, `type`, `locale`, `draft` | `refresh` (reload the item) |
| action (`ActionProps`) | `item`, `type`, `locale` | `refresh`, `navigate` `{ path }` |
| dashboard (`DashboardProps`) | none | none |
| view (`ViewProps`) | `params`, `query` | `navigate` `{ path }` |
| slot (`SlotProps`) | `at`, and `item`/`type` (editor) or `type` (list) | none |

Widgets fill their field row. Actions render their own buttons; the admin wraps them in the toolbar.

## Helpers

- `defineElement(tag, ctor)`: `customElements.define`, skipped with one warning when the tag exists (HMR).
- `emit(el, name, detail?)`: dispatches a bubbling, composed `CustomEvent`.
- `useContext(el): AdminContext`: returns `el.ctx`, or throws `x-foo: missing ctx; was it mounted by the admin?`.

## Svelte 5 (recommended)

Svelte custom elements need no helper. Build with `compilerOptions: { customElement: true }`.

```svelte
<!-- src/BrandColor.svelte -->
<svelte:options customElement={{ tag: 'x-brand-color', props: {
  ctx: { type: 'Object' }, value: { type: 'String' }, readonly: { type: 'Boolean' }, error: { type: 'String' },
} }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'
  let { ctx, value = '', readonly = false, error = '' }: {
    ctx: AdminContext; value?: string; readonly?: boolean; error?: string
  } = $props()
  const host = $host()

  function pick(e: Event) {
    const next = (e.target as HTMLInputElement).value
    host.dispatchEvent(new CustomEvent('change', { detail: { value: next }, bubbles: true, composed: true }))
  }
</script>

<label>
  <span>{ctx.t('brand.color')}</span>
  <input type="color" value={value || '#000000'} disabled={readonly} oninput={pick} />
</label>
{#if error}<small role="alert">{error}</small>{/if}
```

`ctx` is read like any prop. `$host()` is the element, so `emit(host, 'change', { value })` from the SDK does the
same as the `dispatchEvent` above.

## Lit

```ts
import { LitElement, html } from 'lit'
import { defineElement, emit, useContext, type WidgetProps } from '@hydrogencms/admin-sdk'

class XHello extends LitElement implements Partial<WidgetProps> {
  static properties = { ctx: { attribute: false }, value: {} }
  declare ctx: WidgetProps['ctx']
  declare value: string
  render() {
    return html`<input .value=${this.value ?? ''} @input=${(e: Event) =>
      emit(this, 'change', { value: (e.target as HTMLInputElement).value })} />`
  }
}
defineElement('x-hello', XHello)
```

## Plain class

```ts
import { defineElement, emit, useContext, type AdminContext } from '@hydrogencms/admin-sdk'

class XHello extends HTMLElement {
  ctx?: AdminContext
  value = ''
  connectedCallback() {
    const ctx = useContext(this)
    const input = document.createElement('input')
    input.value = this.value
    input.placeholder = ctx.t('hello.placeholder')
    input.oninput = () => emit(this, 'change', { value: input.value })
    this.append(input)
  }
}
defineElement('x-hello', XHello)
```

## Project layout

```
admin/
  package.json     svelte, vite, @hydrogencms/admin-sdk
  vite.config.ts   builds dist/elements.js (ES module, custom elements)
  src/elements.ts  imports every element so the bundle defines them
  logo.svg  theme.css  strings.json
```

`npm run build` writes `dist/elements.js`, which `admin.elements` points at. For live reload run `npm run dev` (the
Vite server, for example on `http://localhost:5174`) and start `hydrogen dev` with
`ADMIN_ELEMENTS_DEV_URL=http://localhost:5174`: the admin then imports the dev entry through the server's proxy
(`admin.elements_dev_entry`, default `/src/elements.ts`) and Vite's HMR redefines the elements. A tag that is already
defined cannot be redefined, so a changed element shows up on its next mount.
