import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { createHydrogen } from '../dist/runtime.js'

const BASE = 'http://cms.test'
const web = (name, match, extra = {}) => ({ name, languages: ['en'], collections: ['page'], options: { site: 's', match, ...extra } })
const CHANNELS = { en: web('en', { host: 'www.acme.com', prefix: '/en' }, { reserved: ['/blog/*'] }), de: web('de', { host: 'www.acme.com', prefix: '/de' }) }
const json = (status, body, headers = {}) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } })
const item = { id: '1', type: 'page', name: 'Home', locale: 'en', fields: {} }

let calls, route
beforeEach(() => {
  calls = []
  route = () => json(200, { located: { kind: 'page', locale: 'en', canonical: 'http://www.acme.com/en', alternates: [{ channel: 'de', locale: 'de', url: 'http://www.acme.com/de' }] }, item }, { 'Surrogate-Key': 'i:1' })
  globalThis.fetch = async (url, init) => {
    const u = new URL(String(url))
    calls.push({ path: u.pathname, search: u.search, headers: Object.fromEntries(new Headers(init?.headers)) })
    const m = /\/channels\/(\w+)$/.exec(u.pathname)
    if (m) return CHANNELS[m[1]] ? json(200, CHANNELS[m[1]]) : json(404, { error: { code: 'not_found', message: 'no' } })
    if (u.pathname.endsWith('/route')) return route(u, init)
    if (/\/items\/\w+$/.test(u.pathname)) return new Headers(init.headers).get('x-hydrogen-preview') ? json(200, { item: { ...item, id: u.pathname.split('/').pop(), name: 'Draft' } }) : json(404, {})
    return json(404, {})
  }
})

let n = 0
const make = (o = {}) => createHydrogen({ baseUrl: `${BASE}${++n}`, channels: ['en', 'de'], templates: ['page'], ...o })
const run = async (h, path, { cookie, headers } = {}) => {
  const jar = new Map(cookie ? [['hydrogen-preview', cookie]] : [])
  const ctx = {
    request: new Request('http://localhost:4321' + path, { headers: { host: 'www.acme.com', ...headers } }),
    cookies: { get: (k) => (jar.has(k) ? { value: jar.get(k) } : undefined) },
    locals: {},
  }
  const nexts = []
  const res = await h.middleware(ctx, async (rw) => (nexts.push(rw), new Response(rw ? 'nf-page' : 'page', { status: 200 })))
  return { res, nexts, locals: ctx.locals.hydrogen }
}
const routeCalls = () => calls.filter((c) => c.path.endsWith('/route'))

test('a page passes with the resolved page in locals', async () => {
  const { res, nexts, locals } = await run(make(), '/en')
  assert.equal(res.status, 200)
  assert.deepEqual(nexts, [undefined])
  assert.equal(locals.channel, 'en')
  assert.equal(locals.page.template, 'page')
  assert.equal(locals.page.metadata.canonical, 'http://www.acme.com/en')
  assert.equal(locals.page.metadata.alternates[0].hreflang, 'de')
  assert.equal(locals.draft, false)
  assert.equal(res.headers.get('cache-control'), null)
})

test('the channel is matched by prefix, and the CMS is asked with the public URL', async () => {
  const h = make()
  await run(h, '/de/about?x=1')
  const r = routeCalls()[0]
  assert.match(r.search, /channel=de/)
  assert.match(decodeURIComponent(r.search), /url=https:\/\/www\.acme\.com\/de\/about\?x=1|url=https:\/\/www\.acme\.com\/de\/about/)
})

test('redirects keep the CMS status and the query', async () => {
  route = () => json(200, { located: { kind: 'redirect', resolution: { status: 301, location: '/en/new' } } })
  const { res } = await run(make(), '/en/old?a=1')
  assert.equal(res.status, 301)
  assert.equal(res.headers.get('location'), '/en/new?a=1')
  route = () => json(200, { located: { kind: 'redirect', resolution: { status: 302, location: '/en/new?b=2' } } })
  const r2 = await run(make(), '/en/old?a=1')
  assert.equal(r2.res.status, 302)
  assert.equal(r2.res.headers.get('location'), '/en/new?b=2')
  route = () => json(200, { located: { kind: 'redirect', resolution: { status: 301, location: 'https://other.test/x#f' } } })
  const r3 = await run(make(), '/en/old?a=1')
  assert.equal(r3.res.headers.get('location'), 'https://other.test/x?a=1#f')
})

test('unknown pages, unknown prefixes and bad paths answer a real 404 through the 404 page', async () => {
  route = () => json(404, { error: { code: 'not_found', message: 'x' } })
  const a = await run(make(), '/en/nope')
  assert.equal(a.res.status, 404)
  assert.deepEqual(a.nexts, ['/404'])
  assert.equal(await a.res.text(), 'nf-page')
  const b = await run(make(), '/fr/anything')
  assert.equal(b.res.status, 404)
  assert.equal(routeCalls().length, 1, 'no channel matched: the CMS is not asked about the route')
  route = () => json(400, { error: { code: 'bad_request', message: 'x' } })
  assert.equal((await run(make(), '/en/%00')).res.status, 404)
})

