import test from 'node:test'
import assert from 'node:assert/strict'
import { applyPatch, PatchError } from '../dist/index.js'

const ap = (doc, ...ops) => applyPatch(doc, ops)

test('RFC 6902 A.1 add an object member', () => {
  assert.deepEqual(ap({ foo: 'bar' }, { op: 'add', path: '/baz', value: 'qux' }), { foo: 'bar', baz: 'qux' })
})
test('RFC 6902 A.2 add an array element', () => {
  assert.deepEqual(ap({ foo: ['bar', 'baz'] }, { op: 'add', path: '/foo/1', value: 'qux' }), { foo: ['bar', 'qux', 'baz'] })
})
test('RFC 6902 A.3 remove an object member', () => {
  assert.deepEqual(ap({ baz: 'qux', foo: 'bar' }, { op: 'remove', path: '/baz' }), { foo: 'bar' })
})
test('RFC 6902 A.4 remove an array element', () => {
  assert.deepEqual(ap({ foo: ['bar', 'qux', 'baz'] }, { op: 'remove', path: '/foo/1' }), { foo: ['bar', 'baz'] })
})
test('RFC 6902 A.5 replace a value', () => {
  assert.deepEqual(ap({ baz: 'qux', foo: 'bar' }, { op: 'replace', path: '/baz', value: 'boo' }), { baz: 'boo', foo: 'bar' })
})
test('RFC 6902 A.10 add a nested member object', () => {
  assert.deepEqual(ap({ foo: 'bar' }, { op: 'add', path: '/child', value: { grandchild: {} } }), { foo: 'bar', child: { grandchild: {} } })
})
test('RFC 6902 A.12/A.13 errors: add to a missing parent, bad array index', () => {
  assert.throws(() => ap({ foo: 'bar' }, { op: 'add', path: '/baz/bat', value: 'qux' }), PatchError)
  assert.throws(() => ap({ foo: ['bar'] }, { op: 'add', path: '/foo/3', value: 1 }), /bad array index/)
  assert.throws(() => ap({ foo: ['bar'] }, { op: 'add', path: '/foo/01', value: 1 }), /bad array index/)
})
test('RFC 6902 A.16 add to the end of an array with "-"', () => {
  assert.deepEqual(ap({ foo: ['bar'] }, { op: 'add', path: '/foo/-', value: ['abc', 'def'] }), { foo: ['bar', ['abc', 'def']] })
})
test('RFC 6901 escapes ~0 and ~1', () => {
  const d = { 'a/b': 1, 'm~n': 2 }
  assert.deepEqual(ap(d, { op: 'replace', path: '/a~1b', value: 9 }, { op: 'replace', path: '/m~0n', value: 8 }), { 'a/b': 9, 'm~n': 8 })
})
test('array bounds: add at length ok, replace/remove at length fail', () => {
  assert.deepEqual(ap([1], { op: 'add', path: '/1', value: 2 }), [1, 2])
  assert.throws(() => ap([1], { op: 'replace', path: '/1', value: 2 }), PatchError)
  assert.throws(() => ap([1], { op: 'remove', path: '/1' }), PatchError)
  assert.throws(() => ap([1], { op: 'add', path: '/-1', value: 2 }), PatchError)
})
test('replace and remove of a missing member fail; add overwrites', () => {
  assert.throws(() => ap({}, { op: 'replace', path: '/a', value: 1 }), /no such path/)
  assert.throws(() => ap({}, { op: 'remove', path: '/a' }), /no such path/)
  assert.deepEqual(ap({ a: 1 }, { op: 'add', path: '/a', value: 2 }), { a: 2 })
})
test('root path replaces the document; removing the root fails', () => {
  assert.deepEqual(ap({ a: 1 }, { op: 'replace', path: '', value: [1] }), [1])
  assert.throws(() => ap({}, { op: 'remove', path: '' }), /root/)
})
test('invalid pointers, unsupported ops, missing values, __proto__', () => {
  assert.throws(() => ap({}, { op: 'add', path: 'a', value: 1 }), /invalid pointer/)
  assert.throws(() => ap({}, { op: 'move', path: '/a', from: '/b' }), /unsupported op "move"/)
  assert.throws(() => ap({}, { op: 'add', path: '/a' }), /needs a value/)
  assert.throws(() => ap({}, { op: 'add', path: '/__proto__/x', value: 1 }), /forbidden/)
  assert.equal({}.x, undefined)
})
test('errors name the failing op', () => {
  assert.throws(() => ap({ a: 1 }, { op: 'add', path: '/b', value: 1 }, { op: 'remove', path: '/zz' }), /ops\[1\] remove \/zz/)
})
test('immutable with structural sharing', () => {
  const doc = { fields: { body: [{ props: { h: 'a' } }, { props: { h: 'b' } }], title: 't' }, other: { x: 1 } }
  const snap = structuredClone(doc)
  const out = ap(doc, { op: 'replace', path: '/fields/body/1/props/h', value: 'c' })
  assert.deepEqual(doc, snap)
  assert.equal(out.fields.body[1].props.h, 'c')
  assert.equal(out.other, doc.other)
  assert.equal(out.fields.body[0], doc.fields.body[0])
  assert.notEqual(out.fields, doc.fields)
})
test('failure leaves the input intact (all-or-nothing)', () => {
  const doc = { a: [1] }
  assert.throws(() => ap(doc, { op: 'add', path: '/a/-', value: 2 }, { op: 'remove', path: '/nope' }))
  assert.deepEqual(doc, { a: [1] })
})
test('block edits in the stored shape', () => {
  const doc = { fields: { body: [{ id: 'a', type: 'hero', props: { heading: 'x' } }] } }
  const out = ap(doc,
    { op: 'add', path: '/fields/body/-', value: { id: 'b', type: 'callout', props: {} } },
    { op: 'add', path: '/fields/body/0/props/tone', value: 'dark' },
    { op: 'remove', path: '/fields/body/0/props/heading' })
  assert.deepEqual(out.fields.body, [{ id: 'a', type: 'hero', props: { tone: 'dark' } }, { id: 'b', type: 'callout', props: {} }])
})
