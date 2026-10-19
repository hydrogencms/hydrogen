import { createError, useHead, useRequestEvent, useState } from 'nuxt/app'
import type { Client, Item } from '@hydrogencms/client'
import type { Metadata } from '@hydrogencms/router'
import type { HydrogenContext } from './context.js'

/** The middleware's findings for this request: server only (it holds the preview token and the clients). */
function context(): HydrogenContext | undefined {
  return import.meta.server ? (useRequestEvent()?.context as { hydrogen?: HydrogenContext } | undefined)?.hydrogen : undefined
}

export interface HydrogenPage<F = any> {
  /** the content type of the item, or `*` for the fallback template */
  template: string
  item: Item<F>
  locale: string
  metadata: Metadata
  /** the visitor is previewing a draft */
  preview: boolean
}

/**
 * The CMS client for pages' own data (navigation, settings, collections). Call it in `setup` and use it inside
 * `useAsyncData` handlers: they run on the server, and the result travels to the browser in the payload. Responses
 * are cached and tagged; in preview the client is uncached and carries the visitor's token.
 */
export function useHydrogen() {
  const c = context()
  return {
    /** a client of a channel (default: the request's) */
    client: <T extends object = Record<string, unknown>>(channel?: string): Client<T> => {
      if (!c) throw new Error('@hydrogencms/nuxt: server only (link pages with plain <a> links)')
      return c.client(channel) as Client<T>
    },
    /** the request's channel, matched by host and prefix (the first configured outside them) */
    channel: c?.channel ?? '',
    channels: c?.channels ?? [],
    preview: !!c?.preview,
  }
}

/** Preview pages prove the token to the bridge and stay out of search engines. Call it from routes of your own too. */
export function useHydrogenPreview(): boolean {
  const hash = useState<string | null>('hydrogen:preview', () => context()?.preview?.hash ?? null)
  if (hash.value) useHead({ meta: [{ name: 'hydrogen-preview', content: hash.value }, { name: 'robots', content: 'noindex' }] })
  return !!hash.value
}

/**
 * The page the CMS located for the URL, with its head (title, canonical, hreflang alternates, preview markers).
 * Not found and an unreachable CMS become real 404 and 503 responses.
 */
export function useHydrogenPage<F = any>(): HydrogenPage<F> {
  const c = context()
  const page = useState<HydrogenPage | null | 'unavailable'>('hydrogen:page', () => {
    const r = c?.resolved
    return r?.kind === 'page' ? { template: r.template, item: r.item, locale: r.locale, metadata: r.metadata, preview: !!c?.preview } : c?.unavailable ? 'unavailable' : null
  })
  const p = page.value
  if (!p || p === 'unavailable') throw createError({ statusCode: p ? 503 : 404, fatal: true })
  const m = p.metadata
  useHead({
    title: m.title,
    link: ([
      ...(m.canonical ? [{ rel: 'canonical', href: m.canonical }] : []),
      ...m.alternates.map((a) => ({ rel: 'alternate', hreflang: a.hreflang, href: a.href })),
    ]) as any,
  })
  useHydrogenPreview()
  return p
}
