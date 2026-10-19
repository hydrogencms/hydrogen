import { test } from 'node:test'
import assert from 'node:assert/strict'
import { keepShared, liveDoc, mergeDoc } from '../dist/merge.js'

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


test('live documents keep delivery-only keys and inlined shared blocks', () => {
  const initial = { links: { a: '/a' }, fields: { body: [{ id: '1', type: 'shared', props: { item: 'S' }, blocks: [{ id: 'x', type: 'hero' }] }] } }
  const incoming = { fields: { body: [{ id: '1', type: 'shared', props: { item: 'S' } }, { id: '2', type: 'hero' }] } }
  const shown = liveDoc(initial, incoming)
  assert.deepEqual(shown.links, { a: '/a' })
  assert.deepEqual(shown.fields.body[0].blocks, [{ id: 'x', type: 'hero' }])
  assert.equal(shown.fields.body[1].blocks, undefined)
  assert.equal(mergeDoc('x', 5), 5)
})
