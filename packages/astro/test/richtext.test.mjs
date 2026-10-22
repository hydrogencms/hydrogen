import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { richTextHtml } from '../dist/richtext.js'

const p = (...content) => ({ type: 'doc', content: [{ type: 'paragraph', content }] })
const t = (text, ...marks) => ({ type: 'text', text, marks })
const link = (href) => ({ type: 'link', attrs: { href } })

test('blocks, marks and escaping', () => {
  const doc = { type: 'doc', content: [
    { type: 'heading', attrs: { level: 2 }, content: [t('A <b> & "c"')] },
    { type: 'paragraph', content: [t('x', { type: 'bold' }, { type: 'italic' }), t(' y')] },
    { type: 'orderedList', attrs: { start: 3 }, content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [t('i')] }] }] },
    { type: 'codeBlock', attrs: { language: 'js"x' }, content: [t('1 < 2')] },
    { type: 'horizontalRule' },
  ] }
  assert.equal(
    richTextHtml(doc),
    '<h2>A &lt;b&gt; &amp; &quot;c&quot;</h2><p><strong><em>x</em></strong> y</p><ol start="3"><li><p>i</p></li></ol><pre><code>1 &lt; 2</code></pre><hr>',
  )
})

test('links: item ids come from links, unsafe schemes lose the anchor', () => {
  const links = { abc: '/en/about' }
  assert.equal(richTextHtml(p(t('a', link('item:abc'))), { links }), '<p><a href="/en/about">a</a></p>')
  assert.equal(richTextHtml(p(t('a', link('item:zzz'))), { links }), '<p>a</p>')
  assert.equal(richTextHtml(p(t('a', link('https://x.test/?a=1&b="2"')))), '<p><a href="https://x.test/?a=1&amp;b=&quot;2&quot;">a</a></p>')
  for (const bad of ['javascript:alert(1)', ' java\tscript:alert(1)', '//evil.test', '\\\\evil', 'data:text/html,x']) {
    assert.equal(richTextHtml(p(t('a', link(bad)))), '<p>a</p>', bad)
  }
  assert.equal(richTextHtml(p(t('a', link('item:q'))), { resolveLink: () => '/x' }), '<p><a href="/x">a</a></p>')
})

test('overrides replace a node type; empty and missing docs render nothing', () => {
  const o = { render: { paragraph: (_a, html) => `<div class="p">${html}</div>` } }
  assert.equal(richTextHtml(p(t('x')), o), '<div class="p">x</div>')
  assert.equal(richTextHtml(null), '')
  assert.equal(richTextHtml({ type: 'doc' }), '')
})

const tbl = (img) => ({ type: 'doc', content: [
  { type: 'table', content: [
    { type: 'tableRow', content: [{ type: 'tableHeader', attrs: { colspan: 2, rowspan: 1 }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'h' }] }] }] },
    { type: 'tableRow', content: [{ type: 'tableCell', attrs: { rowspan: 3 }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'u', marks: [{ type: 'underline' }, { type: 'strike' }, { type: 'superscript' }, { type: 'subscript' }] }] }] }] },
  ] },
  img,
] })

test('tables, extra marks and images', () => {
  assert.equal(
    richTextHtml(tbl({ type: 'image', attrs: { src: '/a.png?x=1&y=2', alt: 'A"', title: 'T' } })),
    '<table><tbody><tr><th colspan="2"><p>h</p></th></tr><tr><td rowspan="3"><p><u><s><sup><sub>u</sub></sup></s></u></p></td></tr></tbody></table><img src="/a.png?x=1&amp;y=2" alt="A&quot;" title="T" loading="lazy">',
  )
  assert.equal(richTextHtml(tbl({ type: 'image', attrs: { src: 'javascript:x' } })).endsWith('</table>'), true)
})

const fixture = JSON.parse(readFileSync(new URL('../../client/test/fixtures/richtext-ext.json', import.meta.url), 'utf8'))
for (const c of fixture) test('fixture: ' + c.name, () => assert.equal(richTextHtml(c.doc, c.options), c.html))

test('render overrides get the attrs and the inner HTML', () => {
  assert.equal(richTextHtml(p(t('a', { type: 'bold' })), { render: { bold: (a, html) => `<b>${html}</b>`, paragraph: (a, html) => `<div>${html}</div>` } }), '<div><b>a</b></div>')
})
