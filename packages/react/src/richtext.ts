import { createElement, Fragment } from 'react'
import type { ReactNode } from 'react'

export interface PMMark { type: string; attrs?: Record<string, any> }
export interface PMNode { type: string; attrs?: Record<string, any>; content?: PMNode[]; marks?: PMMark[]; text?: string }

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

// http(s), mailto and scheme-less (relative) URLs only; no protocol-relative (//host) and no backslashes
function safe(u: string | undefined): string | undefined {
  const s = u?.replace(/[\u0000- \u007f-\u009f]/g, '')
  if (u?.includes('\\') || /^[/\\]{2}/.test(s ?? '')) return undefined
  return s && (!/^[a-z][a-z0-9+.-]*:/i.test(s) || /^(https?|mailto):/i.test(s)) ? u!.trim() : undefined
}

const tags: Record<string, string> = {
  paragraph: 'p', bulletList: 'ul', listItem: 'li', blockquote: 'blockquote', horizontalRule: 'hr', hardBreak: 'br',
  bold: 'strong', italic: 'em', code: 'code',
}

export function RichText(p: RichTextProps): ReactNode {
  const { links, resolveLink, render } = p
  const wrap = (type: string, attrs: Record<string, any>, children: ReactNode, key?: number): ReactNode => {
    const o = render?.[type]
    if (o) return createElement(Fragment, { key }, o({ attrs, children }))
    switch (type) {
      case 'heading': return createElement('h' + Math.min(6, Math.max(1, Number(attrs.level) || 1)), { key }, children)
      case 'orderedList': return createElement('ol', { key, start: attrs.order > 1 ? Number(attrs.order) : undefined }, children)
      case 'codeBlock': return createElement('pre', { key }, createElement('code', { className: /^[\w+-]+$/.test(attrs.language) ? 'language-' + attrs.language : undefined }, children))
      case 'link': {
        const h = String(attrs.href ?? '')
        const href = safe(h.startsWith('item:') ? resolveLink?.(h) ?? links?.[h.slice(5)] ?? links?.[h] : h)
        return href ? createElement('a', { key, href }, children) : createElement(Fragment, { key }, children)
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
