/**
 * Server-side helpers shared by the framework adapters (Next, SvelteKit, Nuxt, Astro): webhook verification, the
 * preview cookie's rules, public URLs and a tagged response cache. Own entry (`@hydrogencms/client/server`), so
 * browser bundles of the client don't carry it. WebCrypto only: it runs in Node >= 20 and on edge runtimes.
 */
import { parseTags } from './index.js'

interface HeaderReader { get(name: string): string | null }

// ---- webhook signature -------------------------------------------------------------------------------------------

export const SIGNATURE_HEADER = 'Hydrogen-Signature'
export const DEFAULT_TOLERANCE_MS = 5 * 60 * 1000
export const MIN_SECRET_LENGTH = 16

const enc = new TextEncoder()

const unhex = (s: string): Uint8Array<ArrayBuffer> | undefined => {
  if (s.length % 2 || !/^[\da-f]*$/i.test(s)) return undefined
  const out = new Uint8Array(new ArrayBuffer(s.length / 2))
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return out
}

/** Reads `Hydrogen-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, t + "." + body)>[,v1=...]`. */
export function parseSignature(header: string | null | undefined): { t: string; sigs: string[] } | undefined {
  if (!header) return undefined
  let t = ''
  const sigs: string[] = []
  for (const part of header.split(',')) {
    const i = part.indexOf('=')
    if (i < 0) return undefined
    const k = part.slice(0, i).trim()
    const v = part.slice(i + 1).trim()
    if (k === 't') t = v
    else if (k === 'v1') sigs.push(v)
  }
  return /^\d+$/.test(t) && sigs.length ? { t, sigs } : undefined
}

/**
 * True when one of the header's v1 signatures matches the raw body and the signed time is within the tolerance
 * of `now` (either direction). The MAC comparison is `crypto.subtle.verify`, which is constant time.
 */
export async function verifySignature(
  secret: string,
  header: string | null | undefined,
  body: Uint8Array,
  now: number = Date.now(),
  toleranceMs: number = DEFAULT_TOLERANCE_MS,
): Promise<boolean> {
  const p = parseSignature(header)
  if (!p || !secret) return false
  if (Math.abs(now - Number(p.t) * 1000) > toleranceMs) return false
  const prefix = enc.encode(p.t + '.')
  const data = new Uint8Array(new ArrayBuffer(prefix.length + body.length))
  data.set(prefix)
  data.set(body, prefix.length)
  const key = await crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['verify'])
  let ok = false
  for (const s of p.sigs) {
    const sig = unhex(s)
    if (sig && (await crypto.subtle.verify('HMAC', key, sig, data))) ok = true
  }
  return ok
}

/** Reads a request body of at most `cap` bytes; undefined when it is larger (Content-Length first, then a running count). */
export async function readCapped(req: { headers: HeaderReader; body: ReadableStream<Uint8Array> | null }, cap: number): Promise<Uint8Array | undefined> {
  const len = req.headers.get('content-length')
  if (len !== null && Number(len) > cap) return undefined
  const parts: Uint8Array[] = []
  let n = 0
  if (req.body) {
    const r = req.body.getReader()
    for (;;) {
      const { done, value } = await r.read()
      if (done) break
      n += value.length
      if (n > cap) {
        await r.cancel()
        return undefined
      }
      parts.push(value)
    }
  }
  const out = new Uint8Array(n)
  let o = 0
  for (const p of parts) (out.set(p, o), (o += p.length))
  return out
}

// ---- purge tags ----------------------------------------------------------------------------------------------------

/** Hydrogen purge tags: `i:<id>`, `c:<type>`, `s:<singleton>`, `r:<site>`, `nav:<site>`, and `*` for everything. */
export const ALL_TAG = '*'
export const MAX_TAG_LENGTH = 256

export const validTag = (t: unknown): t is string => typeof t === 'string' && t.length > 0 && t.length <= MAX_TAG_LENGTH && !/\s/.test(t)

/** The tags to purge for a webhook body (`{"tags": [...]}`); anything malformed is ignored. */
export function webhookTags(body: unknown): string[] {
  const t = (body as { tags?: unknown } | null)?.tags
  return Array.isArray(t) ? [...new Set(t.filter(validTag))].slice(0, 1000) : []
}

// ---- preview ---------------------------------------------------------------------------------------------------------

export const PREVIEW_COOKIE = 'hydrogen-preview'
export const PREVIEW_PARAM = 'token'
const MAX_TOKEN = 2048

/** Preview tokens are opaque (`a.b` or `hps_…`); anything outside this alphabet is not worth storing. */
export const validToken = (t: unknown): t is string => typeof t === 'string' && t.length > 0 && t.length <= MAX_TOKEN && /^[\w.-]+$/.test(t)

