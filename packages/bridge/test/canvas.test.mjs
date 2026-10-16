import test from 'node:test'
import assert from 'node:assert/strict'
import { Window } from 'happy-dom'
import { createBridge } from '../dist/index.js'

const ADMIN = 'https://admin.test'
const tick = () => new Promise((r) => setTimeout(r, 15))

const PAGE = `
<h1 data-r="0,0,800,40">Welcome   home</h1>
<div data-h-root data-r="0,0,800,900">
  <h2 data-r="0,50,800,30">Page   title</h2>
  <section data-h-path="fields/body/0" data-h-block="hero" data-r="0,100,800,300">
    <h3 data-r="10,110,300,30">Big title</h3>
    <h3 data-r="10,150,300,30">Big title</h3>
    <p data-r="10,190,300,20">Sub <b>bold</b></p>
    <div data-h-path="fields/body/0/slots/cards/0" data-h-block="card" data-r="10,220,200,100"><h3 data-r="12,222,100,20">Card only</h3></div>
    <span data-r="0,0,0,10">Hidden</span>
    <a href="/other" data-r="10,330,50,20">Go</a>
  </section>
  <section data-h-path="fields/body/1" data-h-block="text" data-r="0,420,800,100">Body copy</section>
  <div id="empty" data-r="0,600,800,100"></div>
</div>`

function setup({ page = PAGE, url = 'https://site.test/p?preview=tok' } = {}) {
  const w = new Window({ url })
  w.document.body.innerHTML = page
  w.Element.prototype.getBoundingClientRect = function () {
    const [x, y, ww, h] = (this.getAttribute?.('data-r') ?? '0,0,0,0').split(',').map(Number)
    return { x, y, left: x, top: y, width: ww, height: h, right: x + ww, bottom: y + h }
  }
  Object.defineProperty(w, 'innerWidth', { value: 1000 })
  Object.defineProperty(w, 'innerHeight', { value: 700 })
  w.requestAnimationFrame = (f) => queueMicrotask(f)
  const sent = []
  const parent = { postMessage: (m, o) => sent.push([m, o]) }
  Object.defineProperty(w, 'parent', { value: parent })
  const send = (data, o = {}) => w.dispatchEvent(new w.MessageEvent('message', { data, origin: o.origin ?? ADMIN, source: o.source ?? parent }))
  const b = createBridge({ adminOrigin: ADMIN, mode: 'live', window: w })
  b.connect()
  const of = (type) => sent.filter(([m]) => m.type === type).map(([m]) => m)
  const q = (s) => w.document.querySelector(s)
  const ev = (el, type, init = {}) => {
    const e = type.startsWith('key') ? new w.KeyboardEvent(type, { bubbles: true, cancelable: true, ...init }) : new w.MouseEvent(type, { bubbles: true, cancelable: true, ...init })
    el.dispatchEvent(e)
    return e
  }
  return { w, b, sent, send, of, q, ev }
}

const TEXTS = [
  { path: 'fields/title', text: 'Page title', scope: '' },
  { path: 'fields/body/0/props/heading', text: 'Big   title', scope: 'fields/body/0' },
  { path: 'fields/body/0/props/sub', text: 'Card only', scope: 'fields/body/0' },
  { path: 'fields/body/1/props/body', text: 'Body copy', scope: 'fields/body/1' },
  { path: 'fields/body/0/props/hidden', text: 'Hidden', scope: 'fields/body/0' },
]

test('overlay on posts a layout of explicit and mapped nodes, in document order', async () => {
  const { send, of } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const l = of('layout')
  assert.equal(l.length, 1)
  assert.equal(l[0].vw, 1000)
  assert.equal(l[0].vh, 700)
  assert.deepEqual(l[0].nodes, [
    { path: 'fields/title', rect: [0, 50, 800, 30], text: true },
    { path: 'fields/body/0', rect: [0, 100, 800, 300], block: 'hero' },
    { path: 'fields/body/0/props/heading', rect: [10, 110, 300, 30], text: true },
    { path: 'fields/body/0/slots/cards/0', rect: [10, 220, 200, 100], block: 'card' },
    { path: 'fields/body/1', rect: [0, 420, 800, 100], block: 'text', text: true },
  ])
})

