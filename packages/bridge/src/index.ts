/** Preview bridge, frontend (child) side of protocol v1: see docs/bridge-protocol.md. */

export const SDK = '@hydrogencms/bridge@0.1.0'
export const PREVIEW_COOKIE = 'hydrogen-preview'

export type Json = null | boolean | number | string | Json[] | { [k: string]: Json }
export type PatchOp =
  | { op: 'add' | 'replace'; path: string; value: unknown }
  | { op: 'remove'; path: string }

export class PatchError extends Error {
  constructor(m: string) {
    super(m)
    this.name = 'PatchError'
  }
}

type Obj = Record<string, unknown>
const isArr = Array.isArray
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !isArr(v)

function pointer(path: string): string[] {
  if (path === '') return []
  if (path[0] !== '/') throw new PatchError(`invalid pointer "${path}": must be empty or start with "/"`)
  return path.slice(1).split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'))
}

function index(arr: unknown[], t: string, path: string, max: number): number {
  if (!/^(0|[1-9]\d*)$/.test(t) || +t > max) throw new PatchError(`${path}: bad array index "${t}" (length ${arr.length})`)
  return +t
}

function child(node: unknown, t: string, path: string): unknown {
  if (isArr(node)) return node[index(node, t, path, node.length - 1)]
  if (isObj(node) && Object.hasOwn(node, t)) return node[t]
  throw new PatchError(`${path}: no such path "${t}"`)
}

function put(node: unknown, t: string, v: unknown, path: string): unknown {
  if (isArr(node)) {
    const c = node.slice()
    c[index(node, t, path, node.length - 1)] = v
    return c
  }
  return { ...(node as Obj), [t]: v }
}

function apply(node: unknown, t: string[], i: number, o: PatchOp): unknown {
  const k = t[i] as string
  if (i < t.length - 1) return put(node, k, apply(child(node, k, o.path), t, i + 1, o), o.path)
  const { op, path } = o
  if (isArr(node)) {
    if (op === 'add') {
      const at = k === '-' ? node.length : index(node, k, path, node.length)
      return [...node.slice(0, at), (o as { value: unknown }).value, ...node.slice(at)]
    }
    const at = index(node, k, path, node.length - 1)
    if (op === 'remove') return [...node.slice(0, at), ...node.slice(at + 1)]
    return put(node, k, (o as { value: unknown }).value, path)
  }
  if (!isObj(node)) throw new PatchError(`${path}: parent is not an object or array`)
  if (op === 'add') return { ...node, [k]: (o as { value: unknown }).value }
  if (!Object.hasOwn(node, k)) throw new PatchError(`${path}: no such path "${k}"`)
  if (op === 'replace') return { ...node, [k]: (o as { value: unknown }).value }
  const { [k]: _, ...rest } = node
  return rest
}

/** RFC 6902 add/remove/replace, immutably (untouched subtrees are shared). Throws PatchError; `doc` is never mutated. */
export function applyPatch<T = unknown>(doc: T, ops: readonly PatchOp[]): T {
  let d: unknown = doc
  ops.forEach((raw, n) => {
    const o = raw as PatchOp
    try {
      if (!isObj(raw) || (o.op !== 'add' && o.op !== 'remove' && o.op !== 'replace')) throw new PatchError(`unsupported op "${(raw as Obj)?.op}"`)
      if (o.op !== 'remove' && !('value' in (raw as Obj))) throw new PatchError(`${(raw as Obj).op} needs a value`)
      const t = pointer(o.path)
      if (t.includes('__proto__')) throw new PatchError(`${o.path}: forbidden key`)
      if (!t.length) {
        if (o.op === 'remove') throw new PatchError('cannot remove the document root')
        d = o.value
      } else d = apply(d, t, 0, o)
    } catch (e) {
      if (e instanceof PatchError) e.message = `ops[${n}] ${o?.op} ${o?.path}: ${e.message}`
      throw e
    }
  })
  return d as T
}

export type Mode = 'live' | 'reload'

type W = Window & typeof globalThis

/** True when the page was opened for preview: `?preview=` in the URL, the `hydrogen-preview` cookie (when the
 * server left it readable) or `<meta name="hydrogen-preview">` (set by the frontend when its server knows). */
export function isPreview(win: W = window as W): boolean {
  try {
    if (new URLSearchParams(win.location.search).get('preview')) return true
    if (win.document.cookie.split(/;\s*/).some((c) => c.startsWith(PREVIEW_COOKIE + '='))) return true
    return !!win.document.querySelector('meta[name="hydrogen-preview"]')
  } catch {
    return false
  }
}

const CAP = { capture: true }
const HASH = /^[0-9a-f]{64}$/i

/** The proof of preview: the SHA-256 hex of the preview token, from `<meta name="hydrogen-preview">` (the old content `1` has none). */
export function previewProof(win: W = window as W): string | undefined {
  try {
    const c = win.document.querySelector('meta[name="hydrogen-preview"]')?.getAttribute('content') ?? ''
    return HASH.test(c) ? c : undefined
  } catch {
    return undefined
  }
}

export interface BridgeOptions {
  adminOrigin: string
  mode: Mode
  onInit?: (init: { item: string; itemType: string; locale: string; rev: number; doc: any }) => void
  onPatch?: (doc: any, patch: { rev: number; base: number; ops: PatchOp[] }) => void
  /** reload mode: replaces the default (reloadSwap + rendered) */
  onReload?: (rev: number) => void | Promise<void>
  /** override the preview detection (default isPreview()) */
  preview?: boolean
  window?: W
}

