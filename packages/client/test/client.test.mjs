import test from 'node:test'
import assert from 'node:assert/strict'
import { createClient, buildQuery, linkTo, HydrogenError, parseTags } from '../dist/index.js'

function mock(respond) {
  const calls = []
  const fetch = async (url, init) => {
    calls.push({ url, init })
    const r = respond(url, init)
    return new Response(r.body === undefined ? '' : JSON.stringify(r.body), { status: r.status ?? 200, headers: r.headers ?? {} })
  }
  return { fetch, calls }
}

test('buildQuery orders parameters and encodes values', () => {
  assert.equal(buildQuery({ sort: '-a,b', limit: 5, q: 'a b&c', channel: undefined, offset: 0 }),
    'limit=5&offset=0&q=a%20b%26c&sort=-a%2Cb')
  assert.equal(buildQuery({ limit: 1 }, { title: { contains: 'x', prefix: 'y' }, slug: 'a', n: { in: [1, 2] }, d: { exists: true } }),
    'limit=1&where[d][exists]=true&where[n][in]=1%2C2&where[slug][eq]=a&where[title][contains]=x&where[title][prefix]=y')
})

test('boolean groups and facets', async () => {
  assert.equal(
    buildQuery({ facets: 'a,b' }, { n: { gt: 1 } }, [{ x: 'a', y: { in: [1, 2] } }, { z: true }], [{ k: 'press' }]),
    'facets=a%2Cb&where[n][gt]=1&where[not][0][k][eq]=press&where[or][0][x][eq]=a&where[or][0][y][in]=1%2C2&where[or][1][z][eq]=true')
  const m = mock(() => ({ body: { items: [], hasMore: false, facets: { a: [{ value: 'x', count: 2 }] } } }))
  const c = createClient({ baseUrl: 'https://x.test', channel: 'web', fetch: m.fetch })
  const r = await c.list('article', { or: [{ title: 'a' }, { title: 'b' }], not: [{ slug: 'z' }], facets: ['title', 'slug'] })
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/collections/article/items?channel=web&facets=title%2Cslug&where[not][0][slug][eq]=z&where[or][0][title][eq]=a&where[or][1][title][eq]=b')
  assert.deepEqual(r.data.facets, { a: [{ value: 'x', count: 2 }] })
})

test('buildQuery is independent of key insertion order', () => {
  const a = buildQuery({ limit: 1, q: 'x' }, { a: 1, b: { gt: 2 } })
  const b = buildQuery({ q: 'x', limit: 1 }, { b: { gt: 2 }, a: 1 })
  assert.equal(a, b)
})

test('item: URL, headers, tags, etag, included', async () => {
  const m = mock(() => ({ body: { item: { id: 'i1', name: 'N' }, included: { x: { id: 'x' } } },
    headers: { 'Surrogate-Key': 'i:i1 c:article  r:main', ETag: '"abc"' } }))
  const c = createClient({ baseUrl: 'https://cms.test/', channel: 'web', token: 'tok', preview: 'pv', fetch: m.fetch, headers: { 'X-A': '1' } })
  const r = await c.item('i1', { locale: 'de', include: 2 })
  assert.equal(m.calls[0].url, 'https://cms.test/api/deliver/v1/items/i1?channel=web&include=2&locale=de')
  const h = m.calls[0].init.headers
  assert.equal(h.Authorization, 'Bearer tok')
  assert.equal(h['X-Hydrogen-Preview'], 'pv')
  assert.equal(h['X-A'], '1')
  assert.equal(m.calls[0].init.method, 'GET')
  assert.deepEqual(r.data, { id: 'i1', name: 'N' })
  assert.deepEqual(r.tags, ['i:i1', 'c:article', 'r:main'])
  assert.equal(r.etag, '"abc"')
  assert.equal(r.status, 200)
  assert.deepEqual(r.included, { x: { id: 'x' } })
})

