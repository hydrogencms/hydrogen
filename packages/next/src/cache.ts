import { cacheLife, cacheTag } from 'next/cache'
import { HydrogenError } from '@hydrogencms/client'
import type { Response } from '@hydrogencms/client'
import type { Resolved } from '@hydrogencms/router'
import { clientOf, routerOf } from './delivery.js'
import { ALL_TAG, cacheTags } from './tags.js'

/**
 * The cached CMS calls. Each is a `"use cache"` function: Next keys the entry by the (serializable) arguments,
 * and the tags are read from the response's Surrogate-Key header after the fetch, which `cacheTag` allows and
 * `fetch(..., { next: { tags } })` does not. Arguments are plain data without secrets (tokens: delivery.ts).
 */
export interface Cfg {
  baseUrl: string
  channel?: string
  types: string[]
  fallback: boolean
  /** seconds an entry is fresh without a purge (the webhook is the normal way entries go stale) */
  ttl: number
}

export type Method = 'item' | 'itemByKey' | 'singleton' | 'list' | 'route' | 'tree' | 'channel'
export type Called = { ok: Response<unknown> } | { err: { status: number; code: string; message: string } }

/** An uncached resolve (draft mode). */
export const resolveFresh = (c: Cfg, url: string, preview?: string, headers?: Record<string, string>): Promise<Resolved> =>
  routerOf({ baseUrl: c.baseUrl, channel: c.channel!, types: c.types, fallback: c.fallback }).resolve(url, { preview, headers })

// `stale` of 30 s and `expire` of 5 min are the least Next keeps in prerenders; below them an entry is a dynamic hole.
const life = (ttl: number, short: boolean) =>
  cacheLife(short ? { stale: 30, revalidate: 30, expire: 300 } : { stale: 30, revalidate: ttl, expire: Math.max(ttl * 24, 300) })

export async function resolveCached(c: Cfg, url: string, headers?: Record<string, string>): Promise<Resolved> {
  'use cache'
  const r = await resolveFresh(c, url, undefined, headers)
  const { tags, truncated } = cacheTags(r.kind === 'reserved' ? [] : r.tags)
  cacheTag(...tags)
  life(c.ttl, truncated || (r.kind === 'notFound' && r.tags.length === 0))
  return r
}

export async function callCached(c: Cfg, method: Method, args: unknown[]): Promise<Called> {
  'use cache'
  try {
    const r = await (clientOf(c.baseUrl, c.channel) as unknown as Record<Method, (...a: unknown[]) => Promise<Response<unknown>>>)[method]!(...args)
    const { tags, truncated } = cacheTags(r.tags)
    cacheTag(...tags)
    life(c.ttl, truncated)
    return { ok: r }
  } catch (e) {
    if (!(e instanceof HydrogenError) || e.status >= 500) throw e
    cacheTag(ALL_TAG)
    cacheLife({ stale: 30, revalidate: 10, expire: 300 })
    return { err: { status: e.status, code: e.code, message: e.message } }
  }
}
