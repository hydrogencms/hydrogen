import test from 'node:test'
import assert from 'node:assert/strict'
import { defineHook } from '../dist/index.js'

function mock(input, chunk = 5) {
  const bytes = new TextEncoder().encode(input)
  let at = 0
  const out = []
  globalThis.Javy = {
    readSync(fd, buf) {
      assert.equal(fd, 0)
      const n = Math.min(chunk, buf.length, bytes.length - at)
      buf.set(bytes.subarray(at, at + n))
      at += n
      return n
    },
    writeSync(fd, buf) {
      assert.equal(fd, 1)
      const n = Math.min(3, buf.length) // short writes
      out.push(...buf.subarray(0, n))
      return n
    },
  }
  return () => new TextDecoder().decode(new Uint8Array(out))
}

const req = { v: 1, event: 'beforeSave', locale: 'en', item: { id: 'i', type: 'article', version: 0, mainLocale: 'en' }, doc: { title: 'Grüße 🌍' } }

test('reads stdin in chunks, calls fn, writes the JSON response', () => {
  const out = mock(JSON.stringify(req))
  let seen
  defineHook((r) => { seen = r; return { set: { n: [...r.doc.title].length } } })
  assert.deepEqual(seen, req)
  assert.deepEqual(JSON.parse(out()), { set: { n: 7 } })
})

test('no return value writes {}', () => {
  const out = mock(JSON.stringify(req))
  defineHook(() => {})
  assert.equal(out(), '{}')
})

test('errors propagate (the hook fails with a non-zero exit)', () => {
  mock('not json')
  assert.throws(() => defineHook(() => ({})))
  mock(JSON.stringify(req))
  assert.throws(() => defineHook(() => { throw new Error('boom') }), /boom/)
})

test('supports the older Javy.IO namespace', () => {
  const out = mock(JSON.stringify(req))
  globalThis.Javy = { IO: globalThis.Javy }
  defineHook(() => ({ error: 'x' }))
  assert.equal(out(), '{"error":"x"}')
})

test('explains a missing Javy', () => {
  delete globalThis.Javy
  assert.throws(() => defineHook(() => ({})), /hydrogen hooks build/)
})

test('rejects an async hook instead of writing {}', () => {
  const out = mock(JSON.stringify(req))
  assert.throws(() => defineHook(async () => ({ error: 'x' })), /must be synchronous/)
  assert.equal(out(), '')
})
