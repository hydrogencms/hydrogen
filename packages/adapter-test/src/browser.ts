import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { resolve } from 'node:path'
import { blockMarkers, pathMarkers } from './html.js'
import { diffMultiset } from './expect.js'
import { Recorder, assert, equal } from './report.js'
import type { Cms } from './http.js'
import { blockPaths, canvasTexts } from './texts.js'

/** Playwright from the user's installation: `playwright`, `@playwright/test` or `playwright-core`, resolved from `from`. */
export async function loadPlaywright(from?: string): Promise<any | undefined> {
  const req = createRequire(resolve(from ?? process.cwd(), 'noop.js'))
  for (const name of ['playwright', '@playwright/test', 'playwright-core']) {
    try {
      const m = req(name)
      if (m?.chromium) return m
    } catch {
      // try the next one
    }
  }
  return undefined
}

/** The hex SHA-256 of a preview token: what a frontend that knows the token puts in `<meta name="hydrogen-preview">` and `ready.preview`. */
export const tokenHash = (token: string): string => createHash('sha256').update(token).digest('hex')

/** The admin's gate before it sends `init`: `ready.preview` must be the hash of the token it minted for the preview URL. */
export function checkPreviewProof(ready: Record<string, any> | undefined, token: string): void {
  const got = ready?.preview
  assert(got !== undefined, "frontend doesn't prove the preview token: ready has no `preview` (render <meta name=\"hydrogen-preview\" content=\"<sha256 hex of the token>\"> in preview)")
  assert(typeof got === 'string' && got.toLowerCase() === tokenHash(token), `frontend doesn't prove the preview token: ready.preview is ${JSON.stringify(got)}, want the SHA-256 hex of the token from the preview URL`)
}

interface Msg {
  origin: string
  data: Record<string, any>
}

const HOST = `<!doctype html><title>adapter-test host</title>
<iframe id="f" style="width:1000px;height:700px;border:0"></iframe>
<script>
window.__msgs = []
var f = document.getElementById('f')
addEventListener('message', function (e) { if (e.source === f.contentWindow) window.__msgs.push({ origin: e.origin, data: e.data }) })
window.__load = function (src) { f.src = src }
window.__send = function (m, origin) { f.contentWindow.postMessage(m, origin) }
</script>`

export interface BridgeContext {
  rec: Recorder
  /** prefix of the check names (`post`: ...) */
  label?: string
  /** also check that a published page stays silent */
  inert?: boolean
  cms: Cms
  frontend: string
  adminOrigin: string
  channel: string
  /** `blocks`: the block types the published page renders (shared blocks inlined) */
  page: { id: string; type: string; path: string; title: string; blocks: string[] }
  mint: (channel: string, item: string, locale?: string) => Promise<{ token: string; url: string; plain: string }>
  playwrightFrom?: string
  cleanups: (() => Promise<void>)[]
}

/**
 * The bridge in a real browser. The suite plays the admin: its host page is served at the admin origin (a
 * request interception), embeds the preview URL in an iframe and exchanges protocol messages with it.
 */
