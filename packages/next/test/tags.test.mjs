import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ALL_TAG, MAX_TAGS, cacheTags, webhookTags } from '../dist/tags.js'

test('cache tags always carry the purge-everything tag, sorted by kind', () => {
  const { tags, truncated } = cacheTags(['i:a', 'c:page', 'r:corporate', 'nav:corporate', 'i:a'])
  assert.deepEqual(tags, ['*', 'nav:corporate', 'r:corporate', 'c:page', 'i:a'])
  assert.equal(truncated, false)
  assert.deepEqual(cacheTags([]).tags, [ALL_TAG])
})

test('invalid tags are dropped', () => {
  const { tags } = cacheTags(['ok', 'has space', '', 'x'.repeat(257), 'x'.repeat(256), 5])
  assert.deepEqual(tags, ['*', 'x'.repeat(256), 'ok'].sort((a, b) => (a === '*' ? -1 : b === '*' ? 1 : a < b ? -1 : 1)))
})

test('beyond the limit item tags go first and the result is flagged', () => {
  const many = Array.from({ length: 200 }, (_, i) => `i:${String(i).padStart(3, '0')}`)
  const { tags, truncated } = cacheTags([...many, 'r:site', 'nav:site', 'c:page'])
  assert.equal(tags.length, MAX_TAGS)
  assert.equal(truncated, true)
  assert.deepEqual(tags.slice(0, 5), ['*', 'nav:site', 'r:site', 'c:page', 'i:000'])
})

test('webhook tags map one to one onto revalidation tags', () => {
  const body = { id: '7', topic: 'item.published', tags: ['i:a', 'c:article', 'r:corporate', 'nav:corporate', '*', 'i:a'] }
  assert.deepEqual(webhookTags(body), ['i:a', 'c:article', 'r:corporate', 'nav:corporate', '*'])
})

test('malformed webhook bodies yield no tags', () => {
  for (const b of [null, undefined, 3, 'x', {}, { tags: 'i:a' }, { tags: {} }, { tags: [1, null, '', 'a b'] }]) assert.deepEqual(webhookTags(b), [], JSON.stringify(b))
})
