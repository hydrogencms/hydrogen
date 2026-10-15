import { test } from 'node:test'
import assert from 'node:assert/strict'
import { previewHash } from '../dist/hash.js'

test('previewHash is the SHA-256 hex of the token', () => {
  assert.equal(previewHash('abc'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
  assert.match(previewHash('payload.sig'), /^[0-9a-f]{64}$/)
})
