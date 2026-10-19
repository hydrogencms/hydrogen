import { test } from 'node:test'
import assert from 'node:assert/strict'
import { adminOriginWarning } from '../dist/admin.js'

test('adminOriginWarning: local and public https are fine, http and internal hosts are warned about', () => {
  for (const ok of ['http://localhost:8080', 'http://127.0.0.1:1', 'https://admin.example.com']) assert.equal(adminOriginWarning(ok), undefined, ok)
  assert.match(adminOriginWarning('http://admin.example.com'), /not https/)
  for (const bad of ['https://cms', 'https://cms.internal', 'http://10.0.0.5:8080', 'https://192.168.1.2', 'https://admin.corp.local']) assert.match(adminOriginWarning(bad), /internal/, bad)
})
