import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { Window } from 'happy-dom'
import { createBridge, isPreview, previewProof, reloadSwap, autoStart, SDK } from '../dist/index.js'

const ADMIN = 'https://admin.test'

function fake({ url = 'https://site.test/p?preview=tok', framed = true, cookie = '' } = {}) {
  const t = new EventTarget()
  const sent = []
  const parent = { postMessage: (m, o) => sent.push([m, o]) }
  const w = Object.assign(t, {
    location: new URL(url), href: url,
    document: { cookie, querySelector: () => null },
    performance: { now: () => Date.now() },
    requestAnimationFrame: (f) => queueMicrotask(f),
  })
  w.parent = framed ? parent : w
  w.postMessage = () => {}
  w.send = (data, { origin = ADMIN, source = w.parent } = {}) => w.dispatchEvent(Object.assign(new Event('message'), { data, origin, source }))
  return { w, sent }
}
const tick = () => new Promise((r) => setTimeout(r, 5))

test('connect posts ready to the admin origin only', () => {
  const { w, sent } = fake()
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w }).connect()
  assert.equal(sent.length, 1)
  assert.equal(sent[0][1], ADMIN)
  assert.deepEqual({ ...sent[0][0], url: 0 }, { h: 1, type: 'ready', mode: 'live', edit: true, sdk: SDK, url: 0 })
  assert.match(SDK, new RegExp('@' + JSON.parse(readFileSync(new URL('../package.json', import.meta.url))).version.replace(/\./g, '\\.') + '$'))
})

test('inert outside a frame or outside preview', () => {
  for (const o of [{ framed: false }, { url: 'https://site.test/p' }]) {
    const { w, sent } = fake(o)
    let inits = 0
    const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onInit: () => inits++ })
    b.connect()
    w.send({ h: 1, type: 'init', rev: 1, doc: {} })
    b.rendered(1)
    assert.equal(sent.length, 0)
    assert.equal(inits, 0)
  }
})

test('preview detection: query, readable cookie, meta tag, explicit option', () => {
  assert.equal(isPreview(fake().w), true)
  assert.equal(isPreview(fake({ url: 'https://s.test/', cookie: 'a=1; hydrogen-preview=tok' }).w), true)
  assert.equal(isPreview(fake({ url: 'https://s.test/' }).w), false)
  const m = fake({ url: 'https://s.test/' }).w
  m.document.querySelector = (q) => (q === 'meta[name="hydrogen-preview"]' ? {} : null)
  assert.equal(isPreview(m), true)
  const { w, sent } = fake({ url: 'https://s.test/' })
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, preview: true }).connect()
  assert.equal(sent.length, 1)
})

test('rejects bad admin origins', () => {
  for (const o of ['*', 'admin.test', 'https://admin.test/path', '']) assert.throws(() => createBridge({ adminOrigin: o, mode: 'live', window: fake().w }))
})

test('messages from other origins, sources or without h:1 are ignored', () => {
  const { w, sent } = fake()
  const got = []
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onInit: (m) => got.push(m) }).connect()
  const init = { h: 1, type: 'init', item: 'i', itemType: 'page', locale: 'en', rev: 1, doc: { a: 1 } }
  w.send(init, { origin: 'https://evil.test' })
  w.send(init, { source: {} })
  w.send({ ...init, h: 2 })
  w.send('init')
  w.send(null)
  assert.equal(got.length, 0)
  assert.equal(sent.length, 1)
  w.send(init)
  assert.equal(got.length, 1)
  assert.deepEqual(got[0].doc, { a: 1 })
  assert.equal(got[0].itemType, 'page')
})

