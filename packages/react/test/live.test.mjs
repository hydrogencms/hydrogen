import test from 'node:test'
import assert from 'node:assert/strict'
import { Window } from 'happy-dom'

const ADMIN = 'https://admin.test'
const win = new Window({ url: 'https://site.test/p?preview=tok' })
const sent = []
const parent = { postMessage: (m, o) => sent.push([m, o]) }
Object.defineProperty(win, 'parent', { value: parent })
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Node', 'MutationObserver']) {
  try { globalThis[k] = k === 'window' ? win : win[k] } catch {}
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true
const { createElement: h, act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { useLivePreview, mergeDoc, LivePreview } = await import('../dist/live.js')

const send = (data, o = {}) => win.dispatchEvent(Object.assign(new win.Event('message'), { data, origin: ADMIN, source: parent, ...o }))

test('mergeDoc lays the admin doc over the initial one', () => {
  assert.deepEqual(mergeDoc({ id: 1, links: { a: 'b' }, fields: { t: 1 } }, { fields: { t: 2 } }), { id: 1, links: { a: 'b' }, fields: { t: 2 } })
  assert.deepEqual(mergeDoc('x', [1]), [1])
})

test('useLivePreview: initial, init, patch, resync, rendered after commit', async () => {
  const initial = { links: { a: '/a' }, fields: { title: 'Initial' } }
  const seen = []
  function Page() {
    const d = useLivePreview(initial, { adminOrigin: ADMIN })
    seen.push(d)
    return h('h1', null, d.fields.title)
  }
  const el = win.document.createElement('div')
  win.document.body.appendChild(el)
  const root = createRoot(el)
  await act(async () => root.render(h(Page)))
  assert.equal(el.textContent, 'Initial')
  assert.deepEqual(sent.map(([m]) => m.type), ['ready'])
  assert.equal(sent[0][1], ADMIN)
  assert.equal(sent[0][0].mode, 'live')

  await act(async () => send({ h: 1, type: 'init', item: 'i', itemType: 'page', locale: 'en', rev: 1, doc: { fields: { title: 'Draft' } } }))
  assert.equal(el.textContent, 'Draft')
  assert.equal(seen.at(-1).links.a, '/a')

  await act(async () => send({ h: 1, type: 'patch', rev: 2, base: 1, ops: [{ op: 'replace', path: '/fields/title', value: 'Typed' }] }))
  assert.equal(el.textContent, 'Typed')

  await act(async () => send({ h: 1, type: 'patch', rev: 9, base: 5, ops: [] }))
  assert.equal(el.textContent, 'Typed')
  assert.deepEqual(sent.map(([m]) => m).findLast((m) => m.type === 'resync'), { h: 1, type: 'resync', rev: 2 })

  await act(async () => send({ h: 1, type: 'init', rev: 1, doc: { fields: { title: 'X' } } }, { origin: 'https://evil.test' }))
  assert.equal(el.textContent, 'Typed')

  await new Promise((r) => setTimeout(r, 80))
  const rendered = sent.map(([m]) => m).filter((m) => m.type === 'rendered')
  assert.deepEqual(rendered.map((m) => m.rev), [1, 2])
  await act(async () => root.unmount())
  const n = sent.length
  send({ h: 1, type: 'init', rev: 3, doc: { fields: { title: 'late' } } })
  assert.equal(sent.length, n)
})

test('disabled hook stays inert and returns the initial document', async () => {
  const before = sent.length
  function Page() {
    return h('p', null, useLivePreview({ n: 1 }, { adminOrigin: ADMIN, enabled: false }).n)
  }
  const el = win.document.createElement('div')
  const root = createRoot(el)
  await act(async () => root.render(h(Page)))
  assert.equal(el.textContent, '1')
  assert.equal(sent.length, before)
  await act(async () => root.unmount())
})

test('LivePreview passes the live document to its render function', async () => {
  const el = win.document.createElement('div')
  const root = createRoot(el)
  await act(async () => root.render(h(LivePreview, { initial: { fields: { title: 'A' } }, adminOrigin: ADMIN }, (d) => h('b', null, d.fields.title))))
  assert.equal(el.textContent, 'A')
  await act(async () => root.unmount())
})
