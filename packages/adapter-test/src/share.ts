import { h1s } from './html.js'
import { Jar, get } from './http.js'
import type { Cms } from './http.js'
import { loadPlaywright } from './browser.js'
import { Recorder, assert, equal } from './report.js'

export interface ShareContext {
  rec: Recorder
  cms: Cms
  frontend: string
  channel: string
  browser: boolean
  playwrightFrom?: string
  /** an item that has a page on the frontend */
  page: { id: string; title: string }
  /** maps a URL the CMS returned onto the frontend under test */
  onFrontend: (url: string) => string
  cleanups: (() => Promise<void>)[]
}

const G = 'share'
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/**
 * Share links: an opaque `hps_` token opens the preview outside a frame, a relayed (unsaved) draft shows within
 * a few seconds on a page that is already open (the bridge's viewer mode), and a revoked link is refused.
 */
export async function shareChecks(c: ShareContext): Promise<void> {
  const { rec, cms, channel, page } = c
  const id = page.id
  let share: { id: string; token: string; url: string } | undefined
  const draft = `${page.title} (shared)`
  const relayed = `${page.title} (relayed)`
  c.cleanups.push(async () => {
    await cms.manage('DELETE', `/preview-relay/${id}?locale=${channel}`).catch(() => undefined)
    await cms.manage('DELETE', `/items/${id}/draft?locale=${channel}`).catch(() => undefined)
    if (share) await cms.manage('DELETE', `/preview-shares/${share.id}`).catch(() => undefined)
  })

  await rec.check(G, 'a share link opens outside a frame and shows the draft', async () => {
    await cms.manage('PATCH', `/items/${id}`, { locale: channel, fields: { title: draft } })
    const r = await cms.manage('POST', '/preview-shares', { item: id, channel, locale: channel, ttl: '1h' })
    assert(r.token?.startsWith('hps_') && r.url && r.id, `unexpected share reply ${JSON.stringify(r)}`)
    share = { id: r.id, token: r.token, url: c.onFrontend(r.url) }
    const reply = await get(share.url, { jar: new Jar(), follow: 8 })
    equal(reply.status, 200, 'status with the share link')
    assert(h1s(reply.body).includes(draft), `no <h1> reads "${draft}" (found ${JSON.stringify(h1s(reply.body))})`)
  })
  if (!share) return

  let browser: any
  let pg: any
  let ctx: any
  const nav401: number[] = []
  try {
    if (!c.browser) rec.skip(G, 'a relayed draft shows on an open page', 'run with --browser (needs Playwright installed)')
    else {
      const pw = await loadPlaywright(c.playwrightFrom)
      if (!pw) rec.skip(G, 'a relayed draft shows on an open page', 'Playwright is not installed here')
      else {
        browser = await pw.chromium.launch()
        ctx = await browser.newContext()
        pg = await ctx.newPage()
        pg.on('response', (r: any) => {
          if (r.request().isNavigationRequest() && r.status() === 401) nav401.push(Date.now())
        })
        await rec.check(G, 'a relayed (unsaved) draft shows on the open page within 5 s', async () => {
          await pg.goto(share!.url)
          await pg.waitForFunction((t: string) => document.querySelector('h1')?.textContent?.trim() === t, draft, { timeout: 8000 })
          await sleep(500) // the viewer connects after load
          const doc = await cms.manage('GET', `/items/${id}?locale=${channel}&draft=true`)
          const put = await cms.manage('PUT', `/preview-relay/${id}?locale=${channel}`, { fields: { ...doc.fields, title: relayed } })
          assert(typeof put.rev === 'number', `PUT /preview-relay returned ${JSON.stringify(put)}`)
          await pg.waitForFunction((t: string) => document.querySelector('h1')?.textContent?.trim() === t, relayed, { timeout: 5000 }).catch(async () => {
            throw new Error(`the open page's <h1> reads "${await pg.evaluate(() => document.querySelector('h1')?.textContent?.trim())}", want "${relayed}" within 5 s (does the page load the bridge, and is it in viewer mode?)`)
          })
          const stored = await cms.manage('GET', `/items/${id}?locale=${channel}&draft=true`)
          equal(stored.fields.title, draft, 'the saved draft (a relay must not save)')
        })
      }
    }

    // refused: a 401, or a frontend that falls back to the published page (no draft title, no preview meta)
    const showsDraft = (body: string) => h1s(body).some((t) => t === draft || t === relayed) || /<meta[^>]+name=["']hydrogen-preview["']/i.test(body)
    await rec.check(G, 'a revoked share link is refused', async () => {
      await cms.manage('DELETE', `/preview-shares/${share!.id}`)
      const t = Date.now()
      for (;;) {
        const r = await get(share!.url, { jar: new Jar(), follow: 8 })
        if (r.status === 401 || (r.status === 200 && !showsDraft(r.body))) break
        assert(Date.now() - t < 15_000, `still ${r.status} and showing the draft 15 s after revoking`)
        await sleep(500)
      }
    })

    if (pg)
      await rec.check(G, 'an open page reloads into the refusal after the revoke', async () => {
        const t = Date.now()
        for (;;) {
          if (nav401.length) break
          const gone = await pg
            .evaluate((w: string[]) => !w.includes(document.querySelector('h1')?.textContent?.trim() ?? '') && !document.querySelector('meta[name="hydrogen-preview"]'), [draft, relayed])
            .catch(() => false) // mid-navigation
          if (gone) break
          assert(Date.now() - t < 15_000, 'the open page still shows the draft 15 s after the revoke (no reload into a refusal or the published page)')
          await sleep(200)
        }
      })
  } finally {
    await ctx?.close().catch(() => undefined)
    await browser?.close().catch(() => undefined)
    // The draft edit and the relayed draft would show in every later preview of this item (other groups expect
    // the fixture's draft): undo both now, not at the end of the suite.
    await cms.manage('DELETE', `/preview-relay/${id}?locale=${channel}`).catch(() => undefined)
    await cms.manage('DELETE', `/items/${id}/draft?locale=${channel}`).catch(() => undefined)
  }
}
