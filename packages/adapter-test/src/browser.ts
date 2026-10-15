import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { resolve } from 'node:path'
import { blockMarkers, pathMarkers } from './html.js'
import { diffMultiset } from './expect.js'
import { Recorder, assert, equal } from './report.js'
import type { Cms } from './http.js'

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
      }
    } finally {
      await s.ctx.close()
    }
  } finally {
    await browser.close()
  }
}