test('patch flow: apply when base matches, resync otherwise', () => {
  const { w, sent } = fake()
  const docs = []
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onPatch: (d, p) => docs.push([d, p.rev]) }).connect()
  w.send({ h: 1, type: 'patch', rev: 2, base: 1, ops: [] })
  assert.deepEqual(sent.at(-1)[0], { h: 1, type: 'resync', rev: -1 })
  w.send({ h: 1, type: 'init', rev: 1, doc: { a: [1] } })
  w.send({ h: 1, type: 'patch', rev: 2, base: 1, ops: [{ op: 'add', path: '/a/-', value: 2 }] })
  w.send({ h: 1, type: 'patch', rev: 3, base: 2, ops: [{ op: 'replace', path: '/a/0', value: 9 }] })
  assert.deepEqual(docs, [[{ a: [1, 2] }, 2], [{ a: [9, 2] }, 3]])
  w.send({ h: 1, type: 'patch', rev: 7, base: 5, ops: [] })
  assert.deepEqual(sent.at(-1)[0], { h: 1, type: 'resync', rev: 3 })
  assert.equal(docs.length, 2)
  w.send({ h: 1, type: 'init', rev: 7, doc: { a: [] } })
  w.send({ h: 1, type: 'patch', rev: 8, base: 7, ops: [{ op: 'add', path: '/a/-', value: 1 }] })
  assert.deepEqual(docs.at(-1), [{ a: [1] }, 8])
})

test('a patch that does not apply reports an error and resyncs, keeping the doc', () => {
  const { w, sent } = fake()
  const docs = []
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onPatch: (d) => docs.push(d) }).connect()
  w.send({ h: 1, type: 'init', rev: 1, doc: { a: 1 } })
  w.send({ h: 1, type: 'patch', rev: 2, base: 1, ops: [{ op: 'remove', path: '/zz' }] })
  assert.equal(docs.length, 0)
  const types = sent.slice(1).map(([m]) => m.type)
  assert.deepEqual(types, ['error', 'resync'])
  assert.match(sent[1][0].message, /no such path/)
  assert.deepEqual(sent[2][0], { h: 1, type: 'resync', rev: 1 })
})

test('rendered posts after two frames with ms; disconnect stops listening', async () => {
  const { w, sent } = fake()
  const got = []
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w, onInit: (m) => got.push(m) })
  b.connect()
  w.send({ h: 1, type: 'init', rev: 4, doc: {} })
  b.rendered(4)
  assert.equal(sent.length, 1)
  await tick()
  const r = sent[1][0]
  assert.equal(r.type, 'rendered')
  assert.equal(r.rev, 4)
  assert.equal(typeof r.ms, 'number')
  b.rendered(4, 12)
  await tick()
  assert.equal(sent[2][0].ms, 12)
  b.disconnect()
  w.send({ h: 1, type: 'init', rev: 5, doc: {} })
  assert.equal(got.length, 1)
  b.rendered(5)
  await tick()
  assert.equal(sent.length, 3)
})

test('error() posts an error message', () => {
  const { w, sent } = fake()
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w })
  b.connect()
  b.error('boom')
  assert.deepEqual(sent[1][0], { h: 1, type: 'error', message: 'boom' })
})

test('onReload overrides the default reload handling', () => {
  const { w } = fake()
  const revs = []
  createBridge({ adminOrigin: ADMIN, mode: 'reload', window: w, onReload: (r) => revs.push(r) }).connect()
  w.send({ h: 1, type: 'reload', rev: 3 })
  assert.deepEqual(revs, [3])
})

function dom(html) {
  const win = new Window({ url: 'https://site.test/p?preview=tok' })
  win.document.write(html)
  return win
}
const page = (title, body) => `<!doctype html><html><head><title>${title}</title></head><body>${body}</body></html>`
const resp = (html, ok = true) => async () => ({ ok, status: ok ? 200 : 500, statusText: 'x', text: async () => html })

test('reloadSwap replaces [data-h-root], keeps the rest, scroll and reports ms', async () => {
  const win = dom(page('Old', '<header id=h>head</header><main data-h-root><h1>Old</h1></main>'))
  const calls = []
  let scrolled
  win.scrollTo = (x, y) => (scrolled = [x, y])
  Object.defineProperty(win, 'scrollY', { value: 120 })
  const keep = win.document.getElementById('h')
  let ready = 0
  const ms = await reloadSwap({
    window: win, ready: () => ready++,
    fetch: async (u, init) => (calls.push([u, init.credentials]), resp(page('New', '<header>x</header><main data-h-root><h1>New</h1></main>'))()),
  })
  assert.deepEqual(calls, [['https://site.test/p?preview=tok', 'include']])
  assert.equal(win.document.querySelector('h1').textContent, 'New')
  assert.equal(win.document.getElementById('h'), keep)
  assert.equal(win.document.title, 'New')
  assert.deepEqual(scrolled, [0, 120])
  assert.equal(ready, 1)
  assert.equal(typeof ms, 'number')
})