test('single-valued parameters appear once', async () => {
  const m = mock(() => ({ body: { items: [], hasMore: false } }))
  const c = createClient({ baseUrl: 'https://x.test', channel: 'web', fetch: m.fetch })
  await c.list('article', { locale: 'en', include: 1, limit: 10, offset: 20, q: 'hi', sort: ['-publishedAt', 'title'], total: true,
    where: { title: { contains: 'a' } } })
  const u = new URL(m.calls[0].url)
  for (const k of ['channel', 'locale', 'include', 'limit', 'offset', 'q', 'sort']) assert.equal(u.searchParams.getAll(k).length, 1, k)
  assert.equal(u.pathname, '/api/deliver/v1/collections/article/items')
  assert.equal(u.searchParams.get('sort'), '-publishedAt,title')
  assert.equal(u.searchParams.get('where[title][contains]'), 'a')
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/collections/article/items?channel=web&include=1&limit=10&locale=en&offset=20&q=hi&sort=-publishedAt%2Ctitle&total=true&where[title][contains]=a')
})

test('total is sent only when true', async () => {
  const m = mock(() => ({ body: { items: [], hasMore: false } }))
  const c = createClient({ baseUrl: 'https://x.test', fetch: m.fetch })
  await c.list('a', { total: false })
  await c.list('a')
  assert.ok(m.calls.every((x) => !x.url.includes('total')))
})

test('no channel param without a channel; singleton and by-key paths', async () => {
  const m = mock(() => ({ body: { item: {} } }))
  const c = createClient({ baseUrl: 'https://x.test', token: 't', fetch: m.fetch })
  await c.singleton('settings')
  await c.itemByKey('my-key')
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/singletons/settings')
  assert.equal(m.calls[1].url, 'https://x.test/api/deliver/v1/items/by-key/my-key')
})

test('fields: one comma-joined parameter on item, list and tree, not route', async () => {
  const m = mock((u) => u.includes('/tree') ? { body: { nodes: [{ id: '1', kind: 'page', name: 'Home', fields: { title: 'T' } }] } } : { body: { item: { id: 'i' }, items: [] } })
  const c = createClient({ baseUrl: 'https://x.test', channel: 'web', fetch: m.fetch })
  await c.item('i1', { fields: ['title', 'seo.title'] })
  await c.singleton('s', { fields: 'title' })
  await c.list('post', { fields: ['a', 'b'] })
  const t = await c.tree({ fields: ['title'] })
  await c.route('/x', { fields: ['title'] })
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/items/i1?channel=web&fields=title%2Cseo.title')
  assert.equal(m.calls[1].url, 'https://x.test/api/deliver/v1/singletons/s?channel=web&fields=title')
  assert.equal(m.calls[2].url, 'https://x.test/api/deliver/v1/collections/post/items?channel=web&fields=a%2Cb')
  assert.equal(m.calls[3].url, 'https://x.test/api/deliver/v1/tree?channel=web&fields=title')
  assert.equal(m.calls[4].url, 'https://x.test/api/deliver/v1/route?channel=web&url=%2Fx')
  assert.deepEqual(t.data[0].fields, { title: 'T' })
})

test('route and tree', async () => {
  const m = mock((u) => u.includes('/tree') ? { body: { site: 's', locale: 'en', nodes: [{ id: '1', kind: 'page', name: 'Home' }] } }
    : { body: { located: { kind: 'page' } } })
  const c = createClient({ baseUrl: 'https://x.test', channel: 'web', fetch: m.fetch })
  const r = await c.route('https://www.acme.com/en/about?x=1')
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/route?channel=web&url=https%3A%2F%2Fwww.acme.com%2Fen%2Fabout%3Fx%3D1')
  assert.equal(r.data.located.kind, 'page')
  const t = await c.tree({ depth: 2, site: 'corp', locale: 'de' })
  assert.equal(m.calls[1].url, 'https://x.test/api/deliver/v1/tree?channel=web&depth=2&locale=de&site=corp')
  assert.deepEqual(t.data, [{ id: '1', kind: 'page', name: 'Home' }])
})

