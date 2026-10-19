import { error, isHttpError, isRedirect, json, redirect } from '@sveltejs/kit'
import type { Handle, RequestEvent } from '@sveltejs/kit'
import { HydrogenError } from '@hydrogencms/client'
import type { ChannelInfo, Client, Item, TreeNode } from '@hydrogencms/client'
import { matchSite } from '@hydrogencms/router'
import type { Metadata as RouteMetadata, Resolved } from '@hydrogencms/router'
import { adminOriginWarning } from './admin.js'
import {
  MIN_SECRET_LENGTH, PREVIEW_COOKIE, SIGNATURE_HEADER, matchPreviewPath, matcherHeaders, parsePreviewRequest, previewCookieOptions,
  previewHash, publicUrl, readCapped, tokenFromCookie, validToken, verifySignature, webhookTags,
} from '@hydrogencms/client/server'
import { createDelivery } from './delivery.js'

export type { ChannelInfo, Client, Item, TreeNode, RouteMetadata, Resolved }
export { PREVIEW_COOKIE, createTagCache, verifySignature, webhookTags } from '@hydrogencms/client/server'
export type { TagCache, TagCacheOptions } from '@hydrogencms/client/server'

export interface HydrogenOptions {
  /** the CMS, e.g. "https://cms.acme.com" */
  baseUrl: string
  /** the one channel this deployment serves ... */
  channel?: string
  /** ... or several, told apart by the host and prefix matchers in their options */
  channels?: string[]
  /** token of a token-protected channel */
  token?: string
  /** the content types that have a template (the keys of the template map of `<Page>`) */
  templates: string[] | Record<string, unknown>
  /** a fallback template exists */
  fallback?: boolean
  /** webhook secret for `revalidate`; at least 16 characters */
  revalidateSecret?: string
  /** public origin to locate URLs with instead of the request's (dev on localhost, or another host than the channel's) */
  origin?: string
  /** use X-Forwarded-Host/-Proto for the public origin (default false; only behind a front proxy that overwrites them) */
  trustForwarded?: boolean
  /** path of the preview URLs for items without a route (default "/preview": `/preview/<collection>/<id>`) */
  previewPath?: string
  /** where the preview route lives (default "/api/hydrogen/preview"): `?preview=<token>` URLs go there */
  previewRoutePath?: string
  /** seconds a cached CMS response stays fresh when no webhook purges it (default 3600) */
  ttl?: number
  /** milliseconds the CMS may take per request (default 5000) */
  timeoutMs?: number
  /** origin of the admin that embeds draft pages for live preview (default: the origin of `baseUrl`) */
  adminOrigin?: string
}

/** `event.locals.hydrogen`, set by `handle`. */
export interface HydrogenLocals {
  /** the preview token of the visitor's cookie, if there is one */
  token?: string
  /** the channel the request's host and path belong to (undefined: none matches) */
  channel(): Promise<string | undefined>
  /** what the CMS says the request's URL is; asked once per request. Throws a SvelteKit redirect for a refused preview token. */
  resolve(): Promise<Resolved>
}

/** What `<Page>` renders: the data of `load` and `render`. */
export interface PageData<F = any> {
  /** the template key (the item's content type, or `*` for the fallback) */
  template: string
  item: Item<F>
  locale: string
  metadata: RouteMetadata
  /** the visitor holds a preview token: the page is a draft, uncached, with `data-h-path` markers and the bridge */
  preview: boolean
  /** SHA-256 of the preview token for `<meta name="hydrogen-preview">` (never the token) */
  previewHash?: string
  adminOrigin: string
}

declare global {
  // eslint-disable-next-line @typescript-eslint/no-namespace
  namespace App {
    interface Locals {
      hydrogen: HydrogenLocals
    }
  }
}

const MAX_BODY = 1 << 20
const NO_STORE = { 'cache-control': 'private, no-store' }

