import { createClient } from '@hydrogencms/client'
import type { ChannelInfo, Client } from '@hydrogencms/client'
import { createRouter } from '@hydrogencms/router'
import type { Router } from '@hydrogencms/router'
import { createTagCache } from '@hydrogencms/client/server'
import type { TagCache } from '@hydrogencms/client/server'

export interface DeliveryOptions {
  baseUrl: string
  /** token of a token-protected channel (kept out of anything that is cached or rendered) */
  token?: string
  /** content types that have a template */
  types: string[]
  fallback: boolean
  ttl?: number
  timeoutMs?: number
}

const TTL = 300_000

/**
 * CMS access of one deployment: clients on the tagged cache (drafts bypass it with the preview token), one router per
 * channel and one channel-info lookup, both remembered for five minutes.
 */
export function createDelivery(o: DeliveryOptions) {
  const cache: TagCache = createTagCache({ ttl: o.ttl, timeoutMs: o.timeoutMs })
  const routers = new Map<string, { at: number; router: Router }>()
  let infos: { at: number; names: string; p: Promise<ChannelInfo[]> } | undefined

  const client = (channel?: string, preview?: string): Client<Record<string, unknown>> =>
    createClient({ baseUrl: o.baseUrl, channel, token: o.token, preview, fetch: cache.fetch })

  const router = (channel: string): Router => {
    let e = routers.get(channel)
    if (!e || performance.now() - e.at > TTL) {
      e = {
        at: performance.now(),
        router: createRouter({
          client: client(channel),
          templates: Object.fromEntries(o.types.map((t) => [t, t])),
          fallback: o.fallback ? '*' : undefined,
        }),
      }
      routers.set(channel, e)
    }
    return e.router
  }

  /** Infos of the deployment's channels (the match options tell them apart). */
  const channelInfos = (names: string[]): Promise<ChannelInfo[]> => {
    const key = names.join(',')
    if (infos && infos.names === key && performance.now() - infos.at < TTL) return infos.p
    const p = Promise.all(names.map(async (n) => (await client(n).channel(n)).data))
    const e = { at: performance.now(), names: key, p }
    p.catch(() => infos === e && (infos = undefined))
    return (infos = e).p
  }

  return { cache, client, router, channelInfos }
}
