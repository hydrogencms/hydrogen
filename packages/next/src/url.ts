/**
 * The request path, set by the proxy (which first deletes what the client sent) for the navigation, whose layout
 * cannot see the path. It only selects among the configured channels for that one response; pages never use it.
 */
export const PATH_HEADER = 'x-hydrogen-path'

interface HeaderReader { get(name: string): string | null }

/**
 * The public origin of a request: `origin` when given (dev, or a frontend served from another host than its
 * channel), else the forwarded or plain Host with the forwarded protocol (https by default). Only the host name
 * matters to channel matching; ports are ignored.
 */
export function originOf(h: HeaderReader, origin?: string, trustForwarded = true): string {
  if (origin) return origin.replace(/\/+$/, '')
  const first = (v: string | null) => v?.split(',')[0]?.trim()
  const host = (trustForwarded && first(h.get('x-forwarded-host'))) || first(h.get('host')) || 'localhost'
  return `${(trustForwarded && first(h.get('x-forwarded-proto'))) || 'https'}://${host}`
}

/** The URL the delivery `/route` is asked about: the public origin plus the request's (still encoded) path. */
export const publicUrl = (h: HeaderReader, pathname: string, origin?: string, trustForwarded = true) =>
  originOf(h, origin, trustForwarded) + (pathname || '/')