export function createHydrogen<T extends object = Record<string, unknown>>(o: HydrogenOptions) {
  const names = o.channels ?? (o.channel ? [o.channel] : [])
  if (!names.length) throw new Error('createHydrogen: no channel')
  const types = Array.isArray(o.templates) ? o.templates : Object.keys(o.templates)
  const previewBase = o.previewPath ?? '/preview'
  const routePath = o.previewRoutePath ?? '/api/hydrogen/preview'
  const d = createDelivery({ baseUrl: o.baseUrl, token: o.token, types, fallback: !!o.fallback, ttl: o.ttl, timeoutMs: o.timeoutMs ?? 5000 })
  const adminOrigin = new URL(o.adminOrigin ?? o.baseUrl).origin
  let warned = false

  const clientFor = (channel: string | undefined, token?: string) => d.client(channel, token) as unknown as Client<T>

  function locals(event: RequestEvent): HydrogenLocals {
    const { pathname } = event.url
    const headers = event.request.headers
    const token = tokenFromCookie(event.cookies.get(PREVIEW_COOKIE))
    const url = publicUrl(headers, pathname, o.origin, o.trustForwarded)
    let fwd: Record<string, string> | undefined
    let chan: Promise<string | undefined> | undefined
    let res: Promise<Resolved> | undefined

    const channel = () =>
      (chan ??= (async () => {
        if (!o.channels) return o.channel
        const all = await d.channelInfos(names)
        fwd = matcherHeaders(all, headers)
        return matchSite(url, all, headers)?.channel.name
      })())

    const run = async (): Promise<Resolved> => {
      const ch = await channel()
      if (!ch) return { kind: 'notFound', tags: [] }
      fwd ??= matcherHeaders(await d.channelInfos(names), headers)
      const pv = token ? matchPreviewPath(pathname, previewBase) : undefined
      try {
        if (token && pv) {
          // An item without a route yet: the channel's preview URL names it; the token picks the channel.
          const item = (await d.client(undefined, token).item(pv.id)).data
          const template = types.includes(item.type) ? item.type : o.fallback ? '*' : undefined
          if (template === undefined) return { kind: 'notFound', tags: [] }
          const locale = item.locale
          return { kind: 'page', template, item, locale, tags: [], metadata: { title: item.name, alternates: [], locale } }
        }
        return await d.router(ch).resolve(url, { headers: fwd, preview: token })
      } catch (e) {
        if (!(e instanceof HydrogenError) || e.status >= 500) throw e
        // An expired or refused token: leave preview (clears the cookie) and come back to the public page.
        if (e.status === 401) redirect(307, `${routePath}?exit=1&path=${encodeURIComponent(pathname)}`)
        return { kind: 'notFound', tags: [] }
      }
    }
    return { token, channel, resolve: () => (res ??= run()) }
  }

  // setHeaders throws when a header is set twice
  const marked = new WeakSet<object>()
  const noStore = (event: RequestEvent) => {
    if (marked.has(event)) return
    marked.add(event)
    event.setHeaders(NO_STORE)
  }

  /** SHA-256 of each request's preview token (async, so computed in `handle`), for `pageData` */
  const hashes = new WeakMap<object, string>()

  const handle: Handle = async ({ event, resolve }) => {
    const { pathname, searchParams } = event.url
    // The CMS's preview URLs are the normal URL plus ?preview=<token>: hand the token to the preview route, which
    // validates the path and sets the cookie. Nothing is logged; the token is a bearer secret.
    if (searchParams.has('preview') && (event.request.method === 'GET' || event.request.method === 'HEAD') && pathname !== routePath) {
      const q = new URLSearchParams(searchParams)
      const pv = q.get('preview')
      q.delete('preview')
      const rest = q.toString()
      const to = new URLSearchParams({ token: validToken(pv) ? pv : '', path: pathname + (rest ? '?' + rest : '') })
      return new Response(null, { status: 307, headers: { location: `${routePath}?${to}`, ...NO_STORE } })
    }
    const l = (event.locals.hydrogen = locals(event))
    if (l.token) hashes.set(l, await previewHash(l.token))
    return resolve(event)
  }

  const unavailable = (e: unknown): never => {
    if (isRedirect(e) || isHttpError(e)) throw e
    console.error('@hydrogencms/sveltekit: the CMS request failed:', e instanceof Error ? e.message : e)
    error(502, 'The content service is unavailable')
  }

  function pageData(event: RequestEvent, p: { template?: string; item: Item<any>; locale: string; metadata: RouteMetadata }): PageData {
    const token = event.locals.hydrogen.token
    if (!token) return { template: p.template ?? p.item.type, item: p.item, locale: p.locale, metadata: p.metadata, preview: false, adminOrigin }
    if (!warned) {
      warned = true
      const w = adminOriginWarning(adminOrigin)
      if (w) console.warn(`@hydrogencms/sveltekit: ${w}`)
    }
    noStore(event)
    return { template: p.template ?? p.item.type, item: p.item, locale: p.locale, metadata: p.metadata, preview: true, previewHash: hashes.get(event.locals.hydrogen), adminOrigin }
  }

  /** The data of a route of your own that shows an item: the same shape `load` returns, for `<Page>`. */
  const render = (event: RequestEvent, p: { template?: string; item: Item<any>; locale: string; metadata?: RouteMetadata }): PageData =>
    pageData(event, { ...p, metadata: p.metadata ?? { title: p.item.name, alternates: [], locale: p.locale } })

  /** `load` of the catch-all page: a redirect or 404 is decided here, before anything renders. */
  async function load(event: RequestEvent): Promise<PageData> {
    // in preview even redirects, 404s and error pages must not be cached
    if (event.locals.hydrogen.token) noStore(event)
    let r: Resolved
    try {
      r = await event.locals.hydrogen.resolve()
    } catch (e) {
      return unavailable(e)
    }
    switch (r.kind) {
      case 'page':
        return pageData(event, r)
      case 'redirect': {
        const to = new URL(r.location, event.url)
        if (!to.search) to.search = event.url.search
        return redirect(r.status, to.origin === event.url.origin ? to.pathname + to.search + to.hash : to.href)
      }
      default:
        error(404, 'Not found')
    }
  }

  return {
    /** `export const handle` of hooks.server.ts (compose with `sequence`) */
    handle,
    /** `load` of `src/routes/[...path]/+page.server.ts` */
    load,
    render,

    /** `GET` of the preview route: `?token=…&path=/…` keeps the token in an httpOnly cookie and redirects; `?exit=1` leaves */
    previewRoute: async (event: RequestEvent): Promise<Response> => {
      const p = parsePreviewRequest(event.url)
      if ('error' in p) return json({ error: p.error }, { status: 400 })
      if (p.exit) event.cookies.delete(PREVIEW_COOKIE, { path: '/' })
      else {
        const secure = (event.request.headers.get('x-forwarded-proto') ?? event.url.protocol.slice(0, -1)) === 'https'
        event.cookies.set(PREVIEW_COOKIE, p.token, previewCookieOptions(secure))
      }
      return new Response(null, { status: 307, headers: { location: p.path, ...NO_STORE } })
    },

    /** `POST` of the revalidation route: the signed webhook, purging the cache entries that carry the event's tags */
    revalidate: async ({ request }: { request: Request }): Promise<Response> => {
      const secret = o.revalidateSecret
      if (!secret || secret.length < MIN_SECRET_LENGTH)
        return json({ error: `revalidateSecret: ${MIN_SECRET_LENGTH}+ characters` }, { status: 500 })
      const body = await readCapped(request, MAX_BODY)
      if (!body) return json({ error: 'body too large' }, { status: 413 })
      if (!(await verifySignature(secret, request.headers.get(SIGNATURE_HEADER), body)))
        return json({ error: 'invalid signature' }, { status: 401 })
      let parsed: unknown
      try {
        parsed = JSON.parse(new TextDecoder().decode(body))
      } catch {
        return json({ error: 'invalid body' }, { status: 400 })
      }
      const tags = webhookTags(parsed)
      d.cache.purge(tags)
      return json({ revalidated: tags })
    },

    /** a client of a channel for load functions: cached and tagged, or uncached and carrying the preview token while previewing */
    client: (event: RequestEvent, channel?: string): Client<T> => clientFor(channel ?? names[0], event.locals.hydrogen.token),

    /** the channel of the request, for layouts (the first configured one when none matches) */
    channel: async (event: RequestEvent): Promise<string> => (await event.locals.hydrogen.channel()) ?? names[0]!,

    /** the navigation of the request's channel */
    async nav(event: RequestEvent, a: { depth?: number; site?: string; locale?: string } = {}): Promise<TreeNode[]> {
      const ch = (await event.locals.hydrogen.channel()) ?? names[0]
      return (await clientFor(ch, event.locals.hydrogen.token).tree(a)).data
    },

    /** the preview token of the request, for routes of your own */
    previewToken: (event: RequestEvent): string | undefined => event.locals.hydrogen.token,

    /** drops the cached CMS responses that carry the tags (what the webhook does) */
    purge: (tags: string[]): number => d.cache.purge(tags),

    adminOrigin,
    channels: names,
    /** the template keys (content types) for `hydrogen check-frontend -templates` */
    templates: (): string[] => [...types].sort(),
  }
}
