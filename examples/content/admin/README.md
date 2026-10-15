# examples/content admin extensions

Svelte 5 custom elements on `@hydrogencms/admin-sdk`, one per extension point of the admin, wired in
`../config/app.yaml` (`admin:`):

| Element | Kind | Notes |
| --- | --- | --- |
| `x-brand-color` | field widget | `article.color` (`admin: { widget: brand-color }`); colour input plus hex text |
| `x-color-swatch` | list cell | `admin.list` entry `color:swatch` |
| `x-seo-panel` | editor panel | `article` only; score from title/summary length; compares with the published view |
| `x-translate-action` | editor action | policy `content.edit`; toasts and emits `refresh` |
| `x-recent-widget` | dashboard | last 5 audit entries through `ctx.api`, linking with `ctx.navigate` |
| `x-import-tool` | view | `/tools/import`, menu Tools, policy `import.run` (registered in `../main.go`) |
| `x-banner` | slot | `editor.before` |

Plus `logo.svg`, `theme.css` (tokens) and `strings.json` (overrides `dashboard.welcome`, adds two keys).

## Build

`make example-admin` (from the repo root) builds the SDK, runs `npm ci` and writes `dist/elements.js`, which is
committed so `config:check` and the Go tests need no Node. `make example-admin-check` rebuilds and fails when the
committed file differs (it is part of `make check`). The `@hydrogencms/admin-sdk` dependency is a `file:` path with
`install-links=true` (`.npmrc`), so the SDK's `dist` must exist first (`make example-admin` does that). The bundle
includes the Svelte runtime: about 105 KB, 31 KB gzip, loaded only when a screen uses an element.

## Styles and the CSP

Svelte custom elements put their component `<style>` into the shadow root as an inline `<style>` element. The
admin's CSP therefore allows inline styles (`style-src 'self' 'unsafe-inline'`); scripts stay `'self'`. The elements
here use normal `<style>` blocks, read the admin's `--h-*` tokens (custom properties inherit into shadow roots) and
set dynamic colours with Svelte's `style:` directive (CSSOM, never an attribute string). `theme.css` only sets tokens.
The e2e spec checks that the swatch is styled from its own block.

## Live reload

```sh
cd examples/content/admin && npm run dev                      # Vite on :5174 (ADMIN_PREFIX if the prefix differs)
cd examples/content && ADMIN_ELEMENTS_DEV_URL=http://localhost:5174 APP_ENV=dev go run . serve
```

`config.json` then lists `/admin/x/dev/src/elements.ts` instead of the built file; the server proxies `/admin/x/dev/*`
to Vite. Observed (Chromium, once): after editing the text in `src/Banner.svelte` the open editor showed the new
text about 0.4 s later without a page reload (`[vite] hot updated: /src/Banner.svelte`), and a reload kept it.
Limits: the module is imported when a screen first renders an element, so open one that uses the element you edit;
this proves in-place updates for edits inside a component. Changing the tag name or `<svelte:options>` still needs a
reload, because `customElements.define` cannot redefine a tag.

## `refresh`

`x-seo-panel` and `x-translate-action` emit `refresh` to ask the editor to reload the item. When the form has
unsaved edits the admin asks before discarding them, so an element can emit it without checking for dirty state.
