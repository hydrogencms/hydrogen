import test from 'node:test'
import assert from 'node:assert/strict'
import { createElement as h } from 'react'
import { renderToStaticMarkup as html } from 'react-dom/server'
import { Blocks, Block, Field, RichText, createRegistry, pathAttrs } from '../dist/index.js'

const doc = (...content) => ({ type: 'doc', content })
const p = (...content) => ({ type: 'paragraph', content })
const t = (text, ...marks) => ({ type: 'text', text, marks: marks.map((m) => (typeof m === 'string' ? { type: m } : m)) })
const rt = (d, o = {}) => html(h(RichText, { doc: d, ...o }))

test('RichText renders the contract nodes', () => {
  assert.equal(rt(doc(
    { type: 'heading', attrs: { level: 2 }, content: [t('Hi')] },
    p(t('a'), { type: 'hardBreak' }, t('b')),
    { type: 'bulletList', content: [{ type: 'listItem', content: [p(t('x'))] }] },
    { type: 'orderedList', attrs: { order: 3 }, content: [{ type: 'listItem', content: [p(t('y'))] }] },
    { type: 'blockquote', content: [p(t('q'))] },
    { type: 'codeBlock', attrs: { language: 'go' }, content: [t('x := 1')] },
    { type: 'horizontalRule' },
  )), '<h2>Hi</h2><p>a<br/>b</p><ul><li><p>x</p></li></ul><ol start="3"><li><p>y</p></li></ol><blockquote><p>q</p></blockquote><pre><code class="language-go">x := 1</code></pre><hr/>')
})

test('RichText marks nest and text is escaped', () => {
  assert.equal(rt(doc(p(t('<b>&', 'bold', 'italic', 'code')))), '<p><strong><em><code>&lt;b&gt;&amp;</code></em></strong></p>')
  assert.equal(rt(doc({ type: 'heading', attrs: { level: 9 }, content: [t('x')] })), '<h6>x</h6>')
})

