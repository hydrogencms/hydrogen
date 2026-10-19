import { test } from 'node:test'
import assert from 'node:assert/strict'
import { blockAttrs, componentOf, pathAttrs } from '../dist/blocks.js'


test('block markers and registry lookup', () => {
  assert.deepEqual(blockAttrs('hero', 'fields/body/0', true), { 'data-h-block': 'hero', 'data-h-path': 'fields/body/0' })
  assert.deepEqual(blockAttrs('hero', 'fields/body/0', false), { 'data-h-block': 'hero' })
  assert.deepEqual(pathAttrs(undefined, true), {})
  const fallback = () => null
  const reg = { components: { hero: 1 }, fallback, warned: new Set() }
  assert.equal(componentOf(reg, 'hero'), 1)
  const warn = console.warn
  const seen = []
  console.warn = (m) => seen.push(m)
  try {
    assert.equal(componentOf(reg, 'nope'), fallback)
    componentOf(reg, 'nope')
  } finally {
    console.warn = warn
  }
  assert.equal(seen.length, 1)
})
