import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { parseSignature, verifySignature } from '../dist/signature.js'

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
