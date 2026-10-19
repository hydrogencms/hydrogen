import { createClient, HydrogenError } from '@hydrogencms/client'
import type { ChannelInfo, Client, Item, TreeNode } from '@hydrogencms/client'
import { createRouter, matchSite, reservedMatch } from '@hydrogencms/router'
import type { Metadata, Resolved, Router } from '@hydrogencms/router'
import {
  MIN_SECRET_LENGTH, PREVIEW_COOKIE, SIGNATURE_HEADER, createTagCache, matchPreviewPath, matcherHeaders, originOf, parsePreviewRequest,
  previewCookieOptions, previewHash, readCapped, tokenFromCookie, validToken, verifySignature, webhookTags,
} from '@hydrogencms/client/server'

export type { ChannelInfo, Client, Item, TreeNode, Metadata }
export { createRegistry, blockAttrs, blockSlots, pickBlock } from './blocks.js'
export type { BlockAttrs, BlockComponentProps, BlockData, BlocksProps, Registry } from './blocks.js'
export { richTextHtml, escapeHtml } from './richtext.js'
export type { PMNode, RichTextOptions, RenderOverrides } from './richtext.js'
export { PREVIEW_COOKIE, verifySignature, webhookTags } from '@hydrogencms/client/server'

export interface HydrogenOptions {
  /** the CMS, e.g. "https://cms.acme.com" */
  baseUrl: string
  /** the one channel this deployment serves ... */
  channel?: string
  /** ... or several, told apart by the host and prefix matchers in their options */
  channels?: string[]
  /** token of a token-protected channel */
  token?: string
  /** the content types that have a template (page types and mounted collections) */
  templates: string[] | Record<string, unknown>
  /** a fallback template exists: types without one render with template key `*` */
  fallback?: boolean
  /** webhook secret for the revalidation route; at least 16 characters */
  revalidateSecret?: string
  /** public origin to locate URLs with instead of the request's (dev on localhost, or another host than the channel's) */
  origin?: string
  /** Astro's `base` (`import.meta.env.BASE_URL`); the integration sets it, but code that runs before the middleware loads (`getStaticPaths`) needs it here */
  base?: string
  /** use X-Forwarded-Host/-Proto for the public origin (default false; only behind a front proxy that overwrites them) */
  trustForwarded?: boolean
  /** path of the preview URLs for items without a route (default "/preview": `/preview/<collection>/<id>`) */
  previewPath?: string
  /** where the preview route lives (default "/api/hydrogen/preview") */
  previewRoutePath?: string
  /** where unknown URLs are rewritten to, answered with status 404 (default the site's `/404` page) */
  notFoundPath?: string
  /** origin of the admin that embeds draft pages (default: the origin of `baseUrl`) */
  adminOrigin?: string
  /** seconds a cached CMS response stays fresh when no webhook purges it (default 3600) */
  ttl?: number
  /** milliseconds a CMS call may take (default 5000) */
  timeoutMs?: number
  /** paths the middleware leaves alone (default: Astro's and Vite's internals, `/api/` and `/__hydrogen/`) */
  skip?: (pathname: string) => boolean
}

export interface PageData {
  /** the template key: the content type, or `*` for the fallback */
  template: string
  item: Item<any>
  locale: string
  metadata: Metadata
}

/** `Astro.locals.hydrogen`, set by the middleware for every page request. */
export interface HydrogenLocals {
  /** the request is a preview (valid-looking token in the cookie): drafts, no caching, `data-h-path`, the bridge */
  draft: boolean
  token?: string
  /** SHA-256 hex of the token, for `<meta name="hydrogen-preview">` */
  previewHash?: string
  /** the channel matched by host and prefix (undefined: none matched) */
  channel?: string
  /** the page the CMS resolved for the URL (not for reserved paths, which the frontend serves itself) */
  page?: PageData
  /** the CMS could not be asked */
  unavailable?: boolean
  adminOrigin: string
  /** a client of a channel: cached and tagged normally, uncached with the preview token in preview */
  client(channel?: string): Client<any>
  /** the navigation of the request's channel (the first configured channel when none matched) */
  nav(o?: { depth?: number; site?: string; locale?: string }): Promise<TreeNode[]>
  /** a CMS URL (absolute, from `item.url`, `links`, tree nodes) as a path of this site with the Astro `base`; other origins stay as they are */
  href(url: string): string
  /** the URL that leaves preview (clears the cookie) and returns to `path`: for a token the CMS refuses */
  exitUrl(path: string): string
}

