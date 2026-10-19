/**
 * Hydrogen purge tags (`i:<id>`, `c:<type>`, `s:<singleton>`, `r:<site>`, `nav:<site>`, `*`) are used as Next
 * cache tags unchanged, so a webhook's `tags` can be handed to `revalidateTag` as they are.
 */

import { ALL_TAG, validTag as valid } from '@hydrogencms/client/server'

export { ALL_TAG }
/** Next's limits on cache tags. */
export const MAX_TAGS = 128

const rank = (t: string) => (t === ALL_TAG ? 0 : t.startsWith('r:') || t.startsWith('nav:') ? 1 : t.startsWith('c:') || t.startsWith('s:') ? 2 : 3)

/**
 * The cache tags of a response: `*` (so one webhook tag purges everything) plus the response's own
 * Surrogate-Key tags. Beyond Next's limit the item tags are dropped first and `truncated` is set, so the caller
 * can give the entry a short lifetime instead.
 */
export function cacheTags(responseTags: readonly string[]): { tags: string[]; truncated: boolean } {
  const all = [...new Set([ALL_TAG, ...responseTags.filter(valid)])].sort((a, b) => rank(a) - rank(b) || (a < b ? -1 : 1))
  return { tags: all.slice(0, MAX_TAGS), truncated: all.length > MAX_TAGS }
}
