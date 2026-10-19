import { test } from 'node:test'
import assert from 'node:assert/strict'
import { linkHref, richTextNodes, safeHref } from '../dist/richtext.js'

const doc = (...content) => ({ type: 'doc', content })
const p = (...content) => ({ type: 'paragraph', content })
const t = (text, ...marks) => ({ type: 'text', text, marks: marks.map((m) => (typeof m === 'string' ? { type: m } : m)) })

test('paragraphs, headings, marks and lists', () => {
  const out = richTextNodes(doc({ type: 'heading', attrs: { level: 9 }, content: [t('H')] }, p(t('a ', 'bold'), t('b', 'italic', 'bold')), { type: 'orderedList', attrs: { order: 3 }, content: [{ type: 'listItem', content: [p(t('x'))] }] }))
  assert.deepEqual(out[0], { tag: 'h6', attrs: {}, kids: ['H'] })
  assert.deepEqual(out[1].kids[0], { tag: 'strong', attrs: {}, kids: ['a '] })
  assert.deepEqual(out[1].kids[1], { tag: 'em', attrs: {}, kids: [{ tag: 'strong', attrs: {}, kids: ['b'] }] })
  assert.equal(out[2].tag, 'ol')
  assert.equal(out[2].attrs.start, 3)
})

test('item links come from delivery, other schemes are dropped', () => {
  const link = (href) => richTextNodes(doc(p(t('go', { type: 'link', attrs: { href } }))), { links: { abc: '/en/about' } })[0].kids[0]
  assert.deepEqual(link('item:abc'), { tag: 'a', attrs: { href: '/en/about' }, kids: ['go'] })
  assert.equal(link('item:unknown'), 'go')
  assert.equal(link('javascript:alert(1)'), 'go')
  assert.equal(link('//evil.test/'), 'go')
  assert.equal(link('https://ok.test/a').attrs.href, 'https://ok.test/a')
  assert.equal(richTextNodes(doc(p(t('x', { type: 'link', attrs: { href: 'item:q' } }))), { resolveLink: () => '/r', links: { q: '/l' } })[0].kids[0].attrs.href, '/r')
})

test('void nodes have no children; code blocks keep a safe language class', () => {
  const out = richTextNodes(doc({ type: 'horizontalRule' }, p({ type: 'hardBreak' }), { type: 'codeBlock', attrs: { language: 'ts"x' }, content: [t('1')] }))
  assert.deepEqual(out[0], { tag: 'hr', attrs: {}, kids: undefined })
  assert.equal(out[1].kids[0].kids, undefined)
  assert.equal(out[2].kids[0].attrs.class, undefined)
  assert.equal(richTextNodes(null).length, 0)
})

test('safeHref', () => {
  assert.equal(safeHref('mailto:a@b.c'), 'mailto:a@b.c')
  assert.equal(safeHref('/a/b'), '/a/b')
  assert.equal(safeHref('java\nscript:x'), undefined)
  assert.equal(safeHref('/\\evil'), undefined)
})

test('linkHref resolves item links through resolveLink, then links', () => {
  assert.equal(linkHref({ href: 'item:a' }, { links: { a: '/a' } }), '/a')
  assert.equal(linkHref({ href: 'item:a' }, { links: { a: '/a' }, resolveLink: () => '/r' }), '/r')
  assert.equal(linkHref({ href: 'javascript:1' }), undefined)
  assert.equal(linkHref({}), undefined)
})
