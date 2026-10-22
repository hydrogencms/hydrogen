import { richTextNodes } from '@hydrogencms/client/richtext'
import type { PMNode, RichNode, RichTextOptions as NodeOptions } from '@hydrogencms/client/richtext'

export type { PMMark, PMNode, RichTextSchema } from '@hydrogencms/client/richtext'

/** Replaces the HTML of a node or mark type (they share one namespace): `{ heading: (attrs, html) => … }`. The result is trusted markup. */
export type RenderOverrides = Record<string, (attrs: Record<string, any>, html: string) => string>

export interface RichTextOptions extends Omit<NodeOptions, 'render'> {
  render?: RenderOverrides
}

const E: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
export const escapeHtml = (s: string): string => s.replace(/[&<>"']/g, (c) => E[c]!)

const html = (ns: RichNode[]): string =>
  ns.map((n) => typeof n === 'string' ? escapeHtml(n) : !n.tag ? String(n.raw)
    : `<${n.tag}${Object.entries(n.attrs).map(([k, v]) => (v === undefined ? '' : ` ${k}="${escapeHtml(String(v))}"`)).join('')}>${n.kids ? html(n.kids) + `</${n.tag}>` : ''}`).join('')

/** The HTML of a ProseMirror rich text document; text and attributes are escaped, links go through the item's `links`. */
export function richTextHtml(doc: PMNode | null | undefined, { render, ...o }: RichTextOptions = {}): string {
  const ov = render && Object.fromEntries(Object.entries(render).map(([t, f]) => [t, (attrs: Record<string, any>, kids: RichNode[]): RichNode[] => [{ tag: '', attrs: {}, raw: f(attrs, html(kids)) }]]))
  return html(richTextNodes(doc, { ...o, render: ov }))
}
