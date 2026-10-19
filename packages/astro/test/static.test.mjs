import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { createHydrogen } from '../dist/runtime.js'
import hydrogenIntegration from '../dist/index.js'

const SITE = 'https://www.acme.com'
const json = (status, body, headers = {}) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } })
const web = (name, prefix, languages, extra = {}) => ({ name, languages, collections: ['page', 'post', 'settings'], options: { site: 's', match: { host: 'www.acme.com', prefix }, ...extra } })
const CHANNELS = { en: web('en', '/en', ['en', 'de'], { reserved: ['/blog/*'] }), shop: web('shop', '/shop', ['en']) }
const it = (id, url) => ({ id, type: 'page', name: id, locale: 'en', fields: {}, url })

let calls, lists
beforeEach(() => {
  calls = []
  lists = {}
  globalThis.fetch = async (url) => {
    const u = new URL(String(url))
    calls.push(u.pathname + u.search)
    const ch = /\/channels\/(\w+)$/.exec(u.pathname)
    if (ch) return json(200, CHANNELS[ch[1]])
    const col = /\/collections\/(\w+)\/items$/.exec(u.pathname)
    if (col) {
      const q = u.searchParams
      const all = lists[`${q.get('channel')}/${col[1]}/${q.get('locale')}`]
      if (col[1] === 'settings') return json(400, { error: { code: 'bad_request', message: 'singleton' } })
      const offset = Number(q.get('offset') ?? 0), limit = Number(q.get('limit'))
      const items = (all ?? []).slice(offset, offset + limit)
      return json(200, { items, hasMore: offset + limit < (all ?? []).length })
    }
    if (u.pathname.endsWith('/route')) {
      return json(200, { located: { kind: 'page', locale: 'en', canonical: SITE + '/en' }, item: it('1', SITE + '/en') })
    }
    return json(404, {})
  }
})

let n = 0
const make = (o = {}) => createHydrogen({ baseUrl: `http://cms.static${++n}`, channels: ['en', 'shop'], templates: ['page'], ...o })
const paths = (r) => r.map((e) => e.params.path)

test('staticPaths lists every collection and language, drops reserved and foreign URLs, and dedupes', async () => {
  lists['en/page/en'] = [it('a', SITE + '/en'), it('b', SITE + '/en/about'), it('c', SITE + '/en/about/'), it('d', SITE + '/en/blog/post'), it('e', 'https://other.test/en/x'), it('f', SITE + '/shop/cart'), { ...it('g'), url: undefined }]
  lists['en/page/de'] = [it('h', SITE + '/en/ueber')]
  lists['en/post/en'] = [it('i', SITE + '/en/news/%C3%BC')]
  lists['shop/page/en'] = [it('j', SITE + '/shop/cart'), it('k', SITE + '/shop')]
  const r = await make().staticPaths()
  assert.deepEqual(paths(r), ['en', 'en/about', 'en/news/ü', 'en/ueber', 'shop', 'shop/cart'])
  assert.deepEqual(Object.keys(r[0].params), ['path'])
})

test('staticPaths: the root is undefined, the param can be renamed, listings are paged', async () => {
  lists['en/page/en'] = Array.from({ length: 1001 }, (_, i) => it(String(i), SITE + (i ? `/p${i}` : '/')))
  CHANNELS.root = { name: 'root', languages: ['en'], collections: ['page'], options: { match: { host: 'www.acme.com' } } }
  lists['root/page/en'] = lists['en/page/en']
  const h = make({ channels: ['root'] })
  const r = await h.staticPaths({ param: 'slug' })
  delete CHANNELS.root
  assert.equal(r.length, 1001)
  assert.deepEqual(r[0], { params: { slug: undefined } })
  const pages = calls.filter((c) => c.includes('/collections/page/items'))
  assert.deepEqual(pages.map((c) => new URL('http://x' + c).searchParams.get('offset')), ['0', '500', '1000'])
  assert.ok(pages.every((c) => c.includes('limit=500')))
})

test('staticPaths fails with a clear message when a collection cannot be listed', async () => {
  globalThis.fetch = async (url) =>
    /\/channels\//.test(url) ? json(200, CHANNELS.shop) : json(500, { error: { code: 'internal', message: 'boom' } })
  await assert.rejects(make({ channels: ['shop'] }).staticPaths(), /cannot list page \(en\) of channel shop/)
})

const run = async (h, path, ctx = {}) => {
  const touched = []
  const cookies = new Proxy({}, { get: (_, k) => (touched.push(k), () => undefined) })
  const c = { request: new Request('http://localhost:4321' + path), cookies, locals: {}, isPrerendered: true, ...ctx }
  const nexts = []
  const res = await h.middleware(c, async (rw) => (nexts.push(rw), new Response(rw ? 'nf' : 'page')))
  return { res, nexts, touched, locals: c.locals.hydrogen }
}
const routeCalls = () => calls.filter((c) => c.includes('/route'))

