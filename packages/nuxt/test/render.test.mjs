import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createSSRApp, defineComponent, h } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { HydrogenBlocks, HydrogenField, createRegistry, useLinks } from '../dist/blocks.js'
import { HydrogenRichText } from '../dist/richtext.js'
import { HydrogenImage } from '../dist/image.js'

const render = async (c) => (await renderToString(createSSRApp({ render: () => c }))).replace(/<!--.*?-->/g, '')

const Hero = defineComponent({ props: ['props'], setup: (p) => () => h('section', { class: 'hero' }, [h('h2', p.props.heading), h('a', { href: useLinks()[p.props.link] }, 'go')]) })
const Cards = defineComponent({ props: ['props'], setup: (_, { slots }) => () => h('div', slots.items?.()) })
const Card = defineComponent({ props: ['props'], setup: (p) => () => h('article', p.props.title) })
const registry = createRegistry({ hero: Hero, cards: Cards, card: Card })
const blocks = [
  { id: '1', type: 'hero', props: { heading: 'Hi', link: 'x' } },
  { id: '2', type: 'cards', props: {}, slots: { items: [{ id: '3', type: 'card', props: { title: 'A' } }] } },
  { id: '4', type: 'nope', props: {} },
  { id: '5', type: 'shared', props: { item: 'z' }, blocks: [{ id: '6', type: 'card', props: { title: 'S' } }] },
]

test('every block root carries data-h-block, links reach the components, unknown blocks render nothing', async () => {
  const html = await render(h(HydrogenBlocks, { blocks, registry, links: { x: '/en/x' } }))
  assert.match(html, /<section class="hero" data-h-block="hero">/)
  assert.match(html, /href="\/en\/x"/)
  assert.match(html, /<article data-h-block="card">A<\/article>/)
  assert.match(html, /<div style="display:contents;" data-h-block="shared"><article data-h-block="card">S<\/article>/)
  assert.doesNotMatch(html, /nope/)
  assert.doesNotMatch(html, /data-h-path/)
})

test('in preview blocks carry their document path, shared contents none', async () => {
  const html = await render(h(HydrogenBlocks, { blocks, registry, path: 'fields/body', preview: true }))
  assert.match(html, /data-h-block="hero" data-h-path="fields\/body\/0"/)
  assert.match(html, /data-h-block="card" data-h-path="fields\/body\/1\/slots\/items\/0"/)
  assert.match(html, /data-h-block="shared" data-h-path="fields\/body\/3"/)
  assert.match(html, /<article data-h-block="card">S<\/article>/)
  assert.match(html, /<div data-h-block="nope" data-h-path="fields\/body\/2">Unknown block &quot;nope&quot;<\/div>/)
})

test('fields are marked in preview only', async () => {
  assert.equal(await render(h(HydrogenField, { path: 'fields/title', preview: true }, () => 'T')), '<span data-h-path="fields/title">T</span>')
  assert.equal(await render(h(HydrogenField, { path: 'fields/title' }, () => 'T')), 'T')
})

const doc = (...content) => ({ type: 'doc', content })
const link = (href, text) => ({ type: 'text', text, marks: [{ type: 'link', attrs: { href } }] })

test('rich text: structure, item links from `links`, unsafe links dropped', async () => {
  const d = doc(
    { type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'T' }] },
    { type: 'paragraph', content: [link('item:abc', 'in'), link('javascript:alert(1)', 'bad'), link('//evil.com', 'proto'), link('item:missing', 'gone'), link('https://a.b/c?d=1&e=2', 'out'),
      { type: 'text', text: 'b', marks: [{ type: 'bold' }] }] },
    { type: 'orderedList', attrs: { order: 3 }, content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: '<x>' }] }] }] },
  )
  const html = await render(h(HydrogenRichText, { doc: d, links: { abc: '/en/about' } }))
  assert.equal(html, '<h2>T</h2><p><a href="/en/about">in</a>badprotogone<a href="https://a.b/c?d=1&amp;e=2">out</a><strong>b</strong></p><ol start="3"><li><p>&lt;x&gt;</p></li></ol>')
  assert.equal(await render(h(HydrogenRichText, { doc: null })), '')
})

test('image: the preset variant, a focal point and the asset alt', async () => {
  const asset = { url: '/a.jpg', width: 10, height: 5, focal: { x: 0.25, y: 0.5 }, fields: { alt: 'A' }, variants: { card: { url: '/v.webp', width: 4, height: 2, type: 'image/webp', srcset: '/v.webp 4w' } } }
  assert.match(await render(h(HydrogenImage, { asset, preset: 'card' })), /<img loading="lazy" decoding="async" src="\/v.webp" srcset="\/v.webp 4w" width="4" height="2" alt="A" style="object-position:25% 50%">/)
  assert.match(await render(h(HydrogenImage, { asset, alt: 'B', class: 'x' })), /src="\/a.jpg"[^>]*alt="B"/)
})
