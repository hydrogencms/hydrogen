// Fails when a package's dist/*.js (all modules, concatenated; the bridge's IIFE copy is not counted) exceeds its gzip budget (5 KB; the Next adapter 7.25 KB (share links: week-long cookie): proxy, cache and preview plus the live-preview wrapper; the bridge 5.5 KB with canvas editing and viewer mode; the SvelteKit, Nuxt and Astro adapters 7 KB each, framework excluded). Separate entry points of a package that browsers don't load together are budgeted on their own: the client's `server` subpath (the adapters' shared server code) 3.5 KB, its `richtext` subpath 1.25 KB, the bridge's `merge` subpath 1 KB.
import { readFileSync, readdirSync } from 'node:fs'
import { gzipSync } from 'node:zlib'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
// [label, directory, budget in KB, the files counted (default: every .js but the IIFE copy)]
const entries = [
  ['client', 'client', 5, ['index.js']],
  ['client/server', 'client', 3.5, ['server.js']],
  ['client/richtext', 'client', 1.25, ['richtext.js']],
  ['router', 'router', 5],
  ['bridge', 'bridge', 5.5, ['index.js']],
  ['bridge/merge', 'bridge', 1, ['merge.js']],
  ['react', 'react', 5],
  ['next', 'next', 7.25],
  ['sveltekit', 'sveltekit', 7],
  ['nuxt', 'nuxt', 7],
  ['astro', 'astro', 8],
  ['admin-sdk', 'admin-sdk', 5],
  ['hook', 'hook', 2],
]
let failed = false
for (const [label, pkg, kb, only] of entries) {
  const dir = join(root, pkg, 'dist')
  let src
  try {
    src = Buffer.concat([
      ...listJs(dir).filter((f) => (only ? only.includes(f) : !f.endsWith('.iife.js'))).sort().map((f) => readFileSync(join(dir, f))),
      // component sources that ship next to the JS (SvelteKit's dist/*.svelte, Astro's components/*.astro)
      ...(only ? [] : listJs(join(root, pkg, 'components'), '', '.astro').sort().map((f) => readFileSync(join(root, pkg, 'components', f)))),
    ])
  } catch { console.error(`${pkg}: ${dir} missing, run npm run build`); process.exit(1) }
  const gz = gzipSync(src, { level: 9 }).length
  const over = gz > kb * 1024
  failed ||= over
  console.log(`@hydrogencms/${label}: ${src.length} B raw, ${gz} B gzip (${(gz / 1024).toFixed(2)} KB of ${kb} KB)${over ? '  OVER BUDGET' : ''}`)
}
process.exit(failed ? 1 : 0)

// .js (and .svelte) files below dist (Astro's routes/ are modules too), as paths relative to it
function listJs(dir, rel = '', ext = '') {
  let entries
  try { entries = readdirSync(join(dir, rel), { withFileTypes: true }) } catch { return [] }
  return entries.flatMap((e) => (e.isDirectory() ? listJs(dir, join(rel, e.name), ext) : (ext ? e.name.endsWith(ext) : e.name.endsWith('.js') || e.name.endsWith('.svelte')) ? [join(rel, e.name)] : []))
}
