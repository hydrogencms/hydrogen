import type { Options } from './suite.js'

export const USAGE = `hydrogen-adapter-test: conformance suite for Hydrogen frontend adapters

  hydrogen-adapter-test --cms <url> --token <manage token> --frontend <url> [options]

The CMS must hold the examples/starter fixture (hydrogen import examples/starter/fixture) and have the frontend
under test as its channels' frontend. The suite publishes and edits one page and one post and puts them back.

  --cms <url>              the CMS (HYDROGEN_URL)
  --token <token>          a management token with every right (HYDROGEN_TOKEN)
  --frontend <url>         the frontend under test, e.g. http://localhost:3000
  --admin-origin <url>     origin the frontend accepts bridge messages from (default: the CMS's)
  --browser                run the bridge checks in a browser (Playwright from your install)
  --playwright-from <dir>  directory to resolve Playwright from (default: the working directory)
  --channels <a,b>         web channels to test (default en,de)
  --only <groups>          comma-separated groups: pages links seo notfound redirects reserved blog preview bridge revalidate
  --revalidate-timeout <s> seconds to wait for a published change to show (default 20)
  --json                   print the report as JSON
`

export type Parsed = { options: Options; json: boolean } | { help: string; error?: boolean }

const flags = new Set(['browser', 'json'])

/** Parses the command line; environment variables HYDROGEN_URL and HYDROGEN_TOKEN fill --cms and --token. */
export function parseArgs(argv: string[], env: Record<string, string | undefined> = {}): Parsed {
  const v = new Map<string, string | true>()
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]!
    if (a === '-h' || a === '--help') return { help: USAGE }
    if (!a.startsWith('--')) return { help: `unexpected argument ${a}\n\n${USAGE}`, error: true }
    const eq = a.indexOf('=')
    const name = a.slice(2, eq < 0 ? undefined : eq)
    if (flags.has(name)) v.set(name, true)
    else {
      const val = eq < 0 ? argv[++i] : a.slice(eq + 1)
      if (val === undefined) return { help: `--${name} needs a value\n\n${USAGE}`, error: true }
      v.set(name, val)
    }
  }
  const known = new Set([...flags, 'cms', 'token', 'frontend', 'admin-origin', 'playwright-from', 'channels', 'only', 'revalidate-timeout'])
  for (const k of v.keys()) if (!known.has(k)) return { help: `unknown option --${k}\n\n${USAGE}`, error: true }
  const s = (k: string) => (typeof v.get(k) === 'string' ? (v.get(k) as string) : undefined)
  const cms = s('cms') ?? env.HYDROGEN_URL
  const token = s('token') ?? env.HYDROGEN_TOKEN
  const frontend = s('frontend')
  const missing = [!cms && '--cms', !token && '--token', !frontend && '--frontend'].filter(Boolean)
  if (missing.length) return { help: `missing ${missing.join(', ')}\n\n${USAGE}`, error: true }
  const list = (k: string) => s(k)?.split(',').map((x) => x.trim()).filter(Boolean)
  const secs = s('revalidate-timeout')
  if (secs !== undefined && !(Number(secs) > 0)) return { help: `--revalidate-timeout must be a number of seconds\n\n${USAGE}`, error: true }
  return {
    json: v.has('json'),
    options: {
      cms: cms!, token: token!, frontend: frontend!, adminOrigin: s('admin-origin'), browser: v.has('browser'),
      playwrightFrom: s('playwright-from'), channels: list('channels'), only: list('only'),
      revalidateTimeoutMs: secs ? Number(secs) * 1000 : undefined,
    },
  }
}