export async function bridgeChecks(c: BridgeContext): Promise<void> {
  const G = 'bridge'
  const pre = c.label ? `${c.label}: ` : ''
  const rec = {
    check: (g: string, n: string, f: () => Promise<void> | void) => c.rec.check(g, pre + n, f),
    skip: (g: string, n: string, r: string) => c.rec.skip(g, pre + n, r),
  }
  const pw = await loadPlaywright(c.playwrightFrom)
  if (!pw) return rec.skip(G, 'handshake and live update', 'Playwright is not installed here: npm i -D playwright (or pass --playwright-from <dir>) and `npx playwright install chromium`')
  let browser: any
  try {
    // The host page is a fulfilled request, which Chromium treats as public: without this it blocks its frame from a loopback frontend.
    browser = await pw.chromium.launch({ args: ['--disable-features=LocalNetworkAccessChecks'] })
  } catch (e) {
    rec.check(G, 'launch a browser', () => {
      throw new Error(`${(e as Error).message.split('\n')[0]} (npx playwright install chromium)`)
    })
    return
  }
  try {
    const feOrigin = new URL(c.frontend).origin
    const hostUrl = `${c.adminOrigin}/__adapter-test/host`
    const open = async (src: string) => {
      const ctx = await browser.newContext()
      const page = await ctx.newPage()
      await page.route(hostUrl, (r: any) => r.fulfill({ status: 200, contentType: 'text/html', body: HOST }))
      await page.goto(hostUrl)
      await page.evaluate((s: string) => (window as any).__load(s), src)
      const msgs = (): Promise<Msg[]> => page.evaluate(() => (window as any).__msgs)
      const frame = () => page.frames().find((f: any) => f !== page.mainFrame())
      const wait = async (pred: (m: Msg['data']) => boolean, from: number, what: string, ms = 15_000): Promise<Msg['data']> => {
        const t = Date.now()
        for (;;) {
          const all = await msgs()
          const hit = all.slice(from).find((m) => m.origin === feOrigin && m.data?.h === 1 && pred(m.data))
          if (hit) return hit.data
          if (Date.now() - t > ms) throw new Error(`no ${what} within ${ms} ms (messages: ${JSON.stringify(all.slice(from).map((m) => m.data?.type))})`)
          await new Promise((r) => setTimeout(r, 40))
        }
      }
      const send = (m: Record<string, unknown>) => page.evaluate(([x, o]: [unknown, string]) => (window as any).__send(x, o), [{ h: 1, ...m }, feOrigin])
      const h1 = () => frame().evaluate(() => document.querySelector('h1')?.textContent?.trim() ?? '')
      const untilH1 = (t: string) => frame().waitForFunction((x: string) => document.querySelector('h1')?.textContent?.trim() === x, t, { timeout: 8000 })
        .catch(async () => { throw new Error(`the <h1> reads "${await h1()}", want "${t}"`) })
      const html = () => frame().content() as Promise<string>
      return { page, ctx, msgs, wait, send, untilH1, html, h1, frame }
    }

    // The bridge does nothing for a page that is not in preview.
    if (c.inert !== false) await rec.check(G, 'inert without a preview token', async () => {
      const s = await open(c.frontend + c.page.path)
      try {
        await new Promise((r) => setTimeout(r, 2500))
        equal((await s.msgs()).map((m) => m.data?.type), [], 'messages from a published page')
      } finally {
        await s.ctx.close()
      }
    })

    const doc = await c.cms.manage('GET', `/items/${c.page.id}?locale=${c.channel}&draft=true`)
    const m = await c.mint(c.channel, c.page.id)
    const want = c.page.blocks
    const s = await open(m.url)
    try {
      let ready: Msg['data'] | undefined
      const ok = await rec.check(G, 'handshake: ready arrives from the frontend origin', async () => {
        ready = await s.wait((d) => d.type === 'ready', 0, 'ready')
        assert(ready.mode === 'live' || ready.mode === 'reload', `ready.mode is ${JSON.stringify(ready.mode)}`)
        assert(typeof ready.sdk === 'string' && /@/.test(ready.sdk), `ready.sdk is ${JSON.stringify(ready.sdk)}`)
        assert(typeof ready.url === 'string', 'ready.url missing')
      })
      if (!ok || !ready) return
      // Like the admin: `init` (the draft) goes only to a frontend that proves it knows the token.
      const proven = await rec.check(G, 'handshake: ready proves the preview token (ready.preview is the SHA-256 of it)', () => checkPreviewProof(ready, m.token))
      if (!proven) return
      const mode = ready.mode as string
      let cursor = (await s.msgs()).length
      const next = async <T>(fn: () => Promise<T>): Promise<T> => {
        cursor = (await s.msgs()).length
        return fn()
      }

      if (mode === 'live') {
        let rev = 1
        await rec.check(G, 'live: init renders the working document and answers rendered', async () => {
          await next(() => s.send({ type: 'init', item: c.page.id, itemType: c.page.type, locale: c.channel, rev, doc: { fields: doc.fields } }))
          const r = await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, 'rendered 1')
          assert(typeof r.ms === 'number', 'rendered.ms is not a number')
          const bad = diffMultiset(want, blockMarkers(await s.html()))
          assert(!bad, `after init the blocks differ from the page: ${bad}`)
        })
        const title = `${c.page.title} (live)`
        await rec.check(G, 'live: a replace patch changes the DOM and answers rendered', async () => {
          await next(() => s.send({ type: 'patch', rev: rev + 1, base: rev, ops: [{ op: 'replace', path: '/fields/title', value: title }] }))
          rev++
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(title)
        })
        const block = { id: '01900000-0000-7000-8000-00000000adab', type: 'callout', props: { text: 'Added live', tone: 'brand' } }
        await rec.check(G, 'live: a block added at an index renders first, with its data-h-block and data-h-path', async () => {
          await next(() => s.send({ type: 'patch', rev: rev + 1, base: rev, ops: [{ op: 'add', path: '/fields/body/0', value: block }] }))
          rev++
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          const h = await s.html()
          assert(blockMarkers(h).length === want.length + 1, `${blockMarkers(h).length} blocks, want ${want.length + 1}`)
          assert(blockMarkers(h)[0] === 'callout', `the first block is ${blockMarkers(h)[0]}, want callout`)
          assert(pathMarkers(h).includes('fields/body/0') && pathMarkers(h).includes('fields/body/1'), `paths are not renumbered (found ${JSON.stringify(pathMarkers(h))})`)
          assert(h.includes('Added live'), 'the new block text is not in the page')
        })
        await rec.check(G, 'live: a block added with "-" is appended; remove patches take blocks out again', async () => {
          await next(() => s.send({ type: 'patch', rev: rev + 1, base: rev, ops: [{ op: 'add', path: '/fields/body/-', value: { ...block, id: '01900000-0000-7000-8000-00000000adac', props: { text: 'Appended live', tone: 'dark' } } }] }))
          rev++
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          const last = blockMarkers(await s.html())
          assert(last.length === want.length + 2 && last[last.length - 1] === 'callout', 'the appended block is not last')
          await next(() => s.send({ type: 'patch', rev: rev + 1, base: rev, ops: [{ op: 'remove', path: `/fields/body/${doc.fields.body.length + 1}` }, { op: 'remove', path: '/fields/body/0' }] }))
          rev++
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          const h = await s.html()
          assert(!h.includes('Added live') && !h.includes('Appended live'), 'a removed block is still there')
          assert(!diffMultiset(want, blockMarkers(h)), 'the blocks differ from the page after removing')
        })
        await rec.check(G, 'live: a whole-document replace (path "") re-renders', async () => {
          const whole = `${c.page.title} (whole)`
          await next(() => s.send({ type: 'patch', rev: rev + 1, base: rev, ops: [{ op: 'replace', path: '', value: { fields: { ...doc.fields, title: whole } } }] }))
          rev++
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(whole)
        })
        await rec.check(G, 'live: a patch on a stale base is answered with resync, then init recovers', async () => {
          await next(() => s.send({ type: 'patch', rev: 99, base: 42, ops: [{ op: 'replace', path: '/fields/title', value: 'must not show' }] }))
          const r = await s.wait((d) => d.type === 'resync', cursor, 'resync')
          equal(r.rev, rev, 'resync.rev')
          assert((await s.h1()) !== 'must not show', 'the stale patch was applied')
          rev++
          await next(() => s.send({ type: 'init', item: c.page.id, itemType: c.page.type, locale: c.channel, rev, doc: { fields: doc.fields } }))
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(c.page.title)
        })
      } else {
        let rev = 1
        const edited = `${c.page.title} (reload)`
        c.cleanups.push(async () => {
          await c.cms.manage('DELETE', `/items/${c.page.id}/draft?locale=${c.channel}`).catch(() => undefined)
        })
        await rec.check(G, 'reload: reload after a saved draft re-renders the page, answers ready and rendered', async () => {
          await c.cms.manage('PATCH', `/items/${c.page.id}`, { locale: c.channel, fields: { title: edited } })
          rev++
          await next(() => s.send({ type: 'reload', rev }))
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(edited)
          assert((await s.msgs()).slice(cursor).some((x) => x.data?.type === 'ready'), 'no ready after the swap')
        })
        await rec.check(G, 'reload: a second reload shows the next save', async () => {
          await c.cms.manage('PATCH', `/items/${c.page.id}`, { locale: c.channel, fields: { title: `${edited} 2` } })
          rev++
          await next(() => s.send({ type: 'reload', rev }))
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(`${edited} 2`)
          const bad = diffMultiset(want, blockMarkers(await s.html()))
          assert(!bad, `after reloads the blocks differ: ${bad}`)
        })
        await c.cms.manage('DELETE', `/items/${c.page.id}/draft?locale=${c.channel}`).catch(() => undefined)
        // the frame still shows the last edited draft: show the fixture's current document again, so the texts below map
        await rec.check(G, 'reload: a final reload shows the fixture again', async () => {
          rev++
          await next(() => s.send({ type: 'reload', rev }))
          await s.wait((d) => d.type === 'rendered' && d.rev === rev, cursor, `rendered ${rev}`)
          await s.untilH1(c.page.title)
        })
      }
      await visualChecks(c, rec, s, doc.fields)
    } finally {
      await s.ctx.close()
    }
  } finally {
    await browser.close()
  }
}

