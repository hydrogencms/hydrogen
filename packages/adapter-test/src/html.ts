/** Reading the markup a frontend answers with. Small, dependency-free, and forgiving: it looks at tags, not at a DOM. */

export interface Tag {
  name: string
  attrs: Record<string, string>
}

const entities: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ' }

export function decode(s: string): string {
  return s.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (m, e: string) => {
    if (e[0] === '#') {
      const n = e[1] === 'x' || e[1] === 'X' ? parseInt(e.slice(2), 16) : parseInt(e.slice(1), 10)
      return Number.isFinite(n) && n > 0 && n < 0x110000 ? String.fromCodePoint(n) : m
    }
    return entities[e.toLowerCase()] ?? m
  })
}

/** The markup without scripts, styles, comments and templates: framework payloads quote markup in strings. */
export function visible(html: string): string {
  return html
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/<(script|style|template|noscript)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, '')
}

const tagRe = /<([a-zA-Z][\w:-]*)((?:\s+[^\s"'<>\/=]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>`]+))?)*)\s*\/?>/g
const attrRe = /([^\s"'<>\/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g

/** Every start tag, optionally only those called `name`. Attribute names are lower-cased, values decoded. */
export function tags(html: string, name?: string): Tag[] {
  const out: Tag[] = []
  for (const m of visible(html).matchAll(tagRe)) {
    const n = m[1]!.toLowerCase()
    if (name && n !== name) continue
    const attrs: Record<string, string> = {}
    for (const a of (m[2] ?? '').matchAll(attrRe)) attrs[a[1]!.toLowerCase()] = decode(a[2] ?? a[3] ?? a[4] ?? '')
    out.push({ name: n, attrs })
  }
  return out
}

/** `data-h-block` values in document order: the block types the page rendered. */
export const blockMarkers = (html: string): string[] =>
  tags(html).flatMap((t) => (t.attrs['data-h-block'] !== undefined ? [t.attrs['data-h-block']] : []))

/** `data-h-path` values in document order (only present in preview). */
export const pathMarkers = (html: string): string[] =>
  tags(html).flatMap((t) => (t.attrs['data-h-path'] !== undefined ? [t.attrs['data-h-path']] : []))

const text = (s: string) => decode(s.replace(/<[^>]*>/g, '')).replace(/\s+/g, ' ').trim()

/** The text of every `<h1>`. */
export function h1s(html: string): string[] {
  return [...visible(html).matchAll(/<h1\b[^>]*>([\s\S]*?)<\/h1\s*>/gi)].map((m) => text(m[1]!))
}

export function title(html: string): string | undefined {
  const m = /<title\b[^>]*>([\s\S]*?)<\/title\s*>/i.exec(visible(html))
  return m ? text(m[1]!) : undefined
}

export function meta(html: string, name: string): string | undefined {
  return tags(html, 'meta').find((t) => t.attrs.name?.toLowerCase() === name)?.attrs.content
}

const rel = (t: Tag, r: string) => t.attrs.rel?.toLowerCase().split(/\s+/).includes(r)

export const canonical = (html: string): string | undefined => tags(html, 'link').find((t) => rel(t, 'canonical'))?.attrs.href

/** `<link rel="alternate" hreflang href>` as `hreflang -> href`. */
export function alternates(html: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const t of tags(html, 'link')) if (rel(t, 'alternate') && t.attrs.hreflang && t.attrs.href) out[t.attrs.hreflang] = t.attrs.href
  return out
}

/** Whether the page asks search engines not to index it. */
export const noindex = (html: string): boolean => /noindex/i.test(meta(html, 'robots') ?? '')
