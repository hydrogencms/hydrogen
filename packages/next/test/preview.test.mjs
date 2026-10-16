import { test } from 'node:test'
import assert from 'node:assert/strict'
import { PREVIEW_COOKIE, matchPreviewPath, parsePreviewRequest, previewCookieOptions, safePath, tokenFromCookie, validToken } from '../dist/preview.js'
import { originOf, publicUrl } from '../dist/url.js'

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
  assert.equal(originOf(h({ host: 'internal:3000', 'x-forwarded-host': 'www.acme.com, proxy', 'x-forwarded-proto': 'http' })), 'http://www.acme.com')
  assert.equal(originOf(h({ host: 'localhost:3000' }), 'https://www.acme.com/'), 'https://www.acme.com')
  assert.equal(publicUrl(h({ host: 'a.test' }), '/en/x%20y'), 'https://a.test/en/x%20y')
  assert.equal(publicUrl(h({ host: 'a.test' }), ''), 'https://a.test/')
})
