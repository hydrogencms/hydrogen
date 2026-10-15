import { createElement, Fragment } from 'react'
import type { ComponentType, ReactElement } from 'react'
import { cookies, draftMode, headers } from 'next/headers'
import { notFound, permanentRedirect, redirect } from 'next/navigation'
import { revalidateTag } from 'next/cache'
import type { NextRequest } from 'next/server'
import type { Metadata } from 'next'
import { HydrogenError } from '@hydrogencms/client'
import type { Registry } from '@hydrogencms/react'
import type { ChannelInfo, Client, Item, TreeNode } from '@hydrogencms/client'
import { matchSite } from '@hydrogencms/router'
import type { Metadata as RouteMetadata, Resolved } from '@hydrogencms/router'
import { adminOriginWarning } from './admin.js'
import { Live } from './live.js'
import { previewHash } from './hash.js'
import { callCached, resolveCached, resolveFresh } from './cache.js'
import type { Called, Cfg, Method } from './cache.js'
import { channelInfos, clientOf, matcherHeaders, setToken } from './delivery.js'
import {
  PREVIEW_COOKIE, matchPreviewPath, parsePreviewRequest, previewCookieOptions, tokenFromCookie,
} from './preview.js'
import { MIN_SECRET_LENGTH, SIGNATURE_HEADER, readCapped, verifySignature } from './signature.js'
import { webhookTags } from './tags.js'
import { PATH_HEADER, publicUrl } from './url.js'

export type { ChannelInfo, Client, Item, TreeNode, RouteMetadata }
export { createProxy } from './proxy.js'
export type { ProxyOptions } from './proxy.js'
export { verifySignature } from './signature.js'
export { cacheTags, webhookTags } from './tags.js'
export { PREVIEW_COOKIE } from './preview.js'

/** What a template receives. Templates are server components. */
export interface TemplateProps<F = any, T extends object = any> {
  item: Item<F>
  locale: string
  metadata: RouteMetadata
  /** draft mode: pass it, with `path="fields/body"`, to `<Blocks>` so blocks carry `data-h-path` */
  preview: boolean
  /** the `blocks` option (server templates only) */
  blocks?: Registry
  /** cached and tagged like the page itself; in draft mode an uncached client carrying the preview token. Server templates only. */
  client: Client<T>
}

/**
 * What a client template (`'use client'`) receives: server templates' functions cannot cross to the browser, so
 * there is no `client` or `blocks`. In draft mode only client templates update live while the admin edits.
 */
export type LiveTemplateProps<F = any> = Omit<TemplateProps<F>, 'client' | 'blocks'>

export interface HydrogenOptions<T extends object = Record<string, unknown>> {
  /** the CMS, e.g. "https://cms.acme.com" */
  baseUrl: string
  /** the one channel this deployment serves ... */
  channel?: string
  /** ... or several, told apart by the host and prefix matchers in their options */
  channels?: string[]
  /** token of a token-protected channel */
  token?: string
  /** by content type: page types and mounted collections */
  templates: Record<string, ComponentType<TemplateProps<any, T>>>
  fallback?: ComponentType<TemplateProps<any, T>>
  /** webhook secret for `revalidate()`; at least 16 characters */
  revalidateSecret?: string
  /** public origin to locate URLs with instead of the request's (dev on localhost, or another host than the channel's) */
  origin?: string
  /** use X-Forwarded-Host/-Proto for the public origin (default true: the front proxy must overwrite them) */
  trustForwarded?: boolean
  /** path of the preview URLs for items without a route (default "/preview": `/preview/<collection>/<id>`) */
  previewPath?: string
  /** seconds a cached CMS response stays fresh when no webhook purges it (default 3600) */
  ttl?: number
  /** where the preview route lives (default "/api/hydrogen/preview"); an expired token sends the visitor to its `exit` */
  previewRoutePath?: string
  /** block components, handed to server templates as `blocks` */
  blocks?: Registry
  /** origin of the admin that embeds draft pages for live preview (default: the origin of `baseUrl`) */
  adminOrigin?: string
}

