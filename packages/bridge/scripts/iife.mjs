// Wraps dist/index.js (one module of plain exports) into dist/bridge.iife.js: window.HydrogenBridge, auto-started
// from the script tag's data attributes when data-auto is present.
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist')
let src = readFileSync(join(dist, 'index.js'), 'utf8')
const names = []
src = src.replace(/^export (async function|function|class|const) ([A-Za-z0-9_$]+)/gm, (_, k, n) => (names.push(n), `${k} ${n}`))
if (/^export /m.test(src) || /^import /m.test(src)) throw new Error('bridge: dist/index.js must hold plain export declarations only')
const out = `(function(){"use strict";${src}\nvar B={${names.join(',')}};window.HydrogenBridge=B;autoStart(document.currentScript)})();\n`
writeFileSync(join(dist, 'bridge.iife.js'), out)
