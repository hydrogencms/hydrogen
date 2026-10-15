/** HTTP for the checks: manual redirects, a cookie jar, and the CMS's two APIs. */

export interface Reply {
  status: number
  url: string
  headers: Headers
  body: string
}

/** Cookies by host name (ports and attributes are ignored: enough for the preview cookie). */
export class Jar {
  private byHost = new Map<string, Map<string, string>>()
  store(url: string, res: Response): void {
    const host = new URL(url).hostname
    const jar = this.byHost.get(host) ?? new Map<string, string>()
    for (const c of res.headers.getSetCookie()) {
      const [pair = ''] = c.split(';')
      const i = pair.indexOf('=')
      if (i < 1) continue
      const name = pair.slice(0, i).trim()
      const value = pair.slice(i + 1).trim()
      if (/;\s*max-age=0\b/i.test(c) || value === '') jar.delete(name)
      else jar.set(name, value)
    }
    this.byHost.set(host, jar)
  }
  header(url: string): string | undefined {
    const jar = this.byHost.get(new URL(url).hostname)
    return jar?.size ? [...jar].map(([k, v]) => `${k}=${v}`).join('; ') : undefined
  }
  /** Cookies for the browser: `[{name, value}]`. */
  list(url: string): { name: string; value: string }[] {
    return [...(this.byHost.get(new URL(url).hostname) ?? [])].map(([name, value]) => ({ name, value }))
  }
}

export interface GetOptions {
  jar?: Jar
  headers?: Record<string, string>
  /** follow redirects by hand, at most this many (default: none, the 3xx is the reply) */
  follow?: number
  timeoutMs?: number
}

export async function get(url: string, o: GetOptions = {}): Promise<Reply & { chain: Reply[] }> {
  const chain: Reply[] = []
  let u = url
  for (let hop = 0; ; hop++) {
    const headers: Record<string, string> = { accept: 'text/html,application/json;q=0.9,*/*;q=0.8', ...o.headers }
    const cookie = o.jar?.header(u)
    if (cookie) headers.cookie = cookie
    const res = await fetch(u, { redirect: 'manual', headers, signal: AbortSignal.timeout(o.timeoutMs ?? 20_000) })
    o.jar?.store(u, res)
    const r: Reply = { status: res.status, url: u, headers: res.headers, body: await res.text() }
    chain.push(r)
    const loc = res.headers.get('location')
    if (res.status >= 300 && res.status < 400 && loc && hop < (o.follow ?? 0)) {
      u = new URL(loc, u).toString()
      continue
    }
    return { ...r, chain }
  }
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

/** The CMS: management API with the token, delivery API without. */
export class Cms {
  base: string
  constructor(base: string, private token: string) {
    this.base = base.replace(/\/+$/, '')
  }
  private async call<T>(method: string, path: string, body?: unknown, auth = true): Promise<T> {
    const headers: Record<string, string> = {}
    if (auth) headers.authorization = `Bearer ${this.token}`
    if (body !== undefined) headers['content-type'] = 'application/json'
    const res = await fetch(this.base + path, {
      method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(20_000),
    })
    const text = await res.text()
    if (!res.ok) throw new ApiError(res.status, `${method} ${path}: ${res.status} ${text.slice(0, 300)}`)
    return (text ? JSON.parse(text) : undefined) as T
  }
  manage = <T = any>(method: string, path: string, body?: unknown) => this.call<T>(method, '/api/manage/v1' + path, body)
  deliver = <T = any>(path: string) => this.call<T>('GET', '/api/deliver/v1' + path, undefined, false)
  async deliverStatus(path: string): Promise<number> {
    return (await fetch(this.base + '/api/deliver/v1' + path, { redirect: 'manual' })).status
  }
}
