import { createClient } from '@hydrogencms/client'
import { createTagCache } from '@hydrogencms/client/server'
import type { TagCache } from '@hydrogencms/client/server'
import type { ChannelInfo, Client } from '@hydrogencms/client'
import { createRouter } from '@hydrogencms/router'
import type { Router } from '@hydrogencms/router'

/** What the module puts in the runtime config (`hydrogen`); empty strings stand for "not set" (env overrides work on them). */
export interface HydrogenConfig {
  baseUrl: string
  channel: string
  channels: string[]
  token: string
  /** content types that have a template */
  templates: string[]
  fallback: boolean
  origin: string
  trustForwarded: boolean
  previewPath: string
  /** seconds a cached CMS response stays fresh when no webhook purges it */
  ttl: number
  timeoutMs: number
  revalidateSecret: string
  adminOrigin: string
}

let cache: TagCache | undefined

/** The deployment's response cache: GETs tagged with the responses' `Surrogate-Key`, kept until the TTL ends or a webhook purges a tag. */
const cacheOf = (c: HydrogenConfig): TagCache => (cache ??= createTagCache({ ttl: c.ttl, timeoutMs: c.timeoutMs }))

/** Drops the cached responses that carry one of the tags; returns how many. */
export const purge = (tags: string[]): number => cache?.purge(tags) ?? 0

/** A client of a channel: cached and tagged, or with the visitor's preview token, uncached. */
export const clientOf = (c: HydrogenConfig, channel?: string, preview?: string): Client<Record<string, unknown>> =>
  createClient({ baseUrl: c.baseUrl, channel, token: c.token || undefined, preview, fetch: cacheOf(c).fetch })

const TTL = 300_000
const routers = new Map<string, { at: number; router: Router }>()

/** The router of a channel, reused for five minutes so its channel info (reserved paths) is not fetched per request. */
export function routerOf(c: HydrogenConfig, channel: string, preview?: string): Router {
  const make = () => createRouter({
    client: clientOf(c, channel, preview),
    templates: Object.fromEntries(c.templates.map((t) => [t, t])),
    fallback: c.fallback ? '*' : undefined,
  })
  let e = routers.get(channel)
  if (preview) return make()
  if (!e || Date.now() - e.at > TTL) routers.set(channel, (e = { at: Date.now(), router: make() }))
  return e.router
}

let infos: { at: number; p: Promise<ChannelInfo[]> } | undefined

/** Infos of the deployment's channels (their match options tell them apart), kept for five minutes. */
export function channelInfos(c: HydrogenConfig): Promise<ChannelInfo[]> {
  if (infos && Date.now() - infos.at < TTL) return infos.p
  const p = Promise.all(c.channels.map(async (n) => (await clientOf(c, n).channel(n)).data))
  p.catch(() => (infos = undefined))
  infos = { at: Date.now(), p }
  return p
}
