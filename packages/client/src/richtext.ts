/** Framework-neutral rich text (ProseMirror documents): link safety and resolution, and a walk to plain element nodes. */

export interface PMMark { type: string; attrs?: Record<string, any> }
export interface PMNode { type: string; attrs?: Record<string, any>; content?: PMNode[]; marks?: PMMark[]; text?: string }

/** A rendered node: text, or an element (`kids` undefined for void elements). */
export type RichNode = string | { tag: string; attrs: Record<string, string | number | undefined>; kids?: RichNode[] }

export interface RichTextOptions {
  /** delivery's `links` (item id -> URL); `item:<id>` hrefs are looked up here */
  links?: Record<string, string>
  /** resolves an `item:<id>` href first; return undefined to fall back to `links` */
  resolveLink?: (href: string) => string | undefined
}

// http(s), mailto and scheme-less (relative) URLs only; no protocol-relative (//host) and no backslashes
export function safeHref(u: string | undefined): string | undefined {
  const s = u?.replace(/[\u0000- \u007f-\u009f]/g, '')
  if (u?.includes('\\') || /^[/\\]{2}/.test(s ?? '')) return undefined
  return s && (!/^[a-z][a-z0-9+.-]*:/i.test(s) || /^(https?|mailto):/i.test(s)) ? u!.trim() : undefined
}

/** The checked URL of a link mark's `href`: `item:<id>` through `resolveLink` and `links`, anything unsafe undefined. */
export function linkHref(attrs: Record<string, any>, o: RichTextOptions = {}): string | undefined {
  const h = String(attrs.href ?? '')
  return safeHref(h.startsWith('item:') ? o.resolveLink?.(h) ?? o.links?.[h.slice(5)] ?? o.links?.[h] : h)
}

const tags: Record<string, string> = {
  paragraph: 'p', bulletList: 'ul', listItem: 'li', blockquote: 'blockquote', horizontalRule: 'hr', hardBreak: 'br',
  bold: 'strong', italic: 'em', code: 'code',
}
const VOID = new Set(['hr', 'br'])

/** A ProseMirror document as plain element nodes: links resolved and checked, unknown nodes and marks reduced to their content. */
export function richTextNodes(doc: PMNode | null | undefined, o: RichTextOptions = {}): RichNode[] {
  const wrap = (type: string, a: Record<string, any>, kids: RichNode[]): RichNode[] => {
    switch (type) {
      case 'heading': return [{ tag: 'h' + Math.min(6, Math.max(1, Number(a.level) || 1)), attrs: {}, kids }]
      case 'orderedList': return [{ tag: 'ol', attrs: { start: a.order > 1 ? Number(a.order) : undefined }, kids }]
      case 'codeBlock':
        return [{ tag: 'pre', attrs: {}, kids: [{ tag: 'code', attrs: { class: typeof a.language === 'string' && /^[\w+-]+$/.test(a.language) ? 'language-' + a.language : undefined }, kids }] }]
      case 'link': {
        const href = linkHref(a, o)
        return href ? [{ tag: 'a', attrs: { href }, kids }] : kids
      }
      default: {
        const t = tags[type]
        return t ? [{ tag: t, attrs: {}, kids: VOID.has(t) ? undefined : kids }] : kids
      }
    }
  }
  const node = (n: PMNode): RichNode[] => {
    if (n.type === 'text') {
      if (!n.text) return []
      return (n.marks ?? []).reduceRight<RichNode[]>((c, m) => wrap(m.type, m.attrs ?? {}, c), [n.text])
    }
    return wrap(n.type, n.attrs ?? {}, (n.content ?? []).flatMap(node))
  }
  return doc ? (doc.content ?? []).flatMap(node) : []
}