export interface Bridge {
  connect(): void
  disconnect(): void
  /** report that the DOM reflects `rev` (after two animation frames); ms defaults to the time since the message */
  rendered(rev: number, ms?: number): void
  error(message: string): void
}

export function createBridge(o: BridgeOptions): Bridge {
  const w = o.window ?? (window as W)
  const origin = o.adminOrigin
  if (!/^https?:\/\/[^/*]+$/.test(origin)) throw new Error(`bridge: adminOrigin must be an origin like https://admin.example.com, got "${origin}"`)
  let rev = -1
  let doc: unknown
  let t0 = 0
  let on = false
  const now = () => (w.performance ?? Date).now()
  const post = (m: Obj) => w.parent.postMessage({ h: 1, ...m }, origin)
  const ready = () => {
    const preview = previewProof(w)
    post({ type: 'ready', mode: o.mode, sdk: SDK, url: w.location.href, ...(preview ? { preview } : {}) })
  }
  const reload = () => w.location.reload()
  // Same-site links keep `?preview=<token>` (a frame without cookies navigates inside preview).
  const follow = (e: Event) => {
    try {
      const a = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
      const token = new URLSearchParams(w.location.search).get('preview')
      if (!a || !token) return
      const u = new URL(a.href)
      if (u.origin !== w.location.origin || u.searchParams.has('preview')) return
      u.searchParams.set('preview', token)
      a.href = u.href
    } catch {
      // leave the link alone
    }
  }
  const resync = () => post({ type: 'resync', rev })
  const error = (message: string) => post({ type: 'error', message })

  const onMessage = (e: MessageEvent) => {
    const m = e.data
    if (e.origin !== origin || e.source !== w.parent || !isObj(m) || m.h !== 1) return
    t0 = now()
    try {
      if (m.type === 'init') {
        doc = m.doc
        rev = m.rev as number
        o.onInit?.(m as never)
      } else if (m.type === 'patch') {
        if (m.base !== rev) return resync()
        try {
          doc = applyPatch(doc, m.ops as PatchOp[])
        } catch (x) {
          error((x as Error).message)
          return resync()
        }
        rev = m.rev as number
        o.onPatch?.(doc, m as never)
      } else if (m.type === 'reload') {
        const r = m.rev as number
        if (o.onReload) void o.onReload(r)
        else if (w.origin === 'null') reload()
        else {
          reloadSwap({ window: w, ready }).then((ms) => bridge.rendered(r, ms), (x) => (x?.fetch ? reload() : error(String(x?.message ?? x))))
        }
      }
    } catch (x) {
      error(String((x as Error)?.message ?? x))
    }
  }

  const bridge: Bridge = {
    connect() {
      if (on || w.parent === w || !(o.preview ?? isPreview(w))) return
      on = true
      w.addEventListener('message', onMessage)
      for (const t of ['click', 'auxclick']) w.addEventListener(t, follow, CAP)
      ready()
    },
    disconnect() {
      if (on) {
        w.removeEventListener('message', onMessage)
        for (const t of ['click', 'auxclick']) w.removeEventListener(t, follow, CAP)
      }
      on = false
    },
    rendered(r, ms) {
      if (!on) return
      const raf = w.requestAnimationFrame?.bind(w) ?? ((f: () => void) => setTimeout(f, 0))
      raf(() => raf(() => post({ type: 'rendered', rev: r, ms: ms ?? Math.max(0, Math.round(now() - t0)) })))
    },
    error,
  }
  return bridge
}

export interface SwapOptions {
  /** CSS selector of the element to replace (default `[data-h-root]`; without a match, the body's children) */
  root?: string
  window?: W
  fetch?: typeof fetch
  /** called after the swap (the bridge re-posts `ready` here) */
  ready?: () => void
}

/** Reload mode: refetch the current URL, swap the root, keep the scroll position; resolves with the milliseconds taken. */
export async function reloadSwap(o: SwapOptions = {}): Promise<number> {
  const w = o.window ?? (window as W)
  const t = w.performance.now()
  const res = await (o.fetch ?? w.fetch.bind(w))(w.location.href, { credentials: 'include', headers: { accept: 'text/html' } }).catch((e) => {
    throw Object.assign(new Error(`reload: ${e?.message ?? e}`), { fetch: true })
  })
  if (!res.ok) throw new Error(`reload: ${res.status} ${res.statusText}`)
  const gone = new Error('reload: preview expired or lost')
  if (res.redirected) throw gone
  const next = new w.DOMParser().parseFromString(await res.text(), 'text/html')
  const d = w.document
  const sel = o.root ?? '[data-h-root]'
  const [x, y] = [w.scrollX, w.scrollY]
  const cur = d.querySelector(sel)
  const nxt = next.querySelector(sel)
  if (cur && !nxt) throw gone
  if (cur && nxt) cur.replaceWith(d.importNode(nxt, true))
  else d.body.replaceChildren(...Array.from(next.body.childNodes, (n) => d.importNode(n, true)))
  if (next.title) d.title = next.title
  w.scrollTo(x, y)
  o.ready?.()
  return Math.round(w.performance.now() - t)
}

/** IIFE entry: `<script src=bridge.iife.js data-auto data-admin-origin=… data-mode=reload>` starts a bridge. */
export function autoStart(s: { dataset: DOMStringMap } | null, win?: W): Bridge | undefined {
  if (!s || s.dataset.auto === undefined || !s.dataset.adminOrigin) return
  const b = createBridge({ adminOrigin: s.dataset.adminOrigin, mode: s.dataset.mode === 'live' ? 'live' : 'reload', window: win })
  b.connect()
  return b
}