interface Cookies {
  get(name: string): { value: string } | undefined
  set(name: string, value: string, o?: Record<string, unknown>): void
}
export interface MiddlewareContext { request: Request; cookies: Cookies; locals: Record<string, any>; isPrerendered?: boolean; site?: URL }
export type MiddlewareNext = (rewrite?: string) => Promise<Response>
export interface RouteContext { request: Request; cookies: Cookies }

const MAX_BODY = 1 << 20
const TTL = 300_000
const defaultSkip = (p: string) => p.startsWith('/_') || p.startsWith('/@') || p.startsWith('/api/') || p.startsWith('/node_modules/') || p.startsWith('/src/')
const json = (body: unknown, status = 200) => Response.json(body, { status })
const redirect = (location: string, status: number, draft = false) =>
  new Response(null, { status, headers: { location, ...(draft ? { 'cache-control': 'private, no-store' } : {}) } })

const STATIC_PAGE = 500
const STATIC_CONCURRENCY = 4

async function pool<T>(items: T[], size: number, fn: (x: T) => Promise<void>): Promise<void> {
  let i = 0
  await Promise.all(Array.from({ length: Math.min(size, items.length) }, async () => {
    while (i < items.length) await fn(items[i++]!)
  }))
}

const trimBase = (b: string | undefined) => (b ?? '').replace(/\/+$/, '')

