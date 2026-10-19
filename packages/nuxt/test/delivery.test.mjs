import { test } from 'node:test'
import assert from 'node:assert/strict'
import { clientOf, purge } from '../dist/delivery.js'

const cfg = { baseUrl: 'http://cms.test', channel: 'en', channels: ['en'], token: '', templates: [], fallback: false, ttl: 60, timeoutMs: 1000 }
const calls = []
globalThis.fetch = async (url, init) => {
  calls.push({ url: String(url), headers: new Headers(init?.headers) })
  const id = new URL(String(url)).pathname.split('/').pop()
  return new Response(JSON.stringify({ item: { id } }), { status: id === 'missing' ? 404 : 200, headers: { 'surrogate-key': `i:${id} c:page`, etag: '"1"' } })
}

test('responses are cached with their tags until a tag is purged', async () => {
  const c = clientOf(cfg, 'en')
  const a = await c.item('a')
  assert.deepEqual(a.tags, ['i:a', 'c:page'])
  await c.item('a')
  await c.item('b')
  assert.equal(calls.length, 2)
  assert.equal(purge(['i:a']), 1)
  await c.item('a')
  assert.equal(calls.length, 3)
  assert.equal(purge(['*']), 2)
  assert.equal(purge(['*']), 0)
})

test('a 404 is kept briefly; other channels and preview requests do not share entries', async () => {
  calls.length = 0
  purge(['*'])
  const c = clientOf(cfg, 'en')
  await assert.rejects(c.item('missing'))
  await assert.rejects(c.item('missing'))
  assert.equal(calls.length, 1)
  await clientOf(cfg, 'de').item('a')
  await clientOf(cfg, 'en').item('a')
  assert.equal(calls.length, 3)
  const p = clientOf(cfg, 'en', 'tok.en')
  await p.item('a')
  await p.item('a')
  assert.equal(calls.length, 5)
  assert.equal(calls.at(-1).headers.get('x-hydrogen-preview'), 'tok.en')
  assert.equal(purge(['*']), 3)
})

test('the channel token travels in Authorization', async () => {
  calls.length = 0
  await clientOf({ ...cfg, token: 'secret' }, 'en').item('z')
  assert.equal(calls[0].headers.get('authorization'), 'Bearer secret')
})