test('mapping: first match wins, nested blocks are skipped, explicit markers win, zero-size nodes are left out', async () => {
  const { send, of } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const paths = of('layout')[0].nodes.map((n) => n.path)
  assert.ok(!paths.includes('fields/body/0/props/sub'), 'text inside a nested block is not the outer block')
  assert.ok(!paths.includes('fields/body/0/props/hidden'), 'zero size')
  const heading = of('layout')[0].nodes.find((n) => n.path === 'fields/body/0/props/heading')
  assert.equal(heading.rect[1], 110, 'first of two equal headings')
  assert.ok(!paths.includes('fields/body/1/props/body'), 'the element carries data-h-path already')
})

test('an empty scope maps within the whole body when there is no data-h-root', async () => {
  const { send, of } = setup({ page: '<main data-h-path="fields/body/0"><p data-r="0,0,10,10">Inside</p></main><h1 data-r="0,20,100,30">Top</h1><p data-r="0,60,100,30">Inside</p>' })
  send({ h: 1, type: 'overlay', on: true, texts: [{ path: 'fields/a', text: 'Inside', scope: '' }, { path: 'fields/t', text: 'Top', scope: '' }] })
  await tick()
  const n = of('layout')[0].nodes
  assert.deepEqual(n.map((x) => x.path), ['fields/t', 'fields/a'])
  assert.equal(n[1].rect[1], 60)
})

test('layout is coalesced per frame and follows scroll, resize and DOM changes', async () => {
  const { w, send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: [] })
  await tick()
  assert.equal(of('layout').length, 1)
  q('#empty').setAttribute('data-r', '0,610,800,100')
  q('#empty').setAttribute('data-h-path', 'fields/y')
  for (let i = 0; i < 5; i++) w.dispatchEvent(new w.Event('scroll'))
  await tick()
  assert.equal(of('layout').length, 2)
  q('#empty').setAttribute('data-r', '0,620,800,100')
  w.dispatchEvent(new w.Event('resize'))
  await tick()
  assert.equal(of('layout').length, 3)
  w.dispatchEvent(new w.Event('scroll'))
  w.dispatchEvent(new w.Event('resize'))
  await tick()
  assert.equal(of('layout').length, 3, 'an unchanged layout is not posted again')
  q('#empty').removeAttribute('data-h-path')
  await tick()
  assert.equal(of('layout').length, 4)
  q('#empty').setAttribute('data-h-path', 'fields/x')
  q('#empty').setAttribute('data-r', '0,600,10,10')
  await tick()
  assert.equal(of('layout').length, 5)
  assert.ok(of('layout')[4].nodes.some((n) => n.path === 'fields/x'))
  q('#empty').setAttribute('data-h-block', 'late')
  await tick()
  assert.equal(of('layout').length, 6, 'data-h-block is observed')
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  assert.equal(of('layout').length, 7, 'an overlay message always gets a layout')
  q('#empty').setAttribute('title', 'x')
  await tick()
  assert.equal(of('layout').length, 7, 'unrelated attributes are not observed')
})

test('rendered() re-measures while the overlay is on', async () => {
  const { b, send, of } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: [] })
  await tick()
  b.rendered(3, 1)
  await tick()
  assert.equal(of('rendered').length, 1)
  assert.equal(of('layout').length, 2)
})

