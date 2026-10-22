/** Preview bridge, frontend (child) side of protocol v1: see docs/bridge-protocol.md. */

export const SDK = '@hydrogencms/bridge@0.3.0'
export const PREVIEW_COOKIE = 'hydrogen-preview'

export type Json = null | boolean | number | string | Json[] | { [k: string]: Json }
export type PatchOp =
  | { op: 'add' | 'replace'; path: string; value: unknown }
  | { op: 'remove'; path: string }

export class PatchError extends Error {
  override name = 'PatchError'
}

type Obj = Record<string, unknown>
const isArr = Array.isArray
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !isArr(v)

function pointer(path: string): string[] {
  if (path === '') return []
  if (path[0] !== '/') throw new PatchError(`invalid pointer "${path}"`)
  return path.slice(1).split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'))
}

function apply(node: any, t: string[], i: number, o: PatchOp & { value?: unknown }): unknown {
  const k = t[i] as string
  const arr = isArr(node)
  if (!arr && !isObj(node)) throw new PatchError(`${o.path}: parent is not a container`)
  const last = i === t.length - 1
  const add = last && o.op === 'add'
  const dash = add && k === '-'
  let at: any = k
  if (arr) {
    at = dash ? node.length : +k
    if (!dash && (!/^(0|[1-9]\d*)$/.test(k) || at > node.length - (add ? 0 : 1))) throw new PatchError(`${o.path}: bad array index "${k}"`)
  } else if (!add && !Object.hasOwn(node, k)) throw new PatchError(`${o.path}: no such path "${k}"`)
  const c: any = arr ? node.slice() : { ...node }
  if (!last) c[at] = apply(c[at], t, i + 1, o)
  else if (o.op === 'remove') arr ? c.splice(at, 1) : delete c[k]
  else if (add && arr) c.splice(at, 0, o.value)
  else c[at] = o.value
  return c
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
        if (o.op === 'remove') throw new PatchError('cannot remove root')
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

export interface TextRef {
  path: string
  text: string
  scope: string
}
export interface LayoutNode {
  path: string
  rect: [number, number, number, number]
  block?: string
  text?: boolean
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

const norm = (s: string) => s.replace(/\s+/g, ' ').trim()
const SKIP = /^(script|style|noscript|template)$/i

/** Canvas editing: measures nodes for the admin's overlay, reports pointer events and edits text in place. */
function canvas(w: W, post: (m: Obj) => void, raf: (f: () => void) => void) {
  const d = w.document
  let on = false
  let texts: TextRef[] = []
  let map = new Map<Element, string>()
  let pend = false
  let last = ''
  let hover: string | null = null
  let ab = new AbortController()
  let mo: MutationObserver | undefined
  let ro: ResizeObserver | undefined
  let ed: { el: HTMLElement; path: string; multi: boolean; kids: Node[]; text: Text; orig: string; ac: AbortController } | null = null

  const collect = () => {
    if (ed && !ed.el.isConnected) end(false)
    const els: [Element, string][] = Array.from(d.body.querySelectorAll('[data-h-path]'), (e) => [e, e.getAttribute('data-h-path') as string])
    const n = els.length
    const lists = new Map<string, [string, Element][]>()
    // a path with an explicit marker is never mapped by text
    const used = new Set<unknown>(els.map((x) => x[1]))
    for (const t of texts) {
      if (used.has(t.path)) continue
      let l = lists.get(t.scope)
      if (!l) {
        lists.set(t.scope, (l = []))
        const sc = t.scope ? d.querySelector(`[data-h-path="${t.scope.replace(/["\\]/g, '\\$&')}"]`) : d.querySelector('[data-h-root]') ?? d.body
        const walk = (p: Node) =>
          p.childNodes.forEach((c) => {
            if (c.nodeType === 3) {
              const e = c.parentElement as Element
              if (e.closest('[data-h-path]') === (t.scope ? sc : null)) (l as [string, Element][]).push([norm((c as Text).data), e])
            } else if (c.nodeType === 1 && !SKIP.test(c.nodeName)) walk(c)
          })
        if (sc) walk(sc)
      }
      const want = norm(t.text)
      const hit = l.find(([s, e]) => s === want && !used.has(e))
      if (hit && !hit[1].hasAttribute('data-h-path')) {
        used.add(hit[1])
        els.push([hit[1], t.path])
      }
    }
    if (els.length > n) els.sort((a, b) => (a[0] === b[0] ? 0 : a[0].compareDocumentPosition(b[0]) & 4 ? -1 : 1))
    map = new Map(els)
    return els
  }
  const layout = () => {
    const nodes: LayoutNode[] = []
    for (const [e, path] of collect()) {
      const r = e.getBoundingClientRect()
      const rect = [r.left, r.top, r.width, r.height].map(Math.round) as LayoutNode['rect']
      const block = e.getAttribute('data-h-block')
      const c = e.firstChild
      if (rect[2] && rect[3]) nodes.push({ path, rect, ...(block ? { block } : {}), ...(e.childNodes.length === 1 && c?.nodeType === 3 && (c as Text).data ? { text: true } : {}) })
    }
    return nodes
  }
  const sched = () => {
    if (pend) return
    pend = true
    raf(() => {
      pend = false
      if (!on) return
      const m = { type: 'layout', nodes: layout(), vw: w.innerWidth, vh: w.innerHeight }
      const j = JSON.stringify(m)
      if (j !== last) (last = j), post(m)
    })
  }
  const find = (path: unknown) => (collect(), [...map].find(([, p]) => p === path)?.[0])
  const nodeOf = (t: EventTarget | null) => {
    for (let e = t as Element | null; e; e = e.parentElement) {
      const p = map.get(e)
      const r = p !== undefined && e.getBoundingClientRect()
      if (r && r.width && r.height) return p
    }
    return null
  }
  const hov = (p: string | null) => p !== hover && post({ type: 'hover', path: (hover = p) })
  const stop = (e: Event) => {
    if (ed?.el.contains(e.target as Node)) return
    e.preventDefault()
    e.stopImmediatePropagation()
    if (e.type !== 'auxclick' && e.type !== 'submit') post({ type: 'select', path: (collect(), nodeOf(e.target)), edit: e.type === 'dblclick' })
  }
  const end = (commit: boolean) => {
    const e = ed
    if (!e) return
    ed = null
    const t = (e.multi ? e.el.innerText : null) ?? e.el.textContent ?? ''
    const v = commit ? (e.multi ? t : norm(t)) : e.orig
    e.ac.abort()
    e.el.removeAttribute('contenteditable')
    e.el.replaceChildren(...e.kids)
    e.text.data = v
    post({ type: 'inlineEdit', path: e.path, ...(commit ? { value: v } : { cancel: true }) })
  }
  const off = () => {
    end(false)
    if (!on) return
    on = false
    ab.abort()
    mo?.disconnect()
    ro?.disconnect()
    hover = null
    map = new Map()
  }
  const on_ = () => {
    on = true
    ab = new AbortController()
    const signal = ab.signal
    const on2 = (t: EventTarget, type: string, f: (e: any) => void, o?: AddEventListenerOptions) => t.addEventListener(type, f, { ...o, signal })
    for (const t of ['click', 'dblclick', 'auxclick', 'submit']) on2(w, t, stop, CAP)
    on2(w, 'pointermove', (e) => hov(nodeOf(e.target)), { capture: true, passive: true })
    on2(d.documentElement, 'pointerleave', () => hov(null))
    on2(w, 'scroll', sched, { capture: true, passive: true })
    on2(w, 'resize', sched)
    mo = w.MutationObserver && new w.MutationObserver(sched)
    mo?.observe(d.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['data-h-path', 'data-h-block', 'class', 'style', 'hidden'] })
    ro = w.ResizeObserver && new w.ResizeObserver(sched)
    ro?.observe(d.documentElement)
  }

  return {
    sched: () => on && ((last = ''), sched()),
    off,
    overlay(m: Obj) {
      texts = (isArr(m.texts) ? m.texts : []).filter((t): t is TextRef => isObj(t) && ['path', 'text', 'scope'].every((k) => typeof t[k] === 'string'))
      if (m.on !== true) return off()
      if (!on) on_()
      last = ''
      sched()
    },
    scroll(m: Obj) {
      if (typeof m.by === 'number') return w.scrollBy({ top: m.by, behavior: 'instant' })
      find(m.path)?.scrollIntoView({ block: 'nearest', behavior: w.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    },
    edit(path: string, multi: boolean) {
      end(true)
      const el = find(path) as HTMLElement | undefined
      const text = el?.firstChild as Text | null | undefined
      if (!el || el.childNodes.length !== 1 || text?.nodeType !== 3) return post({ type: 'inlineEdit', path, cancel: true })
      el.setAttribute('contenteditable', 'plaintext-only')
      const plain = el.contentEditable === 'plaintext-only'
      if (!plain) el.setAttribute('contenteditable', 'true')
      const ac = new AbortController()
      const h = (e: any) => {
        if (e.type === 'blur') end(true)
        else if (e.type === 'paste') (e.preventDefault(), d.execCommand('insertText', false, e.clipboardData?.getData('text/plain') ?? ''))
        else if (e.isComposing) return
        else if (e.key === 'Escape') (e.preventDefault(), e.stopPropagation(), end(false))
        else if (e.key === 'Enter' && (!multi || e.metaKey || e.ctrlKey)) (e.preventDefault(), e.stopPropagation(), end(true))
      }
      for (const t of plain ? ['keydown', 'blur'] : ['keydown', 'blur', 'paste']) el.addEventListener(t, h, { signal: ac.signal })
      ed = { el, path, multi, kids: Array.from(el.childNodes), text: text as Text, orig: (text as Text).data, ac }
      try {
        // focus() puts the caret at the start: keep a selection made inside (a double click), else go to the end
        const s = w.getSelection() as Selection
        const k = el.contains(s.anchorNode)
        el.focus()
        k || (s.selectAllChildren(el), s.collapseToEnd())
      } catch {
        // no selection API: the caret stays where the browser put it
      }
    },
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
  /** viewer mode (outside a frame): the events URL, or `false` to disable (default `${adminOrigin}/api/deliver/v1/preview/events`) */
  relay?: string | false
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
  if (!/^https?:\/\/[^/*]+$/.test(origin)) throw new Error(`bridge: bad adminOrigin "${origin}"`)
  let rev = -1
  let doc: unknown
  let t0 = 0
  let on = false
  let es: EventSource | undefined
  const now = () => (w.performance ?? Date).now()
  const post = (m: Obj) => w.parent.postMessage({ h: 1, ...m }, origin)
  const ready = () => {
    const preview = previewProof(w)
    post({ type: 'ready', mode: o.mode, edit: true, sdk: SDK, url: w.location.href, ...(preview ? { preview } : {}) })
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
  const raf = (f: () => void) => (w.requestAnimationFrame ? w.requestAnimationFrame(f) : setTimeout(f, 0))
  const cv = canvas(w, post, raf)
  const resync = () => post({ type: 'resync', rev })
  const error = (message: string) => on && post({ type: 'error', message })

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
      } else if (m.type === 'overlay') cv.overlay(m)
      else if (m.type === 'scrollTo') cv.scroll(m)
      else if (m.type === 'editText') typeof m.path === 'string' && cv.edit(m.path, m.multiline === true)
      else if (m.type === 'reload') {
        const r = m.rev as number
        if (o.onReload) void o.onReload(r)
        else if (w.origin === 'null') reload()
        else {
          reloadSwap({ window: w, ready: () => (ready(), cv.sched()) }).then((ms) => bridge.rendered(r, ms), (x) => (x?.fetch ? reload() : error(String((x as Error)?.message ?? x))))
        }
      }
    } catch (x) {
      error(String((x as Error)?.message ?? x))
    }
  }

  // Viewer mode: a preview page outside a frame (a share link) follows the relayed draft over Server-Sent Events.
  const view = () => {
    const proof = previewProof(w)
    if (es || o.relay === false || !proof || !w.EventSource) return
    const u = new URL(o.relay || `${origin}/api/deliver/v1/preview/events`)
    u.searchParams.set('proof', proof)
    let last: unknown
    let busy = false
    let queued = false
    // one swap in flight and at most one queued (it fetches the latest page), so an older response never lands last
    const swap = () => {
      if (busy) return void (queued = true)
      busy = true
      reloadSwap({ window: w })
        .catch(reload)
        .finally(() => {
          busy = false
          if (queued) (queued = false), swap()
        })
    }
    es = new w.EventSource(u.href)
    es.addEventListener('draft', (e) => {
      try {
        const m = JSON.parse((e as MessageEvent).data)
        if (o.mode === 'live') return o.onInit?.({ item: m.item.id, itemType: m.item.type, locale: m.item.locale, rev: m.rev, doc: m.item })
        const key = m.v ?? m.rev
        if (last === undefined || key !== last) {
          const first = last === undefined
          last = key
          if (!first) swap()
        }
      } catch (x) {
        error(String((x as Error)?.message ?? x))
      }
    })
    es.addEventListener('end', () => (es?.close(), reload()))
  }

  const bridge: Bridge = {
    connect() {
      if (on || es) return
      if (!(o.preview ?? isPreview(w))) return
      if (w.parent === w) return view()
      on = true
      w.addEventListener('message', onMessage)
      for (const t of ['click', 'auxclick']) w.addEventListener(t, follow, CAP)
      ready()
    },
    disconnect() {
      es?.close()
      es = undefined
      if (on) {
        w.removeEventListener('message', onMessage)
        for (const t of ['click', 'auxclick']) w.removeEventListener(t, follow, CAP)
        cv.off()
      }
      on = false
    },
    rendered(r, ms) {
      if (!on) return
      raf(() => raf(() => (post({ type: 'rendered', rev: r, ms: ms ?? Math.max(0, Math.round(now() - t0)) }), cv.sched())))
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
