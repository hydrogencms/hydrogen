import { useRuntimeConfig } from 'nitropack/runtime'
import type { Client } from '@hydrogencms/client'
import type { Resolved } from '@hydrogencms/router'
import type { HydrogenConfig } from './delivery.js'

/**
 * What the server middleware leaves on `event.context.hydrogen` for the rendering of the request. The composables
 * read it; code of your own (server routes that render nothing) normally has no use for it.
 */
export interface HydrogenContext {
  /** a client of a channel (default: the request's): cached and tagged, or uncached with the preview token in preview */
  client(channel?: string): Client<any>
  /** the request's channel: the one matched by host and prefix, else the first configured */
  channel: string
  channels: string[]
  adminOrigin: string
  /** the visitor's preview token and its SHA-256 hex (what the page proves to the bridge) */
  preview?: { token: string; hash: string }
  /** the CMS's answer for the URL; unset for routes of your own that the middleware did not resolve */
  resolved?: Resolved
  /** the CMS could not be asked (down, slow) */
  unavailable?: boolean
}

declare module 'h3' {
  interface H3EventContext { hydrogen?: HydrogenContext }
}

export function config(): HydrogenConfig {
  const c = useRuntimeConfig().hydrogen as HydrogenConfig
  return { ...c, adminOrigin: new URL(c.adminOrigin || c.baseUrl).origin }
}
