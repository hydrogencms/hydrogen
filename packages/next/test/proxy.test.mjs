import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { NextRequest } from 'next/server.js'
import { createProxy } from '../dist/proxy.js'
import { readCapped } from '../dist/signature.js'

const BASE = 'http://cms.test'
const web = (name, match, extra = {}) => ({ name, languages: ['en'], collections: ['page'], options: { site: 's', match, ...extra } })
const CHANNELS = {
  en: web('en', { host: 'www.acme.com', prefix: '/en' }),
  de: web('de', { host: 'www.acme.com', prefix: '/de' }),
  hdr: web('hdr', { host: 'hdr.acme.com', header: { name: 'x-site', value: 'a' } }),
}

let calls, route
const page = { kind: 'page' }
const located = (loc, item) => ({ located: loc, item })
const json = (status, body, headers = {}) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } })

beforeEach(() => {
  calls = []
  route = () => json(200, located({ kind: 'page', locale: 'en', resolution: { kind: 'page' } }, { id: '1', type: 'page', name: 'X', locale: 'en', fields: {} }))
  globalThis.fetch = async (url, init) => {
    const u = new URL(String(url))
    calls.push({ path: u.pathname, search: u.search, headers: Object.fromEntries(new Headers(init?.headers)) })
    const m = /\/channels\/(\w+)$/.exec(u.pathname)
    if (m) return CHANNELS[m[1]] ? json(200, CHANNELS[m[1]]) : json(404, { error: { code: 'not_found', message: 'no' } })
    if (u.pathname.endsWith('/route')) return route(u, init)
    return json(404, {})
  }
})

let n = 0
// Each test gets its own base URL, so the module-level channel-info memo starts empty.
const proxy = (o = {}) => createProxy({ baseUrl: `${BASE}${++n}`, channels: ['en', 'de'], templates: ['page'], ...o })
const req = (path, headers = {}, cookie) =>
  new NextRequest(`http://localhost:3000${path}`, { headers: { host: 'www.acme.com', ...headers, ...(cookie ? { cookie } : {}) } })
const forwarded = (res, name) => res.headers.get(`x-middleware-request-${name}`)
const routeCalls = () => calls.filter((c) => c.path.endsWith('/route'))

test('passes a page through with the path header and strips what the client sent', async () => {
  const res = await proxy()(req('/en/about', { 'x-hydrogen-path': '/de/forged', 'x-hydrogen-url': 'https://evil/', 'x-hydrogen-channel': 'de' }))
  assert.equal(res.status, 200)
  assert.equal(forwarded(res, 'x-hydrogen-path'), '/en/about')
})

test('skipped paths are stripped too', async () => {
  const res = await proxy()(req('/api/x', { 'x-hydrogen-path': '/de/forged' }))
  assert.equal(forwarded(res, 'x-hydrogen-path'), '/api/x')
  assert.equal(routeCalls().length, 0)
})

test('asks once per view after warm-up, with the matched channel', async () => {
  const p = proxy()
  await p(req('/de/x'))
  const before = calls.length
  await p(req('/de/x'))
  assert.equal(calls.length - before, 1)
  assert.match(routeCalls().at(-1).search, /channel=de/)
})

test('unmatched URLs are a 404 rewrite', async () => {
  const res = await proxy()(req('/fr/x'))
  assert.equal(res.status, 404)
  assert.match(res.headers.get('x-middleware-rewrite'), /_not-found/)
})

test('the CMS rejecting the URL (400) is a 404', async () => {
  route = () => json(400, { error: { code: 'bad_request', message: 'url must be valid text' } })
  assert.equal((await proxy()(req('/en/x'))).status, 404)
})

test('a CMS error or timeout lets the request through', async () => {
  route = () => json(503, { error: { code: 'unavailable', message: 'down' } })
  assert.equal(forwarded(await proxy()(req('/en/x')), 'x-hydrogen-path'), '/en/x')
  route = () => { throw new TypeError('fetch failed') }
  assert.equal(forwarded(await proxy()(req('/en/x')), 'x-hydrogen-path'), '/en/x')
  route = (u, init) => new Promise((_, rej) => init.signal.addEventListener('abort', () => rej(init.signal.reason)))
  const t = Date.now()
  const keep = setInterval(() => {}, 10) // AbortSignal.timeout timers are unref'd
  const res = await proxy({ timeoutMs: 50 })(req('/en/x'))
  clearInterval(keep)
  assert.equal(forwarded(res, 'x-hydrogen-path'), '/en/x')
  assert.ok(Date.now() - t < 2000)
})

