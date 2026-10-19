import { linkHref } from '@hydrogencms/client/richtext'
import type { PMNode, RichTextOptions as LinkOptions } from '@hydrogencms/client/richtext'

export type { PMMark, PMNode } from '@hydrogencms/client/richtext'

/** Replaces the HTML of a node or mark type (they share one namespace): `{ heading: (attrs, html) => … }`. The result is trusted markup. */
export type RenderOverrides = Record<string, (attrs: Record<string, any>, html: string) => string>

export interface RichTextOptions extends LinkOptions {
  render?: RenderOverrides
}

const E: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
export const escapeHtml = (s: string): string => s.replace(/[&<>"']/g, (c) => E[c]!)

const tags: Record<string, string> = {
  paragraph: 'p', bulletList: 'ul', listItem: 'li', blockquote: 'blockquote', bold: 'strong', italic: 'em', code: 'code',
}

/** The HTML of a ProseMirror rich text document; text and attributes are escaped, links go through the item's `links`. */
export function richTextHtml(doc: PMNode | null | undefined, o: RichTextOptions = {}): string {
  const wrap = (type: string, attrs: Record<string, any>, inner: string): string => {
    const ov = o.render?.[type]
    if (ov) return ov(attrs, inner)
    switch (type) {
      case 'heading': {
        const h = 'h' + Math.min(6, Math.max(1, Number(attrs.level) || 1))
        return `<${h}>${inner}</${h}>`
      }
      case 'orderedList': return `<ol${attrs.order > 1 ? ` start="${Number(attrs.order)}"` : ''}>${inner}</ol>`
      case 'codeBlock': return `<pre><code${typeof attrs.language === 'string' && /^[\w+-]+$/.test(attrs.language) ? ` class="language-${attrs.language}"` : ''}>${inner}</code></pre>`
      case 'link': {
        const href = linkHref(attrs, o)
        return href ? `<a href="${escapeHtml(href)}">${inner}</a>` : inner
      }
      case 'horizontalRule': return '<hr>'
      case 'hardBreak': return '<br>'
      default: {
        const t = tags[type]
        return t ? `<${t}>${inner}</${t}>` : inner
      }
    }
  }
  const kids = (n: PMNode): string => (n.content ?? []).map(node).join('')
  const node = (n: PMNode): string => {
    if (n.type === 'text') return n.text ? (n.marks ?? []).reduceRight((c, m) => wrap(m.type, m.attrs ?? {}, c), escapeHtml(n.text)) : ''
    return wrap(n.type, n.attrs ?? {}, n.type === 'doc' ? '' : kids(n))
  }
  return doc ? kids(doc) : ''
}
