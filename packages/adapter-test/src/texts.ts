/** What the suite knows about the starter fixture's schema: the string and text fields the canvas can edit. */
const TOP: Record<string, Record<string, 'string' | 'text'>> = {
  page: { title: 'string' },
  landing: { title: 'string' },
  post: { title: 'string', excerpt: 'text' },
}

const PROPS: Record<string, Record<string, 'string' | 'text'>> = {
  hero: { heading: 'string', subheading: 'text', ctaLabel: 'string' },
  cards: { heading: 'string' },
  card: { title: 'string', text: 'text' },
  callout: { text: 'text' },
  image: { alt: 'string', caption: 'string' },
}

export interface TextRef {
  path: string
  text: string
  scope: string
  /** the field's type: `text` edits multiline */
  kind: 'string' | 'text'
}

interface BlockDoc {
  type: string
  props?: Record<string, unknown>
  slots?: Record<string, BlockDoc[]>
}

/** Paths of the blocks a page renders from its own `body` (blocks of a `shared` block are inlined read-only: none). */
export function blockPaths(body: BlockDoc[] | null | undefined, field = 'body'): { path: string; type: string }[] {
  const out: { path: string; type: string }[] = []
  const walk = (list: BlockDoc[], base: string) =>
    list.forEach((b, i) => {
      const path = `${base}/${i}`
      if (b.type === 'shared') return
      out.push({ path, type: b.type })
      for (const s of Object.keys(b.slots ?? {}).sort()) walk(b.slots![s]!, `${path}/slots/${s}`)
    })
  walk(body ?? [], `fields/${field}`)
  return out
}

/**
 * The texts the admin sends with `overlay`: top-level string and text fields (scope `""`) and string and text block
 * props (scope: the block's path), non-empty and at most 500 characters, sorted by path.
 */
export function canvasTexts(type: string, fields: Record<string, any>): TextRef[] {
  const out: TextRef[] = []
  const add = (path: string, v: unknown, scope: string, kind: 'string' | 'text') => {
    if (typeof v === 'string' && v.trim() !== '' && v.length <= 500) out.push({ path, text: v, scope, kind })
  }
  for (const [name, kind] of Object.entries(TOP[type] ?? {})) add(`fields/${name}`, fields[name], '', kind)
  for (const b of blockPaths(fields.body)) {
    let blk: BlockDoc | undefined
    const steps = b.path.split('/').slice(2)
    let list: BlockDoc[] | undefined = fields.body
    for (let i = 0; i < steps.length; ) {
      blk = list?.[Number(steps[i])]
      if (steps[i + 1] === 'slots') {
        list = blk?.slots?.[steps[i + 2]!]
        i += 3
      } else i++
    }
    for (const [p, kind] of Object.entries(PROPS[b.type] ?? {})) add(`${b.path}/props/${p}`, blk?.props?.[p], b.path, kind)
  }
  return out.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
}
