import { createProxy } from '@hydrogencms/next/proxy'
import { channels } from '@/lib/channels'

// Redirects (with the CMS's status) and real 404s are decided here: once a page streams, its status is fixed.
export const proxy = createProxy({
  baseUrl: process.env.HYDROGEN_URL ?? 'http://localhost:8080',
  channels,
  origin: process.env.HYDROGEN_ORIGIN,
  templates: ['page', 'landing', 'article'],
  fallback: true,
})

// Not for static files, Next internals and route handlers.
export const config = { matcher: ['/((?!api/|_next/|.*\\.[a-z0-9]+$).*)'] }
