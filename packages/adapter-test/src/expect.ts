/** Pure helpers of the checks: what a delivered document says the markup must contain. */

export interface BlockLike {
  type: string
  slots?: Record<string, BlockLike[]>
  blocks?: BlockLike[]
}

/** Block types of a tree in document order: a block, then its slots (sorted by name) and, for shared blocks, the inlined blocks. */
export function expectedBlocks(blocks: BlockLike[] | null | undefined): string[] {
  const out: string[] = []
  const walk = (list: BlockLike[]) => {
    for (const b of list) {
      out.push(b.type)
      for (const k of Object.keys(b.slots ?? {}).sort()) walk(b.slots![k]!)
      if (b.blocks) walk(b.blocks)
    }
  }
  walk(blocks ?? [])
  return out
}

/** Compares two lists as multisets; the message names what is missing and what is extra. */
export function diffMultiset(want: string[], got: string[]): string | undefined {
  const count = (l: string[]) => l.reduce((m, k) => m.set(k, (m.get(k) ?? 0) + 1), new Map<string, number>())
  const w = count(want), g = count(got)
  const parts: string[] = []
  for (const [k, n] of w) if ((g.get(k) ?? 0) < n) parts.push(`${n - (g.get(k) ?? 0)} x ${k} missing`)
  for (const [k, n] of g) if ((w.get(k) ?? 0) < n) parts.push(`${n - (w.get(k) ?? 0)} x ${k} unexpected`)
  return parts.length ? parts.join(', ') : undefined
}

/** The path and query of a URL (`/a?b=1`), whatever its origin. */
export function pathOf(u: string): string {
  const x = new URL(u, 'http://x.invalid')
  return x.pathname + x.search
}

/** `u` on the frontend's origin: the CMS builds URLs from the channel's configured frontend, the suite asks where it really runs. */
export function onFrontend(u: string, frontend: string): string {
  return frontend.replace(/\/+$/, '') + pathOf(u)
}

/** Strips the preview token (and keeps the rest) from a preview URL. */
export function withoutPreview(u: string): string {
  const x = new URL(u, 'http://x.invalid')
  x.searchParams.delete('preview')
  return x.pathname + x.search
}

/** Structural equality of JSON values. */
export const same = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b)

/** Sorted `k=v` lines of a record: for comparing hreflang sets. */
export const lines = (r: Record<string, string>): string[] => Object.entries(r).map(([k, v]) => `${k} ${v}`).sort()

/** The alternates of a `/route` answer as `hreflang -> path`, comparing paths (origins differ between CMS config and test run). */
export function alternatesByPath(alts: { locale: string; url: string }[] | undefined): Record<string, string> {
  return Object.fromEntries((alts ?? []).map((a) => [a.locale, pathOf(a.url)]))
}

/** A site path's parts below a channel prefix: `/en/about` with prefix `/en` is `/about`; undefined when it is outside. */
export function underPrefix(path: string, prefix: string): string | undefined {
  if (!prefix || prefix === '/') return path
  if (path === prefix) return '/'
  return path.startsWith(prefix + '/') ? path.slice(prefix.length) : undefined
}

/** Item ids that rich text links (`item:<id>` hrefs) inside a document's fields point at, in document order. */
export function linkIds(v: unknown): string[] {
  const out: string[] = []
  const walk = (x: unknown) => {
    if (Array.isArray(x)) return x.forEach(walk)
    if (!x || typeof x !== 'object') return
    const o = x as Record<string, unknown>
    const href = (o.attrs as { href?: unknown } | undefined)?.href
    if (o.type === 'link' && typeof href === 'string' && href.startsWith('item:')) out.push(href.slice(5))
    for (const k in o) walk(o[k])
  }
  walk(v)
  return out
}
