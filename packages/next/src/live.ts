'use client'
import { createElement, useMemo } from 'react'
import type { ComponentType, ReactElement } from 'react'
import { useLivePreview } from '@hydrogencms/react/live'

type Obj = Record<string, any>

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

export interface LiveProps {
  /** a client component (`'use client'` module): it re-renders with the admin's working document */
  Template: ComponentType<any>
  item: { fields?: unknown }
  adminOrigin: string
  [k: string]: unknown
}

/** Draft mode: renders `Template` from the live document (`item` until the admin connects). */
export function Live({ Template, item, adminOrigin, ...rest }: LiveProps): ReactElement {
  const live = useLivePreview(item, { adminOrigin })
  const shown = useMemo(() => (live === item ? item : keepShared(item, live)), [live, item])
  return createElement(Template, { ...rest, item: shown, preview: true })
}