test('reloadSwap without a root replaces the body children; a custom root selector works', async () => {
  const win = dom(page('Old', '<p>old</p>'))
  await reloadSwap({ window: win, fetch: resp(page('N', '<p>new</p><i>2</i>')) })
  assert.equal(win.document.body.innerHTML, '<p>new</p><i>2</i>')
  const w2 = dom(page('Old', '<div id=a>old</div><div id=b>keep</div>'))
  await reloadSwap({ window: w2, root: '#a', fetch: resp(page('N', '<div id=a>new</div><div id=b>x</div>')) })
  assert.equal(w2.document.getElementById('a').textContent, 'new')
  assert.equal(w2.document.getElementById('b').textContent, 'keep')
})

test('reloadSwap refuses a redirected response or one without the root, and the bridge reports an error', async () => {
  const win = dom(page('Old', '<main data-h-root>old</main>'))
  await assert.rejects(reloadSwap({ window: win, fetch: async () => ({ ...(await resp(page('L', '<main data-h-root>login</main>'))()), redirected: true }) }), /preview expired or lost/)
  await assert.rejects(reloadSwap({ window: win, fetch: resp(page('L', '<p>login</p>')) }), /preview expired or lost/)
  assert.equal(win.document.querySelector('main').textContent, 'old')
  const sent = []
  const parent = { postMessage: (m) => sent.push(m) }
  Object.defineProperty(win, 'parent', { value: parent })
  win.fetch = resp(page('L', '<p>login</p>'))
  createBridge({ adminOrigin: ADMIN, mode: 'reload', window: win }).connect()
  win.dispatchEvent(Object.assign(new win.Event('message'), { data: { h: 1, type: 'reload', rev: 3 }, origin: ADMIN, source: parent }))
  await new Promise((r) => setTimeout(r, 60))
  assert.deepEqual(sent.map((m) => m.type), ['ready', 'error'])
  assert.match(sent[1].message, /preview expired or lost/)
})

test('reloadSwap rejects on HTTP errors and leaves the DOM alone', async () => {
  const win = dom(page('Old', '<p>old</p>'))
  await assert.rejects(reloadSwap({ window: win, fetch: resp('boom', false) }), /reload: 500/)
  assert.equal(win.document.body.innerHTML, '<p>old</p>')
})

test('bridge reload default: swap, re-post ready, post rendered with ms', async () => {
  const win = dom(page('Old', '<main data-h-root>old</main>'))
  const sent = []
  const parent = { postMessage: (m) => sent.push(m) }
  Object.defineProperty(win, 'parent', { value: parent })
  win.fetch = resp(page('N', '<main data-h-root>new</main>'))
  const b = createBridge({ adminOrigin: ADMIN, mode: 'reload', window: win })
  b.connect()
  win.dispatchEvent(Object.assign(new win.Event('message'), { data: { h: 1, type: 'reload', rev: 9 }, origin: ADMIN, source: parent }))
  await new Promise((r) => setTimeout(r, 60))
  assert.equal(win.document.querySelector('main').textContent, 'new')
  assert.deepEqual(sent.map((m) => m.type), ['ready', 'ready', 'rendered'])
  assert.equal(sent[2].rev, 9)
  assert.equal(typeof sent[2].ms, 'number')
})

test('autoStart needs data-auto and data-admin-origin', () => {
  const { w, sent } = fake()
  assert.equal(autoStart(null, w), undefined)
  assert.equal(autoStart({ dataset: { adminOrigin: ADMIN } }, w), undefined)
  assert.equal(autoStart({ dataset: { auto: '' } }, w), undefined)
  const b = autoStart({ dataset: { auto: '', adminOrigin: ADMIN, mode: 'reload' } }, w)
  assert.ok(b)
  assert.equal(sent[0][0].mode, 'reload')
})

test('IIFE build exposes HydrogenBridge and auto-starts', async () => {
  const src = readFileSync(new URL('../dist/bridge.iife.js', import.meta.url), 'utf8')
  const { w, sent } = fake()
  const script = { dataset: { auto: '', adminOrigin: ADMIN, mode: 'live' } }
  new Function('window', 'document', src)(w, { currentScript: script })
  assert.equal(typeof w.HydrogenBridge.createBridge, 'function')
  assert.equal(typeof w.HydrogenBridge.applyPatch, 'function')
  assert.equal(sent[0][0].mode, 'live')
})

