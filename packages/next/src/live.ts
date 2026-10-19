'use client'
import { createElement, useMemo } from 'react'
import type { ComponentType, ReactElement } from 'react'
import { keepShared } from '@hydrogencms/bridge/merge'
import { useLivePreview } from '@hydrogencms/react/live'

export { keepShared }

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
