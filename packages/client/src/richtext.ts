/** Framework-neutral rich text (ProseMirror documents): link safety and resolution, and a walk to plain element nodes. */

export interface PMMark { type: string; attrs?: Record<string, any> }
export interface PMNode { type: string; attrs?: Record<string, any>; content?: PMNode[]; marks?: PMMark[]; text?: string }

/** A rendered node: text, or an element (`kids` undefined for void elements). */
export type RichNode = string | { tag: string; attrs: Record<string, string | number | undefined>; kids?: RichNode[]; raw?: unknown }

/** A custom node or mark as `hydrogen generate` writes it to `gen/richtext.ts`. */
export interface RichDecl { tag?: string; inline?: boolean; attrs?: Record<string, { type: string; default?: unknown }> }
export interface RichTextSchema { nodes?: Record<string, RichDecl>; marks?: Record<string, RichDecl> }

export interface RichTextOptions {
  /** delivery's `links` (item id -> URL); `item:<id>` hrefs are looked up here */
  links?: Record<string, string>
  /** resolves an `item:<id>` href first; return undefined to fall back to `links` */
  resolveLink?: (href: string) => string | undefined
  /** declared custom nodes and marks (tags, inline flags, attr types and defaults); without it they render as div or span */
  schema?: RichTextSchema
  /** node or mark type -> class names added to its elements (tokens `[A-Za-z0-9_ -]`; custom: after `rt-<name>`) */
  classes?: Record<string, string>
  /** replaces the output of a node or mark type with plain nodes (`tag: ''` carries an adapter's own output in `raw`) */
  render?: Record<string, (attrs: Record<string, any>, kids: RichNode[]) => RichNode[]>
}

// http(s), mailto and scheme-less (relative) URLs only; no protocol-relative (//host) and no backslashes
export function safeHref(u: string | undefined): string | undefined {
  const s = u?.replace(/[\u0000- \u007f-\u009f]/g, '')
  if (u?.includes('\\') || /^[/\\]{2}/.test(s ?? '')) return undefined
  return s && (!/^[a-z][a-z0-9+.-]*:/i.test(s) || /^(https?|mailto):/i.test(s)) ? u!.trim() : undefined
}

/** The checked URL of a link mark's `href`: `item:<id>[#frag]` through `resolveLink(item:<id>)` and `links`, then `#frag` appended; `#id` kept; anything unsafe undefined. */
export function linkHref(attrs: Record<string, any>, o: RichTextOptions = {}): string | undefined {
  const h = String(attrs.href ?? ''), i = h.indexOf('#')
  if (!h.startsWith('item:')) return safeHref(h)
  const id = i < 0 ? h : h.slice(0, i), u = safeHref(o.resolveLink?.(id) ?? o.links?.[id.slice(5)] ?? o.links?.[id])
  return u && u + (i < 0 ? '' : h.slice(i))
}

/** The checked `src` of an image node: http(s) or root-relative (not protocol-relative), anything else undefined. */
export function imageSrc(u: string | undefined): string | undefined {
  const s = u?.trim()
  return s && !/[\\\u0000-\u001f\u007f]/.test(s) && /^(\/(?!\/)|https?:\/\/)/i.test(s) ? s : undefined
}

const num = (v: any, max: number): number | undefined => (v >= 1 && v <= max ? Math.floor(v) : undefined)

/** The attributes of an image node, or undefined when its `src` is unsafe. */
export function imageAttrs(a: Record<string, any>): Record<string, string | number | undefined> | undefined {
  const src = imageSrc(a.src)
  return src ? { src, alt: String(a.alt ?? ''), title: a.title || undefined, width: num(a.width, 1e5), height: num(a.height, 1e5), loading: 'lazy' } : undefined
}

/** `colspan` and `rowspan` of a table cell, left out when 1. */
export const cellAttrs = (a: Record<string, any>): Record<string, number | undefined> =>
  ({ colspan: a.colspan > 1 ? num(a.colspan, 1000) : undefined, rowspan: a.rowspan > 1 ? num(a.rowspan, 1000) : undefined })

export const tags: Record<string, string> = {
  paragraph: 'p', bulletList: 'ul', listItem: 'li', blockquote: 'blockquote', horizontalRule: 'hr', hardBreak: 'br',
  bold: 'strong', italic: 'em', code: 'code', underline: 'u', strike: 's', superscript: 'sup', subscript: 'sub',
  tableRow: 'tr',
}
const VOID = new Set(['hr', 'br'])