const VG = 'visual editing'
const collapse = (t: string) => t.replace(/\s+/g, ' ').trim()

/**
 * Canvas editing: the suite plays the admin with `overlay`, expects `layout` for the page's blocks and title, a
 * `select` for a click, an inline edit through `editText`, and silence after `overlay {on: false}`.
 */
async function visualChecks(
  c: BridgeContext,
  rec: { check: (g: string, n: string, f: () => Promise<void> | void) => Promise<boolean>; skip: (g: string, n: string, r: string) => void },
  s: { page: any; msgs: () => Promise<Msg[]>; wait: (pred: (m: Msg['data']) => boolean, from: number, what: string, ms?: number) => Promise<Msg['data']>; send: (m: Record<string, unknown>) => Promise<unknown>; frame: () => any },
  fields: Record<string, any>,
): Promise<void> {
  const feOrigin = new URL(c.frontend).origin
  const texts = canvasTexts(c.page.type, fields)
  const blocks = blockPaths(fields.body)
  const expected = [...blocks.map((b) => b.path), ...(texts.some((t) => t.path === 'fields/title') ? ['fields/title'] : [])]
  const wire = texts.map(({ path, text, scope }) => ({ path, text, scope }))
  const fromFrontend = async (from: number) => (await s.msgs()).slice(from).filter((m) => m.origin === feOrigin && m.data?.h === 1).map((m) => m.data)
  let cursor = (await s.msgs()).length
  let nodes: { path: string; rect: number[]; block?: string; text?: boolean }[] = []

  const supported = await rec.check(VG, "frontend's bridge supports canvas editing (ready.edit is true)", async () => {
    const readies = (await fromFrontend(0)).filter((d) => d.type === 'ready')
    assert(readies.length > 0, 'no ready')
    assert(readies[readies.length - 1]!.edit === true, "frontend's bridge doesn't support canvas editing: ready.edit is not true (update @hydrogencms/bridge)")
  })
  if (!supported) return
  const overlayOn = await rec.check(VG, 'overlay on: layout names every block and the title within 3 s', async () => {
    cursor = (await s.msgs()).length
    await s.send({ type: 'overlay', on: true, texts: wire })
    const t = Date.now()
    let last: string[] = []
    for (;;) {
      const layouts = (await fromFrontend(cursor)).filter((d) => d.type === 'layout')
      const l = layouts[layouts.length - 1]
      if (l) {
        assert(Array.isArray(l.nodes) && typeof l.vw === 'number' && typeof l.vh === 'number', 'layout needs nodes, vw and vh')
        nodes = l.nodes
        last = nodes.map((n) => n.path)
        if (expected.every((p) => last.includes(p))) break
      }
      if (Date.now() - t > 3000)
        throw new Error(layouts.length === 0 ? 'no layout within 3 s of overlay {on: true}' : `layout lacks ${JSON.stringify(expected.filter((p) => !last.includes(p)))} (has ${JSON.stringify(last)})`)
      await new Promise((r) => setTimeout(r, 40))
    }
    for (const n of nodes) {
      assert(Array.isArray(n.rect) && n.rect.length === 4 && n.rect.every((v: number) => Number.isInteger(v)), `node ${n.path}: rect is ${JSON.stringify(n.rect)}, want four integers`)
      assert(n.rect[2]! > 0 && n.rect[3]! > 0, `node ${n.path} has an empty rect`)
    }
    for (const b of blocks) equal(nodes.find((n) => n.path === b.path)?.block, b.type, `layout node ${b.path}: block`)
  })
  if (!overlayOn) return

  const first = blocks[0]
  const latest = async (from: number) => {
    const l = (await fromFrontend(from)).filter((d) => d.type === 'layout')
    return l[l.length - 1] as { nodes: typeof nodes; vw: number; vh: number } | undefined
  }
  // a point inside the block's rect, in the viewport, that no other node's rect covers
  const free = (l: { nodes: typeof nodes; vw: number; vh: number }) => {
    const r = l.nodes.find((n) => n.path === first!.path)?.rect
    if (!r) return undefined
    const others = l.nodes.filter((n) => n.path !== first!.path)
    for (let y = Math.max(r[1]! + 1, 0); y < Math.min(r[1]! + r[3]!, l.vh); y += 6)
      for (let x = Math.max(r[0]! + 1, 0); x < Math.min(r[0]! + r[2]!, l.vw); x += 6)
        if (!others.some((n) => x >= n.rect[0]! && x < n.rect[0]! + n.rect[2]! && y >= n.rect[1]! && y < n.rect[1]! + n.rect[3]!)) return [x, y] as const
    return undefined
  }
  if (!first) rec.skip(VG, 'a click on a block selects it', 'the page has no blocks')
  else
    await rec.check(VG, 'a click on a block selects it and does not navigate the frame', async () => {
      const before = s.frame().url()
      let at = free({ nodes, vw: 1000, vh: 700 })
      if (!at) {
        cursor = (await s.msgs()).length
        await s.frame().evaluate((p: string) => document.querySelector(`[data-h-path="${p}"]`)?.scrollIntoView({ block: 'start', behavior: 'instant' as ScrollBehavior }), first.path)
        await new Promise((r) => setTimeout(r, 600))
        const l = await latest(cursor)
        at = l && free(l)
      }
      assert(at, `no point of ${first.path} is free of other nodes' rectangles, so a click cannot be attributed to the block`)
      const box = await s.page.locator('#f').boundingBox()
      cursor = (await s.msgs()).length
      await s.page.mouse.click(box.x + at[0], box.y + at[1])
      const sel = await s.wait((d) => d.type === 'select', cursor, 'select')
      equal(sel.path, first.path, 'select.path')
      equal(sel.edit, false, 'select.edit')
      await new Promise((r) => setTimeout(r, 300))
      equal(s.frame().url(), before, 'frame URL after the click')
    })

  const ref = texts.find((t) => t.kind === 'string' && t.path.endsWith('/props/heading') && nodes.some((n) => n.path === t.path && n.text)) ?? texts.find((t) => t.path === 'fields/title' && nodes.some((n) => n.path === t.path && n.text))
  if (!ref) rec.skip(VG, 'inline editing commits on Enter', 'no single-line text node in the layout')
  else
    await rec.check(VG, `inline editing: editText on ${ref.path}, typing and Enter answer inlineEdit with the new value`, async () => {
      cursor = (await s.msgs()).length
      // a leftover selection (the click above) would keep the caret where it is: the bridge puts it at the end only without one
      await s.frame().evaluate(() => getSelection()?.removeAllRanges())
      await s.send({ type: 'editText', path: ref.path, multiline: false })
      await s.frame().waitForSelector('[contenteditable]', { timeout: 5000 }).catch(async () => {
        const got = (await fromFrontend(cursor)).filter((d) => d.type === 'inlineEdit' && d.path === ref.path)
        throw new Error(`editText did not make the node editable (inlineEdit: ${got.length ? JSON.stringify(got[got.length - 1]) : 'none'})`)
      })
      const suffix = ' (edited)'
      await s.page.keyboard.type(suffix)
      await s.page.keyboard.press('Enter')
      const r = await s.wait((d) => d.type === 'inlineEdit' && d.path === ref.path, cursor, 'inlineEdit')
      assert(!r.cancel, 'inlineEdit was cancelled, want the committed value')
      equal(r.value, collapse(ref.text) + suffix, 'inlineEdit.value')
      assert((await s.frame().$('[contenteditable]')) === null, 'contenteditable is still set after Enter')
    })

  await rec.check(VG, 'overlay off: no further layout on scroll and clicks are not prevented; on, a scroll does produce a layout', async () => {
    // a page that cannot scroll gets a tall spacer, so the scroll below moves the nodes
    await s.frame().evaluate(() => {
      const sp = document.createElement('div')
      sp.id = '__adapter_test_spacer'
      sp.style.cssText = 'height:4000px'
      document.body.appendChild(sp)
    })
    await new Promise((r) => setTimeout(r, 400))
    cursor = (await s.msgs()).length
    await s.frame().evaluate(() => window.scrollBy({ top: 40, behavior: 'instant' as ScrollBehavior }))
    const t = Date.now()
    while (!(await latest(cursor))) {
      if (Date.now() - t > 3000) throw new Error('no layout within 3 s of a scroll while the overlay is on')
      await new Promise((r) => setTimeout(r, 40))
    }
    await s.send({ type: 'overlay', on: false, texts: wire })
    await new Promise((r) => setTimeout(r, 400))
    cursor = (await s.msgs()).length
    await s.frame().evaluate(() => {
      window.scrollBy({ top: -40, behavior: 'instant' as ScrollBehavior })
      window.dispatchEvent(new Event('scroll'))
      window.dispatchEvent(new Event('resize'))
    })
    const prevented = await s.frame().evaluate(() => {
      const e = new MouseEvent('click', { bubbles: true, cancelable: true })
      document.body.dispatchEvent(e)
      return e.defaultPrevented
    })
    await new Promise((r) => setTimeout(r, 700))
    await s.frame().evaluate(() => document.getElementById('__adapter_test_spacer')?.remove())
    const more = (await fromFrontend(cursor)).filter((d) => d.type === 'layout' || d.type === 'hover' || d.type === 'select')
    assert(more.length === 0, `${more.length} ${more[0]?.type} message(s) after overlay {on: false}`)
    assert(!prevented, 'a click is still prevented after overlay {on: false}')
  })
}
