import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { linkHref, richTextNodes, safeHref } from '../dist/richtext.js'

const eq = (a, b) => assert.deepEqual(JSON.parse(JSON.stringify(a)), JSON.parse(JSON.stringify(b)))
const doc = (...content) => ({ type: 'doc', content })
const p = (...content) => ({ type: 'paragraph', content })
const t = (text, ...marks) => ({ type: 'text', text, marks: marks.map((m) => (typeof m === 'string' ? { type: m } : m)) })

test('paragraphs, headings, marks and lists', () => {
  const out = richTextNodes(doc({ type: 'heading', attrs: { level: 9 }, content: [t('H')] }, p(t('a ', 'bold'), t('b', 'italic', 'bold')), { type: 'orderedList', attrs: { start: 3 }, content: [{ type: 'listItem', content: [p(t('x'))] }] }))
  eq(out[0], { tag: 'h6', attrs: {}, kids: ['H'] })
  eq(out[1].kids[0], { tag: 'strong', attrs: {}, kids: ['a '] })
  eq(out[1].kids[1], { tag: 'em', attrs: {}, kids: [{ tag: 'strong', attrs: {}, kids: ['b'] }] })
  assert.equal(out[2].tag, 'ol')
  assert.equal(out[2].attrs.start, 3)
})

test('item links come from delivery, other schemes are dropped', () => {
  const link = (href) => richTextNodes(doc(p(t('go', { type: 'link', attrs: { href } }))), { links: { abc: '/en/about' } })[0].kids[0]
  eq(link('item:abc'), { tag: 'a', attrs: { href: '/en/about' }, kids: ['go'] })
  assert.equal(link('item:unknown'), 'go')
  assert.equal(link('javascript:alert(1)'), 'go')
  assert.equal(link('//evil.test/'), 'go')
  assert.equal(link('https://ok.test/a').attrs.href, 'https://ok.test/a')
  assert.equal(richTextNodes(doc(p(t('x', { type: 'link', attrs: { href: 'item:q' } }))), { resolveLink: () => '/r', links: { q: '/l' } })[0].kids[0].attrs.href, '/r')
})

test('void nodes have no children; code blocks keep a safe language class', () => {
  const out = richTextNodes(doc({ type: 'horizontalRule' }, p({ type: 'hardBreak' }), { type: 'codeBlock', attrs: { language: 'ts"x' }, content: [t('1')] }))
  eq(out[0], { tag: 'hr', attrs: {}, kids: undefined })
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

const tbl = (img) => ({ type: 'doc', content: [
  { type: 'table', content: [
    { type: 'tableRow', content: [{ type: 'tableHeader', attrs: { colspan: 2, rowspan: 1 }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'h' }] }] }] },
    { type: 'tableRow', content: [{ type: 'tableCell', attrs: { rowspan: 3 }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'u', marks: [{ type: 'underline' }, { type: 'strike' }, { type: 'superscript' }, { type: 'subscript' }] }] }] }] },
  ] },
  img,
] })

test('tables, extra marks and images', () => {
  const out = richTextNodes(tbl({ type: 'image', attrs: { src: 'https://a.b/x.png', alt: 'A', width: 10 } }))
  const th = out[0].kids[0].kids[0].kids[0]
  eq([out[0].tag, out[0].kids[0].tag, th.tag, th.attrs], ['table', 'tbody', 'th', { colspan: 2, rowspan: undefined }])
  const td = out[0].kids[0].kids[1].kids[0]
  assert.equal(td.attrs.rowspan, 3)
  eq(['u', 's', 'sup', 'sub'].reduce(([n, tags], tag) => [n.kids[0], [...tags, n.tag]], [td.kids[0].kids[0], []])[1], ['u', 's', 'sup', 'sub'])
  eq(out[1], { tag: 'img', attrs: { src: 'https://a.b/x.png', alt: 'A', title: undefined, width: 10, height: undefined, loading: 'lazy' } })
  for (const src of ['javascript:x', 'data:image/png;base64,AA', '//evil.test/a.png', '/\\evil', '']) eq(richTextNodes(tbl({ type: 'image', attrs: { src } })).slice(1), [])
  assert.equal(richTextNodes(tbl({ type: 'image', attrs: { src: '/assets/x/y.png' } }))[1].attrs.src, '/assets/x/y.png')
})

// richtext-ext.json is the parity fixture of every renderer (this walk, React, Astro, Nuxt, the Go site module).
// Canonical HTML: attributes in the order id, class, style, the element's own (start, href, target, rel, src, colspan…),
// then data-* in declared order; void elements as `<br>`; `& < >` escaped in text, `& < > "` in attributes.
const E = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => E[c])
const html = (ns) => ns.map((n) => typeof n === 'string' ? esc(n)
  : `<${n.tag}${Object.entries(n.attrs).map(([k, v]) => (v === undefined ? '' : ` ${k}="${esc(v)}"`)).join('')}>${n.kids ? html(n.kids) + `</${n.tag}>` : ''}`).join('')
const cases = JSON.parse(readFileSync(new URL('./fixtures/richtext-ext.json', import.meta.url), 'utf8'))

for (const c of cases) test('fixture: ' + c.name, () => assert.equal(html(richTextNodes(c.doc, c.options)), c.html))

test('render overrides replace a type with plain nodes', () => {
  const out = richTextNodes(doc(p(t('a', 'bold'))), { render: { paragraph: (a, kids) => [{ tag: 'div', attrs: {}, kids }], bold: () => [] } })
  assert.equal(html(out), '<div></div>')
})

test('resolveLink receives the bare item reference', () => {
  const seen = []
  linkHref({ href: 'item:abc#frag' }, { resolveLink: (h) => (seen.push(h), '/x') })
  assert.deepEqual(seen, ['item:abc'])
})