test('channel info uses the client channel, or fails locally', async () => {
  const m = mock(() => ({ body: { name: 'web', languages: ['en'], collections: [], options: {} } }))
  const c = createClient({ baseUrl: 'https://x.test', channel: 'web', fetch: m.fetch })
  await c.channel()
  assert.equal(m.calls[0].url, 'https://x.test/api/deliver/v1/channels/web')
  await assert.rejects(createClient({ baseUrl: 'https://x.test', fetch: m.fetch }).channel(), (e) => e instanceof HydrogenError && e.code === 'bad_request')
  assert.equal(m.calls.length, 1)
})

test('errors come from the envelope', async () => {
  const fields = [{ path: 'a', code: 'x', message: 'm' }]
  const m = mock((u) => u.includes('nf') ? { status: 404, body: { error: { code: 'not_found', message: 'not available' } } }
    : u.includes('bad') ? { status: 400, body: { error: { code: 'bad_request', message: 'nope', fields } } }
    : { status: 502 })
  const c = createClient({ baseUrl: 'https://x.test', fetch: m.fetch })
  await assert.rejects(c.item('nf'), (e) => e instanceof HydrogenError && e.status === 404 && e.code === 'not_found' && e.message === 'not available')
  await assert.rejects(c.item('bad'), (e) => e.status === 400 && e.code === 'bad_request' && e.fields[0].path === 'a')
  await assert.rejects(c.item('other'), (e) => e instanceof HydrogenError && e.status === 502 && e.code === 'error')
})

test('withPreview copies the client; per-call preview wins', async () => {
  const m = mock(() => ({ body: { item: {} } }))
  const c = createClient({ baseUrl: 'https://x.test', fetch: m.fetch })
  await c.item('a')
  await c.withPreview('p1').item('a')
  await c.withPreview('p1').item('a', { preview: 'p2' })
  await c.withPreview('p1').withPreview(undefined).item('a')
  assert.equal(m.calls[0].init.headers['X-Hydrogen-Preview'], undefined)
  assert.equal(m.calls[1].init.headers['X-Hydrogen-Preview'], 'p1')
  assert.equal(m.calls[2].init.headers['X-Hydrogen-Preview'], 'p2')
  assert.equal(m.calls[3].init.headers['X-Hydrogen-Preview'], undefined)
})

test('init hook and per-call init reach fetch', async () => {
  const m = mock(() => ({ body: { item: {} } }))
  const c = createClient({ baseUrl: 'https://x.test', fetch: m.fetch, init: (r) => ({ next: { tags: [r.path] }, headers: { 'X-H': 'hook' } }) })
  const ac = new AbortController()
  await c.item('a', { init: { cache: 'no-store' }, signal: ac.signal })
  const i = m.calls[0].init
  assert.deepEqual(i.next, { tags: ['/items/a'] })
  assert.equal(i.cache, 'no-store')
  assert.equal(i.headers['X-H'], 'hook')
  assert.equal(i.signal, ac.signal)
})

test('linkTo and parseTags', () => {
  const it = { links: { a: '/x' } }
  assert.equal(linkTo(it, 'a'), '/x')
  assert.equal(linkTo(it, 'b'), undefined)
  assert.equal(linkTo(it, 'b', (id) => '/f/' + id), '/f/b')
  assert.equal(linkTo({}, 'a'), undefined)
  assert.deepEqual(parseTags(null), [])
})

test('imageProps: variant, original fallback, alt', async () => {
  const { imageProps } = await import('../dist/index.js')
  const a = { url: '/o.jpg', width: 1000, height: 500, fields: { alt: 'Cat' },
    variants: { card: { url: '/c.jpg', width: 640, height: 320, type: 'image/jpeg', srcset: '/c1.jpg 320w, /c.jpg 640w' } } }
  assert.deepEqual(imageProps(a, 'card'), { src: '/c.jpg', srcSet: '/c1.jpg 320w, /c.jpg 640w', width: 640, height: 320, alt: 'Cat' })
  assert.deepEqual(imageProps(a, 'nope'), { src: '/o.jpg', srcSet: undefined, width: 1000, height: 500, alt: 'Cat' })
  assert.deepEqual(imageProps(a), imageProps(a, 'nope'))
  assert.equal(imageProps({ url: '/x', fields: { alt: 3 } }).alt, '')
  assert.equal(imageProps({ url: '/x' }).alt, '')
})