export function createHydrogen(o: HydrogenOptions) {
  const names = o.channels ?? (o.channel ? [o.channel] : [])
  if (!names.length) throw new Error('createHydrogen: no channel')
  const types = Array.isArray(o.templates) ? o.templates : Object.keys(o.templates)
  const previewBase = o.previewPath ?? '/preview'
  const previewRoute = o.previewRoutePath ?? '/api/hydrogen/preview'
  const skip = o.skip ?? defaultSkip
  const timeout = o.timeoutMs ?? 5000
  const adminOrigin = new URL(o.adminOrigin ?? o.baseUrl).origin
  const cache = createTagCache({ ttl: o.ttl ?? 3600, timeoutMs: timeout })

  const clientOf = (channel?: string, preview?: string): Client<any> =>
    createClient({ baseUrl: o.baseUrl, channel, token: o.token, preview, fetch: cache.fetch })

  const routers = new Map<string, { at: number; router: Router }>()
  function routerOf(channel: string): Router {
    let e = routers.get(channel)
    if (!e || performance.now() - e.at > TTL) {
      e = {
        at: performance.now(),
        router: createRouter({
          client: clientOf(channel),
          templates: Object.fromEntries(types.map((t) => [t, t])),
          fallback: o.fallback ? '*' : undefined,
        }),
      }
      routers.set(channel, e)
    }
    return e.router
  }

  let infos: { at: number; p: Promise<ChannelInfo[]> } | undefined
  // the channels' match hosts, which href() treats as this site's own
  const hosts = new Set<string>()
  function channelInfos(): Promise<ChannelInfo[]> {
    if (infos && performance.now() - infos.at < TTL) return infos.p
    const p = Promise.all(names.map(async (n) => (await clientOf(n).channel(n)).data))
    p.then((all) => all.forEach((c) => c.options.match?.host && hosts.add(c.options.match.host)), () => {})
    p.catch(() => (infos = undefined))
    infos = { at: performance.now(), p }
    return p
  }

  // Astro's `base` (no trailing slash) and `site`, set by the integration's middleware entry
  let base = trimBase(o.base)
  let site: URL | undefined
  const ownOrigin = () => (o.origin ? o.origin.replace(/\/+$/, '') : site?.origin)
  // a channel's frontend may carry the base already: CMS URLs then hold it, and it is never added twice
  const stripBase = (path: string) => (base && (path === base || path.startsWith(base + '/')) ? path.slice(base.length) || '/' : path)
  const withBase = (path: string) => {
    const p = stripBase(path)
    return (base && (p === '/' ? base + '/' : base + p)) || p
  }

  /** The site path (decoded, no slashes at the ends) of a URL that belongs to this channel and is not reserved. */
  function pathOf(url: string, info: ChannelInfo, all: ChannelInfo[]): string | undefined {
    const u = new URL(url)
    const path = stripBase(u.pathname)
    const m = matchSite(u.origin + path, all)
    if (!m || m.bad || m.channel.name !== info.name || reservedMatch(info.options.reserved ?? [], m.path)) return undefined
    return decodeURIComponent(path).replace(/^\/+|\/+$/g, '')
  }

  function href(url: string): string {
    if (url.startsWith('/') && !url.startsWith('//')) return withBase(url)
    let u: URL
    try {
      u = new URL(url)
    } catch {
      return url
    }
    return u.origin === ownOrigin() || hosts.has(u.hostname) ? withBase(u.pathname) + u.search + u.hash : url
  }

  const exitUrl = (path: string) => `${previewRoute}?exit=1&path=${encodeURIComponent(path)}`

  /**
   * Asks the CMS about the request and decides what a page can no longer change: redirects with the CMS's status,
   * 404s with a real 404 status. Everything else passes to the page, with the resolved page in `locals.hydrogen`.
   * A CMS that is down passes the request with `unavailable` set.
   */
  async function middleware(ctx: MiddlewareContext, next: MiddlewareNext): Promise<Response> {
    const u = new URL(ctx.request.url)
    // a prerendering build has no visitor: no cookies (so no preview), and the request carries Astro's base
    const pre = ctx.isPrerendered === true
    // (Astro asks for directory pages with a trailing slash, which the CMS would redirect away)
    const pathname = pre ? stripBase(u.pathname).replace(/\/+$/, '') || '/' : u.pathname
    // a rewrite (the 404 page) enters the middleware again: the first pass has decided
    if (skip(pathname) || ctx.locals.hydrogen) return next()

    // The CMS's preview URLs are the normal URL plus ?preview=<token>: the preview route validates it and sets the cookie.
    const pv = u.searchParams.get('preview')
    if (!pre && pv !== null && (ctx.request.method === 'GET' || ctx.request.method === 'HEAD') && pathname !== previewRoute) {
      u.searchParams.delete('preview')
      const rest = u.searchParams.toString()
      return redirect(`${previewRoute}?${new URLSearchParams({ token: validToken(pv) ? pv : '', path: pathname + (rest ? '?' + rest : '') })}`, 307, true)
    }

    const token = pre ? undefined : tokenFromCookie(ctx.cookies.get(PREVIEW_COOKIE)?.value)
    const here = pathname + u.search
    const h: HydrogenLocals = {
      draft: !!token,
      token,
      previewHash: token ? await previewHash(token) : undefined,
      adminOrigin,
      client: (channel) => clientOf(channel ?? h.channel ?? names[0], token),
      nav: async (a = {}) => (await h.client(h.channel ?? names[0]).tree(a)).data,
      href,
      exitUrl,
    }
    ctx.locals.hydrogen = h
    const notFoundPath = o.notFoundPath ?? '/404'
    // the 404 page is built as a page of its own: it is not a CMS URL
    if (pre && pathname === notFoundPath) return next()
    const notFound = async () => {
      const r = await next(notFoundPath)
      return new Response(r.body, { status: 404, headers: r.headers })
    }
    const done = async (r: Response) => {
      if (!h.draft) return r
      // never cached, never shared: the response shows a draft
      const out = new Response(r.body, r)
      out.headers.set('cache-control', 'private, no-store')
      out.headers.append('vary', 'Cookie')
      return out
    }

    // A CMS error: a refused token leaves preview and returns to the public page.
    const failed = async (e: unknown) => {
      if (!(e instanceof HydrogenError)) h.unavailable = true
      else if (e.status === 401 && token) return redirect(exitUrl(here), 307, true)
      else if (e.status === 400 || e.status === 404) return done(await notFound())
      else h.unavailable = true
      return done(await next())
    }

    // An item with no route yet is named by the channel's preview URL (no channel prefix); the token picks the channel.
    const pp = token ? matchPreviewPath(pathname, previewBase) : undefined
    if (pp) {
      try {
        const item = (await clientOf(undefined, token).item(pp.id)).data
        const template = types.includes(item.type) ? item.type : o.fallback ? '*' : undefined
        if (template === undefined) return done(await notFound())
        h.page = { template, item, locale: item.locale, metadata: { title: item.name, alternates: [], locale: item.locale } }
        return done(await next())
      } catch (e) {
        return failed(e)
      }
    }

    // a build has no visitor headers either (Astro warns on access)
    const headers = pre ? undefined : ctx.request.headers
    const origin = pre ? (ownOrigin() ?? ctx.site?.origin ?? u.origin) : originOf(ctx.request.headers, o.origin, o.trustForwarded)
    const url = origin + (pathname || '/')
    let all: ChannelInfo[]
    try {
      all = await channelInfos()
    } catch {
      h.unavailable = true
      return next()
    }
    h.channel = o.channels ? matchSite(url, all, headers)?.channel.name : o.channel
    if (!h.channel) return done(await notFound())
    let r: Resolved
    try {
      r = await routerOf(h.channel).resolve(url, { headers: headers && matcherHeaders(all, headers), preview: token })
    } catch (e) {
      return failed(e)
    }
    switch (r.kind) {
      case 'redirect': {
        // relative to the public origin, never the internal host the server sees
        // (a prerendered one becomes a meta-refresh page at the requested path; the base is part of its target)
        const to = new URL(r.location, origin + here)
        if (!to.search) to.search = u.search
        return redirect(to.origin === origin ? (pre ? withBase(to.pathname) : to.pathname) + to.search + to.hash : to.href, r.status, h.draft)
      }
      case 'notFound':
        return done(await notFound())
      case 'page':
        h.page = { template: r.template, item: r.item, locale: r.locale, metadata: r.metadata }
        return done(await next())
      default:
        return done(await next())
    }
  }

  return {
    /** the Astro middleware (`onRequest`) */
    middleware,

    /** @internal Astro's `base` and `site`, handed over by the integration's middleware entry */
    configure(a: { base?: string; site?: string }) {
      base = trimBase(a.base ?? o.base)
      site = a.site ? new URL(a.site) : undefined
    },

    href,

    /**
     * The `getStaticPaths` entries of a static build: every URL the CMS routes for the configured channels (the
     * collections each allows, in each of its languages), as `{ params: { path } }` without base and leading slash
     * (`undefined` for the root). Reserved paths and URLs of other channels are left out.
     */
    async staticPaths(a: { param?: string } = {}): Promise<{ params: Record<string, string | undefined> }[]> {
      const all = await channelInfos()
      const jobs = all.flatMap((info) => info.languages.flatMap((locale) => info.collections.map((type) => ({ info, locale, type }))))
      const found = new Set<string>()
      await pool(jobs, STATIC_CONCURRENCY, async ({ info, locale, type }) => {
        const c = clientOf(info.name)
        for (let offset = 0; ; offset += STATIC_PAGE) {
          let l
          try {
            l = (await c.list(type, { locale, limit: STATIC_PAGE, offset })).data
          } catch (e) {
            // a singleton is not a collection
            if (e instanceof HydrogenError && (e.status === 400 || e.status === 404)) return
            throw new Error(`hydrogen: cannot list ${type} (${locale}) of channel ${info.name}: ${e instanceof Error ? e.message : e}`)
          }
          for (const it of l.items) {
            const p = it.url ? pathOf(it.url, info, all) : undefined
            if (p !== undefined) found.add(p)
          }
          if (!l.hasMore) return
        }
      })
      return [...found].sort().map((p) => ({ params: { [a.param ?? 'path']: p || undefined } }))
    },

    /** `GET` of the preview route: `?token=…&path=/…` keeps the token in an httpOnly cookie and redirects; `?exit=1` leaves */
    previewRoute(ctx: RouteContext): Response {
      const url = new URL(ctx.request.url)
      const p = parsePreviewRequest(url)
      if ('error' in p) return json({ error: p.error }, 400)
      if (p.exit) ctx.cookies.set(PREVIEW_COOKIE, '', { path: '/', maxAge: 0 })
      else {
        const secure = (ctx.request.headers.get('x-forwarded-proto') ?? url.protocol.slice(0, -1)) === 'https'
        ctx.cookies.set(PREVIEW_COOKIE, p.token, previewCookieOptions(secure))
      }
      return redirect(p.path, 307, true)
    },

    /** `POST` of the revalidation route: the signed webhook, purging the cache entries tagged like the event */
    async revalidate(ctx: RouteContext): Promise<Response> {
      const secret = o.revalidateSecret
      if (!secret || secret.length < MIN_SECRET_LENGTH) return json({ error: `revalidateSecret: ${MIN_SECRET_LENGTH}+ characters` }, 500)
      const body = await readCapped(ctx.request, MAX_BODY)
      if (!body) return json({ error: 'body too large' }, 413)
      if (!(await verifySignature(secret, ctx.request.headers.get(SIGNATURE_HEADER), body))) return json({ error: 'invalid signature' }, 401)
      let parsed: unknown
      try {
        parsed = JSON.parse(new TextDecoder().decode(body))
      } catch {
        return json({ error: 'invalid body' }, 400)
      }
      const tags = webhookTags(parsed)
      cache.purge(tags)
      return json({ revalidated: tags })
    },

    /** drops the cached CMS responses carrying one of the tags (what the webhook does) */
    purge: (tags: readonly string[]) => cache.purge(tags),

    /** a client for code outside a request (cached, tagged) */
    client: (channel?: string) => clientOf(channel ?? names[0]),

    adminOrigin,

    /** the template keys (content types) for `hydrogen check-frontend -templates` */
    templates: (): string[] => [...types].sort(),
  }
}

export type Hydrogen = ReturnType<typeof createHydrogen>
