import { test } from 'node:test'
import assert from 'node:assert/strict'
import { keepShared } from '../dist/live.js'

const shared = (item, blocks) => ({ id: 's1', type: 'shared', props: { item }, ...(blocks ? { blocks } : {}) })
const callout = { id: 'c1', type: 'callout', props: { text: 'Hi' } }

test('keepShared puts the inlined blocks of the initial shared blocks back', () => {
  const initial = { fields: { body: [{ id: 'h', type: 'hero', props: {} }, shared('x', [callout])] } }
  const live = { fields: { body: [shared('x'), { id: 'cards', type: 'cards', slots: { items: [shared('x')] } }] } }
  const out = keepShared(initial, live)
  assert.deepEqual(out.fields.body[0].blocks, [callout])
  assert.deepEqual(out.fields.body[1].slots.items[0].blocks, [callout])
  assert.equal(live.fields.body[0].blocks, undefined, 'the live document is not mutated')
})

test('keepShared leaves other shared blocks and plain values alone', () => {
  const initial = { fields: { body: [shared('x', [callout])] } }
  assert.deepEqual(keepShared(initial, { fields: { body: [shared('y')] } }), { fields: { body: [shared('y')] } })
  assert.deepEqual(keepShared(initial, { fields: { n: 1, t: null, s: 'a' } }), { fields: { n: 1, t: null, s: 'a' } })
  const own = shared('x', [{ ...callout, id: 'other' }])
  assert.deepEqual(keepShared(initial, { fields: { body: [own] } }).fields.body[0].blocks[0].id, 'other')
})

import { adminOriginWarning } from '../dist/admin.js'
test('adminOriginWarning: local and public https are fine, http and internal hosts are warned about', () => {
  for (const ok of ['http://localhost:8080', 'http://127.0.0.1:1', 'https://admin.example.com']) assert.equal(adminOriginWarning(ok), undefined, ok)
  assert.match(adminOriginWarning('http://admin.example.com'), /not https/)
  for (const bad of ['https://cms', 'https://cms.internal', 'http://10.0.0.5:8080', 'https://192.168.1.2', 'https://admin.corp.local']) assert.match(adminOriginWarning(bad), /internal/, bad)
})
