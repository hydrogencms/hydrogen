import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash, createHmac } from 'node:crypto'
import {
  ALL_TAG, PREVIEW_COOKIE, createTagCache, matchPreviewPath, matcherHeaders, originOf, parsePreviewRequest, parseSignature,
  previewCookieOptions, previewHash, publicUrl, readCapped, safePath, tokenFromCookie, validToken, verifySignature, webhookTags,
} from '../dist/server.js'

// ---- signature
const secret = 'a-test-secret-of-16+'
const body = new TextEncoder().encode('{"id":"1","topic":"item.published","data":{},"tags":["i:a","c:page"],"createdAt":"2026-01-01T00:00:00Z"}')
const mac = (s, t, b) => createHmac('sha256', s).update(`${t}.`).update(b).digest('hex')
const now = 1_800_000_000_000
const t = now / 1000
const header = (ts = t, s = secret, b = body) => `t=${ts},v1=${mac(s, ts, b)}`

test('accepts a valid signature', async () => {
  assert.equal(await verifySignature(secret, header(), body, now), true)
})

test('rejects a wrong secret, a changed body and a changed timestamp', async () => {
  assert.equal(await verifySignature(secret, header(t, 'another-secret-0123'), body, now), false)
  assert.equal(await verifySignature(secret, header(), new TextEncoder().encode('{"tags":["*"]}'), now), false)
  assert.equal(await verifySignature(secret, `t=${t + 1},v1=${mac(secret, t, body)}`, body, now), false)
})

test('timestamp tolerance is five minutes in either direction', async () => {
  assert.equal(await verifySignature(secret, header(t - 299), body, now), true)
  assert.equal(await verifySignature(secret, header(t - 301), body, now), false)
  assert.equal(await verifySignature(secret, header(t + 299), body, now), true)
  assert.equal(await verifySignature(secret, header(t + 301), body, now), false)
  assert.equal(await verifySignature(secret, header(t - 301), body, now, 600_000), true)
})

test('any of several v1 signatures may match (secret rotation)', async () => {
  const h = `t=${t},v1=${'0'.repeat(64)},v1=${mac(secret, t, body)}`
  assert.equal(await verifySignature(secret, h, body, now), true)
  assert.equal(await verifySignature(secret, `t=${t},v1=${'0'.repeat(64)}`, body, now), false)
})

test('rejects malformed headers and empty secrets', async () => {
  for (const h of [undefined, null, '', 'garbage', `v1=${mac(secret, t, body)}`, `t=${t}`, `t=abc,v1=00`, `t=${t},v1=xyz`, `t=${t},v1=abc`, `t=${t},v1=`]) {
    assert.equal(await verifySignature(secret, h, body, now), false, String(h))
  }
  assert.equal(await verifySignature('', header(t, ''), body, now), false)
})

test('hex is case-insensitive and spaces around parts are tolerated', async () => {
  assert.equal(await verifySignature(secret, `t=${t}, v1=${mac(secret, t, body).toUpperCase()}`, body, now), true)
})

test('parseSignature', () => {
  assert.deepEqual(parseSignature('t=12,v1=ab,v1=cd'), { t: '12', sigs: ['ab', 'cd'] })
  assert.equal(parseSignature('t=12'), undefined)
  assert.equal(parseSignature('t=-1,v1=ab'), undefined)
})

test('readCapped stops at the cap', async () => {
  const req = (b, len) => new Request('http://x/', { method: 'POST', body: b, headers: len === undefined ? {} : { 'content-length': String(len) } })
  assert.equal((await readCapped(req('abcd'), 10)).length, 4)
  assert.equal(await readCapped(req('abcdefghijkl'), 10), undefined)
  assert.equal(await readCapped({ headers: new Headers({ 'content-length': '99' }), body: null }, 10), undefined)
  assert.equal((await readCapped({ headers: new Headers(), body: null }, 10)).length, 0)
})

// ---- tags
test('webhook tags are the event tags, deduplicated', () => {
  const body = { id: '7', topic: 'item.published', tags: ['i:a', 'c:article', 'r:corporate', 'nav:corporate', '*', 'i:a'] }
  assert.deepEqual(webhookTags(body), ['i:a', 'c:article', 'r:corporate', 'nav:corporate', '*'])
  assert.equal(ALL_TAG, '*')
})

test('malformed webhook bodies yield no tags', () => {
  for (const b of [null, undefined, 3, 'x', {}, { tags: 'i:a' }, { tags: {} }, { tags: [1, null, '', 'a b', 'x'.repeat(257)] }]) assert.deepEqual(webhookTags(b), [], JSON.stringify(b))
})

