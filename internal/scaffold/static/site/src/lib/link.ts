import type { HydrogenLocals } from '@hydrogencms/astro/runtime'

/**
 * Where a block's link goes: a page picked in the admin (an item id, looked up in the page's `links`), else a URL typed
 * there. Pages get the site's base path; other origins stay as they are.
 */
export function target(h: HydrogenLocals, links: Record<string, string>, ref?: string, url?: string): string | undefined {
  const to = (ref && links[ref]) || url
  return to ? h.href(to) : undefined
}
