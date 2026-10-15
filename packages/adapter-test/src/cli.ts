#!/usr/bin/env node
import { parseArgs } from './args.js'
import { runSuite } from './suite.js'
import { format } from './report.js'

const parsed = parseArgs(process.argv.slice(2), process.env)
if ('help' in parsed) {
  console.log(parsed.help)
  process.exit(parsed.error ? 2 : 0)
}
const o = parsed.options
const t = Date.now()
const report = await runSuite({
  ...o,
  onResult: (r) => {
    if (!parsed.json) process.stderr.write(r.status === 'pass' ? '.' : r.status === 'fail' ? 'F' : 's')
  },
})
if (!parsed.json) process.stderr.write('\n')
console.log(parsed.json ? JSON.stringify(report, null, 2) : format(report) + `(${((Date.now() - t) / 1000).toFixed(1)} s)\n`)
process.exit(report.ok ? 0 : 1)
