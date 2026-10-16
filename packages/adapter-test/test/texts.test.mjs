import { test } from 'node:test'
import assert from 'node:assert/strict'
import { blockPaths, canvasTexts } from '../dist/texts.js'

const body = [
  { type: 'hero', props: { heading: 'Hi', subheading: 'Sub', ctaLabel: '' } },
  { type: 'shared', props: {} },
  { type: 'cards', props: { heading: 'Cs' }, slots: { items: [{ type: 'card', props: { title: 'A', text: 'x' } }] } },
]

test('block paths skip shared blocks and keep indexes', () => {
  assert.deepEqual(blockPaths(body).map((b) => b.path), ['fields/body/0', 'fields/body/2', 'fields/body/2/slots/items/0'])
})

test('texts: string and text props, empty skipped, sorted, scoped', () => {
  const t = canvasTexts('landing', { title: 'T', slug: 's', body })
  assert.deepEqual(t.map((x) => [x.path, x.scope]), [
    ['fields/body/0/props/heading', 'fields/body/0'],
    ['fields/body/0/props/subheading', 'fields/body/0'],
    ['fields/body/2/props/heading', 'fields/body/2'],
    ['fields/body/2/slots/items/0/props/text', 'fields/body/2/slots/items/0'],
    ['fields/body/2/slots/items/0/props/title', 'fields/body/2/slots/items/0'],
    ['fields/title', ''],
  ])
})

test('post texts include the excerpt', () => {
  assert.deepEqual(canvasTexts('post', { title: 'T', excerpt: 'E', body: [] }).map((x) => x.path), ['fields/excerpt', 'fields/title'])
})
