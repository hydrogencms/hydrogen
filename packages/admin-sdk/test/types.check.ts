import type { ExtensionManifest, WidgetProps, AdminContext, WidgetChangeEvent } from '../dist/index.js'
import { useContext } from '../dist/index.js'

const m: ExtensionManifest = {
  elements: ['/admin/x/0.js'],
  widgets: { c: 'x-c' },
  panels: [{ tag: 'x-p', title: 'P', types: ['a'] }],
  slots: [{ tag: 'x-b', at: 'editor.before' }],
}
// @ts-expect-error unknown slot position
const bad: ExtensionManifest = { slots: [{ tag: 'x-b', at: 'nowhere' }] }
declare const p: WidgetProps
const ctx: AdminContext = p.ctx
const ev: WidgetChangeEvent = new CustomEvent('change', { detail: { value: 1 } })
void [m, bad, ctx, ev, useContext]
