// Fails when a package's dist/*.js (all modules, concatenated; the bridge's IIFE copy is not counted) exceeds its gzip budget (5 KB; the Next adapter 7.25 KB (share links: week-long cookie): proxy, cache and preview plus the live-preview wrapper; the bridge 5.5 KB with canvas editing and viewer mode).
import { readFileSync, readdirSync } from 'node:fs'
import { gzipSync } from 'node:zlib'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const budgets = { client: 5, router: 5, next: 7.25, 'admin-sdk': 5, bridge: 5.5, react: 5, hook: 2 }
let failed = false
for (const pkg of ['client', 'router', 'bridge', 'react', 'next', 'admin-sdk', 'hook']) {
  const dir = join(root, pkg, 'dist')
  let src
  try { src = Buffer.concat(readdirSync(dir).filter((f) => f.endsWith('.js') && !f.endsWith('.iife.js')).sort().map((f) => readFileSync(join(dir, f)))) } catch { console.error(`${pkg}: ${dir} missing, run npm run build`); process.exit(1) }
  const gz = gzipSync(src, { level: 9 }).length
  const budget = budgets[pkg] * 1024
  const over = gz > budget
  failed ||= over
  console.log(`@hydrogencms/${pkg}: ${src.length} B raw, ${gz} B gzip (${(gz / 1024).toFixed(2)} KB of ${budgets[pkg]} KB)${over ? '  OVER BUDGET' : ''}`)
}
process.exit(failed ? 1 : 0)
