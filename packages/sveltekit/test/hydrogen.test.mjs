import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { isHttpError, isRedirect } from '@sveltejs/kit'
import { createHydrogen } from '../dist/index.js'

const BASE = 'http://cms.test'
const web = (name, match) => ({ name, languages: ['en'], collections: ['page'], options: { site: 's', match, reserved: ['/blog/*'] } })
const CHANNELS = { en: web('en', { host: 'www.acme.com', prefix: '/en' }), de: web('de', { host: 'www.acme.com', prefix: '/de' }) }
const json = (status, body, headers = {}) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } })
const item = { id: '1', type: 'page', name: 'About', locale: 'en', fields: { title: 'About' } }

let calls, route
beforeEach(() => {
  calls = []
  route = () => json(200, { located: { kind: 'page', locale: 'en', canonical: 'https://www.acme.com/en/about', alternates: [{ channel: 'de', locale: 'de', url: 'https://www.acme.com/de/about' }] }, item }, { 'surrogate-key': 'i:1 c:page' })
  globalThis.fetch = async (url, init) => {
    const u = new URL(String(url))
    calls.push({ path: u.pathname, search: u.search, headers: Object.fromEntries(new Headers(init?.headers)) })
    const m = /\/channels\/(\w+)$/.exec(u.pathname)
    if (m) return CHANNELS[m[1]] ? json(200, CHANNELS[m[1]]) : json(404, { error: { code: 'not_found', message: 'no' } })
    if (u.pathname.endsWith('/route')) return route(u, init)
    return json(404, {})
  }
})
const routeCalls = () => calls.filter((c) => c.path.endsWith('/route'))

let n = 0
const hy = (o = {}) => createHydrogen({ baseUrl: `${BASE}${++n}`, channels: ['en', 'de'], templates: ['page'], revalidateSecret: 'a-test-secret-of-16+', ...o })

function event(path, { cookie, headers = {}, method = 'GET' } = {}) {
  const url = new URL('http://localhost:3000' + path)
  const set = []
  return {
    url,
    request: new Request(url, { method, headers: { host: 'www.acme.com', ...headers } }),
    cookies: { get: (k) => (k === 'hydrogen-preview' ? cookie : undefined), set: (...a) => set.push(['set', ...a]), delete: (...a) => set.push(['delete', ...a]) },
    locals: {},
    setHeaders: (h) => set.push(['headers', h]),
    set,
  }
}
const through = async (h, e) => {
  const res = await h.handle({ event: e, resolve: async () => new Response('ok') })
  return res
}
const thrown = async (p) => {
  try {
    await p
  } catch (e) {
    return e
  }
  assert.fail('did not throw')
}

test('?preview=<token> is handed to the preview route without the parameter, with no-store', async () => {
  const h = hy()
  const res = await through(h, event('/en/about?preview=tok.en&x=1'))
  assert.equal(res.status, 307)
  const loc = new URL(res.headers.get('location'), 'http://x')
  assert.equal(loc.pathname, '/api/hydrogen/preview')
  assert.equal(loc.searchParams.get('token'), 'tok.en')
  assert.equal(loc.searchParams.get('path'), '/en/about?x=1')
  assert.match(res.headers.get('cache-control'), /no-store/)
  const bad = await through(h, event('/en/about?preview=has%20space'))
  assert.equal(new URL(bad.headers.get('location'), 'http://x').searchParams.get('token'), '')
})

test('a page resolves to its template data, locale and metadata', async () => {
  const h = hy()
  const e = event('/en/about')
  await through(h, e)
  const d = await h.load(e)
  assert.equal(d.template, 'page')
  assert.equal(d.item.id, '1')
  assert.equal(d.preview, false)
  assert.equal(d.metadata.canonical, 'https://www.acme.com/en/about')
  assert.deepEqual(d.metadata.alternates, [{ hreflang: 'de', href: 'https://www.acme.com/de/about' }])
  assert.match(routeCalls()[0].search, /channel=en/)
})

test('a repeated view comes from the tagged cache until a webhook purges it', async () => {
  const h = hy()
  const view = async () => {
    const e = event('/en/about')
    await through(h, e)
    return h.load(e)
  }
  await view()
  await view()
  assert.equal(routeCalls().length, 1)
  assert.equal(h.purge(['i:1']), 1)
  await view()
  assert.equal(routeCalls().length, 2)
})

test('unknown pages, unknown prefixes and the CMS 404 are 404s', async () => {
  const h = hy()
  route = () => json(404, { error: { code: 'not_found', message: 'no' } })
  for (const path of ['/en/nope', '/zz/about']) {
    const e = event(path)
    await through(h, e)
    const err = await thrown(h.load(e))
    assert.ok(isHttpError(err) && err.status === 404, path)
  }
})

test('reserved paths are never answered from the CMS', async () => {
  const h = hy()
  const e = event('/en/blog/anything')
  await through(h, e)
  const err = await thrown(h.load(e))
  assert.equal(err.status, 404)
  assert.equal(routeCalls().length, 0)
})

test('redirects keep the CMS status and the query', async () => {
  const h = hy()
  route = () => json(200, { located: { kind: 'redirect', resolution: { status: 301, location: '/en/new' } } })
  const e = event('/en/old?a=1')
  await through(h, e)
  const err = await thrown(h.load(e))
  assert.ok(isRedirect(err))
  assert.deepEqual([err.status, err.location], [301, '/en/new?a=1'])
})

