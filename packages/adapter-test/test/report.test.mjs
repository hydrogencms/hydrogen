import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Recorder, format, summarize, assert as check, equal } from '../dist/report.js'
import { Jar } from '../dist/http.js'

test('a throwing check is a failure with its message; the report is grouped and counted', async () => {
  const r = new Recorder()
  await r.check('pages', '/a renders', () => {})
  await r.check('pages', '/b renders', () => check(false, 'no <h1>'))
  await r.check('seo', '/a canonical', () => equal(1, 2, 'canonical path'))
  r.skip('bridge', 'live update', 'run with --browser')
  const rep = summarize(r.results)
  assert.deepEqual([rep.passed, rep.failed, rep.skipped, rep.ok], [1, 2, 1, false])
  const out = format(rep)
  assert.match(out, /pages\n {2}ok {4}\/a renders\n {2}FAIL {2}\/b renders\n {10}no <h1>/)
  assert.match(out, /canonical path: got 1, want 2/)
  assert.match(out, /skip {2}live update\n {10}run with --browser/)
  assert.match(out, /1 passed, 2 failed, 1 skipped\n$/)
})

test('an all-passing report is ok', async () => {
  const r = new Recorder()
  await r.check('g', 'n', () => {})
  assert.ok(summarize(r.results).ok)
})

test('the cookie jar keeps cookies per host and drops cleared ones', () => {
  const jar = new Jar()
  const res = (...c) => ({ headers: { getSetCookie: () => c } })
  jar.store('http://127.0.0.1:1/a', res('hydrogen-preview=tok; Path=/; HttpOnly', 'x=1'))
  assert.equal(jar.header('http://127.0.0.1:2/b'), 'hydrogen-preview=tok; x=1')
  assert.equal(jar.header('http://other/'), undefined)
  jar.store('http://127.0.0.1:1/a', res('x=; Max-Age=0; Path=/'))
  assert.deepEqual(jar.list('http://127.0.0.1:1/'), [{ name: 'hydrogen-preview', value: 'tok' }])
})