/** A same-site path: one leading slash, no scheme, no backslash or control characters (no open redirect). */
export function safePath(p: string | null | undefined): string | undefined {
  if (!p) return '/'
  if (p[0] !== '/' || p[1] === '/' || p[1] === '\\' || /[\\\u0000-\u001f\u007f]/.test(p)) return undefined
  return p
}

export type PreviewRequest = { exit: true; path: string } | { exit?: false; token: string; path: string } | { error: string }

/** Reads the preview route's query: `token` and `path`, or `exit=1` to leave preview. */
export function parsePreviewRequest(url: URL): PreviewRequest {
  const path = safePath(url.searchParams.get('path'))
  if (path === undefined) return { error: 'path must be a site path' }
  if (url.searchParams.get('exit')) return { exit: true, path }
  const token = url.searchParams.get(PREVIEW_PARAM)
  if (!validToken(token)) return { error: 'bad token' }
  return { token, path }
}

/**
 * Cookie attributes: httpOnly, scoped to the site, living as long as a token can (a share link: 7 days).
 * Over https it is SameSite=None so the admin can embed the preview in a frame.
 */
export function previewCookieOptions(secure: boolean): { httpOnly: true; sameSite: 'lax' | 'none'; secure: boolean; path: string; maxAge: number } {
  return { httpOnly: true, sameSite: secure ? 'none' : 'lax', secure, path: '/', maxAge: 604800 }
}

/** The token from a cookie value, if it is one. */
export const tokenFromCookie = (v: string | undefined): string | undefined => (validToken(v) ? v : undefined)

/** `[/<channel prefix>]/preview/<collection>/<id>`: the URL the channels' preview patterns point at for items with no route yet. */
export function matchPreviewPath(pathname: string, base: string): { collection: string; id: string } | undefined {
  const at = pathname.lastIndexOf(base + '/')
  const m = at >= 0 ? pathname.slice(at + base.length + 1).split('/') : []
  if (m.length !== 2 || !m[0] || !m[1]) return undefined
  try {
    return { collection: decodeURIComponent(m[0]), id: decodeURIComponent(m[1]) }
  } catch {
    return undefined
  }
}

/** SHA-256 hex of the preview token: what `<meta name="hydrogen-preview">` carries (never the token). */
export async function previewHash(token: string): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest('SHA-256', enc.encode(token)))
  return Array.from(d, (b) => b.toString(16).padStart(2, '0')).join('')
}

// ---- URLs and channel matching ---------------------------------------------------------------------------------------

/**
 * The public origin of a request: `origin` when given (dev, or a frontend served from another host than its
 * channel), else the plain Host (https by default), or the forwarded host and protocol when `trustForwarded` is set
 * (only behind a proxy that overwrites them). Only the host name
 * matters to channel matching; ports are ignored.
 */
export function originOf(h: HeaderReader, origin?: string, trustForwarded = false): string {
  if (origin) return origin.replace(/\/+$/, '')
  const first = (v: string | null) => v?.split(',')[0]?.trim()
  const host = (trustForwarded && first(h.get('x-forwarded-host'))) || first(h.get('host')) || 'localhost'
  return `${(trustForwarded && first(h.get('x-forwarded-proto'))) || 'https'}://${host}`
}

/** The URL the delivery `/route` is asked about: the public origin plus the request's (still encoded) path. */
export const publicUrl = (h: HeaderReader, pathname: string, origin?: string, trustForwarded = false): string =>
  originOf(h, origin, trustForwarded) + (pathname || '/')

/** The request headers that header matchers of the channels (`options.match.header.name`) look at, to forward to the CMS. */
export function matcherHeaders(all: { options: { match?: { header?: { name?: string } } } }[], h: HeaderReader): Record<string, string> | undefined {
  let out: Record<string, string> | undefined
  for (const c of all) {
    const n = c.options.match?.header?.name
    const v = n ? h.get(n) : null
    if (n && v !== null) (out ??= {})[n] = v
  }
  return out
}

// ---- tagged response cache -------------------------------------------------------------------------------------------

export interface TagCacheOptions {
  /** seconds an entry is fresh when no webhook purges it (default 3600) */
  ttl?: number
  /** entries kept, least recently used first out (default 500) */
  max?: number
  /** the fetch behind the cache (default the global one) */
  fetch?: typeof fetch
  /** milliseconds a request may take (default no limit) */
  timeoutMs?: number
  /** the clock, in milliseconds (default Date.now) */
  now?: () => number
}

export interface TagCache {
  fetch: typeof fetch
  /** drops the entries carrying any of the tags (`*`: all); returns how many */
  purge(tags: readonly string[]): number
  size(): number
}

interface Entry { status: number; statusText: string; headers: [string, string][]; body: string; exp: number; tags: string[] }

const MAX_TAGS = 128
const MAX_MISSES = 100