test('reserved paths pass to the frontend without a page', async () => {
  const { res, locals } = await run(make(), '/en/blog/hello')
  assert.equal(res.status, 200)
  assert.equal(locals.page, undefined)
  assert.equal(locals.channel, 'en')
  assert.equal(routeCalls().length, 0)
})

test('internals and /api are not asked about; a rewrite does not decide twice', async () => {
  const h = make()
  for (const p of ['/_astro/x.js', '/api/hydrogen/revalidate', '/__hydrogen/bridge.js']) {
    const { res, locals } = await run(h, p)
    assert.equal(res.status, 200)
    assert.equal(locals, undefined)
  }
  assert.equal(calls.length, 0)
})

test('?preview=<token> is handed to the preview route, keeping the other parameters', async () => {
  const { res } = await run(make(), '/en/about?preview=hps_abc&x=1')
  assert.equal(res.status, 307)
  const to = new URL(res.headers.get('location'), 'http://x')
  assert.equal(to.pathname, '/api/hydrogen/preview')
  assert.equal(to.searchParams.get('token'), 'hps_abc')
  assert.equal(to.searchParams.get('path'), '/en/about?x=1')
  assert.equal(calls.length, 0)
})

test('with a preview cookie the page is a draft: token to the CMS, proof hash, never cached', async () => {
  const { res, locals } = await run(make(), '/en', { cookie: 'tok.en' })
  assert.equal(locals.draft, true)
  assert.match(locals.previewHash, /^[0-9a-f]{64}$/)
  assert.equal(routeCalls()[0].headers['x-hydrogen-preview'], 'tok.en')
  assert.equal(res.headers.get('cache-control'), 'private, no-store')
  assert.match(res.headers.get('vary'), /Cookie/)
})

test('a refused token leaves preview and returns to the public page', async () => {
  route = () => json(401, { error: { code: 'unauthorized', message: 'x' } })
  const { res } = await run(make(), '/en/about?x=1', { cookie: 'old.tok' })
  assert.equal(res.status, 307)
  assert.equal(res.headers.get('location'), '/api/hydrogen/preview?exit=1&path=' + encodeURIComponent('/en/about?x=1'))
})

test('/preview/<collection>/<id> shows the item with the token only', async () => {
  const withToken = await run(make(), '/preview/post/abc', { cookie: 'tok.en' })
  assert.equal(withToken.locals.page.item.id, 'abc')
  assert.equal(withToken.locals.page.metadata.title, 'Draft')
  assert.equal(withToken.res.status, 200)
  route = () => json(404, {})
  const without = await run(make(), '/en/preview/post/abc')
  assert.equal(without.res.status, 404)
})

test('a CMS that is down passes the request as unavailable', async () => {
  globalThis.fetch = async () => {
    throw new Error('connect refused')
  }
  const { res, locals } = await run(make(), '/en')
  assert.equal(res.status, 200)
  assert.equal(locals.unavailable, true)
})

test('route answers are cached and a webhook purges them by tag', async () => {
  const h = make()
  await run(h, '/en')
  await run(h, '/en')
  assert.equal(routeCalls().length, 1)
  h.purge(['i:1'])
  await run(h, '/en')
  assert.equal(routeCalls().length, 2)
})

test('the preview route sets and clears the cookie, and refuses bad input', () => {
  const h = make()
  const set = []
  const ctx = (q) => ({ request: new Request('http://127.0.0.1:4321/api/hydrogen/preview?' + q), cookies: { set: (...a) => set.push(a) } })
  const ok = h.previewRoute(ctx('token=hps_abc&path=/en/about'))
  assert.equal(ok.status, 307)
  assert.equal(ok.headers.get('location'), '/en/about')
  assert.deepEqual(set[0], ['hydrogen-preview', 'hps_abc', { httpOnly: true, sameSite: 'lax', secure: false, path: '/', maxAge: 604800 }])
  h.previewRoute(ctx('exit=1&path=/en'))
  assert.deepEqual(set[1], ['hydrogen-preview', '', { path: '/', maxAge: 0 }])
  assert.equal(h.previewRoute(ctx('token=bad token&path=/en')).status, 400)
  assert.equal(h.previewRoute(ctx('token=a.b&path=//evil.test')).status, 400)
})

test('the webhook needs a long secret and a valid signature, then purges', async () => {
  const { createHmac } = await import('node:crypto')
  const secret = 'e2e-revalidate-0123456789abcdef'
  const h = make({ revalidateSecret: secret })
  await run(h, '/en')
  const post = (body, sig) => h.revalidate({ request: new Request('http://x/api/hydrogen/revalidate', { method: 'POST', body, headers: sig ? { 'Hydrogen-Signature': sig } : {} }) })
  const body = JSON.stringify({ tags: ['i:1'] })
  const t = Math.floor(Date.now() / 1000)
  assert.equal((await post(body)).status, 401)
  assert.equal((await post(body, `t=${t},v1=${'0'.repeat(64)}`)).status, 401)
  const ok = await post(body, `t=${t},v1=${createHmac('sha256', secret).update(`${t}.${body}`).digest('hex')}`)
  assert.equal(ok.status, 200)
  assert.deepEqual(await ok.json(), { revalidated: ['i:1'] })
  await run(h, '/en')
  assert.equal(routeCalls().length, 2)
  assert.equal((await make().revalidate({ request: new Request('http://x', { method: 'POST', body }) })).status, 500)
})
