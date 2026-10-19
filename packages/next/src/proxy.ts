import { NextResponse } from 'next/server.js'
import type { NextRequest } from 'next/server.js'
import { HydrogenError } from '@hydrogencms/client'
import type { ChannelInfo } from '@hydrogencms/client'
import type { Resolved } from '@hydrogencms/router'
import { matchSite } from '@hydrogencms/router'
import { PREVIEW_COOKIE, PREVIEW_PARAM, matchPreviewPath, matcherHeaders, publicUrl, tokenFromCookie, validToken } from '@hydrogencms/client/server'
import { channelInfos, routerOf, setToken } from './delivery.js'
import { PATH_HEADER } from './url.js'

export interface ProxyOptions {
  baseUrl: string
  /** the one channel this deployment serves ... */
  channel?: string
  /** ... or the channels it serves, told apart by their host and prefix matchers */
  channels?: string[]
  token?: string
  /** the content types that have a template (keys of the template map) */
  templates: string[] | Record<string, unknown>
  /** a fallback template exists */
  fallback?: boolean
  origin?: string
  /** use X-Forwarded-Host/-Proto for the public origin (default false; only behind a front proxy that overwrites them) */
  trustForwarded?: boolean
  /** where unknown URLs are rewritten to, with status 404 (default the app's `/_not-found`) */
  notFoundPath?: string
  /** path of the preview URLs for items without a route (default "/preview") */
  previewPath?: string
  /** where the preview route lives (default "/api/hydrogen/preview"): `?preview=<token>` URLs go there */
  previewRoutePath?: string
  /** paths the proxy leaves alone (default: `/_next/` and `/api/`) */
  skip?: (pathname: string) => boolean
  /** milliseconds the CMS may take before the proxy lets the request through (default 3000) */
  timeoutMs?: number
}

const defaultSkip = (p: string) => p.startsWith('/_next/') || p.startsWith('/api/')

/**
 * Edge proxy (Next's `proxy.ts`, formerly middleware): asks the CMS about every page request and answers what a
 * streamed page can no longer change: redirects with the CMS's status (301 included) and 404s with a real 404
 * status. A CMS that is down or slow does not take the site down: the request passes and the page renders from its
 * cache. It costs one uncached `/route` request per page view. A preview cookie only adds the token to that call.
 */
export function createProxy(o: ProxyOptions): (req: NextRequest) => Promise<NextResponse | undefined> {
  const skip = o.skip ?? defaultSkip
  const names = o.channels ?? (o.channel ? [o.channel] : [])
  const types = Array.isArray(o.templates) ? o.templates : Object.keys(o.templates)
  const previewBase = o.previewPath ?? '/preview'
  const timeout = o.timeoutMs ?? 3000
  setToken(o.baseUrl, o.token)

  return async (req) => {
    const pathname = new URL(req.url).pathname
    // What the client sent in our header is never kept.
    const headers = new Headers(req.headers)
    headers.set(PATH_HEADER, pathname)
    const pass = () => NextResponse.next({ request: { headers } })
    const notFound = () => {
      const u = req.nextUrl.clone()
      u.pathname = o.notFoundPath ?? '/_not-found'
      u.search = ''
      return NextResponse.rewrite(u, { status: 404 })
    }
    if (skip(pathname)) return pass()

    // The CMS's preview URLs are the normal URL plus ?preview=<token>: hand the token to the preview route, which
    // validates the path, enables draft mode and sets the cookie. Nothing is logged; the token is a bearer secret.
    const q = new URLSearchParams(req.nextUrl.search)
    const pv = q.get('preview')
    if (pv !== null) {
      q.delete('preview')
      const rest = q.toString()
      const u = req.nextUrl.clone()
      u.pathname = o.previewRoutePath ?? '/api/hydrogen/preview'
      u.search = new URLSearchParams({ [PREVIEW_PARAM]: validToken(pv) ? pv : '', path: pathname + (rest ? '?' + rest : '') }).toString()
      return NextResponse.redirect(u, 307)
    }

    const token = tokenFromCookie(req.cookies.get(PREVIEW_COOKIE)?.value)
    // An item with no route yet is shown by the page, which asks the CMS with the token itself.
    if (token && matchPreviewPath(pathname, previewBase)) return pass()

    const url = publicUrl(req.headers, pathname, o.origin, o.trustForwarded)
    let all: ChannelInfo[]
    try {
      all = await channelInfos(o.baseUrl, names, timeout)
    } catch {
      return pass()
    }
    const channel = o.channels ? matchSite(url, all, req.headers)?.channel.name : o.channel
    if (!channel) return notFound()

    const router = routerOf({ baseUrl: o.baseUrl, channel, types, fallback: !!o.fallback }, timeout)
    const fwd = matcherHeaders(all, req.headers)
    let r: Resolved
    try {
      try {
        r = await router.resolve(url, { headers: fwd, preview: token })
      } catch (e) {
        // A token the CMS refuses (expired, forged) is no preview: the normal path applies.
        if (!token || !(e instanceof HydrogenError) || e.status !== 401) throw e
        r = await router.resolve(url, { headers: fwd })
      }
    } catch (e) {
      return e instanceof HydrogenError && e.status === 400 ? notFound() : pass()
    }
    if (r.kind === 'redirect') {
      const to = new URL(r.location, req.url)
      if (!to.search) to.search = req.nextUrl.search
      return NextResponse.redirect(to, r.status)
    }
    return r.kind === 'notFound' ? notFound() : pass()
  }
}
