import { defineEventHandler, getCookie, getRequestHeaders, getRequestURL, sendRedirect, setResponseHeader } from 'h3'
import { HydrogenError } from '@hydrogencms/client'
import { matchSite } from '@hydrogencms/router'
import type { Resolved } from '@hydrogencms/router'
import { config } from './context.js'
import type { HydrogenContext } from './context.js'
import { PREVIEW_COOKIE, PREVIEW_PARAM, matchPreviewPath, matcherHeaders, previewHash, publicUrl, tokenFromCookie, validToken } from '@hydrogencms/client/server'
import { channelInfos, clientOf, routerOf } from './delivery.js'

const PREVIEW_ROUTE = '/api/hydrogen/preview'

// Not for Nuxt's own files, API routes and static files (a page slug such as `v1.2` is still a page).
const skip = (p: string) => /^\/(_|api\/)|\.(js|css|map|json|txt|xml|ico|png|jpe?g|gif|svg|webp|avif|woff2?)$/i.test(p)

/**
 * Asks the CMS about every page request, before Nuxt renders anything: redirects answer with the CMS's status
 * (301 included) and the rest is left in `event.context.hydrogen` for the page (`useHydrogenPage`), which turns
 * "not found" into a real 404. A CMS that is down or slow does not take the site down: its cached answers still
 * serve. One `/route` request per page view, cached by the response's tags. The preview cookie makes it uncached.
 */
export default defineEventHandler(async (event) => {
  const url = getRequestURL(event)
  const pathname = url.pathname
  if ((event.method !== 'GET' && event.method !== 'HEAD') || skip(pathname)) return
  const cfg = config()
  const channels = cfg.channels

  // The CMS's preview URLs are the normal URL plus ?preview=<token>: hand the token to the preview route, which
  // validates the path and sets the cookie. The token is a bearer secret and is never logged.
  const q = new URLSearchParams(url.search)
  const pv = q.get('preview')
  if (pv !== null) {
    q.delete('preview')
    const rest = q.toString()
    const to = new URLSearchParams({ [PREVIEW_PARAM]: validToken(pv) ? pv : '', path: pathname + (rest ? '?' + rest : '') })
    setResponseHeader(event, 'cache-control', 'private, no-store')
    return sendRedirect(event, `${PREVIEW_ROUTE}?${to}`, 307)
  }

  const token = tokenFromCookie(getCookie(event, PREVIEW_COOKIE))
  const ctx: HydrogenContext = {
    channel: cfg.channel || channels[0]!,
    channels,
    adminOrigin: cfg.adminOrigin,
    client: (ch) => clientOf(cfg, ch ?? ctx.channel, token),
    preview: token ? { token, hash: await previewHash(token) } : undefined,
  }
  event.context.hydrogen = ctx
  if (token) setResponseHeader(event, 'cache-control', 'private, no-store')

  const raw = getRequestHeaders(event) as Record<string, string | undefined>
  const headers = { get: (n: string) => raw[n.toLowerCase()] ?? null }
  const notFound: Resolved = { kind: 'notFound', tags: [] }
  const exit = () => sendRedirect(event, `${PREVIEW_ROUTE}?exit=1&path=${encodeURIComponent(pathname)}`, 307)
  // An item with no route yet: the channel's preview URL names it, and the token picks the channel.
  const pp = token && matchPreviewPath(pathname, cfg.previewPath)
  try {
    if (pp) {
      const item = (await clientOf(cfg, undefined, token).item(pp.id)).data
      const template = cfg.templates.includes(item.type) ? item.type : cfg.fallback ? '*' : undefined
      ctx.resolved = template === undefined ? notFound : {
        kind: 'page', template, item, locale: item.locale, tags: [],
        metadata: { title: item.name, alternates: [], locale: item.locale },
      }
      return
    }
    const reqUrl = publicUrl(headers, pathname, cfg.origin || undefined, cfg.trustForwarded)
    const all = await channelInfos(cfg)
    const channel = cfg.channel || matchSite(reqUrl, all, raw)?.channel.name
    if (!channel) return void (ctx.resolved = notFound)
    ctx.channel = channel
    const r = await routerOf(cfg, channel, token).resolve(reqUrl, { headers: matcherHeaders(all, headers), preview: token })
    if (r.kind === 'redirect') {
      const base = new URL(reqUrl).origin
      const to = new URL(r.location, reqUrl)
      if (!to.search) to.search = url.search
      return sendRedirect(event, to.origin === base ? to.pathname + to.search + to.hash : to.href, r.status)
    }
    ctx.resolved = r
  } catch (e) {
    if (e instanceof HydrogenError && e.status < 500) {
      // A token the CMS refuses (expired, forged) is no preview: leave it and come back to the public page.
      if (token && e.status === 401) return exit()
      ctx.resolved = notFound
    } else ctx.unavailable = true
  }
})
