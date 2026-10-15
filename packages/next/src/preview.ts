/** Draft preview: the cookie that carries the CMS preview token and validation of the preview route's input. */

export const PREVIEW_COOKIE = 'hydrogen-preview'
export const PREVIEW_PARAM = 'token'
const MAX_TOKEN = 2048

/** Preview tokens are `<base64url>.<base64url>`; anything else is not worth storing. */
export const validToken = (t: unknown): t is string => typeof t === 'string' && t.length > 0 && t.length <= MAX_TOKEN && /^[\w.-]+$/.test(t)

/** A same-site path: one leading slash, no scheme, no backslash or control characters (no open redirect). */
export function safePath(p: string | null | undefined): string | undefined {
  if (!p) return '/'
  if (p[0] !== '/' || p[1] === '/' || p[1] === '\\' || /[\\\u0000-\u001f\u007f]/.test(p)) return undefined
  return p
}

export type PreviewRequest = { exit: true; path: string } | { exit?: false; token: string; path: string } | { error: string }

/** Reads the preview route's query: `token` and `path`, or `exit=1` to leave preview. */
export function parsePreviewRequest(url: URL): PreviewRequest {
  const path = safePath(url.searchParams.get('path'))
  if (path === undefined) return { error: 'path must be a path on this site' }
  if (url.searchParams.get('exit')) return { exit: true, path }
  const token = url.searchParams.get(PREVIEW_PARAM)
  if (!validToken(token)) return { error: 'token is missing or malformed' }
  return { token, path }
}

/**
 * Cookie attributes: httpOnly, scoped to the site, living as long as a preview token can (an hour at most).
 * Over https it is SameSite=None like Next's own draft mode cookie, so the admin can embed the preview in a frame.
 */
export function previewCookieOptions(secure: boolean): { httpOnly: true; sameSite: 'lax' | 'none'; secure: boolean; path: string; maxAge: number } {
  return { httpOnly: true, sameSite: secure ? 'none' : 'lax', secure, path: '/', maxAge: 3600 }
}

/** The token from a cookie value, if it is one. */
export const tokenFromCookie = (v: string | undefined): string | undefined => (validToken(v) ? v : undefined)

/** `/preview/<collection>/<id>`: the URL the channels' preview patterns point at for items with no route yet. */
export function matchPreviewPath(pathname: string, base: string): { collection: string; id: string } | undefined {
  const m = pathname.startsWith(base + '/') ? pathname.slice(base.length + 1).split('/') : []
  if (m.length !== 2 || !m[0] || !m[1]) return undefined
  try {
    return { collection: decodeURIComponent(m[0]), id: decodeURIComponent(m[1]) }
  } catch {
    return undefined
  }
}
