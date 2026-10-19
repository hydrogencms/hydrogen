/** Helpers for frontends that render the admin's working document live (see `useLivePreview` in the framework adapters). */
type Obj = Record<string, any>

/** The initial document with the admin's working document laid over it (top-level keys of `doc` win), so delivery-only keys such as `links` survive. */
export function mergeDoc<T>(initial: T, doc: unknown): T {
  const o = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)
  return o(initial) && o(doc) ? ({ ...initial, ...doc } as T) : (doc as T)
}

/** Shared blocks arrive with their blocks inlined by delivery; the admin's working document has only `props.item`. */
function collect(v: unknown, out: Map<string, unknown>): Map<string, unknown> {
  if (v && typeof v === 'object') {
    const o = v as Obj
    if (o.type === 'shared' && o.blocks) out.set(o.props?.item, o.blocks)
    for (const k in o) collect(o[k], out)
  }
  return out
}
function inline(v: unknown, m: Map<string, unknown>): unknown {
  if (Array.isArray(v)) return v.map((x) => inline(x, m))
  if (!v || typeof v !== 'object') return v
  const o: Obj = {}
  for (const k in v as Obj) o[k] = inline((v as Obj)[k], m)
  if (o.type === 'shared' && !o.blocks && m.has(o.props?.item)) o.blocks = m.get(o.props.item)
  return o
}

/** `live` with the inlined blocks of `initial`'s shared blocks put back (matched by the shared item). */
export const keepShared = (initial: unknown, live: unknown): unknown => inline(live, collect(initial, new Map()))

/** The live document over `initial`, with the inlined blocks of `initial`'s shared blocks put back. */
export const liveDoc = <T>(initial: T, doc: unknown): T => keepShared(initial, mergeDoc(initial, doc)) as T
