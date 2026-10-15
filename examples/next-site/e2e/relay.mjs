// A webhook relay for the e2e script: the CMS delivers to POST /hook, the relay holds the deliveries until
// POST /release forwards them (with their original headers and body) to the Next app. That makes "the page is
// still cached before the webhook arrives" testable. GET /queued returns how many are held; POST /clear drops them.
// usage: node relay.mjs <port> <target url>
import { createServer } from 'node:http'

const [port, target] = [Number(process.argv[2]), process.argv[3]]
let held = []

const send = (res, code, body) => {
  res.writeHead(code, { 'content-type': 'application/json' })
  res.end(JSON.stringify(body))
}

createServer(async (req, res) => {
  const chunks = []
  for await (const c of req) chunks.push(c)
  const body = Buffer.concat(chunks)
  if (req.url === '/hook') {
    held.push({ headers: req.headers, body })
    return send(res, 200, { held: held.length })
  }
  if (req.url === '/queued') return send(res, 200, { queued: held.length })
  if (req.url === '/clear') {
    held = []
    return send(res, 200, {})
  }
  if (req.url === '/release') {
    const out = []
    for (const h of held.splice(0)) {
      const r = await fetch(target, { method: 'POST', body: h.body, headers: {
        'content-type': 'application/json', 'hydrogen-signature': h.headers['hydrogen-signature'], 'hydrogen-event': h.headers['hydrogen-event'] } })
      out.push(r.status)
    }
    return send(res, 200, { statuses: out })
  }
  send(res, 404, {})
}).listen(port, '127.0.0.1')