const pat = (re: RegExp, v: any): string | undefined => (typeof v === 'string' && re.test(v) ? v : undefined)
const ID = /^[A-Za-z][\w-]{0,63}$/, NAME = /^[a-z][a-zA-Z0-9]*$/, ALIGN = ['left', 'center', 'right', 'justify']

/** A ProseMirror document as plain element nodes: links resolved and checked; custom nodes and marks as `<tag class="rt-name" data-attr>` (tag and attrs from `schema`). */
export function richTextNodes(doc: PMNode | null | undefined, o: RichTextOptions = {}): RichNode[] {
  const wrap = (type: string, a: Record<string, any>, kids: RichNode[], mark?: boolean, inl?: boolean): RichNode[] => {
    const ov = o.render?.[type]
    if (ov) return ov(a, kids)
    const cls = pat(/^[\w -]+$/, o.classes?.[type])
    const el = (tag: string, x: Record<string, any> = {}, k: RichNode[] | null = kids): RichNode[] => {
      const { id, style, ...r } = x
      return [{ tag, attrs: { id, class: cls, style, ...r }, kids: k ?? undefined }]
    }
    switch (type) {
      case 'paragraph': case 'heading':
        return el(type === 'paragraph' ? 'p' : 'h' + Math.min(6, Math.max(1, Number(a.level) || 1)), { id: pat(ID, a.id), style: ALIGN.includes(a.align) ? 'text-align:' + a.align : undefined })
      case 'orderedList': return el('ol', { start: typeof a.start === 'number' && a.start !== 1 && Math.abs(a.start) <= 1e6 ? Math.trunc(a.start) : undefined })
      case 'codeBlock': return el('pre', {}, [{ tag: 'code', attrs: { class: pat(/^[\w+-]+$/, a.language) && 'language-' + a.language }, kids }])
      case 'link': {
        const href = linkHref(a, o), b = a.target === '_blank'
        return href ? el('a', { href, target: b ? '_blank' : undefined, rel: b ? 'noopener noreferrer' : undefined }) : kids
      }
      case 'table': return el('table', {}, [{ tag: 'tbody', attrs: {}, kids }])
      case 'tableCell': case 'tableHeader': return el(type === 'tableCell' ? 'td' : 'th', cellAttrs(a))
      case 'image': {
        const attrs = imageAttrs(a)
        return attrs ? el('img', attrs, null) : []
      }
      default: {
        const t = tags[type]
        if (t) return el(t, {}, VOID.has(t) ? null : kids)
        if (!NAME.test(type)) return kids
        const d = (mark ? o.schema?.marks : o.schema?.nodes)?.[type], ds = d?.attrs
        const attrs: Record<string, string> = { class: 'rt-' + type + (cls ? ' ' + cls : '') }
        for (const k of Object.keys(ds ?? a)) {
          let v = a[k] ?? ds?.[k]?.default
          if (ds?.[k]?.type === 'url') v = imageSrc(`${v}`)
          if (NAME.test(k) && v != null && v !== false) attrs['data-' + k.replace(/[A-Z]/g, (c) => '-' + c.toLowerCase())] = v === true ? '' : String(v)
        }
        return [{ tag: d?.tag ?? (mark || (d ? d.inline : inl) ? 'span' : 'div'), attrs, kids }]
      }
    }
  }
  const node = (n: PMNode, inl?: boolean, tight?: boolean): RichNode[] => {
    if (n.type === 'text') {
      if (!n.text) return []
      return (n.marks ?? []).reduceRight<RichNode[]>((c, m) => wrap(m.type, m.attrs ?? {}, c, true), [n.text])
    }
    const t = n.type, c = n.content ?? [], ci = t === 'paragraph' || t === 'heading' || c.some((x) => x.type === 'text'), lt = (t === 'bulletList' || t === 'orderedList') && !!n.attrs?.tight
    return wrap(t, n.attrs ?? {}, c.flatMap((x) => (tight && x.type === 'paragraph' ? (x.content ?? []).flatMap((y) => node(y, true)) : node(x, ci, lt))), false, inl)
  }
  return doc ? (doc.content ?? []).flatMap((x) => node(x)) : []
}
