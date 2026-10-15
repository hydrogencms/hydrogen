import { test } from 'node:test'
import assert from 'node:assert/strict'
import { parseArgs } from '../dist/args.js'

test('required options, flags and lists', () => {
  const p = parseArgs(['--cms', 'http://c', '--token=t', '--frontend', 'http://f', '--browser', '--channels', 'en, de', '--only', 'pages,seo', '--revalidate-timeout', '5'])
  assert.deepEqual(p.options, {
    cms: 'http://c', token: 't', frontend: 'http://f', adminOrigin: undefined, browser: true, playwrightFrom: undefined,
    channels: ['en', 'de'], only: ['pages', 'seo'], revalidateTimeoutMs: 5000,
  })
})

test('the environment fills --cms and --token', () => {
  const p = parseArgs(['--frontend', 'http://f'], { HYDROGEN_URL: 'http://c', HYDROGEN_TOKEN: 't' })
  assert.equal(p.options.cms, 'http://c')
  assert.equal(p.options.token, 't')
  assert.equal(p.options.browser, false)
})

test('usage errors name the problem', () => {
  assert.match(parseArgs(['--cms', 'http://c']).help, /missing --token, --frontend/)
  assert.match(parseArgs(['--bogus', '1']).help, /unknown option --bogus/)
  assert.match(parseArgs(['x']).help, /unexpected argument x/)
  assert.match(parseArgs(['--cms']).help, /--cms needs a value/)
  assert.match(parseArgs(['--cms', 'c', '--token', 't', '--frontend', 'f', '--revalidate-timeout', 'soon']).help, /number of seconds/)
  assert.ok(parseArgs(['--help']).help.includes('hydrogen-adapter-test'))
  assert.ok(!parseArgs(['--help']).error)
})
