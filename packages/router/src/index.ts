import { HydrogenError } from '@hydrogencms/client'
import type { ChannelInfo, Client, Item } from '@hydrogencms/client'

export interface RouterOptions<T> {
  client: Client<T>
  /** content type (page types and mounted collections) to template key */
  templates: Record<string, string>
  /** template key for unknown types (default: none, so notFound) */
  fallback?: string
  /** extra frontend-owned site paths (exact, or `/x/*`); the channel's own reserved list is fetched too */
  reserved?: string[]
}

export interface Metadata {
  title: string
  canonical?: string
  alternates: { hreflang: string; href: string }[]
  locale: string
}

export interface ResolveOptions {
  preview?: string
  /** request headers that header matchers look at (forwarded to the server) */
  headers?: Record<string, string>
  locale?: string
}

export type Resolved =
  | { kind: 'page'; template: string; item: Item<unknown>; locale: string; metadata: Metadata; tags: string[] }
  | { kind: 'redirect'; location: string; status: 301 | 302 | 307 | 308; tags: string[] }
  | { kind: 'notFound'; tags: string[] }
  | { kind: 'reserved' }

export interface Router {
  resolve(url: string, o?: ResolveOptions): Promise<Resolved>
  /** declared content types, sorted (what `hydrogen check-frontend -templates` compares with the schema) */
  templates(): string[]
}

type HeaderSource = Headers | Record<string, string | undefined>

export interface SiteMatch {
  channel: ChannelInfo
  /** the site path after the channel's prefix, decoded; "/" for the root */
  path: string
  /** the path can never be a site path (invalid text or an encoded slash); the server answers not found */
  bad: boolean
}

const header = (h: HeaderSource | undefined, name: string): string | undefined => {
  if (!h) return undefined
  if (typeof (h as Headers).get === 'function') return (h as Headers).get(name) ?? undefined
  const l = name.toLowerCase()
  for (const k in h) if (k.toLowerCase() === l) return (h as Record<string, string | undefined>)[k]
  return undefined
}

function split(raw: string): { host: string; path: string; bad: boolean } {
  const abs = /^[a-z][a-z\d+.-]*:\/\//i.test(raw)
  const u = new URL(raw, abs ? undefined : 'http://-.invalid')
  const escaped = u.pathname
  let path = escaped
  let bad = /%2f/i.test(escaped)
  try {
    path = decodeURIComponent(escaped)
  } catch {
    bad = true
  }
  return {
    host: abs ? u.hostname.replace(/^\[|\]$/g, '').replace(/\.$/, '') : '',
    path: path || '/',
    bad,
  }
}

const cutPrefix = (path: string, prefix: string): string | undefined => {
  if (path === prefix) return '/'
  return path.startsWith(prefix) && path[prefix.length] === '/' ? path.slice(prefix.length) : undefined
}

const trimSlash = (p: string) => p.replace(/\/+$/, '') || '/'

const sitePath = (m: NonNullable<ChannelInfo['options']['match']>, host: string, path: string, h?: HeaderSource) => {
  if (m.host && m.host !== host) return undefined
  if (m.header && header(h, m.header.name) !== m.header.value) return undefined
  return m.prefix ? cutPrefix(path, m.prefix) : path
}

const parts = (c: ChannelInfo) => {
  const m = c.options.match!
  return [m.host ? 1 : 0, m.prefix ? 1 : 0, m.header ? 1 : 0, m.prefix?.length ?? 0] as const
}

/** Orders channels like the server: (host, prefix, header) present, then the longer prefix, then the name. */
const moreSpecific = (a: ChannelInfo, b: ChannelInfo) => {
  const pa = parts(a), pb = parts(b)
  for (let i = 0; i < 4; i++) if (pa[i]! !== pb[i]!) return pb[i]! - pa[i]!
  return a.name < b.name ? -1 : a.name > b.name ? 1 : 0
}

/** Finds the most specific web channel (one with a `match` option) the URL fits, like the server does. */
export function matchSite(url: string, channels: ChannelInfo[], headers?: HeaderSource): SiteMatch | undefined {
  const { host, path, bad } = split(url)
  const web = channels.filter((c) => c.options?.match).sort(moreSpecific)
  for (const c of web) {
    const rest = sitePath(c.options.match!, host, path, headers)
    if (rest !== undefined) return { channel: c, path: rest, bad }
  }
  return undefined
}

export function matchChannel(url: string, channels: ChannelInfo[], headers?: Headers): ChannelInfo | undefined {
  return matchSite(url, channels, headers)?.channel
}

const reservedMatch = (patterns: string[], path: string) => {
  const p = trimSlash(path)
  return patterns.some((r) => {
    const wild = r.endsWith('/*')
    const base = wild ? r.slice(0, -2) : r
    return p === base || (wild && p.startsWith(base + '/'))
  })
}

const REDIRECT = new Set([301, 302, 307, 308])

export function createRouter<T>(o: RouterOptions<T>): Router {
  const { client } = o
  let info: Promise<ChannelInfo | undefined> | undefined

  const channelInfo = () =>
    (info ??= client.channel().then(
      (r) => r.data,
      (e) => {
        if (!(e instanceof HydrogenError) || e.status >= 500) info = undefined
        return undefined
      },
    ))

  async function isReserved(url: string, h?: Record<string, string>): Promise<boolean> {
    let site: string | undefined
    let patterns = o.reserved ?? []
    const ch = await channelInfo()
    if (ch?.options.reserved) patterns = [...patterns, ...ch.options.reserved]
    if (!patterns.length) return false
    try {
      const s = split(url)
      if (s.bad) return false
      const m = ch?.options.match
      site = m ? sitePath(m, s.host, s.path, h) : s.path
    } catch {
      return false
    }
    return site !== undefined && reservedMatch(patterns, site)
  }

  return {
    templates: () => Object.keys(o.templates).sort(),

    async resolve(url, ro = {}) {
      if (await isReserved(url, ro.headers)) return { kind: 'reserved' }
      let res
      try {
        res = await client.route(url, { preview: ro.preview, headers: ro.headers, locale: ro.locale })
      } catch (e) {
        if (e instanceof HydrogenError && e.status === 404) return { kind: 'notFound', tags: [] }
        throw e
      }
      const { located: l, item } = res.data
      const tags = res.tags
      switch (l.kind) {
        case 'reserved':
          return { kind: 'reserved' }
        case 'redirect': {
          const s = l.resolution?.status ?? 0
          const location = l.resolution?.location
          if (!location) return { kind: 'notFound', tags }
          return { kind: 'redirect', location, status: (REDIRECT.has(s) ? s : 307) as 301 | 302 | 307 | 308, tags }
        }
        case 'page':
        case 'item': {
          const template = item && (o.templates[item.type] ?? o.fallback)
          if (!item || template === undefined) return { kind: 'notFound', tags }
          const locale = l.locale ?? item.locale
          return {
            kind: 'page',
            template,
            item,
            locale,
            tags,
            metadata: {
              title: item.name,
              canonical: l.canonical,
              alternates: (l.alternates ?? []).map((a) => ({ hreflang: a.locale, href: a.url })),
              locale,
            },
          }
        }
        default:
          return { kind: 'notFound', tags }
      }
    },
  }
}
