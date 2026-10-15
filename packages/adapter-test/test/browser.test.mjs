import { test } from 'node:test'
import assert from 'node:assert/strict'
import { checkPreviewProof, tokenHash } from '../dist/browser.js'

test('tokenHash is the SHA-256 hex', () => {
  assert.equal(tokenHash('abc'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
})

test('a ready proves the token only with the hash of that token', () => {
  const tok = 'payload.sig'
  checkPreviewProof({ type: 'ready', preview: tokenHash(tok) }, tok)
  checkPreviewProof({ type: 'ready', preview: tokenHash(tok).toUpperCase() }, tok)
  for (const ready of [{ type: 'ready' }, { type: 'ready', preview: '1' }, { type: 'ready', preview: tokenHash('other') }, { type: 'ready', preview: tok }, { type: 'ready', preview: 5 }, undefined])
    assert.throws(() => checkPreviewProof(ready, tok), /doesn't prove the preview token/, JSON.stringify(ready))
})