const pairs = (h: Headers): [string, string][] => {
  const out: [string, string][] = []
  h.forEach((v, k) => out.push([k, v]))
  return out
}

/**
 * A small in-memory cache of delivery responses, keyed by request (URL and headers except Accept) and tagged with
 * the response's `Surrogate-Key` plus `*`. It wraps `fetch`, so every client call is cached without the pages
 * knowing. A webhook's tags purge exactly the entries carrying them. Per process: several instances each need the
 * webhook (or wait for `ttl`).
 *
 * Never stored: non-GET requests, preview requests (`X-Hydrogen-Preview`, `?preview=`) and 5xx answers; only 200 and
 * 404 are kept (a 404 for 30 s (at most 100 of them), a tagless 200 such as channel info for at most 5 min). Concurrent misses share one
 * request, a response that started before a purge is not stored, and a CMS that is down is answered from an
 * expired entry when there is one.
 */
export function createTagCache(o: TagCacheOptions = {}): TagCache {
  const ttl = o.ttl ?? 3600
  const max = o.max ?? 500
  const real = o.fetch ?? ((u, i) => fetch(u, i))
  const now = o.now ?? Date.now
  const entries = new Map<string, Entry>()
  const byTag = new Map<string, Set<string>>()
  const inflight = new Map<string, Promise<Entry>>()
  // 404s (random paths, unknown hosts) get their own small quota so they cannot push hot 200s out
  const misses = new Set<string>()
  let gen = 0

  const drop = (key: string) => {
    const e = entries.get(key)
    if (!e) return
    entries.delete(key)
    misses.delete(key)
    for (const t of e.tags) {
      const s = byTag.get(t)
      s?.delete(key)
      if (s && !s.size) byTag.delete(t)
    }
  }
  const store = (key: string, e: Entry) => {
    drop(key)
    if (e.status === 404) while (misses.size >= MAX_MISSES) drop(misses.values().next().value!)
    while (entries.size >= max) drop(entries.keys().next().value!)
    entries.set(key, e)
    if (e.status === 404) misses.add(key)
    for (const t of e.tags) (byTag.get(t) ?? byTag.set(t, new Set()).get(t)!).add(key)
  }
  const respond = (e: Entry) => new Response(e.status === 204 ? null : e.body, { status: e.status, statusText: e.statusText, headers: e.headers })

  async function load(key: string, url: string, init: RequestInit | undefined, stale: Entry | undefined): Promise<Entry> {
    const started = gen
    let res: Response
    try {
      res = await real(url, init)
    } catch (e) {
      if (stale) return stale
      throw e
    }
    if (res.status >= 500 && stale) {
      void res.body?.cancel().catch(() => {})
      return stale
    }
    const e: Entry = {
      status: res.status,
      statusText: res.statusText,
      headers: pairs(res.headers).filter(([k]) => !/^(content-(encoding|length)|transfer-encoding|connection|keep-alive)$/i.test(k)),
      body: await res.text(),
      exp: 0,
      tags: [],
    }
    const own = parseTags(res.headers.get('Surrogate-Key')).filter(validTag)
    const secs = res.status === 200 ? (own.length ? ttl : Math.min(ttl, 300)) : res.status === 404 ? Math.min(ttl, 30) : 0
    if (secs > 0 && started === gen) {
      e.exp = now() + secs * 1000
      e.tags = [ALL_TAG, ...own.filter((t) => t !== ALL_TAG)].slice(0, MAX_TAGS)
      store(key, e)
    }
    return e
  }

  return {
    size: () => entries.size,
    purge(tags) {
      gen++
      const keys = new Set<string>()
      for (const t of tags) for (const k of byTag.get(t) ?? []) keys.add(k)
      for (const k of keys) drop(k)
      return keys.size
    },
    async fetch(input, init) {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const headers = new Headers(init?.headers ?? (typeof input === 'object' && 'headers' in input ? input.headers : undefined))
      const method = init?.method ?? (typeof input === 'object' && 'method' in input ? input.method : 'GET')
      if (method !== 'GET' || headers.has('x-hydrogen-preview') || /[?&]preview=/.test(url)) return real(input, init)
      const key = url + '\n' + pairs(headers).filter(([k]) => k !== 'accept').sort().join('\n')
      const hit = entries.get(key)
      if (hit && hit.exp > now()) {
        entries.delete(key)
        entries.set(key, hit)
        return respond(hit)
      }
      let p = inflight.get(key)
      if (!p) {
        // The shared request must not die with the first caller's abort signal.
        p = load(key, url, { ...init, headers, signal: o.timeoutMs ? AbortSignal.timeout(o.timeoutMs) : undefined }, hit).finally(() => inflight.delete(key))
        inflight.set(key, p)
      }
      return respond(await p)
    },
  }
}
