import { test } from 'node:test'
import assert from 'node:assert/strict'
import { blockAttrs, blockSlots, createRegistry, pickBlock } from '../dist/blocks.js'

const builtin = { shared: 'SHARED', unknown: 'UNKNOWN' }

test('data-h-block always, data-h-path only in preview', () => {
  assert.deepEqual(blockAttrs('hero', 'fields/body/0', false), { 'data-h-block': 'hero' })
  assert.deepEqual(blockAttrs('hero', 'fields/body/0', true), { 'data-h-block': 'hero', 'data-h-path': 'fields/body/0' })
  assert.deepEqual(blockAttrs('hero', undefined, true), { 'data-h-block': 'hero' })
})

test('pickBlock: registry, built-in shared, unknown only in preview; warns once', () => {
  const warns = []
  const w = console.warn
  console.warn = (m) => warns.push(m)
  try {
    const r = createRegistry({ hero: 'HERO' })
    assert.equal(pickBlock(r, 'hero', builtin), 'HERO')
    assert.equal(pickBlock(r, 'shared', builtin), 'SHARED')
    assert.equal(pickBlock(r, 'nope', builtin, false), undefined)
    assert.equal(pickBlock(r, 'nope', builtin, true), 'UNKNOWN')
    assert.equal(warns.length, 1)
    assert.equal(pickBlock(createRegistry({}, { fallback: 'FB' }), 'x', builtin), 'FB')
    assert.equal(pickBlock(createRegistry({ shared: 'MINE' }), 'shared', builtin), 'MINE')
  } finally {
    console.warn = w
  }
})

test('blockSlots: slot paths in preview, shared blocks inlined read-only', () => {
  const r = createRegistry({})
  const kid = { id: '2', type: 'card' }
  const s = blockSlots({ id: '1', type: 'cards', slots: { items: [kid] }, blocks: [kid] }, 'fields/body/0', r, true, { a: '/a' })
  assert.equal(s.items.path, 'fields/body/0/slots/items')
  assert.deepEqual(s.items.blocks, [kid])
  assert.equal(s.blocks.path, undefined)
  assert.equal(s.blocks.links.a, '/a')
})
