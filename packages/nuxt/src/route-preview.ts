import { defineEventHandler, deleteCookie, getRequestHeader, getRequestURL, sendRedirect, setCookie, setResponseHeader, setResponseStatus } from 'h3'
import { PREVIEW_COOKIE, parsePreviewRequest, previewCookieOptions } from '@hydrogencms/client/server'

/** `GET /api/hydrogen/preview?token=…&path=/…` keeps the token in an httpOnly cookie and redirects; `?exit=1` leaves. */
export default defineEventHandler((event) => {
  const url = getRequestURL(event)
  const p = parsePreviewRequest(url)
  setResponseHeader(event, 'cache-control', 'private, no-store')
  if ('error' in p) {
    setResponseStatus(event, 400)
    return { error: p.error }
  }
  if (p.exit) deleteCookie(event, PREVIEW_COOKIE, { path: '/' })
  else {
    const secure = (getRequestHeader(event, 'x-forwarded-proto') ?? url.protocol.slice(0, -1)) === 'https'
    setCookie(event, PREVIEW_COOKIE, p.token, previewCookieOptions(secure))
  }
  return sendRedirect(event, p.path, 307)
})
