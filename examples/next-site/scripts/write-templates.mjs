// Writes hydrogen-templates.json (the template keys the app declares) from the prerendered route handler.
import { copyFileSync, existsSync } from 'node:fs'

const src = '.next/server/app/hydrogen-templates.json.body'
if (!existsSync(src)) throw new Error(`${src} missing: the templates route was not prerendered`)
copyFileSync(src, 'hydrogen-templates.json')
console.log('hydrogen-templates.json written')
