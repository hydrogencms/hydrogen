import { alternates, blockMarkers, canonical, h1s, pathMarkers, tags } from './html.js'
import { alternatesByPath, diffMultiset, expectedBlocks, linkIds, onFrontend, pathOf, underPrefix, withoutPreview } from './expect.js'
import { Cms, Jar, get } from './http.js'
import { Recorder, assert, equal, summarize } from './report.js'
import type { Report, Result } from './report.js'
import { bridgeChecks } from './browser.js'
import { shareChecks } from './share.js'

export interface Options {
  /** the CMS, e.g. http://127.0.0.1:8080 */
  cms: string
  /** a management token with every right */
  token: string
  /** the frontend under test, e.g. http://localhost:3000 */
  frontend: string
  /** the origin the frontend accepts bridge messages from (default: the CMS's origin) */
  adminOrigin?: string
  /** run the bridge checks in a browser (needs Playwright) */
  browser?: boolean
  /** a directory to resolve Playwright from (default: the working directory) */
  playwrightFrom?: string
  /** web channels to test (default `en,de`, the fixture's) */
  channels?: string[]
  /** check groups to run (default all) */
  only?: string[]
  /** how long revalidation after publish may take (default 20 s) */
  revalidateTimeoutMs?: number
  /** called for every result as it happens */
  onResult?: (r: Result) => void
}

export const GROUPS = ['pages', 'links', 'seo', 'notfound', 'redirects', 'reserved', 'blog', 'preview', 'share', 'bridge', 'revalidate'] as const

interface PageInfo {
  channel: string
  path: string
  item: any
  located: any
  html: string
  status: number
}