test('click selects the nearest node and is prevented; dblclick sets edit; empty space is null', async () => {
  const { send, of, q, ev } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  let e = ev(q('section a'), 'click')
  assert.equal(e.defaultPrevented, true)
  assert.deepEqual(of('select'), [{ h: 1, type: 'select', path: 'fields/body/0', edit: false }])
  e = ev(q('section b') ?? q('section p'), 'dblclick')
  assert.equal(e.defaultPrevented, true)
  assert.equal(of('select')[1].edit, true)
  ev(q('#empty'), 'click')
  assert.equal(of('select')[2].path, null)
  e = ev(q('section a'), 'auxclick')
  assert.equal(e.defaultPrevented, true)
  assert.equal(of('select').length, 3)
  ev(q('section h3'), 'click')
  assert.equal(of('select')[3].path, 'fields/body/0/props/heading')
})

test('hover posts only when the node changes', async () => {
  const { w, send, of, q, ev } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  ev(q('section a'), 'pointermove')
  ev(q('section p'), 'pointermove')
  ev(q('#empty'), 'pointermove')
  ev(q('section .nothing') ?? q('#empty'), 'pointermove')
  ev(q('section h3'), 'pointermove')
  ev(w.document.documentElement, 'pointerleave')
  assert.deepEqual(of('hover').map((m) => m.path), ['fields/body/0', null, 'fields/body/0/props/heading', null])
})

test('off and disconnect remove the listeners and observers', async () => {
  for (const how of ['off', 'disconnect']) {
    const { w, b, send, of, q, ev } = setup()
    send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
    await tick()
    if (how === 'off') send({ h: 1, type: 'overlay', on: false, texts: [] })
    else b.disconnect()
    const n = of('layout').length
    const e = ev(q('section a'), 'click')
    assert.equal(e.defaultPrevented, false)
    ev(q('section a'), 'pointermove')
    w.dispatchEvent(new w.Event('scroll'))
    q('#empty').setAttribute('data-h-path', 'fields/x')
    await tick()
    assert.equal(of('layout').length, n)
    assert.equal(of('select').length, 0)
    assert.equal(of('hover').length, 0)
  }
})

test('H6 link rewriting still works while the overlay is off', async () => {
  const { q, ev } = setup()
  const a = q('section a')
  ev(a, 'click')
  assert.match(a.href, /\?preview=tok$/)
})

