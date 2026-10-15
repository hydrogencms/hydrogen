// A counting reverse proxy in front of the CMS for the e2e script: forwards everything and counts requests by path.
// GET /__count?prefix=/api/deliver/v1/tree returns {"n": …} (requests whose path starts with the prefix).
// usage: node counter.mjs <port> <cms base url>
import { createServer } from 'node:http'

const [port, cms] = [Number(process.argv[2]), process.argv[3]]
const seen = []

createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x')
  if (url.pathname === '/__count') {
    const prefix = url.searchParams.get('prefix') ?? ''
    res.writeHead(200, { 'content-type': 'application/json' })
    return res.end(JSON.stringify({ n: seen.filter((p) => p.startsWith(prefix)).length }))
  }
  seen.push(url.pathname + url.search)
  const headers = { ...req.headers }
  delete headers.host
  try {
    const up = await fetch(cms + req.url, { method: req.method, headers })
    const out = Object.fromEntries(up.headers)
    for (const h of ['content-encoding', 'content-length', 'transfer-encoding', 'connection']) delete out[h]
    res.writeHead(up.status, out)
    res.end(Buffer.from(await up.arrayBuffer()))
  } catch (e) {
    res.writeHead(502)
    res.end(String(e))
  }
}).listen(port, '127.0.0.1')