const enc = encodeURIComponent
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** Runs the conformance suite against a frontend and a CMS loaded with the starter fixture. */
export async function runSuite(o: Options): Promise<Report> {
  const cms = new Cms(o.cms, o.token)
  const fe = o.frontend.replace(/\/+$/, '')
  const channels = o.channels?.length ? o.channels : ['en', 'de']
  const rec = new Recorder(o.onResult)
  const want = (g: string) => !o.only?.length || o.only.includes(g)
  const prefixes: Record<string, string> = {}
  const pages: PageInfo[] = []
  const cleanups: (() => Promise<void>)[] = []

  // -- discovery: the pages of every channel, from the CMS's own tree and route answers
  const ready = await rec.check('setup', 'CMS reachable, channels and pages found', async () => {
    for (const ch of channels) {
      const info = await cms.deliver(`/channels/${ch}`)
      prefixes[ch] = info.options?.match?.prefix ?? ''
      const tree = (await cms.deliver(`/tree?channel=${ch}&depth=8`)).nodes ?? []
      const urls: string[] = []
      const walk = (ns: any[]) => {
        for (const n of ns) {
          if (n.url && (n.kind === 'page' || n.kind === 'mount')) urls.push(n.url)
          if (n.nodes) walk(n.nodes)
        }
      }
      walk(tree)
      assert(urls.length > 0, `channel ${ch}: the tree has no pages (is the starter fixture imported?)`)
      const origin = new URL(urls[0]!).origin
      const all = [origin + (prefixes[ch] || '/'), ...urls]
      for (const u of all) {
        const r = await cms.deliver(`/route?channel=${ch}&url=${enc(u)}`)
        assert(r.located?.kind === 'page' && r.item, `channel ${ch}: ${u} does not resolve to a page`)
        const path = pathOf(u)
        const reply = await get(fe + path)
        pages.push({ channel: ch, path, item: r.item, located: r.located, html: reply.body, status: reply.status })
      }
    }
  })
  if (!ready) return summarize(rec.results)

  const titleOf = (i: any) => String(i.fields.title)

  if (want('pages'))
    for (const p of pages) {
      await rec.check('pages', `${p.path} renders`, () => {
        equal(p.status, 200, 'status')
        const t = titleOf(p.item)
        assert(h1s(p.html).includes(t), `no <h1> reads "${t}" (found ${JSON.stringify(h1s(p.html))})`)
        const bad = diffMultiset(expectedBlocks(p.item.fields.body), blockMarkers(p.html))
        assert(!bad, `data-h-block markers differ: ${bad}`)
        assert(pathMarkers(p.html).length === 0, 'data-h-path is emitted outside preview')
      })
    }

  if (want('links'))
    for (const p of pages) {
      const hrefs = [...new Set(linkIds(p.item.fields))]
      if (!hrefs.length) continue
      await rec.check('links', `${p.path} rich text links render with an href`, async () => {
        const anchors = new Set(tags(p.html, 'a').map((t) => t.attrs.href).filter(Boolean).map((h) => pathOf(h!)))
        const missing: string[] = []
        for (const id of hrefs) {
          const url = p.item.links?.[id]
          if (!url) missing.push(`${id}: delivery's links has no URL (a post needs a channel links: pattern)`)
          else if (!anchors.has(pathOf(url))) missing.push(`${id}: no <a href="${pathOf(url)}">`)
        }
        assert(!missing.length, missing.join('; '))
      })
    }

  if (want('seo'))
    for (const p of pages) {
      await rec.check('seo', `${p.path} canonical and hreflang`, () => {
        const c = canonical(p.html)
        assert(c, 'no <link rel="canonical">')
        assert(/^https?:\/\//.test(c), `canonical ${c} is not absolute`)
        equal(pathOf(c), pathOf(p.located.canonical), 'canonical path')
        const want = alternatesByPath(p.located.alternates)
        const got = Object.fromEntries(Object.entries(alternates(p.html)).map(([k, v]) => [k, pathOf(v)]))
        equal(Object.entries(got).sort(), Object.entries(want).sort(), 'hreflang alternates')
      })
    }

  if (want('notfound')) {
    const first = channels[0]!
    const p = prefixes[first]!
    for (const path of [`${p}/no-such-page-9f3a`, `${p}/about/no/such/deep/path`, `/zz-no-channel/about`]) {
      await rec.check('notfound', `${path} answers 404`, async () => {
        const r = await get(fe + path)
        equal(r.status, 404, 'status')
        assert(/html/i.test(r.headers.get('content-type') ?? ''), 'the 404 is not an HTML page')
      })
    }
    await rec.check('notfound', `${p}/blog/no-such-post-9f3a answers 404`, async () => {
      equal((await get(`${fe}${p}/blog/no-such-post-9f3a`)).status, 404, 'status')
    })
  }

  // -- redirects: the CMS's manual redirect items, as the CMS itself resolves them
  if (want('redirects')) {
    const list = (await cms.manage('GET', '/collections/redirect/items')).items as { id: string }[]
    if (!list.length) rec.skip('redirects', 'manual redirect', 'the CMS has no redirect items')
    for (const it of list) {
      const r = await cms.manage('GET', `/items/${it.id}`)
      const from = r.fields.from as string
      for (const ch of channels) {
        const path = (prefixes[ch] ?? '') + from
        const via = (await cms.deliver(`/route?channel=${ch}&url=${enc(onFrontend(path, fe))}`)).located
        if (via?.kind !== 'redirect') {
          rec.skip('redirects', path, `the CMS does not redirect it in channel ${ch}`)
          continue
        }
        await rec.check('redirects', `${path} answers ${via.resolution.status}`, async () => {
          const reply = await get(fe + path)
          equal(reply.status, via.resolution.status, 'status')
          const loc = reply.headers.get('location')
          assert(loc, 'no Location header')
          equal(pathOf(loc), pathOf(via.resolution.location), 'location')
          equal((await get(new URL(loc, fe).toString())).status, 200, 'the target')
        })
      }
    }
  }

  if (want('reserved'))
    for (const ch of channels) {
      await rec.check('reserved', `${prefixes[ch]}/blog/* and /api/* are the frontend's`, async () => {
        for (const rest of ['/blog/anything', '/api/anything']) {
          const r = (await cms.deliver(`/route?channel=${ch}&url=${enc(onFrontend((prefixes[ch] ?? '') + rest, fe))}`)).located
          equal(r?.kind, 'reserved', `CMS routing of ${rest}`)
        }
      })
    }

  // -- blog: a plain collection routed by the frontend
  const posts: Record<string, any[]> = {}
  for (const ch of channels) posts[ch] = (await cms.deliver(`/collections/post/items?channel=${ch}&locale=${ch}&limit=100`).catch(() => ({ items: [] }))).items ?? []
  if (want('blog'))
    for (const ch of channels) {
      if (!posts[ch]!.length) rec.skip('blog', `${ch} posts`, 'the CMS has no posts in this channel')
      else
        await rec.check('blog', `${prefixes[ch]}/blog lists every post`, async () => {
          const r = await get(`${fe}${prefixes[ch]}/blog`, { follow: 2 })
          equal(r.status, 200, 'status')
          const linked = new Set(tags(r.body, 'a').map((a) => pathOf(a.attrs.href ?? '').replace(/\/+$/, '')))
          const missing = posts[ch]!.map((p) => `${prefixes[ch]}/blog/${p.fields.slug}`).filter((p) => !linked.has(p))
          assert(!missing.length, `no link to ${missing.join(', ')}`)
        })
      for (const post of posts[ch]!) {
        const path = `${prefixes[ch]}/blog/${post.fields.slug}`
        await rec.check('blog', `${path} renders`, async () => {
          const r = await get(fe + path)
          equal(r.status, 200, 'status')
          assert(h1s(r.body).includes(post.fields.title), `no <h1> reads "${post.fields.title}" (found ${JSON.stringify(h1s(r.body))})`)
          const bad = diffMultiset(expectedBlocks(post.fields.body), blockMarkers(r.body))
          assert(!bad, `data-h-block markers differ: ${bad}`)
        })
      }
    }

  // -- draft preview through the CMS's own preview URLs
  const mint = async (channel: string, item: string, locale = channel) => {
    const r = await cms.manage('POST', '/preview-tokens', { channel, item, locale })
    assert(r.url, `the CMS returned no preview URL for ${item} (does the channel have a frontend and a preview pattern?)`)
    return { token: r.token as string, url: onFrontend(r.url, fe), plain: withoutPreview(r.url) }
  }
  const discard = (id: string, locale: string) => async () => {
    await cms.manage('DELETE', `/items/${id}/draft?locale=${locale}`).catch(() => undefined)
  }
  const preview = async (url: string) => get(url, { jar: new Jar(), follow: 8 })

  if (want('preview')) {
    const ch = channels[0]!
    // a page that was never published
    for (const type of ['landing', 'page']) {
      const items = (await cms.manage('GET', `/collections/${type}/items`)).items as any[]
      for (const it of items.filter((i) => i.status === 'draft' && i.version === 0)) {
        const locale = it.draftParts?.[0] ?? ch
        const channel = channels.find((c) => c === locale) ?? ch
        await rec.check('preview', `never-published page "${it.name}" renders with the token only`, async () => {
          const doc = await cms.manage('GET', `/items/${it.id}?locale=${locale}&draft=true`)
          const m = await mint(channel, it.id, locale)
          equal((await get(fe + m.plain)).status, 404, 'status without the token')
          const r = await preview(m.url)
          equal(r.status, 200, 'status with the token')
          assert(h1s(r.body).includes(String(doc.fields.title)), `no <h1> reads "${doc.fields.title}" (found ${JSON.stringify(h1s(r.body))})`)
          const bad = diffMultiset(expectedBlocks(doc.fields.body), blockMarkers(r.body))
          assert(!bad, `data-h-block markers differ: ${bad}`)
          assert(pathMarkers(r.body).length > 0, 'no data-h-path markers in preview')
        })
      }
    }
    const target = pages.find((p) => p.channel === ch && p.path !== (prefixes[ch] || '/'))
    if (target) {
      const id = target.item.id as string
      await rec.check('preview', `${target.path}: a draft edit shows with the token, not without`, async () => {
        cleanups.push(discard(id, ch))
        const draft = `${titleOf(target.item)} (draft)`
        await cms.manage('PATCH', `/items/${id}`, { locale: ch, fields: { title: draft } })
        const m = await mint(ch, id)
        const r = await preview(m.url)
        equal(r.status, 200, 'status with the token')
        assert(h1s(r.body).includes(draft), `no <h1> reads "${draft}" (found ${JSON.stringify(h1s(r.body))})`)
        const plain = await get(fe + target.path)
        assert(h1s(plain.body).includes(titleOf(target.item)), 'the published page shows the draft')
        await discard(id, ch)()
      })
    }
    const post = posts[ch]?.[0]
    if (post) {
      await rec.check('preview', `post ${post.fields.slug}: the preview URL pattern shows the draft`, async () => {
        cleanups.push(discard(post.id, ch))
        const draft = `${post.fields.title} (draft)`
        await cms.manage('PATCH', `/items/${post.id}`, { locale: ch, fields: { title: draft } })
        const m = await mint(ch, post.id)
        const r = await preview(m.url)
        equal(r.status, 200, `status with the token (${m.plain})`)
        assert(h1s(r.body).includes(draft), `no <h1> reads "${draft}" (found ${JSON.stringify(h1s(r.body))})`)
        equal((await get(`${fe}${prefixes[ch]}/blog/${post.fields.slug}`)).status, 200, 'the published post')
        await discard(post.id, ch)()
      })
    }
  }

  // -- share links and relayed drafts
  if (want('share')) {
    const ch = channels[0]!
    const target = pages.find((p) => p.channel === ch && p.path !== (prefixes[ch] || '/'))
    if (!target) rec.skip('share', 'share links', 'no page to share')
    else
      await shareChecks({
        rec, cms, frontend: fe, channel: ch, browser: !!o.browser, playwrightFrom: o.playwrightFrom, cleanups,
        page: { id: target.item.id, title: titleOf(target.item) }, onFrontend: (u) => onFrontend(u, fe),
      })
  }

  // -- bridge, in a browser
  if (want('bridge')) {
    if (!o.browser) rec.skip('bridge', 'handshake and live update', 'run with --browser (needs Playwright installed)')
    else {
      const ch = channels[0]!
      const target = pages.find((p) => p.channel === ch && p.path !== (prefixes[ch] || '/') && expectedBlocks(p.item.fields.body).length > 0)
      if (!target) rec.skip('bridge', 'handshake and live update', 'no page to preview')
      else {
        const ctx = {
          rec, cms, frontend: fe, adminOrigin: (o.adminOrigin ?? cms.base).replace(/\/+$/, ''), channel: ch,
          mint, playwrightFrom: o.playwrightFrom, cleanups,
        }
        await bridgeChecks({
          ...ctx,
          page: { id: target.item.id, type: target.item.type, path: target.path, title: titleOf(target.item), blocks: expectedBlocks(target.item.fields.body) },
        })
        const post = posts[ch]?.[0]
        if (post)
          await bridgeChecks({
            ...ctx, label: 'post', inert: false,
            page: { id: post.id, type: 'post', path: `${prefixes[ch]}/blog/${post.fields.slug}`, title: String(post.fields.title), blocks: expectedBlocks(post.fields.body) },
          })
      }
    }
  }

  // -- revalidation: publish a change and wait for the frontend to show it
  if (want('revalidate')) {
    const ch = channels[0]!
    const target = pages.find((p) => p.channel === ch && p.path !== (prefixes[ch] || '/'))
    if (!target) rec.skip('revalidate', 'publish shows on the frontend', 'no page')
    else {
      const id = target.item.id as string
      const orig = titleOf(target.item)
      const limit = o.revalidateTimeoutMs ?? 20_000
      const waitFor = async (title: string) => {
        const t = Date.now()
        for (;;) {
          const r = await get(fe + target.path)
          if (r.status === 200 && h1s(r.body).includes(title)) return Date.now() - t
          if (Date.now() - t > limit) throw new Error(`${target.path} still shows ${JSON.stringify(h1s(r.body))} ${limit} ms after publishing "${title}" (is the frontend's revalidation connected: the CMS webhook, or the module's purge?)`)
          await sleep(250)
        }
      }
      const publish = async (title: string) => {
        await cms.manage('PATCH', `/items/${id}`, { locale: ch, fields: { title } })
        await cms.manage('POST', `/items/${id}/publish`, {})
      }
      const changed = `${orig} (published)`
      let restored = false
      await rec.check('revalidate', `${target.path}: a published change shows`, async () => {
        await get(fe + target.path) // prime the frontend's cache
        await publish(changed)
        await waitFor(changed)
      })
      await rec.check('revalidate', `${target.path}: the original shows again`, async () => {
        await publish(orig)
        await waitFor(orig)
        restored = true
      })
      if (!restored) await publish(orig).catch(() => undefined)
    }
  }

  for (const c of cleanups) await c()
  return summarize(rec.results)
}