test('editText: Enter commits, restores the original text node with the new data', async () => {
  const { send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  const node = h.firstChild
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  assert.equal(h.getAttribute('contenteditable'), 'plaintext-only')
  h.textContent = '  New\n  title '
  const k = new h.ownerDocument.defaultView.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
  h.dispatchEvent(k)
  assert.equal(k.defaultPrevented, true)
  assert.deepEqual(of('inlineEdit'), [{ h: 1, type: 'inlineEdit', path: 'fields/body/0/props/heading', value: 'New title' }])
  assert.equal(h.hasAttribute('contenteditable'), false)
  assert.equal(h.childNodes.length, 1)
  assert.equal(h.firstChild, node)
  assert.equal(node.data, 'New title')
})

test('editText: blur commits, an unchanged value still posts', async () => {
  const { w, send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  h.dispatchEvent(new w.FocusEvent('blur'))
  assert.deepEqual(of('inlineEdit').map((m) => m.value), ['Big title'])
  h.dispatchEvent(new w.FocusEvent('blur'))
  assert.equal(of('inlineEdit').length, 1, 'ended once')
})

test('editText multiline: Enter keeps editing, Mod+Enter commits and keeps newlines', async () => {
  const { w, send, of, q } = setup({ page: '<p data-h-path="fields/body" data-r="0,0,10,10">Line</p>' })
  send({ h: 1, type: 'editText', path: 'fields/body', multiline: true })
  const p = q('p')
  const key = (init) => {
    const e = new w.KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
    p.dispatchEvent(e)
    return e
  }
  assert.equal(key({ key: 'Enter' }).defaultPrevented, false)
  assert.equal(of('inlineEdit').length, 0)
  p.textContent = 'a\nb'
  Object.defineProperty(p, 'innerText', { get: () => 'a\nb' }) // happy-dom collapses newlines like CSS does
  assert.equal(key({ key: 'Enter', metaKey: true }).defaultPrevented, true)
  assert.equal(of('inlineEdit')[0].value, 'a\nb')
})

test('editText: Escape cancels and restores the original', async () => {
  const { w, send, of, q } = setup()
  const h = q('section h3')
  const node = h.firstChild
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  h.textContent = 'zzz'
  h.dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
  assert.deepEqual(of('inlineEdit'), [{ h: 1, type: 'inlineEdit', path: 'fields/body/0/props/heading', cancel: true }])
  assert.equal(h.firstChild, node)
  assert.equal(node.data, 'Big title')
  assert.equal(h.hasAttribute('contenteditable'), false)
})

test('editText is refused for unknown paths and nodes with more than one text child', async () => {
  const { send, of } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  send({ h: 1, type: 'editText', path: 'fields/nope', multiline: false })
  send({ h: 1, type: 'editText', path: 'fields/body/0', multiline: false })
  assert.deepEqual(of('inlineEdit').map((m) => [m.path, m.cancel]), [['fields/nope', true], ['fields/body/0', true]])
})

test('clicks inside the element being edited are not prevented', async () => {
  const { send, of, q, ev } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  assert.equal(ev(h, 'click').defaultPrevented, false)
  assert.equal(of('select').length, 0)
  assert.equal(ev(q('section a'), 'click').defaultPrevented, true)
})

test('turning the overlay off cancels an inline edit', async () => {
  const { send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  h.textContent = 'x'
  send({ h: 1, type: 'overlay', on: false, texts: [] })
  assert.equal(of('inlineEdit')[0].cancel, true)
  assert.equal(h.textContent, 'Big title')
})

test('scrollTo: a path scrolls the node into view, by scrolls the window', async () => {
  const { w, send, q } = setup()
  const calls = []
  const h = q('section')
  h.scrollIntoView = (o) => calls.push(['view', o.block])
  w.scrollBy = (o) => calls.push(['by', o])
  send({ h: 1, type: 'overlay', on: true, texts: [] })
  send({ h: 1, type: 'scrollTo', path: 'fields/body/0' })
  send({ h: 1, type: 'scrollTo', by: -16 })
  send({ h: 1, type: 'scrollTo', path: 'fields/nope' })
  assert.deepEqual(calls, [['view', 'nearest'], ['by', { top: -16, behavior: 'instant' }]])
})

test('canvas messages from the wrong origin or source are ignored', async () => {
  const { send, of, q, ev, sent } = setup()
  const before = sent.length
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS }, { origin: 'https://evil.test' })
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS }, { source: {} })
  send({ h: 1, type: 'editText', path: 'fields/body/0', multiline: false }, { origin: 'https://evil.test' })
  await tick()
  assert.equal(sent.length, before)
  assert.equal(ev(q('section a'), 'click').defaultPrevented, false)
  assert.equal(of('layout').length, 0)
})

test('zero-size nodes are skipped: hover and click resolve to the next ancestor that is a node', async () => {
  const { send, of, q, ev } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const hidden = q('section span')
  ev(hidden, 'pointermove')
  ev(hidden, 'click')
  assert.deepEqual(of('hover').map((m) => m.path), ['fields/body/0'])
  assert.equal(of('select')[0].path, 'fields/body/0')
})

test('hover is seen by pages that stop propagation (capture phase)', async () => {
  const { send, of, q, ev } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  q('section a').addEventListener('pointermove', (e) => e.stopPropagation())
  ev(q('section a'), 'pointermove')
  assert.deepEqual(of('hover').map((m) => m.path), ['fields/body/0'])
})

test('form submits are prevented while on, without a select', async () => {
  const { w, send, of, q } = setup({ page: '<form data-h-path="fields/body/0" data-r="0,0,100,100"><button data-r="0,0,10,10">Go</button></form>' })
  const submit = () => {
    const e = new w.Event('submit', { bubbles: true, cancelable: true })
    let seen = false
    q('form').addEventListener('submit', () => (seen = true))
    q('form').dispatchEvent(e)
    return [e.defaultPrevented, seen]
  }
  assert.deepEqual(submit(), [false, true])
  send({ h: 1, type: 'overlay', on: true, texts: [] })
  await tick()
  assert.deepEqual(submit(), [true, false])
  assert.equal(of('select').length, 0)
})

