import { createElement, Fragment } from 'react'
import type { ReactNode } from 'react'
import { richTextNodes } from '@hydrogencms/client/richtext'
import type { PMNode, RichNode, RichTextOptions } from '@hydrogencms/client/richtext'

export type { PMMark, PMNode, RichTextSchema } from '@hydrogencms/client/richtext'

/** Replaces the output for a node or mark type (they share one namespace): `{ heading: ({ attrs, children }) => … }`. */
export type RenderOverrides = Record<string, (p: { attrs: Record<string, any>; children: ReactNode }) => ReactNode>

export interface RichTextProps extends Omit<RichTextOptions, 'render'> {
  doc: PMNode | null | undefined
  render?: RenderOverrides
}

const prop: Record<string, string> = { class: 'className', colspan: 'colSpan', rowspan: 'rowSpan' }

export function RichText({ doc, render, ...o }: RichTextProps): ReactNode {
  const el = (n: RichNode, key: number): ReactNode => {
    if (typeof n === 'string') return n
    if (!n.tag) return createElement(Fragment, { key }, n.raw as ReactNode)
    const p: Record<string, any> = { key }
    for (const k in n.attrs) p[prop[k] ?? k] = k === 'style' && n.attrs[k] ? { textAlign: String(n.attrs[k]).slice(11) } : n.attrs[k]
    return createElement(n.tag, p, n.kids?.map(el))
  }
  const ov = render && Object.fromEntries(Object.entries(render).map(([t, f]) => [t, (attrs: Record<string, any>, kids: RichNode[]): RichNode[] => [{ tag: '', attrs: {}, raw: f({ attrs, children: kids.map(el) }) }]]))
  return doc ? createElement(Fragment, null, richTextNodes(doc, { ...o, render: ov }).map(el)) : null
}