// ---- preview
test('previewHash is the SHA-256 hex of the token', async () => {
  assert.equal(await previewHash('abc'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
  assert.equal(PREVIEW_COOKIE, 'hydrogen-preview')
})

test('preview paths of items with no route: encoded segments, prefixes', () => {
  assert.deepEqual(matchPreviewPath('/en/preview/post/a%20b', '/preview'), { collection: 'post', id: 'a b' })
  for (const p of ['/preview', '/preview/page/a/b', '/preview//a', '/preview/p/%E0%A4%A']) assert.equal(matchPreviewPath(p, '/preview'), undefined, p)
})

// ---- urls
test('publicUrl: origin wins, then forwarded headers, then host', () => {
  const h = (o) => new Headers(o)
  assert.equal(publicUrl(h({ host: 'a.test' }), '/x', 'http://fixed.test/'), 'http://fixed.test/x')
  assert.equal(publicUrl(h({ host: 'a.test', 'x-forwarded-host': 'b.com, c.com', 'x-forwarded-proto': 'http' }), '/x', undefined, true), 'http://b.com/x')
  assert.equal(publicUrl(h({ host: 'a.test', 'x-forwarded-host': 'b.test' }), '/x', undefined, false), 'https://a.test/x')
  assert.equal(publicUrl(h({}), ''), 'https://localhost/')
  assert.equal(originOf(h({ host: 'a.test' })), 'https://a.test')
})

test('matcherHeaders forwards the headers the channels match on', () => {
  const ch = (name) => ({ options: { match: name ? { header: { name } } : {} } })
  assert.equal(matcherHeaders([ch(), ch()], new Headers({ 'x-site': 'a' })), undefined)
  assert.deepEqual(matcherHeaders([ch('X-Site'), ch('x-other')], new Headers({ 'x-site': 'a' })), { 'X-Site': 'a' })
})

// ---- tagged response cache
const mk = (o = {}) => {
  let n = 0
  const calls = []
  let clock = 1000
  const real = async (url, init) => {
    calls.push({ url: String(url), init })
    n++
    const r = o.respond?.(url, n) ?? new Response(JSON.stringify({ n }), { headers: { 'Surrogate-Key': 'i:a c:page', 'content-length': '999' } })
    if (r instanceof Error) throw r
    return r
  }
  const cache = createTagCache({ ttl: 60, fetch: real, now: () => clock, ...o.cache })
  return { cache, calls, tick: (ms) => (clock += ms) }
}
const bodyN = async (p) => (await (await p).json()).n

test('a GET is answered from memory until it expires, with its headers', async () => {
  const { cache, calls, tick } = mk()
  assert.equal(await bodyN(cache.fetch('http://c/a')), 1)
  const r = await cache.fetch('http://c/a')
  assert.equal((await r.json()).n, 1)
  assert.equal(r.headers.get('surrogate-key'), 'i:a c:page')
  assert.equal(calls.length, 1)
  tick(61_000)
  assert.equal(await bodyN(cache.fetch('http://c/a')), 2)
})

test('preview and non-GET requests are never cached', async () => {
  const { cache, calls } = mk()
  await cache.fetch('http://c/a', { headers: { 'X-Hydrogen-Preview': 'tok' } })
  await cache.fetch('http://c/a', { headers: { 'X-Hydrogen-Preview': 'tok' } })
  await cache.fetch('http://c/a', { method: 'POST' })
  assert.equal(calls.length, 3)
  assert.equal(cache.size(), 0)
})

test('other request headers (channel token, forwarded matchers) make other entries', async () => {
  const { cache, calls } = mk()
  await cache.fetch('http://c/a', { headers: { accept: 'application/json', authorization: 'Bearer x' } })
  await cache.fetch('http://c/a', { headers: { accept: 'application/json', authorization: 'Bearer y' } })
  await cache.fetch('http://c/a', { headers: { accept: 'application/json', authorization: 'Bearer y' } })
  assert.equal(calls.length, 2)
})

test('purge by tag drops exactly the entries carrying it; * drops all', async () => {
  const { cache, calls } = mk({
    respond: (u) => new Response('{"n":1}', { headers: { 'Surrogate-Key': String(u).endsWith('/a') ? 'i:a' : 'i:b' } }),
  })
  await cache.fetch('http://c/a')
  await cache.fetch('http://c/b')
  assert.equal(cache.purge(['i:a']), 1)
  await cache.fetch('http://c/a')
  await cache.fetch('http://c/b')
  assert.equal(calls.length, 3)
  assert.equal(cache.purge(['*']), 2)
  assert.equal(cache.size(), 0)
  assert.equal(cache.purge(['nope']), 0)
})

test('concurrent misses share one request, and a purge during a fetch is not undone', async () => {
  let release
  const { cache, calls } = mk({ respond: () => new Promise((r) => (release = () => r(new Response('{"n":1}', { headers: { 'Surrogate-Key': 'i:a' } })))) })
  const a = cache.fetch('http://c/a')
  const b = cache.fetch('http://c/a')
  await new Promise((r) => setImmediate(r))
  cache.purge(['i:a'])
  release()
  await Promise.all([a, b])
  assert.equal(calls.length, 1)
  assert.equal(cache.size(), 0, 'the response that started before the purge is not stored')
})

test('errors are not cached; a 404 lives 30 s; a failing CMS is answered from an expired entry', async () => {
  let fail = false
  const { cache, calls, tick } = mk({ respond: () => (fail ? new Response('boom', { status: 503 }) : new Response('{"n":1}')) })
  assert.equal(await bodyN(cache.fetch('http://c/a')), 1)
  tick(400_000) // tagless success lives min(ttl, 300) s
  fail = true
  assert.equal(await bodyN(cache.fetch('http://c/a')), 1, 'stale on 503')
  assert.equal(calls.length, 2)
  fail = false
  const nf = mk({ respond: () => new Response('{}', { status: 404 }) })
  assert.equal((await nf.cache.fetch('http://c/x')).status, 404)
  await nf.cache.fetch('http://c/x')
  assert.equal(nf.calls.length, 1)
  nf.tick(31_000)
  await nf.cache.fetch('http://c/x')
  assert.equal(nf.calls.length, 2)
  const bad = mk({ respond: () => new Response('boom', { status: 500 }) })
  assert.equal((await bad.cache.fetch('http://c/x')).status, 500)
  await bad.cache.fetch('http://c/x')
  assert.equal(bad.calls.length, 2)
})

test('a network error without a stale entry throws', async () => {
  const { cache } = mk({ respond: () => new Error('down') })
  await assert.rejects(cache.fetch('http://c/a'), /down/)
})

test('the cache holds at most max entries', async () => {
  const { cache } = mk({ cache: { max: 2 } })
  for (const p of ['a', 'b', 'c']) await cache.fetch('http://c/' + p)
  assert.equal(cache.size(), 2)
})


test('?preview= URLs are never stored, the key ignores Accept, a hit refreshes the entry (least recently used first out)', async () => {
  const { cache, calls } = mk({ cache: { max: 2 } })
  await cache.fetch('http://c/x?preview=tok')
  assert.equal(cache.size(), 0)
  await cache.fetch('http://c/1', { headers: { accept: 'application/json' } })
  await cache.fetch('http://c/1', { headers: { accept: '*/*' } })
  assert.equal(calls.length, 2)
  await cache.fetch('http://c/2')
  await cache.fetch('http://c/1')
  await cache.fetch('http://c/3')
  assert.equal(cache.size(), 2)
  const n = calls.length
  await cache.fetch('http://c/1')
  assert.equal(calls.length, n, '1 stayed')
  await cache.fetch('http://c/2')
  assert.equal(calls.length, n + 1, '2 went')
})

test('an entry carries its Surrogate-Key tags and the * tag', async () => {
  const { cache } = mk()
  await cache.fetch('http://c/a')
  assert.equal(cache.purge(['unrelated']), 0)
  assert.equal(cache.purge(['c:page']), 1)
  await cache.fetch('http://c/a')
  assert.equal(cache.purge(['*']), 1)
})

test('timeoutMs aborts a slow request', async () => {
  const cache = createTagCache({ timeoutMs: 20, fetch: (_u, i) => new Promise((_r, rej) => i.signal.addEventListener('abort', () => rej(new Error('aborted')))) })
  const keep = setTimeout(() => {}, 1000) // AbortSignal.timeout's timer does not keep the process alive
  await assert.rejects(cache.fetch('http://c/slow'), /aborted/)
  clearTimeout(keep)
  assert.equal(cache.size(), 0)
})

test('a Request input and a 204 come back intact', async () => {
  const cache = createTagCache({ fetch: async () => new Response(null, { status: 204 }) })
  assert.equal((await cache.fetch(new Request('http://c/a'))).status, 204)
})

// ---- the Next adapter's original preview/url cases
{

const tok = 'eyJjaCI6ImEifQ.c2ln-_x'
const req = (qs) => parsePreviewRequest(new URL('https://site.test/api/hydrogen/preview?' + qs))

test('preview request: token and path', () => {
  assert.deepEqual(req(`token=${tok}&path=/en/news/x%3Fa%3D1`), { token: tok, path: '/en/news/x?a=1' })
  assert.deepEqual(req(`token=${tok}`), { token: tok, path: '/' })
})

test('preview request: bad input', () => {
  assert.ok('error' in req('path=/x'))
  assert.ok('error' in req('token=has space&path=/x'))
  assert.ok('error' in req(`token=${'a'.repeat(3000)}`))
  assert.ok('error' in req(`token=${tok}&path=//evil.example`))
  assert.ok('error' in req(`token=${tok}&path=https://evil.example/`))
  assert.ok('error' in req(`token=${tok}&path=/%5Cevil.example`))
  assert.ok('error' in req(`token=${tok}&path=/a%0d%0aSet-Cookie:x`))
})

test('exit needs no token', () => {
  assert.deepEqual(req('exit=1&path=/en'), { exit: true, path: '/en' })
})

test('safePath', () => {
  assert.equal(safePath(null), '/')
  assert.equal(safePath('/a/b'), '/a/b')
  for (const p of ['a', '//x', '/\\x', 'http://x', '/a\\b', '/a\nb', '/a\u007f']) assert.equal(safePath(p), undefined, p)
})

test('cookie values and options', () => {
  assert.equal(PREVIEW_COOKIE, 'hydrogen-preview')
  assert.equal(tokenFromCookie(tok), tok)
  assert.equal(tokenFromCookie(undefined), undefined)
  assert.equal(tokenFromCookie('a b'), undefined)
  assert.equal(validToken(''), false)
  assert.equal(tokenFromCookie('hps_Ab-_09'), 'hps_Ab-_09')
  const http = previewCookieOptions(false)
  assert.deepEqual([http.httpOnly, http.sameSite, http.secure, http.path], [true, 'lax', false, '/'])
  const https = previewCookieOptions(true)
  assert.deepEqual([https.httpOnly, https.sameSite, https.secure], [true, 'none', true])
  assert.equal(https.maxAge, 604800)
})

test('preview URL of an item without a route', () => {
  assert.deepEqual(matchPreviewPath('/preview/page/abc', '/preview'), { collection: 'page', id: 'abc' })
  // The pages module prefixes pattern URLs of a channel that has a path prefix.
  assert.deepEqual(matchPreviewPath('/en/preview/page/abc', '/preview'), { collection: 'page', id: 'abc' })
  for (const p of ['/preview', '/en/preview/page/a/b', '/preview/page', '/preview/page/a/b', '/other/page/abc', '/preview//abc', '/preview/%E0/abc']) {
    assert.equal(matchPreviewPath(p, '/preview'), undefined, p)
  }
})

test('public origin and URL', () => {
  const h = (o) => ({ get: (n) => o[n.toLowerCase()] ?? null })
  assert.equal(originOf(h({ host: 'www.acme.com' })), 'https://www.acme.com')
  assert.equal(originOf(h({ host: 'internal:3000', 'x-forwarded-host': 'www.acme.com, proxy', 'x-forwarded-proto': 'http' }), undefined, true), 'http://www.acme.com')
  assert.equal(originOf(h({ host: 'localhost:3000' }), 'https://www.acme.com/'), 'https://www.acme.com')
  assert.equal(publicUrl(h({ host: 'a.test' }), '/en/x%20y'), 'https://a.test/en/x%20y')
  assert.equal(publicUrl(h({ host: 'a.test' }), ''), 'https://a.test/')
})
}

test('404s have their own quota: random paths cannot evict cached 200s', async () => {
  const { cache, calls } = mk({ cache: { max: 150 }, respond: (u) => (String(u).includes('/miss') ? new Response('{}', { status: 404 }) : new Response('{"n":1}', { headers: { 'Surrogate-Key': 'i:a' } })) })
  for (let i = 0; i < 50; i++) await cache.fetch('http://c/hot' + i)
  for (let i = 0; i < 1000; i++) await cache.fetch('http://c/miss' + i)
  assert.ok(cache.size() <= 150)
  const n = calls.length
  for (let i = 0; i < 50; i++) await cache.fetch('http://c/hot' + i)
  assert.equal(calls.length, n, 'every hot entry survived')
})

test('forwarded headers count only when trusted', () => {
  const h = new Headers({ host: 'internal:3000', 'x-forwarded-host': 'www.acme.com', 'x-forwarded-proto': 'http' })
  assert.equal(publicUrl(h, '/x'), 'https://internal:3000/x')
  assert.equal(publicUrl(h, '/x', undefined, true), 'http://www.acme.com/x')
  assert.equal(publicUrl(h, '/x', 'https://fixed.test', true), 'https://fixed.test/x')
})

test('a stale entry is served on 5xx', async () => {
  let fail = false
  const { cache, tick } = mk({ respond: () => (fail ? new Response('boom', { status: 503 }) : new Response('{"n":1}', { headers: { 'Surrogate-Key': 'i:a' } })) })
  await cache.fetch('http://c/a')
  tick(3_700_000)
  fail = true
  assert.equal((await (await cache.fetch('http://c/a')).json()).n, 1)
})
