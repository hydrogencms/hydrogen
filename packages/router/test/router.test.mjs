import test from 'node:test'
import assert from 'node:assert/strict'
import { createClient } from '@hydrogencms/client'
import { createRouter, matchChannel, matchSite } from '../dist/index.js'

function setup({ route, info, templates = { page: 'page', article: 'article' }, ...ro } = {}) {
  const calls = []
  const fetch = async (url, init) => {
    const u = new URL(url)
    calls.push({ path: u.pathname.replace('/api/deliver/v1', ''), url, init })
    if (u.pathname.endsWith('/route')) {
      const r = typeof route === 'function' ? route(u, init) : route
      return new Response(JSON.stringify(r.body), { status: r.status ?? 200, headers: { 'Surrogate-Key': 'r:main i:1' } })
    }
    if (u.pathname.includes('/channels/')) {
      return new Response(JSON.stringify(info ?? { name: 'web', languages: ['en'], collections: [], options: {} }), { status: 200 })
    }
    throw new Error('unexpected ' + url)
  }
  const router = createRouter({ client: createClient({ baseUrl: 'https://x.test', channel: 'web', fetch }), templates, ...ro })
  return { router, calls, routes: () => calls.filter((c) => c.path === '/route') }
}

const item = { id: '1', type: 'article', locale: 'de', version: 1, publishedAt: 't', name: 'Hello', fields: {} }

test('page: one /route request, metadata, tags', async () => {
  const { router, routes } = setup({ route: { body: {
    located: { channel: 'web', kind: 'page', locale: 'de', canonical: 'https://a.test/de/hello',
      alternates: [{ channel: 'web_en', locale: 'en', url: 'https://a.test/en/hello' }, { channel: 'web', locale: 'de', url: 'https://a.test/de/hello' }] },
    item } } })
  const r = await router.resolve('https://a.test/de/hello', { preview: 'pv', headers: { 'X-App': '1' } })
  assert.equal(r.kind, 'page')
  assert.equal(r.template, 'article')
  assert.equal(r.locale, 'de')
  assert.equal(r.item.name, 'Hello')
  assert.deepEqual(r.tags, ['r:main', 'i:1'])
  assert.deepEqual(r.metadata, { title: 'Hello', canonical: 'https://a.test/de/hello', locale: 'de',
    alternates: [{ hreflang: 'en', href: 'https://a.test/en/hello' }, { hreflang: 'de', href: 'https://a.test/de/hello' }] })
  assert.equal(routes().length, 1)
  assert.equal(routes()[0].init.headers['X-Hydrogen-Preview'], 'pv')
  assert.equal(routes()[0].init.headers['X-App'], '1')
  assert.match(routes()[0].url, /route\?channel=web&url=https%3A%2F%2Fa\.test%2Fde%2Fhello$/)
})

test('mounted collection item resolves like a page; unknown type uses fallback or notFound', async () => {
  const body = { located: { kind: 'item', locale: 'en' }, item: { ...item, type: 'product', locale: 'en' } }
  let s = setup({ route: { body } })
  assert.equal((await s.router.resolve('/p')).kind, 'notFound')
  s = setup({ route: { body }, fallback: 'generic' })
  const r = await s.router.resolve('/p')
  assert.equal(r.kind, 'page')
  assert.equal(r.template, 'generic')
  assert.equal(r.metadata.canonical, undefined)
})

test('redirect', async () => {
  const { router } = setup({ route: { body: { located: { kind: 'redirect', resolution: { kind: 'redirect', status: 301, location: '/en/new' } } } } })
  assert.deepEqual(await router.resolve('/old'), { kind: 'redirect', location: '/en/new', status: 301, tags: ['r:main', 'i:1'] })
})

