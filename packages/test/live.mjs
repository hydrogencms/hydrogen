// Live check against a running delivery API: HYDROGEN_URL=http://localhost:8080 [HYDROGEN_CHANNEL=web_en]
// [HYDROGEN_TOKEN=...] [HYDROGEN_PATH=/] node packages/test/live.mjs   (build the packages first)
import { createClient } from '../client/dist/index.js'
import { createRouter } from '../router/dist/index.js'

const baseUrl = process.env.HYDROGEN_URL
if (!baseUrl) { console.error('set HYDROGEN_URL'); process.exit(2) }
const client = createClient({ baseUrl, channel: process.env.HYDROGEN_CHANNEL, token: process.env.HYDROGEN_TOKEN })
const info = await client.channel(process.env.HYDROGEN_CHANNEL)
console.log('channel', JSON.stringify(info.data))
const tree = await client.tree({ depth: 2 })
console.log('tree nodes', tree.data.length, 'tags', tree.tags.join(' '))
for (const c of info.data.collections.slice(0, 3)) {
  try {
    const l = await client.list(c, { limit: 2, total: true })
    console.log('list', c, l.data.items.length, 'hasMore', l.data.hasMore, 'total', l.data.total)
  } catch (e) { console.log('list', c, 'skipped:', e.code, e.message) }
}
const router = createRouter({ client, templates: {}, fallback: 'any' })
const base = process.env.HYDROGEN_PATH ?? '/'
const r = await router.resolve(base.startsWith('http') ? base : (info.data.options.match?.host ? `https://${info.data.options.match.host}` : '') + base)
console.log('resolve', base, '->', r.kind, r.kind === 'page' ? `${r.item.name} canonical=${r.metadata.canonical} alternates=${r.metadata.alternates.length}` : '')
