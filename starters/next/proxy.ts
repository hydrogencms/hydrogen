import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'
import { createClient } from '@hydrogencms/client'
import { PREVIEW_COOKIE } from '@hydrogencms/next'
import { createProxy } from '@hydrogencms/next/proxy'
import { channels } from '@/lib/channels'

const baseUrl = process.env.HYDROGEN_URL ?? 'http://localhost:8080'

// Redirects (with the CMS's status) and real 404s are decided here: once a page streams, its status is fixed.
const pages = createProxy({ baseUrl, channels, origin: process.env.HYDROGEN_ORIGIN, templates: ['page', 'landing'] })

const notFound = (req: NextRequest) => {
  const u = req.nextUrl.clone()
  u.pathname = '/_not-found'
  u.search = ''
  return NextResponse.rewrite(u, { status: 404 })
}

async function postExists(channel: string, slug: string): Promise<boolean> {
  try {
    const { data } = await createClient({ baseUrl, channel, fetch: (u, i) => fetch(u, { ...i, signal: AbortSignal.timeout(3000) }) })
      .list('post', { locale: channel, where: { slug }, limit: 1 })
    return data.items.length > 0
  } catch {
    return true // the CMS is down: let the page decide
  }
}

/**
 * Pages go through the CMS. Posts are the app's own routes (the CMS reserves `/blog/*`), so their 404 is decided
 * here. `/blog/{slug}` without a language is the CMS's preview URL of a post: only the preview hand-off applies.
 */
export async function proxy(req: NextRequest) {
  const { pathname, searchParams } = req.nextUrl
  const draft = req.cookies.has(PREVIEW_COOKIE)
  if (pathname.startsWith('/blog/') && !searchParams.has('preview')) return draft ? NextResponse.next() : notFound(req)
  const m = /^\/([^/]+)\/blog\/([^/]+)\/?$/.exec(pathname)
  if (m && channels.includes(m[1]!) && !draft && !(await postExists(m[1]!, decodeURIComponent(m[2]!)))) return notFound(req)
  return pages(req)
}

// Not for static files, Next internals and route handlers.
export const config = { matcher: ['/((?!api/|_next/|.*\\.[a-z0-9]+$).*)'] }
