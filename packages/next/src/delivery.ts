import { createClient } from '@hydrogencms/client'
import type { ChannelInfo, Client } from '@hydrogencms/client'
import { createRouter } from '@hydrogencms/router'
import type { Router } from '@hydrogencms/router'

/**
 * CMS access shared by the page (server bundle) and the proxy (its own bundle): one router per channel and one
 * channel-info lookup per deployment, kept in memory for five minutes. Edge-safe: nothing here imports Next.
 */

/** Channel tokens live here, not in cache keys: `"use cache"` arguments may be stored or logged by a cache handler. */
const tokens = new Map<string, string>()
export const setToken = (baseUrl: string, token?: string) => void (token ? tokens.set(baseUrl, token) : tokens.delete(baseUrl))

const withTimeout = (ms: number): typeof fetch => (u, i) => fetch(u, { ...i, signal: i?.signal ?? AbortSignal.timeout(ms) })

export const clientOf = (baseUrl: string, channel?: string, preview?: string, timeoutMs?: number): Client<Record<string, unknown>> =>
  createClient({ baseUrl, channel, token: tokens.get(baseUrl), preview, fetch: timeoutMs ? withTimeout(timeoutMs) : undefined })

export interface RouterKey {
  baseUrl: string
  channel: string
  /** content types that have a template */
  types: string[]
  fallback: boolean
}

const TTL = 300_000
const routers = new Map<string, { at: number; router: Router }>()

/** The router of a channel; reused so the channel info (reserved paths) is not fetched per request. */
export function routerOf(k: RouterKey, timeoutMs?: number): Router {
  const key = JSON.stringify([k.baseUrl, k.channel, k.types, k.fallback, timeoutMs])
  let e = routers.get(key)
  if (!e || performance.now() - e.at > TTL) {
    e = {
      at: performance.now(),
      router: createRouter({
        client: clientOf(k.baseUrl, k.channel, undefined, timeoutMs),
        templates: Object.fromEntries(k.types.map((t) => [t, t])),
        fallback: k.fallback ? '*' : undefined,
      }),
    }
    if (routers.size > 50) routers.clear()
    routers.set(key, e)
  }
  return e.router
}

const infos = new Map<string, { at: number; p: Promise<ChannelInfo[]> }>()

/** Infos of the deployment's channels (the match options tell them apart). */
export function channelInfos(baseUrl: string, names: string[], timeoutMs?: number): Promise<ChannelInfo[]> {
  const key = baseUrl + '|' + names.join(',')
  const hit = infos.get(key)
  if (hit && performance.now() - hit.at < TTL) return hit.p
  const p = Promise.all(names.map(async (n) => (await clientOf(baseUrl, n, undefined, timeoutMs).channel(n)).data))
  p.catch(() => infos.delete(key))
  infos.set(key, { at: performance.now(), p })
  return p
}