test('editText: a single line commits textContent, so CSS text-transform never leaks; multiline keeps innerText', async () => {
  const { send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  Object.defineProperty(h, 'innerText', { get: () => 'SHOUTED', configurable: true })
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  h.textContent = ' quiet   words '
  h.dispatchEvent(new h.ownerDocument.defaultView.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  assert.equal(of('inlineEdit')[0].value, 'quiet words')
})

test('editText: an element detached by a re-render ends the edit as a cancel', async () => {
  const { w, send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const h = q('section h3')
  send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
  h.remove()
  w.dispatchEvent(new w.Event('resize'))
  await tick()
  assert.deepEqual(of('inlineEdit'), [{ h: 1, type: 'inlineEdit', path: 'fields/body/0/props/heading', cancel: true }])
  w.dispatchEvent(new w.Event('resize'))
  await tick()
  assert.equal(of('inlineEdit').length, 1, 'ended once')
})

test('overlay off and disconnect end an inline edit even when the overlay was never on', async () => {
  for (const how of ['overlay', 'disconnect']) {
    const { b, send, of, q } = setup()
    const h = q('section h3')
    send({ h: 1, type: 'overlay', on: false, texts: [{ path: 'fields/body/0/props/heading', text: 'Big title', scope: 'fields/body/0' }] })
    send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
    assert.equal(h.hasAttribute('contenteditable'), true, 'editing')
    if (how === 'overlay') send({ h: 1, type: 'overlay', on: false, texts: [] })
    else b.disconnect()
    assert.deepEqual(of('inlineEdit').map((m) => m.cancel), [true], how)
    assert.equal(h.hasAttribute('contenteditable'), false)
  }
})

test('editText: Escape and Enter that end an edit do not reach page hotkeys', async () => {
  const { w, send, of, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  const seen = []
  w.document.addEventListener('keydown', (e) => seen.push(e.key))
  const h = q('section h3')
  for (const key of ['Escape', 'Enter']) {
    send({ h: 1, type: 'editText', path: 'fields/body/0/props/heading', multiline: false })
    h.dispatchEvent(new w.KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  }
  assert.equal(of('inlineEdit').length, 2)
  assert.deepEqual(seen, [])
})

test('a path with an explicit marker is not mapped by text elsewhere (a hidden nav link with the same text)', async () => {
  const page = `<nav><a href="/about" data-r="0,0,0,0">About</a></nav><h1 data-h-path="fields/title" data-r="0,50,800,40">About</h1>`
  const { send, of, q } = setup({ page })
  send({ h: 1, type: 'overlay', on: true, texts: [{ path: 'fields/title', text: 'About', scope: '' }] })
  await tick()
  assert.deepEqual(of('layout')[0].nodes.map((n) => n.path), ['fields/title'])
  send({ h: 1, type: 'editText', path: 'fields/title', multiline: false })
  assert.ok(q('h1').hasAttribute('contenteditable'))
  assert.ok(!q('a').hasAttribute('contenteditable'))
})

test('editText without a selection inside puts the caret at the end', async () => {
  const { w, send, q } = setup()
  send({ h: 1, type: 'overlay', on: true, texts: TEXTS })
  await tick()
  w.getSelection().removeAllRanges()
  send({ h: 1, type: 'editText', path: 'fields/title', multiline: false })
  const s = w.getSelection()
  const el = q('h2')
  assert.ok(el.contains(s.anchorNode))
  assert.equal(s.isCollapsed, true)
  assert.equal(s.anchorOffset, s.anchorNode.nodeType === 3 ? s.anchorNode.data.length : s.anchorNode.childNodes.length)
})
