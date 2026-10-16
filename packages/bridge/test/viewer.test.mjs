import test from 'node:test'
import assert from 'node:assert/strict'
import { createBridge } from '../dist/index.js'

const ADMIN = 'https://admin.test'
const PROOF = 'a'.repeat(64)
const tick = () => new Promise((r) => setTimeout(r, 5))

function setup({ proof = PROOF, es = true, url = 'https://site.test/p?preview=hps_x' } = {}) {
  const sources = []
  class ES extends EventTarget {
    constructor(u) {
      super()
      this.url = u
      this.closed = false
      sources.push(this)
    }
    close() {
      this.closed = true
    }
    emit(type, data) {
      this.dispatchEvent(Object.assign(new Event(type), { data: JSON.stringify(data) }))
    }
  }
  const w = {
    location: Object.assign(new URL(url), { reload: () => w.reloads++ }),
    document: { cookie: '', querySelector: (q) => (q === 'meta[name="hydrogen-preview"]' && proof ? { getAttribute: () => proof } : null) },
    performance: { now: () => Date.now() },
    addEventListener() {},
    removeEventListener() {},
    reloads: 0,
  }
  w.parent = w
  if (es) w.EventSource = ES
  return { w, sources }
}
const item = (title) => ({ id: 'i1', type: 'page', locale: 'en', fields: { title } })

test('viewer live: opens the events URL with the proof; draft calls onInit', () => {
  const { w, sources } = setup()
  const inits = []
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onInit: (i) => inits.push(i) })
  b.connect()
  assert.equal(sources.length, 1)
  assert.equal(sources[0].url, `${ADMIN}/api/deliver/v1/preview/events?proof=${PROOF}`)
  sources[0].emit('draft', { rev: 3, item: item('A') })
  sources[0].emit('draft', { rev: 4, item: item('B') })
  assert.equal(inits.length, 2)
  assert.deepEqual(inits[1], { item: 'i1', itemType: 'page', locale: 'en', rev: 4, doc: item('B') })
  b.rendered(4)
  b.connect()
  assert.equal(sources.length, 1)
})

test('viewer: end reloads, disconnect closes', () => {
  const { w, sources } = setup()
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w })
  b.connect()
  sources[0].emit('end', { reason: 'revoked' })
  assert.equal(w.reloads, 1)
  assert.equal(sources[0].closed, true)
  b.disconnect()
  assert.equal(sources[0].closed, true)
})

test('viewer reload mode: skips the first draft and repeats of a revision, swaps after', async () => {
  const { w, sources } = setup()
  let fetched = 0
  w.fetch = async () => (fetched++, { ok: false, status: 500, statusText: 'x' })
  const b = createBridge({ adminOrigin: ADMIN, mode: 'reload', window: w })
  b.connect()
  sources[0].emit('draft', { rev: 1, item: item('A') })
  sources[0].emit('draft', { rev: 1, item: item('A') })
  await tick()
  assert.equal(fetched, 0)
  sources[0].emit('draft', { rev: 2, item: item('B') })
  await tick()
  assert.equal(fetched, 1)
  assert.equal(w.reloads, 1)
})

test('viewer stays off without a proof, EventSource or with relay: false', () => {
  for (const [s, o] of [[{ proof: '' }, {}], [{ es: false }, {}], [{}, { relay: false }]]) {
    const { w, sources } = setup(s)
    createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, ...o }).connect()
    assert.equal(sources.length, 0)
  }
})

test('relay option overrides the URL', () => {
  const { w, sources } = setup()
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, relay: 'https://cms.test/x/events' }).connect()
  assert.equal(sources[0].url, `https://cms.test/x/events?proof=${PROOF}`)
})

test('viewer reload mode: dedupes on v, falling back to rev', async () => {
  const { w, sources } = setup()
  let fetched = 0
  w.fetch = async () => (fetched++, { ok: false, status: 500, statusText: 'x' })
  createBridge({ adminOrigin: ADMIN, mode: 'reload', window: w }).connect()
  const e = (d) => sources[0].emit('draft', { item: item('A'), ...d })
  e({ rev: 1, v: 'a' })
  e({ rev: 2, v: 'a' })
  await tick()
  assert.equal(fetched, 0)
  e({ rev: 2, v: 'b' })
  await tick()
  assert.equal(fetched, 1)
  e({ rev: 3 })
  await tick()
  assert.equal(fetched, 2)
})

test('viewer reload mode: swaps are serialized, one queued, the latest wins', async () => {
  const { w, sources } = setup()
  const pending = []
  w.fetch = () => new Promise((r) => pending.push(r))
  createBridge({ adminOrigin: ADMIN, mode: 'reload', window: w }).connect()
  const e = (rev) => sources[0].emit('draft', { rev, item: item('A') })
  e(1)
  e(2)
  e(3)
  e(4)
  await tick()
  assert.equal(pending.length, 1)
  pending.shift()({ ok: false, status: 500, statusText: 'x' })
  await tick()
  assert.equal(pending.length, 1, 'exactly one queued swap runs after the first')
  pending.shift()({ ok: false, status: 500, statusText: 'x' })
  await tick()
  assert.equal(pending.length, 0)
  assert.equal(w.reloads, 2)
})

test('viewer mode posts nothing on error', () => {
  const { w, sources } = setup()
  let posted = 0
  w.postMessage = () => posted++
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onInit: () => { throw new Error('boom') } })
  b.connect()
  sources[0].emit('draft', { rev: 1, item: item('A') })
  b.error('x')
  assert.equal(posted, 0)
})
