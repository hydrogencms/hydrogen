export type Status = 'pass' | 'fail' | 'skip'

export interface Result {
  group: string
  name: string
  status: Status
  detail?: string
  ms: number
}

export interface Report {
  results: Result[]
  passed: number
  failed: number
  skipped: number
  ok: boolean
}

export function summarize(results: Result[]): Report {
  const n = (s: Status) => results.filter((r) => r.status === s).length
  return { results, passed: n('pass'), failed: n('fail'), skipped: n('skip'), ok: n('fail') === 0 }
}

const mark: Record<Status, string> = { pass: 'ok  ', fail: 'FAIL', skip: 'skip' }

/** A readable report: results grouped, failures with their detail, a summary line. */
export function format(r: Report): string {
  const out: string[] = []
  let group = ''
  for (const x of r.results) {
    if (x.group !== group) {
      group = x.group
      out.push('', group)
    }
    out.push(`  ${mark[x.status]}  ${x.name}${x.status === 'pass' ? '' : x.detail ? `\n          ${x.detail.replace(/\n/g, '\n          ')}` : ''}`)
  }
  out.push('', `${r.passed} passed, ${r.failed} failed, ${r.skipped} skipped`)
  return out.join('\n').trimStart() + '\n'
}

/** Collects results; `check` runs one check and turns a throw into a failure. */
export class Recorder {
  results: Result[] = []
  constructor(private onResult?: (x: Result) => void) {}
  private add(x: Result) {
    this.results.push(x)
    this.onResult?.(x)
  }
  async check(group: string, name: string, fn: () => Promise<void> | void): Promise<boolean> {
    const t = Date.now()
    try {
      await fn()
      this.add({ group, name, status: 'pass', ms: Date.now() - t })
      return true
    } catch (e) {
      this.add({ group, name, status: 'fail', detail: e instanceof Error ? e.message : String(e), ms: Date.now() - t })
      return false
    }
  }
  skip(group: string, name: string, reason: string) {
    this.add({ group, name, status: 'skip', detail: reason, ms: 0 })
  }
}

export class Assertion extends Error {}
export function assert(cond: unknown, message: string): asserts cond {
  if (!cond) throw new Assertion(message)
}
export function equal<T>(got: T, want: T, what: string): void {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Assertion(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
}
