/** Verification of the `Hydrogen-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, t + "." + body)>` header. */

export const SIGNATURE_HEADER = 'Hydrogen-Signature'
export const DEFAULT_TOLERANCE_MS = 5 * 60 * 1000
export const MIN_SECRET_LENGTH = 16

const enc = new TextEncoder()

const unhex = (s: string): Uint8Array<ArrayBuffer> | undefined => {
  if (s.length % 2 || !/^[\da-f]*$/i.test(s)) return undefined
  const out = new Uint8Array(new ArrayBuffer(s.length / 2))
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return out
}

export function parseSignature(header: string | null | undefined): { t: string; sigs: string[] } | undefined {
  if (!header) return undefined
  let t = ''
  const sigs: string[] = []
  for (const part of header.split(',')) {
    const i = part.indexOf('=')
    if (i < 0) return undefined
    const k = part.slice(0, i).trim()
    const v = part.slice(i + 1).trim()
    if (k === 't') t = v
    else if (k === 'v1') sigs.push(v)
  }
  return /^\d+$/.test(t) && sigs.length ? { t, sigs } : undefined
}

/**
 * True when one of the header's v1 signatures matches the raw body and the signed time is within the tolerance
 * of `now` (either direction). The MAC comparison is `crypto.subtle.verify`, which is constant time.
 */
export async function verifySignature(
  secret: string,
  header: string | null | undefined,
  body: Uint8Array,
  now: number = Date.now(),
  toleranceMs: number = DEFAULT_TOLERANCE_MS,
): Promise<boolean> {
  const p = parseSignature(header)
  if (!p || !secret) return false
  if (Math.abs(now - Number(p.t) * 1000) > toleranceMs) return false
  const prefix = enc.encode(p.t + '.')
  const data = new Uint8Array(new ArrayBuffer(prefix.length + body.length))
  data.set(prefix)
  data.set(body, prefix.length)
  const key = await crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['verify'])
  let ok = false
  for (const s of p.sigs) {
    const sig = unhex(s)
    if (sig && (await crypto.subtle.verify('HMAC', key, sig, data))) ok = true
  }
  return ok
}

/** Reads a request body of at most `cap` bytes; undefined when it is larger (Content-Length first, then a running count). */
export async function readCapped(req: { headers: { get(n: string): string | null }; body: ReadableStream<Uint8Array> | null }, cap: number): Promise<Uint8Array | undefined> {
  const len = req.headers.get('content-length')
  if (len !== null && Number(len) > cap) return undefined
  const parts: Uint8Array[] = []
  let n = 0
  if (req.body) {
    const r = req.body.getReader()
    for (;;) {
      const { done, value } = await r.read()
      if (done) break
      n += value.length
      if (n > cap) {
        await r.cancel()
        return undefined
      }
      parts.push(value)
    }
  }
  const out = new Uint8Array(n)
  let o = 0
  for (const p of parts) (out.set(p, o), (o += p.length))
  return out
}
