import { createElement, Fragment } from 'react'
import type { ReactNode } from 'react'
import { cellAttrs, imageAttrs, linkHref, tags } from '@hydrogencms/client/richtext'
import type { PMNode } from '@hydrogencms/client/richtext'

export type { PMMark, PMNode } from '@hydrogencms/client/richtext'

/** Replaces the output for a node or mark type (they share one namespace): `{ heading: ({ attrs, children }) => … }`. */
export type RenderOverrides = Record<string, (p: { attrs: Record<string, any>; children: ReactNode }) => ReactNode>

export interface RichTextProps {
  doc: PMNode | null | undefined
  /** delivery's `links` (item id → URL); `item:<id>` hrefs are looked up here */
  links?: Record<string, string>
  /** resolves an `item:<id>` href first; return undefined to fall back to `links` */
  resolveLink?: (href: string) => string | undefined
  render?: RenderOverrides
}

export function RichText(p: RichTextProps): ReactNode {
  const { links, resolveLink, render } = p
  const wrap = (type: string, attrs: Record<string, any>, children: ReactNode, key?: number): ReactNode => {
    const o = render?.[type]
    if (o) return createElement(Fragment, { key }, o({ attrs, children }))
    switch (type) {
      case 'heading': return createElement('h' + Math.min(6, Math.max(1, Number(attrs.level) || 1)), { key }, children)
      case 'orderedList': return createElement('ol', { key, start: attrs.order > 1 ? Number(attrs.order) : undefined }, children)
      case 'codeBlock': return createElement('pre', { key }, createElement('code', { className: typeof attrs.language === 'string' && /^[\w+-]+$/.test(attrs.language) ? 'language-' + attrs.language : undefined }, children))
      case 'link': {
        const href = linkHref(attrs, { links, resolveLink })
        return href ? createElement('a', { key, href }, children) : createElement(Fragment, { key }, children)
      }
      case 'table': return createElement('table', { key }, createElement('tbody', null, children))
      case 'tableCell': case 'tableHeader': return createElement(type === 'tableCell' ? 'td' : 'th', { key, colSpan: cellAttrs(attrs).colspan, rowSpan: cellAttrs(attrs).rowspan }, children)
      case 'image': {
        const img = imageAttrs(attrs)
        return img ? createElement('img', { key, ...img }) : null
      }
      case 'horizontalRule': case 'hardBreak': return createElement(tags[type]!, { key })
      default: {
        const t = tags[type]
        return t ? createElement(t, { key }, children) : createElement(Fragment, { key }, children)
      }
    }
  }
  const kids = (n: PMNode): ReactNode[] => (n.content ?? []).map(node)
  const node = (n: PMNode, i: number): ReactNode => {
    if (n.type === 'text') {
      if (!n.text) return null
      const out = (n.marks ?? []).reduceRight<ReactNode>((c, m) => wrap(m.type, m.attrs ?? {}, c), n.text)
      return createElement(Fragment, { key: i }, out)
    }
    return wrap(n.type, n.attrs ?? {}, n.type === 'doc' ? undefined : kids(n), i)
  }
  return p.doc ? createElement(Fragment, null, kids(p.doc)) : null
}
