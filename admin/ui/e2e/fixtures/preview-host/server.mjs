// A tiny stand-in for a customer's frontend, for the admin's preview e2e. Previews for channel de are rendered by
// this server from the CMS draft and use the bridge in reload mode; everything else renders in the browser (live mode).
//   PORT, CMS_URL (delivery API base), ADMIN_ORIGIN, BRIDGE_JS (packages/bridge/dist/bridge.iife.js)
import { createServer } from 'node:http'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const port = Number(process.env.PORT || 18600)
const cms = process.env.CMS_URL || 'http://127.0.0.1:18434'
const admin = process.env.ADMIN_ORIGIN || cms
const bridge = readFileSync(process.env.BRIDGE_JS || join(here, '../../../../../packages/bridge/dist/bridge.iife.js'))
const renderSrc = readFileSync(join(here, 'render.js'))
await import('./render.js')

const page = (body, head = '') => `<!doctype html><html><head><meta charset="utf-8"><title>host</title>${head}</head><body>${body}</body></html>`

const server = createServer(async (req, res) => {
  const u = new URL(req.url, 'http://x')
  if (u.pathname === '/bridge.js') return void res.writeHead(200, { 'content-type': 'text/javascript' }).end(bridge)
  if (u.pathname === '/render.js') return void res.writeHead(200, { 'content-type': 'text/javascript' }).end(renderSrc)
  res.setHeader('content-type', 'text/html; charset=utf-8')
  let claims = {}
  try {
    claims = JSON.parse(Buffer.from((u.searchParams.get('preview') || '').split('.')[0], 'base64url').toString())
  } catch {}
  // The preview token names the channel: de pages are server-rendered (reload mode), the others render in the browser.
  if (claims.ch === 'de') {
    let fields = {}
    try {
      const r = await fetch(`${cms}/api/deliver/v1/items/${claims.item}?channel=de&locale=de`, { headers: { 'X-Hydrogen-Preview': u.searchParams.get('preview') } })
      fields = (await r.json()).item?.fields ?? {}
    } catch {}
    return void res.end(
      page(
        `<main data-h-root data-mode="reload">${globalThis.renderDoc({ fields })}</main>` +
          `<script src="/bridge.js" data-auto data-admin-origin="${admin}" data-mode="reload"></script>`,
      ),
    )
  }
  res.end(
    page(
      `<main data-h-root data-mode="live"><p id="wait">waiting</p></main><script src="/bridge.js"></script><script src="/render.js"></script>` +
        `<script>
const root = document.querySelector('main')
const b = HydrogenBridge.createBridge({
  adminOrigin: ${JSON.stringify(admin)}, mode: 'live',
  onInit: (m) => { root.innerHTML = renderDoc(m.doc); b.rendered(m.rev) },
  onPatch: (doc, m) => { root.innerHTML = renderDoc(doc); b.rendered(m.rev) },
})
b.connect()
</script>`,
    ),
  )
})
server.listen(port, '127.0.0.1', () => console.log(`preview host on ${port}`))
