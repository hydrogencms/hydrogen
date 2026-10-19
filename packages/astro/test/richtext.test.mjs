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
    { type: 'orderedList', attrs: { order: 3 }, content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [t('i')] }] }] },
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