const HASH = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'
const metaDoc = (content) => ({ cookie: '', querySelector: (q) => (q === 'meta[name="hydrogen-preview"]' ? { getAttribute: () => content } : null) })

test('ready carries preview only for a 64-hex meta; the old "1" still counts as preview', () => {
  for (const [content, want] of [[HASH, HASH], [HASH.toUpperCase(), HASH.toUpperCase()], ['1', undefined], ['', undefined], [HASH + '0', undefined], ['zz' + HASH.slice(2), undefined]]) {
    const { w, sent } = fake({ url: 'https://site.test/p' })
    w.document = metaDoc(content)
    assert.equal(previewProof(w), want)
    assert.equal(isPreview(w), true)
    createBridge({ adminOrigin: ADMIN, mode: 'live', window: w }).connect()
    assert.equal(sent.length, 1)
    assert.equal('preview' in sent[0][0], want !== undefined)
    assert.equal(sent[0][0].preview, want)
  }
  const { w } = fake()
  assert.equal(previewProof(w), undefined)
})

test('reload: an opaque origin or a failed fetch falls back to location.reload(), without rendered', async () => {
  for (const kind of ['opaque', 'fetch']) {
    const { w, sent } = fake({ url: 'https://site.test/p?preview=tok' })
    let reloads = 0
    w.location.reload = () => reloads++
    w.origin = kind === 'opaque' ? 'null' : 'https://site.test'
    w.fetch = kind === 'opaque' ? () => assert.fail('no fetch from an opaque origin') : () => Promise.reject(new TypeError('Failed to fetch'))
    createBridge({ adminOrigin: ADMIN, mode: 'reload', window: w }).connect()
    w.send({ h: 1, type: 'reload', rev: 3 })
    await tick()
    assert.equal(reloads, 1, kind)
    assert.deepEqual(sent.map(([m]) => m.type), ['ready'], kind)
  }
})

test('reload: an HTTP error is still reported, not reloaded', async () => {
  const win = dom(page('Old', '<main data-h-root>old</main>'))
  const sent = []
  const parent = { postMessage: (m) => sent.push(m) }
  Object.defineProperty(win, 'parent', { value: parent })
  let reloads = 0
  win.location.reload = () => reloads++
  win.fetch = resp('boom', false)
  createBridge({ adminOrigin: ADMIN, mode: 'reload', window: win }).connect()
  win.dispatchEvent(Object.assign(new win.Event('message'), { data: { h: 1, type: 'reload', rev: 2 }, origin: ADMIN, source: parent }))
  await new Promise((r) => setTimeout(r, 40))
  assert.equal(reloads, 0)
  assert.equal(sent.at(-1).type, 'error')
})

test('links: same-site navigations get ?preview=<token> on click, nothing on load, nothing without a token or a frame', () => {
  const click = (w, href) => {
    const a = { href, closest: () => a }
    const ev = new Event('click')
    Object.defineProperty(ev, 'target', { value: a })
    w.dispatchEvent(ev)
    return a.href
  }
  const { w } = fake({ url: 'https://site.test/p?preview=tok&x=1' })
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w })
  b.connect()
  assert.equal(click(w, 'https://site.test/about?a=b#h'), 'https://site.test/about?a=b&preview=tok#h')
  assert.equal(click(w, 'https://site.test/about?preview=other'), 'https://site.test/about?preview=other')
  assert.equal(click(w, 'https://other.test/x'), 'https://other.test/x')
  assert.equal(click(w, 'mailto:a@b.c'), 'mailto:a@b.c')
  b.disconnect()
  assert.equal(click(w, 'https://site.test/z'), 'https://site.test/z')

  const meta = fake({ url: 'https://site.test/p' })
  meta.w.document = metaDoc(HASH)
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: meta.w }).connect()
  assert.equal(click(meta.w, 'https://site.test/z'), 'https://site.test/z', 'no token in the URL')

  const top = fake({ framed: false })
  createBridge({ adminOrigin: ADMIN, mode: 'live', window: top.w }).connect()
  assert.equal(click(top.w, 'https://site.test/z'), 'https://site.test/z', 'not in a frame')
})
