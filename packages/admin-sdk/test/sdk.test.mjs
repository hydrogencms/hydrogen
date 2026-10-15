import test from 'node:test'
import assert from 'node:assert/strict'
import { defineElement, emit, useContext } from '../dist/index.js'

test('emit dispatches a bubbling composed CustomEvent', () => {
  const target = new EventTarget()
  let got
  target.addEventListener('change', (e) => { got = e })
  emit(target, 'change', { value: 3 })
  assert.ok(got instanceof CustomEvent)
  assert.deepEqual(got.detail, { value: 3 })
  assert.equal(got.bubbles, true)
  assert.equal(got.composed, true)
})

test('useContext returns ctx or throws a clear error', () => {
  const ctx = { prefix: '/admin' }
  assert.equal(useContext({ ctx }), ctx)
  assert.throws(() => useContext({ localName: 'x-foo' }), /x-foo: missing ctx; was it mounted by the admin\?/)
})

test('defineElement is idempotent and warns once per tag', () => {
  const defs = new Map()
  globalThis.customElements = { get: (t) => defs.get(t), define: (t, c) => defs.set(t, c) }
  const warns = []
  const orig = console.warn
  console.warn = (m) => warns.push(m)
  try {
    class A {}
    class B {}
    defineElement('x-a', A)
    defineElement('x-a', B)
    defineElement('x-a', B)
    assert.equal(defs.get('x-a'), A)
    assert.equal(warns.length, 1)
    assert.match(warns[0], /x-a/)
  } finally {
    console.warn = orig
    delete globalThis.customElements
  }
})