test('prerender: no cookies, the base is stripped and the origin comes from site', async () => {
  const h = make()
  h.configure({ base: '/sub/', site: SITE })
  const { res, touched, locals } = await run(h, '/sub/en/about/?preview=x', { site: new URL(SITE) })
  assert.equal(res.status, 200)
  assert.deepEqual(touched, [])
  assert.equal(locals.draft, false)
  assert.equal(locals.page.template, 'page')
  assert.equal(locals.channel, 'en')
  assert.match(decodeURIComponent(routeCalls()[0]), /url=https:\/\/www\.acme\.com\/en\/about(&|$)/)
})

test('prerender: the origin option wins over site; the 404 page is not resolved', async () => {
  const h = make({ origin: 'https://www.acme.com' })
  h.configure({ base: '/sub', site: 'https://elsewhere.test' })
  await run(h, '/sub/en')
  assert.match(decodeURIComponent(routeCalls()[0]), /url=https:\/\/www\.acme\.com\/en(&|$)/)
  const before = calls.length
  const r = await run(h, '/sub/404')
  assert.deepEqual(r.nexts, [undefined])
  assert.equal(calls.length, before)
  assert.ok(r.locals)
})

test('prerender: unknown paths are rewritten to the 404 page; reserved paths pass', async () => {
  const h = make({ origin: SITE })
  h.configure({ base: '/sub' })
  const miss = await run(h, '/sub/fr/x')
  assert.equal(miss.res.status, 404)
  assert.deepEqual(miss.nexts, ['/404'])
  const reserved = await run(h, '/sub/en/blog/x')
  assert.equal(reserved.res.status, 200)
  assert.equal(reserved.locals.page, undefined)
})

test('prerender: a redirect keeps the status and carries the base', async () => {
  const h = make({ origin: SITE })
  h.configure({ base: '/sub' })
  const route = globalThis.fetch
  globalThis.fetch = async (url, init) =>
    String(url).includes('/route') ? json(200, { located: { kind: 'redirect', resolution: { status: 301, location: '/en/new' } } }) : route(url, init)
  const { res } = await run(h, '/sub/en/old')
  assert.equal(res.status, 301)
  assert.equal(res.headers.get('location'), '/sub/en/new')
})

test('href prefixes the base on own URLs and leaves other origins alone', () => {
  const h = make()
  h.configure({ base: '/sub/', site: SITE })
  assert.equal(h.href('/en/about?a=1#x'), '/sub/en/about?a=1#x')
  assert.equal(h.href(SITE + '/en/about'), '/sub/en/about')
  assert.equal(h.href('/'), '/sub/')
  assert.equal(h.href('https://other.test/en'), 'https://other.test/en')
  assert.equal(h.href('mailto:a@b.c'), 'mailto:a@b.c')
  const plain = make()
  plain.configure({})
  assert.equal(plain.href('/en'), '/en')
})

const setup = (config) => {
  const out = { routes: [], middleware: [], vite: undefined }
  hydrogenIntegration().hooks['astro:config:setup']({
    config: { root: new URL('file:///proj/'), base: '/', ...config },
    updateConfig: (c) => (out.vite = c.vite),
    addMiddleware: (m) => out.middleware.push(m),
    injectRoute: (r) => out.routes.push(r.pattern),
  })
  return out
}

test('the integration injects the on-demand routes only with a server', () => {
  const ssr = setup({ output: 'server', adapter: { name: 'node' } })
  assert.deepEqual(ssr.routes, ['/api/hydrogen/preview', '/api/hydrogen/revalidate', '/__hydrogen/bridge.js'])
  const stat = setup({ output: 'static', base: '/sub', site: SITE })
  assert.deepEqual(stat.routes, [])
  assert.equal(stat.middleware.length, 1)
  const plugin = stat.vite.plugins[0]
  assert.equal(plugin.load('\0virtual:hydrogen/env'), 'export default {"base":"/sub","site":"https://www.acme.com"}')
  // static with an adapter (hybrid) is a server build
  assert.equal(setup({ output: 'static', adapter: {} }).routes.length, 3)
})

test('a frontend that carries the base: paths without it, links and redirects get it once', async () => {
  const h = make({ base: '/sub' })
  lists['en/page/en'] = [it('a', SITE + '/sub/en'), it('b', SITE + '/sub/en/about'), it('c', SITE + '/sub/en/blog/x')]
  assert.deepEqual(paths(await h.staticPaths()), ['en', 'en/about'])
  h.configure({ base: '/sub', site: SITE })
  assert.equal(h.href(SITE + '/sub/en/about'), '/sub/en/about')
  assert.equal(h.href('/sub/en'), '/sub/en')
  assert.equal(h.href('/subway'), '/sub/subway')
  const route = globalThis.fetch
  globalThis.fetch = async (url, init) =>
    String(url).includes('/route') ? json(200, { located: { kind: 'redirect', resolution: { status: 301, location: SITE + '/sub/en/new' } } }) : route(url, init)
  const { res } = await run(h, '/sub/en/old')
  assert.equal(res.headers.get('location'), '/sub/en/new')
})