test('a header-matched channel gets the header forwarded and is found', async () => {
  const p = proxy({ channels: ['en', 'hdr'] })
  const res = await p(req('/x', { host: 'hdr.acme.com', 'x-site': 'a' }))
  assert.equal(res.status, 200)
  const c = routeCalls().at(-1)
  assert.match(c.search, /channel=hdr/)
  assert.equal(c.headers['x-site'], 'a')
  assert.equal((await p(req('/x', { host: 'hdr.acme.com', 'x-site': 'b' }))).status, 404)
})

test('redirects keep the status and the query string', async () => {
  route = () => json(200, { located: { kind: 'redirect', resolution: { kind: 'redirect', status: 301, location: '/en/new' } } })
  const res = await proxy()(req('/en/old?utm=1&a=b'))
  assert.equal(res.status, 301)
  assert.equal(new URL(res.headers.get('location')).pathname + new URL(res.headers.get('location')).search, '/en/new?utm=1&a=b')
  route = () => json(200, { located: { kind: 'redirect', resolution: { kind: 'redirect', status: 308, location: '/en/new?x=2' } } })
  const r2 = await proxy()(req('/en/old?utm=1'))
  assert.equal(new URL(r2.headers.get('location')).search, '?x=2')
})

test('a preview cookie is sent as the token; a refused one is ignored', async () => {
  const tok = 'abc.def'
  let seen
  route = (u, init) => {
    seen = new Headers(init.headers).get('x-hydrogen-preview')
    return seen ? json(401, { error: { code: 'unauthorized', message: 'bad token' } }) : json(200, { located: { kind: 'not_found' } })
  }
  const res = await proxy()(req('/en/x', {}, `hydrogen-preview=${tok}`))
  assert.equal(res.status, 404, 'forged cookie does not skip the 404')
  const withTok = routeCalls().filter((c) => c.headers['x-hydrogen-preview'])
  assert.equal(withTok.length, 1)
  assert.equal(routeCalls().length, 2, 'retried without the token')
})

test('preview URLs of unrouted items go to the page', async () => {
  const res = await proxy()(req('/preview/page/abc', {}, 'hydrogen-preview=abc.def'))
  assert.equal(res.status, 200)
  assert.equal(routeCalls().length, 0)
})

test('?preview=<token> goes to the preview route with the rest of the URL', async () => {
  const res = await proxy()(req('/en/about?a=1&preview=abc.def&b=2'))
  assert.equal(res.status, 307)
  const to = new URL(res.headers.get('location'))
  assert.equal(to.pathname, '/api/hydrogen/preview')
  assert.equal(to.searchParams.get('token'), 'abc.def')
  assert.equal(to.searchParams.get('path'), '/en/about?a=1&b=2')
  assert.equal(calls.length, 0, 'no CMS call before the redirect')
  const r2 = await proxy({ previewRoutePath: '/p' })(req('/preview/page/x?preview=abc.def'))
  const t2 = new URL(r2.headers.get('location'))
  assert.equal(t2.pathname, '/p')
  assert.equal(t2.searchParams.get('path'), '/preview/page/x')
})

test('?preview= cannot smuggle a redirect target or a bad token', async () => {
  // The path is only ever the request's own pathname; the preview route refuses "//host" paths and bad tokens.
  const r = await proxy()(req('//evil.example/x?preview=abc.def'))
  const to = new URL(r.headers.get('location'))
  assert.equal(to.origin, 'http://localhost:3000')
  const bad = await proxy()(req('/en/x?preview=has space'))
  assert.equal(new URL(bad.headers.get('location')).searchParams.get('token'), '')
})

test('readCapped: Content-Length first, then a running count', async () => {
  const stream = (n) => new ReadableStream({ start(c) { c.enqueue(new Uint8Array(n)); c.enqueue(new Uint8Array(n)); c.close() } })
  assert.equal(await readCapped({ headers: new Headers({ 'content-length': '999' }), body: stream(1) }, 100), undefined)
  assert.equal(await readCapped({ headers: new Headers(), body: stream(60) }, 100), undefined)
  assert.equal((await readCapped({ headers: new Headers(), body: stream(40) }, 100)).length, 80)
  assert.equal((await readCapped({ headers: new Headers(), body: null }, 100)).length, 0)
})
