/** The events a hook can handle (the `event` of the hook's registration and of every request). */
export type HookEvent =
  | 'beforeValidate' | 'validate' | 'beforeSave' | 'beforePublish' | 'afterPublish'
  | 'beforeDelete' | 'afterDelete' | 'beforeUnpublish' | 'afterUnpublish'

/** The item a hook runs for. */
export interface HookItem {
  id: string
  type: string
  key?: string
  version: number
  mainLocale: string
}

/** What a hook reads from stdin (ABI v1). `doc` is the merged document of the locale; null on delete and unpublish events. */
export interface HookRequest<Doc = Record<string, unknown>, E extends HookEvent = HookEvent> {
  v: 1
  event: E
  locale: string
  item: HookItem
  doc: Doc | null
}

/** A field error (`validate` and `beforePublish`). */
export interface HookFieldError {
  path: string
  message: string
}

/**
 * What a hook writes to stdout; every key is optional. `set` is for beforeValidate and beforeSave, `errors` for
 * validate and beforePublish, `error` blocks any before* event. A key the event does not allow fails the call.
 */
export interface HookResponse<Doc = Record<string, unknown>> {
  set?: Partial<Doc>
  errors?: HookFieldError[]
  error?: string
}

/** A hook function: synchronous, returns the response (nothing = no change). */
export type HookFn<Doc = Record<string, unknown>, E extends HookEvent = HookEvent> =
  (req: HookRequest<Doc, E>) => HookResponse<Doc> | void | undefined

/** The streams Javy provides with `-J javy-stream-io` (`Javy.readSync` / `Javy.writeSync`; older builds: `Javy.IO`). */
export interface JavyIO {
  readSync(fd: number, buffer: Uint8Array): number
  writeSync(fd: number, buffer: Uint8Array): number
}

function io(): JavyIO {
  const j = (globalThis as { Javy?: Partial<JavyIO> & { IO?: JavyIO } }).Javy
  const x = j?.IO ?? j
  if (!x || typeof x.readSync !== 'function' || typeof x.writeSync !== 'function') {
    throw new Error('Javy stream I/O is missing: build the hook with `hydrogen hooks build` (javy build -J javy-stream-io -J text-encoding)')
  }
  return x as JavyIO
}

function readAll(x: JavyIO): string {
  const chunks: Uint8Array[] = []
  let total = 0
  for (;;) {
    const buf = new Uint8Array(8192)
    const n = x.readSync(0, buf)
    if (n === 0) break
    chunks.push(buf.subarray(0, n))
    total += n
  }
  const all = new Uint8Array(total)
  let at = 0
  for (const c of chunks) { all.set(c, at); at += c.length }
  return new TextDecoder().decode(all)
}

function writeAll(x: JavyIO, text: string): void {
  let buf = new TextEncoder().encode(text)
  while (buf.length > 0) {
    const n = x.writeSync(1, buf)
    if (n <= 0) throw new Error('could not write the response')
    buf = buf.subarray(n)
  }
}

/**
 * Registers the hook: reads the request from stdin, calls `fn` and writes its response to stdout. Type the document
 * with a generated type: `defineHook<Article>((req) => ({ set: { readingMinutes: 3 } }))`.
 */
export function defineHook<Doc = Record<string, unknown>, E extends HookEvent = HookEvent>(fn: HookFn<Doc, E>): void {
  const x = io()
  const req = JSON.parse(readAll(x)) as HookRequest<Doc, E>
  const res = fn(req) as unknown
  if (res && typeof (res as { then?: unknown }).then === 'function') throw new Error('hooks must be synchronous: do not return a Promise (no async functions)')
  writeAll(x, JSON.stringify(res ?? {}))
}