test('the channel is chosen by host and prefix', async () => {
  const h = hy()
  const e = event('/de/ueber')
  await through(h, e)
  assert.equal(await h.channel(e), 'de')
  assert.match(routeCalls().length ? '' : 'x', /x/)
  await h.load(e)
  assert.match(routeCalls()[0].search, /channel=de/)
  const other = event('/de/ueber', { headers: { host: 'other.test' } })
  await through(h, other)
  assert.equal(await other.locals.hydrogen.channel(), undefined)
  assert.equal(await h.channel(other), 'en')
})

test('preview: the cookie token is sent uncached, the page is a draft with the proof', async () => {
  const h = hy()
  const e = event('/en/about', { cookie: 'tok.en' })
  await through(h, e)
  const d = await h.load(e)
  assert.equal(d.preview, true)
  assert.match(d.previewHash, /^[0-9a-f]{64}$/)
  assert.equal(routeCalls().at(-1).headers['x-hydrogen-preview'], 'tok.en')
  assert.ok(e.set.some((s) => s[0] === 'headers' && /no-store/.test(s[1]['cache-control'])))
  const e2 = event('/en/about', { cookie: 'tok.en' })
  await through(h, e2)
  await h.load(e2)
  assert.equal(routeCalls().filter((c) => c.headers['x-hydrogen-preview']).length, 2)
})

test('a refused preview token leaves preview through the exit route', async () => {
  const h = hy()
  route = () => json(401, { error: { code: 'unauthorized', message: 'no' } })
  const e = event('/en/about', { cookie: 'tok.en' })
  await through(h, e)
  const err = await thrown(h.load(e))
  assert.ok(isRedirect(err))
  assert.equal(err.status, 307)
  assert.equal(err.location, '/api/hydrogen/preview?exit=1&path=%2Fen%2Fabout')
})

test('an item without a route yet is shown at the preview path with the token only', async () => {
  const h = hy()
  globalThis.fetch = ((orig) => async (url, init) => {
    const u = new URL(String(url))
    if (u.pathname.endsWith('/items/abc')) return json(200, { item: { ...item, id: 'abc', type: 'page' } })
    return orig(url, init)
  })(globalThis.fetch)
  const e = event('/en/preview/page/abc', { cookie: 'tok.en' })
  await through(h, e)
  assert.equal((await h.load(e)).item.id, 'abc')
  route = () => json(404, { error: { code: 'not_found', message: 'no' } })
  const anon = event('/en/preview/page/abc')
  await through(h, anon)
  assert.equal((await thrown(h.load(anon))).status, 404)
})

test('a CMS that is down is a 502, not a 404', async () => {
  const h = hy()
  route = () => json(500, { error: { code: 'internal', message: 'boom' } })
  const e = event('/en/about')
  await through(h, e)
  const err = await thrown(h.load(e))
  assert.equal(err.status, 502)
})

test('preview route: sets an httpOnly cookie and redirects to the path; exit clears it', async () => {
  const h = hy()
  const e = event('/api/hydrogen/preview?token=tok.en&path=/en/about')
  const res = await h.previewRoute(e)
  assert.equal(res.status, 307)
  assert.equal(res.headers.get('location'), '/en/about')
  assert.deepEqual(e.set[0].slice(0, 3), ['set', 'hydrogen-preview', 'tok.en'])
  assert.equal(e.set[0][3].httpOnly, true)
  const out = event('/api/hydrogen/preview?exit=1&path=/en')
  await h.previewRoute(out)
  assert.equal(out.set[0][0], 'delete')
  assert.equal((await h.previewRoute(event('/api/hydrogen/preview?token=x&path=//evil.test'))).status, 400)
  assert.equal((await h.previewRoute(event('/api/hydrogen/preview?path=/en'))).status, 400)
})

const sign = (secret, body, t = Math.floor(Date.now() / 1000)) => `t=${t},v1=${createHmac('sha256', secret).update(`${t}.`).update(body).digest('hex')}`

test('revalidate verifies the signature and purges the event tags', async () => {
  const h = hy()
  const e = event('/en/about')
  await through(h, e)
  await h.load(e)
  const body = JSON.stringify({ topic: 'item.published', tags: ['i:1'] })
  const post = (headers, b = body) => h.revalidate({ request: new Request('http://x/api/hydrogen/revalidate', { method: 'POST', body: b, headers }) })
  assert.equal((await post({})).status, 401)
  assert.equal((await post({ 'hydrogen-signature': sign('another-secret-0123456', body) })).status, 401)
  assert.equal((await post({ 'hydrogen-signature': sign('a-test-secret-of-16+', 'x') })).status, 401)
  const ok = await post({ 'hydrogen-signature': sign('a-test-secret-of-16+', body) })
  assert.equal(ok.status, 200)
  assert.deepEqual(await ok.json(), { revalidated: ['i:1'] })
  const e2 = event('/en/about')
  await through(h, e2)
  await h.load(e2)
  assert.equal(routeCalls().length, 2)
  const bad = 'not json'
  assert.equal((await post({ 'hydrogen-signature': sign('a-test-secret-of-16+', bad) }, bad)).status, 400)
})

test('revalidate needs a secret of 16+ characters', async () => {
  const res = await hy({ revalidateSecret: 'short' }).revalidate({ request: new Request('http://x', { method: 'POST', body: '{}' }) })
  assert.equal(res.status, 500)
})