test('not found, no_channel, and 404 for another channel', async () => {
  let s = setup({ route: { body: { located: { kind: 'not_found' } } } })
  assert.deepEqual(await s.router.resolve('/x'), { kind: 'notFound', tags: ['r:main', 'i:1'] })
  s = setup({ route: { status: 404, body: { error: { code: 'not_found', message: 'the url is not served by this channel' } } } })
  assert.deepEqual(await s.router.resolve('/x'), { kind: 'notFound', tags: [] })
  s = setup({ route: { status: 500, body: { error: { code: 'internal', message: 'boom' } } } })
  await assert.rejects(s.router.resolve('/x'), { code: 'internal' })
})

test('reserved short-circuits locally; channel info is fetched once', async () => {
  const info = { name: 'web', languages: ['en'], collections: [], options: { reserved: ['/cart', '/account/*'], match: { prefix: '/en' } } }
  const { router, calls, routes } = setup({ info, reserved: ['/api/*'], route: { body: { located: { kind: 'not_found' } } } })
  for (const u of ['/en/cart', '/en/cart/', '/en/account', '/en/account/orders/1', 'https://h.test/en/api/x']) {
    assert.deepEqual(await router.resolve(u), { kind: 'reserved' }, u)
  }
  assert.equal(routes().length, 0)
  assert.equal(calls.filter((c) => c.path.startsWith('/channels/')).length, 1)
  assert.equal((await router.resolve('/en/carts')).kind, 'notFound')
  assert.equal((await router.resolve('/en/accounting')).kind, 'notFound')
  assert.equal((await router.resolve('/cart')).kind, 'notFound') // outside the prefix: the server decides
  assert.equal(routes().length, 3)
  assert.equal(calls.filter((c) => c.path.startsWith('/channels/')).length, 1)
})

test('server-side reserved kind maps to reserved', async () => {
  const { router } = setup({ route: { body: { located: { kind: 'reserved', channel: 'web' } } } })
  assert.deepEqual(await router.resolve('/zzz'), { kind: 'reserved' })
})

test('channel info failure does not break resolve', async () => {
  const fetch = async (url) => url.includes('/channels/') ? new Response('{"error":{"code":"unauthorized","message":"x"}}', { status: 401 })
    : new Response(JSON.stringify({ located: { kind: 'not_found' } }), { status: 200 })
  const router = createRouter({ client: createClient({ baseUrl: 'https://x.test', channel: 'web', fetch }), templates: {} })
  assert.equal((await router.resolve('/a')).kind, 'notFound')
})

test('templates() lists declared content types sorted', () => {
  const { router } = setup({ templates: { page: 'p', article: 'a', landing: 'l' } })
  assert.deepEqual(router.templates(), ['article', 'landing', 'page'])
})

// matchChannel mirrors modules/pages web_test.go (testIndex, TestMatchSpecificity, TestMatcherPrecedence, tie breaks)
const ch = (name, match) => ({ name, languages: ['en'], collections: [], options: match === undefined ? {} : { match } })
const idx = () => [
  ch('corp_en', { host: 'www.acme.com', prefix: '/en' }),
  ch('corp_de', { host: 'www.acme.com', prefix: '/de' }),
  ch('corp_dech', { host: 'www.acme.com', prefix: '/de-ch' }),
  ch('corp_host', { host: 'www.acme.com' }),
  ch('careers', { host: 'jobs.acme.com' }),
  ch('mobile', { prefix: '/m', header: { name: 'X-App', value: '1' } }),
  ch('anyprefix', { prefix: '/m' }),
  ch('ios'),
]

