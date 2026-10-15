import { test } from 'node:test'
import assert from 'node:assert/strict'
import { linkIds, alternatesByPath, diffMultiset, expectedBlocks, onFrontend, pathOf, underPrefix, withoutPreview } from '../dist/expect.js'

const tree = [
  { type: 'hero' },
  { type: 'cards', slots: { items: [{ type: 'card' }, { type: 'card' }] } },
  { type: 'shared', blocks: [{ type: 'callout' }, { type: 'richText' }] },
]

test('expectedBlocks walks slots and inlined shared blocks', () => {
  assert.deepEqual(expectedBlocks(tree), ['hero', 'cards', 'card', 'card', 'shared', 'callout', 'richText'])
  assert.deepEqual(expectedBlocks(undefined), [])
})

test('diffMultiset names what is missing and what is extra, ignoring order', () => {
  assert.equal(diffMultiset(['a', 'b', 'a'], ['a', 'a', 'b']), undefined)
  assert.equal(diffMultiset(['a', 'b'], ['b']), '1 x a missing')
  assert.equal(diffMultiset(['a'], ['a', 'c', 'c']), '2 x c unexpected')
})

test('url helpers', () => {
  assert.equal(pathOf('https://cms.test/en/about?x=1#f'), '/en/about?x=1')
  assert.equal(onFrontend('http://localhost:3000/en/about?preview=t', 'http://127.0.0.1:5000/'), 'http://127.0.0.1:5000/en/about?preview=t')
  assert.equal(withoutPreview('http://h/blog/x?preview=t&a=1'), '/blog/x?a=1')
  assert.equal(withoutPreview('http://h/blog/x?preview=t'), '/blog/x')
  assert.equal(underPrefix('/en/about', '/en'), '/about')
  assert.equal(underPrefix('/en', '/en'), '/')
  assert.equal(underPrefix('/english', '/en'), undefined)
  assert.equal(underPrefix('/about', ''), '/about')
})

test('alternatesByPath keys by locale and drops origins', () => {
  assert.deepEqual(alternatesByPath([{ locale: 'de', url: 'http://a/de/x' }, { locale: 'en', url: 'http://a/en/y' }]), { de: '/de/x', en: '/en/y' })
  assert.deepEqual(alternatesByPath(undefined), {})
})

test('linkIds finds item links in nested documents', () => {
  const doc = { body: [{ props: { body: { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'x', marks: [{ type: 'link', attrs: { href: 'item:abc' } }, { type: 'link', attrs: { href: 'https://x' } }] }] }] } } }], t: null }
  assert.deepEqual(linkIds(doc), ['abc'])
})
