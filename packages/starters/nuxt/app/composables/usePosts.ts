import type { HydrogenTypes } from '~/lib/types'

/** Posts are a plain collection: the pages module does not route them, so the app does (the CMS reserves `/blog/*`). */
export function usePosts() {
  const h = useHydrogen()
  return {
    bySlug: async (channel: string, slug: string) =>
      (await h.client<HydrogenTypes>(channel).list('post', { locale: channel, where: { slug }, limit: 1 })).data.items[0],
    all: async (channel: string) =>
      (await h.client<HydrogenTypes>(channel).list('post', { locale: channel, sort: '-publishedAt', limit: 50 })).data.items,
  }
}
