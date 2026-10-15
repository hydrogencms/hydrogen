/**
 * Hydrogen purge tags (`i:<id>`, `c:<type>`, `s:<singleton>`, `r:<site>`, `nav:<site>`, `*`) are used as Next
 * cache tags unchanged, so a webhook's `tags` can be handed to `revalidateTag` as they are.
 */

export const ALL_TAG = '*'
/** Next's limits on cache tags. */
export const MAX_TAGS = 128
export const MAX_TAG_LENGTH = 256

const valid = (t: unknown): t is string => typeof t === 'string' && t.length > 0 && t.length <= MAX_TAG_LENGTH && !/\s/.test(t)

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

/** The tags to revalidate for a webhook body (`{"tags": [...]}`); anything malformed is ignored. */
export function webhookTags(body: unknown): string[] {
  const t = (body as { tags?: unknown } | null)?.tags
  return Array.isArray(t) ? [...new Set(t.filter(valid))].slice(0, 1000) : []
}