test('RichText links: http(s), mailto, relative; unsafe schemes dropped', () => {
  const l = (href) => rt(doc(p(t('x', { type: 'link', attrs: { href } }))))
  assert.equal(l('https://a.test/?q=1&r="2"'), '<p><a href="https://a.test/?q=1&amp;r=&quot;2&quot;">x</a></p>')
  assert.equal(l('mailto:a@b.test'), '<p><a href="mailto:a@b.test">x</a></p>')
  assert.equal(l('/about#top'), '<p><a href="/about#top">x</a></p>')
  assert.equal(l('page/2'), '<p><a href="page/2">x</a></p>')
  for (const bad of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', ' java\tscript:alert(1)', 'data:text/html,x', 'vbscript:x', '//evil.test/x', ' //evil.test', '/\\evil.test', 'https:\\\\evil.test', '\\\\evil.test', '']) assert.equal(l(bad), '<p>x</p>', bad)
})

test('RichText resolves item: links via resolveLink, then links; unresolved drop the anchor', () => {
  const d = doc(p(t('x', { type: 'link', attrs: { href: 'item:abc' } })))
  assert.equal(rt(d, { links: { abc: '/en/about' } }), '<p><a href="/en/about">x</a></p>')
  assert.equal(rt(d, { links: { 'item:abc': '/en/full' } }), '<p><a href="/en/full">x</a></p>')
  assert.equal(rt(d, { links: { abc: '/en/about' }, resolveLink: (h) => (h === 'item:abc' ? '/custom' : undefined) }), '<p><a href="/custom">x</a></p>')
  assert.equal(rt(d, { resolveLink: () => undefined }), '<p>x</p>')
  assert.equal(rt(d, { links: { abc: 'javascript:alert(1)' } }), '<p>x</p>')
})

test('RichText: unknown nodes/marks keep their content, null doc renders nothing, overrides apply', () => {
  assert.equal(rt(doc({ type: 'weird', content: [p(t('in', 'sparkle'))] })), '<p>in</p>')
  assert.equal(rt(null), '')
  assert.equal(rt(doc({ type: 'heading', attrs: { level: 3 }, content: [t('H')] }), { render: { heading: ({ attrs, children }) => h('div', { 'data-l': attrs.level }, children) } }), '<div data-l="3">H</div>')
  assert.equal(rt(doc(p(t('b', 'bold'))), { render: { bold: ({ children }) => h('b', null, children) } }), '<p><b>b</b></p>')
})

test('RichText never emits raw HTML from text', () => {
  assert.ok(!rt(doc(p(t('<script>alert(1)</script>')))).includes('<script>'))
})

const Hero = ({ props, attrs, slots }) => h('section', attrs, h('h2', null, props.heading), slots.cta)
const Cards = ({ props, attrs, slots }) => h('div', attrs, props.heading, slots.items)
const Card = ({ props, attrs }) => h('article', attrs, props.title)
const registry = createRegistry({ hero: Hero, cards: Cards, card: Card })
const blocks = [
  { id: 'a', type: 'hero', props: { heading: 'Hello' } },
  { id: 'b', type: 'cards', props: { heading: 'C' }, slots: { items: [{ id: 'c', type: 'card', props: { title: 'one' } }, { id: 'd', type: 'card', props: { title: 'two' } }] } },
]

test('Blocks: production adds only data-h-block', () => {
  assert.equal(html(h(Blocks, { blocks, registry, path: 'fields/body' })),
    '<section data-h-block="hero"><h2>Hello</h2></section><div data-h-block="cards">C<article data-h-block="card">one</article><article data-h-block="card">two</article></div>')
})

test('Blocks: preview adds data-h-path for blocks and nested slots', () => {
  assert.equal(html(h(Blocks, { blocks, registry, path: 'fields/body', preview: true })),
    '<section data-h-block="hero" data-h-path="fields/body/0"><h2>Hello</h2></section>' +
    '<div data-h-block="cards" data-h-path="fields/body/1">C<article data-h-block="card" data-h-path="fields/body/1/slots/items/0">one</article><article data-h-block="card" data-h-path="fields/body/1/slots/items/1">two</article></div>')
})

test('Block renders a single block; null blocks render nothing', () => {
  assert.equal(html(h(Block, { block: blocks[0], registry, path: 'fields/body/0', preview: true })), '<section data-h-block="hero" data-h-path="fields/body/0"><h2>Hello</h2></section>')
  assert.equal(html(h(Blocks, { blocks: null, registry })), '')
})

test('unknown types: warn once, render nothing outside preview, a marker in preview, custom fallback', () => {
  const warns = []
  const orig = console.warn
  console.warn = (m) => warns.push(m)
  try {
    const r = createRegistry({})
    const bs = [{ id: 'x', type: 'mystery' }, { id: 'y', type: 'mystery' }]
    assert.equal(html(h(Blocks, { blocks: bs, registry: r })), '')
    assert.equal(html(h(Blocks, { blocks: bs, registry: r, path: 'fields/body', preview: true })),
      '<div data-h-block="mystery" data-h-path="fields/body/0">Unknown block &quot;mystery&quot;</div><div data-h-block="mystery" data-h-path="fields/body/1">Unknown block &quot;mystery&quot;</div>')
    assert.equal(warns.length, 1)
    assert.match(warns[0], /mystery/)
    const r2 = createRegistry({}, { fallback: ({ block, attrs }) => h('i', attrs, block.type) })
    assert.equal(html(h(Blocks, { blocks: bs.slice(0, 1), registry: r2 })), '<i data-h-block="mystery">mystery</i>')
  } finally { console.warn = orig }
})

test('shared blocks render their inlined blocks, read-only (no data-h-path inside)', () => {
  const shared = { id: 's', type: 'shared', props: { item: 'item:1' }, blocks: [{ id: 'h', type: 'hero', props: { heading: 'Shared' } }] }
  assert.equal(html(h(Blocks, { blocks: [shared], registry, path: 'fields/body', preview: true })),
    '<div data-h-block="shared" data-h-path="fields/body/0" style="display:contents"><section data-h-block="hero"><h2>Shared</h2></section></div>')
  const custom = createRegistry({ hero: Hero, shared: ({ slots }) => h('aside', null, slots.blocks) })
  assert.equal(html(h(Blocks, { blocks: [shared], registry: custom })), '<aside><section data-h-block="hero"><h2>Shared</h2></section></aside>')
})

test('Field and pathAttrs', () => {
  assert.equal(html(h(Field, { path: 'fields/title' }, 'T')), 'T')
  assert.equal(html(h(Field, { path: 'fields/title', preview: true }, 'T')), '<span data-h-path="fields/title">T</span>')
  assert.equal(html(h(Field, { path: 'fields/title', preview: true, as: 'h1' }, 'T')), '<h1 data-h-path="fields/title">T</h1>')
  assert.deepEqual(pathAttrs('a', true), { 'data-h-path': 'a' })
  assert.deepEqual(pathAttrs('a', false), {})
})

test('Image: variant, defaults, focal point, overrides', async () => {
  const { Image } = await import('../dist/index.js')
  const a = { url: '/o.jpg', width: 1000, height: 500, focal: { x: 0.25, y: 0.5 }, fields: { alt: 'Cat' },
    variants: { card: { url: '/c.jpg', width: 640, height: 320, type: 'image/jpeg', srcset: '/c1.jpg 320w, /c.jpg 640w' } } }
  assert.equal(html(h(Image, { asset: a, preset: 'card', sizes: '100vw' })),
    '<img loading="lazy" decoding="async" src="/c.jpg" srcSet="/c1.jpg 320w, /c.jpg 640w" width="640" height="320" alt="Cat" sizes="100vw" style="object-position:25% 50%"/>')
  const o = html(h(Image, { asset: { url: '/o.jpg', fields: {} }, alt: 'X', loading: 'lazy', decoding: 'sync', style: { color: 'red' } }))
  assert.equal(o, '<img loading="lazy" decoding="sync" src="/o.jpg" alt="X" style="color:red"/>')
  assert.match(html(h(Image, { asset: a, style: { color: 'red' } })), /style="object-position:25% 50%;color:red"/)
})

test('RichText tables, extra marks and images', () => {
  const cell = (type, attrs, text, marks) => ({ type, attrs, content: [{ type: 'paragraph', content: [{ type: 'text', text, marks }] }] })
  const d = (src) => ({ type: 'doc', content: [
    { type: 'table', content: [{ type: 'tableRow', content: [cell('tableHeader', { colspan: 2, rowspan: 1 }, 'h'), cell('tableCell', { rowspan: 3 }, 'u', [{ type: 'underline' }, { type: 'subscript' }])] }] },
    { type: 'image', attrs: { src, alt: 'A', width: 5 } },
  ] })
  assert.equal(rt(d('/a.png')),
    '<table><tbody><tr><th colSpan="2"><p>h</p></th><td rowSpan="3"><p><u><sub>u</sub></u></p></td></tr></tbody></table><img src="/a.png" alt="A" width="5" loading="lazy"/>')
  assert.ok(!rt(d('javascript:x')).includes('<img'))
})
