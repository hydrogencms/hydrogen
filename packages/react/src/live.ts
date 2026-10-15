'use client'
import { createElement, Fragment, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { createBridge } from '@hydrogencms/bridge'
import type { Bridge } from '@hydrogencms/bridge'

export interface LivePreviewOptions {
  /** origin of the admin that embeds the page (e.g. https://admin.example.com) */
  adminOrigin: string
  /** false keeps the hook inert (default true); the bridge is also inert outside a preview frame */
  enabled?: boolean
  /** override the bridge's preview detection */
  preview?: boolean
}

/** The initial document with the admin's working document laid over it (top-level keys of `doc` win), so
 * delivery-only keys such as `links` survive. Non-object documents replace the initial one. */
export function mergeDoc<T>(initial: T, doc: unknown): T {
  const o = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
  return o(initial) && o(doc) ? ({ ...initial, ...doc } as T) : (doc as T)
}

/** The latest document: `initial` until the admin sends `init`, then every patch applied; posts `rendered` after commit. */
export function useLivePreview<T>(initial: T, opts: LivePreviewOptions): T {
  const { adminOrigin, enabled = true, preview } = opts
  const [live, setLive] = useState<{ doc: unknown; rev: number } | null>(null)
  const bridge = useRef<Bridge | null>(null)
  useEffect(() => {
    if (!enabled) return
    const b = createBridge({
      adminOrigin, mode: 'live', preview,
      onInit: (m) => setLive({ doc: m.doc, rev: m.rev }),
      onPatch: (doc, p) => setLive({ doc, rev: p.rev }),
    })
    bridge.current = b
    b.connect()
    return () => {
      b.disconnect()
      bridge.current = null
    }
  }, [adminOrigin, enabled, preview])
  useEffect(() => {
    if (live) bridge.current?.rendered(live.rev)
  }, [live])
  return useMemo(() => (live ? mergeDoc(initial, live.doc) : initial), [initial, live])
}

/** Client wrapper for server-rendered pages: `<LivePreview initial={item} adminOrigin=…>{(item) => <Page item={item} />}</LivePreview>`. */
export function LivePreview<T>(p: LivePreviewOptions & { initial: T; children: (doc: T) => ReactNode }) {
  return createElement(Fragment, null, p.children(useLivePreview(p.initial, p)))
}