test('matchChannel specificity (TestMatchSpecificity)', () => {
  const app = new Headers({ 'X-App': '1' })
  const cases = [
    ['https://www.acme.com/en/about', undefined, 'corp_en', '/about'],
    ['https://WWW.ACME.COM:8443/en/about', undefined, 'corp_en', '/about'],
    ['https://www.acme.com/en', undefined, 'corp_en', '/'],
    ['https://www.acme.com/en/', undefined, 'corp_en', '/'],
    ['https://www.acme.com/english', undefined, 'corp_host', '/english'],
    ['https://www.acme.com/de-ch/x', undefined, 'corp_dech', '/x'],
    ['https://www.acme.com/de/x', undefined, 'corp_de', '/x'],
    ['https://www.acme.com/', undefined, 'corp_host', '/'],
    ['https://www.acme.com', undefined, 'corp_host', '/'],
    ['https://jobs.acme.com/roles?x=1', undefined, 'careers', '/roles'],
    ['https://other.com/m/page', undefined, 'anyprefix', '/page'],
    ['https://other.com/m/page', app, 'mobile', '/page'],
    ['/m/page', app, 'mobile', '/page'],
    ['https://other.com/x', undefined, undefined, undefined],
    ['/en/about', undefined, undefined, undefined],
  ]
  for (const [url, h, name, path] of cases) {
    const m = matchSite(url, idx(), h)
    assert.equal(m?.channel.name, name, url)
    assert.equal(m?.path, path, url)
    assert.equal(matchChannel(url, idx(), h)?.name, name, url)
  }
})

test('matchChannel header required (TestMatchHeaderRequired)', () => {
  assert.equal(matchChannel('https://other.com/m/page', idx())?.name, 'anyprefix')
  assert.equal(matchChannel('https://other.com/m/page', idx(), new Headers({ 'X-App': '2' }))?.name, 'anyprefix')
  assert.equal(matchChannel('https://other.com/m/page', idx(), { 'x-app': '1' })?.name, 'mobile')
})

test('matchChannel tie breaks (TestSpecificityTieBreaks)', () => {
  const x = [ch('b', { prefix: '/a' }), ch('a', { prefix: '/a/b' }), ch('c', { header: { name: 'X-A', value: '1' } }), ch('d', { header: { name: 'X-B', value: '1' } })]
  assert.deepEqual([matchSite('/a/b/c', x).channel.name, matchSite('/a/b/c', x).path], ['a', '/c'])
  assert.equal(matchChannel('/a/c', x)?.name, 'b')
  assert.equal(matchChannel('/z', x, new Headers({ 'X-A': '1', 'X-B': '1' }))?.name, 'c')
})

test('matchChannel precedence (TestMatcherPrecedence)', () => {
  const hdr = { name: 'X-A', value: '1' }
  const x = [
    ch('host', { host: 'a.test' }), ch('prefix', { prefix: '/long/prefix' }), ch('prefix_header', { prefix: '/long', header: hdr }),
    ch('host_prefix', { host: 'a.test', prefix: '/p' }), ch('header', { header: hdr }), ch('host_header', { host: 'a.test', header: hdr }),
  ]
  const h = new Headers({ 'X-A': '1' })
  for (const [host, path, hh, want] of [
    ['a.test', '/long/prefix/x', undefined, 'host'],
    ['a.test', '/long/x', h, 'host_header'],
    ['a.test', '/p/x', undefined, 'host_prefix'],
    ['a.test', '/p/x', h, 'host_prefix'],
    ['b.test', '/long/prefix/x', h, 'prefix_header'],
    ['b.test', '/z', h, 'header'],
    ['b.test', '/long/prefix/x', undefined, 'prefix'],
  ]) assert.equal(matchChannel(`https://${host}${path}`, x, hh)?.name, want, `${host}${path}`)
  const y = [ch('host', { host: 'a.test' }), ch('prefix_header', { prefix: '/m', header: hdr })]
  assert.equal(matchChannel('https://a.test/m/x', y, h)?.name, 'host')
})

test('matchSite flags paths that can never be site paths', () => {
  const x = [ch('a', {})]
  assert.equal(matchSite('/a%2Fb', x).bad, true)
  assert.equal(matchSite('/caf%C3%A9', x).path, '/café')
  assert.equal(matchSite('/%FF', x).bad, true)
  assert.equal(matchSite('/ok', x).bad, false)
})