type Params = { params: Promise<{ path?: string[] }> }

const unwrap = (c: Called) => {
  if ('err' in c) throw new HydrogenError(c.err.status, c.err.code, c.err.message)
  return c.ok
}

/** Options that cannot be part of a cache key. */
const plain = (a: unknown): unknown => {
  if (!a || typeof a !== 'object' || Array.isArray(a)) return a
  const { signal: _s, init: _i, ...rest } = a as Record<string, unknown>
  return rest
}

const MAX_BODY = 1 << 20

export function createHydrogen<T extends object = Record<string, unknown>>(o: HydrogenOptions<T>) {
  const names = o.channels ?? (o.channel ? [o.channel] : [])
  if (!names.length) throw new Error('createHydrogen: no channel')
  setToken(o.baseUrl, o.token)
  const types = Object.keys(o.templates)
  const previewBase = o.previewPath ?? '/preview'
  const cfgOf = (channel: string | undefined): Cfg => ({
    baseUrl: o.baseUrl, channel, types, fallback: !!o.fallback, ttl: o.ttl ?? 3600,
  })
  const infos = () => channelInfos(o.baseUrl, names)
  const adminOrigin = new URL(o.adminOrigin ?? o.baseUrl).origin
  let warned = false
  const warnAdmin = () => {
    const w = warned ? undefined : adminOriginWarning(adminOrigin)
    if (w) console.warn(`@hydrogencms/next: ${w}`)
    warned = true
  }
  const urlOf = (h: Headers, path: string) => publicUrl(h, path, o.origin, o.trustForwarded)

  /** Cached client with the Client interface; template code stays unaware of caching. */
  const cachedClient = (cfg: Cfg): Client<T> => {
    const call = (m: Method) => async (...a: unknown[]) => unwrap(await callCached(cfg, m, a.map(plain)))
    return {
      item: call('item'), itemByKey: call('itemByKey'), singleton: call('singleton'), list: call('list'),
      route: call('route'), tree: call('tree'), channel: call('channel'),
      withPreview: (t: string | undefined) => clientOf(cfg.baseUrl, cfg.channel, t),
    } as unknown as Client<T>
  }

  /**
   * The public URL, channel and forwarded matcher headers of the current request. Everything comes from the request
   * itself (Host, path, headers) and the configured channels' match options; nothing is taken from the proxy.
   */
  async function locate(params: Params['params']) {
    const h = await headers()
    const path = '/' + ((await params).path ?? []).map(encodeURIComponent).join('/')
    const url = urlOf(h, path)
    const all = await infos()
    return { url, path, channel: o.channels ? matchSite(url, all, h)?.channel.name : o.channel, fwd: matcherHeaders(all, h) }
  }

  /** Client templates (`'use client'` modules) get the live wrapper in draft mode; server templates render as they are. */
  type RenderProps = { item: Item<any>; locale: string; metadata: RouteMetadata; client: Client<T>; draft: boolean; token?: string }
  function render(T: ComponentType<any>, p: RenderProps): ReactElement {
    const { client, draft, token, ...base } = p
    if (draft) warnAdmin()
    const el = (T as { $$typeof?: symbol }).$$typeof === Symbol.for('react.client.reference')
      ? createElement(draft ? Live : T, draft ? { Template: T, adminOrigin, ...base } : { ...base, preview: false })
      : createElement(T, { ...base, preview: draft, blocks: o.blocks, client })
    return draft ? createElement(Fragment, null, createElement('meta', { name: 'hydrogen-preview', content: token ? previewHash(token) : '' }), el) : el
  }

  const draftToken = async () => (await draftMode()).isEnabled ? tokenFromCookie((await cookies()).get(PREVIEW_COOKIE)?.value) : undefined

  /** Server component: reads the draft cookie so routes of your own need not pass the token. */
  async function Drafted({ T, ...p }: RenderProps & { T: ComponentType<any> }) {
    return render(T, { ...p, token: await draftToken() })
  }
  const show = (T: ComponentType<any>, p: RenderProps) => (p.draft ? createElement(Drafted, { T, ...p }) : render(T, p))

  type Loaded = { r: Resolved; client: Client<T>; draft: boolean }

  async function load({ params }: Params): Promise<Loaded> {
    const token = await draftToken()
    const { url, path, channel, fwd } = await locate(params)
    const none: Resolved = { kind: 'notFound', tags: [] }
    if (!token) {
      const cfg = cfgOf(channel)
      return { r: channel ? await resolveCached(cfg, url, fwd) : none, client: cachedClient(cfg), draft: false }
    }
    const client = clientOf(o.baseUrl, channel, token) as unknown as Client<T>
    const pv = matchPreviewPath(path, previewBase)
    try {
      if (!pv) return { r: channel ? await resolveFresh(cfgOf(channel), url, token, fwd) : none, client, draft: true }
      // An item without a route yet: the channel's preview URL names it; the token picks the channel.
      const item = (await clientOf(o.baseUrl, undefined, token).item(pv.id)).data
      const template = item.type in o.templates ? item.type : o.fallback ? '*' : undefined
      if (template === undefined) return { r: none, client, draft: true }
      const locale = item.locale
      return { r: { kind: 'page', template, item, locale, tags: [], metadata: { title: item.name, alternates: [], locale } }, client, draft: true }
    } catch (e) {
      if (!(e instanceof HydrogenError) || e.status >= 500) throw e
      // An expired or refused token: leave preview (clears draft mode) and come back to the public page.
      if (e.status === 401) redirect(`${o.previewRoutePath ?? '/api/hydrogen/preview'}?exit=1&path=${encodeURIComponent(path)}`)
      return { r: none, client, draft: true }
    }
  }

  const treeOf = async (channel: string, a: { site?: string; locale?: string; depth?: number }) =>
    unwrap(await callCached(cfgOf(channel), 'tree', [a])).data as TreeNode[]

  async function currentChannel(): Promise<string> {
    const h = await headers()
    const path = h.get(PATH_HEADER)
    let channel = names[0]!
    if (names.length > 1 && path?.startsWith('/')) {
      try {
        channel = matchSite(urlOf(h, path), await infos(), h)?.channel.name ?? channel
      } catch {
        // a malformed path keeps the first channel
      }
    }
    return channel
  }

  return {
    /** the default export of `app/[[...path]]/page.tsx` */
    page() {
      return async function HydrogenPage(props: Params): Promise<ReactElement> {
        const { r, client, draft } = await load(props)
        switch (r.kind) {
          case 'page': {
            const T = r.template === '*' ? o.fallback : o.templates[r.template]
            if (!T) notFound()
            return show(T, { item: r.item, locale: r.locale, metadata: r.metadata, client, draft })
          }
          case 'redirect':
            return r.status === 301 || r.status === 308 ? permanentRedirect(r.location) : redirect(r.location)
          default:
            return notFound()
        }
      }
    },

    /** `generateMetadata`: title, canonical and hreflang alternates; noindex in draft mode */
    metadata() {
      return async function generateMetadata(props: Params): Promise<Metadata> {
        const { r, draft } = await load(props)
        if (r.kind !== 'page') return { title: 'Not found', robots: { index: false } }
        const m = r.metadata
        const languages: Record<string, string> = {}
        for (const a of m.alternates) languages[a.hreflang] = a.href
        return {
          title: m.title,
          alternates: { ...(m.canonical ? { canonical: m.canonical } : {}), ...(m.alternates.length ? { languages } : {}) },
          ...(draft ? { robots: { index: false, follow: false } } : {}),
        }
      }
    },

    /** optional `generateStaticParams`: every page in the navigation of the configured channels */
    staticParams() {
      return async function generateStaticParams(): Promise<{ path: string[] }[]> {
        const out = new Map<string, string[]>()
        const walk = (nodes: TreeNode[]) => {
          for (const n of nodes) {
            if (n.url && (n.kind === 'page' || n.kind === 'mount')) {
              const segs = new URL(n.url).pathname.split('/').filter(Boolean)
              out.set(segs.join('/'), segs)
            }
            if (n.nodes) walk(n.nodes)
          }
        }
        for (const c of names) walk(await treeOf(c, { depth: 3 }))
        return [...out.values()].map((path) => ({ path }))
      }
    },

    /** `export const POST`: the signed webhook receiver, `revalidateTag` for every tag of the event */
    revalidate() {
      return async function POST(req: Request): Promise<Response> {
        const secret = o.revalidateSecret
        if (!secret || secret.length < MIN_SECRET_LENGTH)
          return Response.json({ error: `revalidateSecret: ${MIN_SECRET_LENGTH}+ characters` }, { status: 500 })
        const body = await readCapped(req, MAX_BODY)
        if (!body) return Response.json({ error: 'body too large' }, { status: 413 })
        if (!(await verifySignature(secret, req.headers.get(SIGNATURE_HEADER), body)))
          return Response.json({ error: 'invalid signature' }, { status: 401 })
        let parsed: unknown
        try {
          parsed = JSON.parse(new TextDecoder().decode(body))
        } catch {
          return Response.json({ error: 'invalid body' }, { status: 400 })
        }
        const tags = webhookTags(parsed)
        for (const t of tags) revalidateTag(t, { expire: 0 })
        return Response.json({ revalidated: tags })
      }
    },

    /** `export const GET`: `?token=…&path=/…` enables draft mode, keeps the token in an httpOnly cookie and redirects; `?exit=1` leaves */
    previewRoute() {
      return async function GET(req: NextRequest | Request): Promise<Response> {
        const url = new URL(req.url)
        const p = parsePreviewRequest(url)
        if ('error' in p) return Response.json({ error: p.error }, { status: 400 })
        const dm = await draftMode()
        const jar = await cookies()
        if (p.exit) {
          dm.disable()
          jar.delete(PREVIEW_COOKIE)
        } else {
          const secure = (req.headers.get('x-forwarded-proto') ?? url.protocol.slice(0, -1)) === 'https'
          dm.enable()
          jar.set(PREVIEW_COOKIE, p.token, previewCookieOptions(secure))
        }
        return redirect(p.path)
      }
    },

    /** the channel of the request (found from the path the proxy passes on), for layouts: they cannot see the route's params */
    channel: currentChannel,

    /** the navigation of the request's channel; cached and tagged `nav:<site>` */
    async nav(a: { depth?: number; site?: string; locale?: string } = {}): Promise<TreeNode[]> {
      const channel = await currentChannel()
      if (!(await draftMode()).isEnabled) return treeOf(channel, a)
      return (await clientOf(o.baseUrl, channel).tree(a)).data
    },

    /** renders a template like the catch-all does (live wrapper in draft mode, proving the preview token from the draft cookie), for routes of your own */
    render: (T: ComponentType<any>, p: { item: Item<any>; locale: string; metadata: RouteMetadata; draft: boolean; client?: Client<T> }): ReactElement =>
      show(T, { client: p.client ?? (clientOf(o.baseUrl, undefined) as unknown as Client<T>), ...p }),

    /** the preview token in draft mode, for routes of your own */
    async previewToken(): Promise<string | undefined> {
      return draftToken()
    },

    /** a client of a channel for routes of your own: uncached with the preview token in draft mode, else cached and tagged */
    async clientFor(channel: string): Promise<Client<T>> {
      const token = await draftToken()
      return token ? (clientOf(o.baseUrl, channel, token) as unknown as Client<T>) : cachedClient(cfgOf(channel))
    },

    adminOrigin,

    /** the template keys (content types) for `hydrogen check-frontend -templates` */
    templates: (): string[] => [...types].sort(),

    /** uncached client of the first configured channel */
    client: clientOf(o.baseUrl, names[0]) as unknown as Client<T>,
  }
}
