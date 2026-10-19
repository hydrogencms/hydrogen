import type { Client } from '@hydrogencms/astro/runtime'
import type { HydrogenTypes } from './types'

/** Posts are a plain collection: the pages module does not route them, so the app does (the CMS reserves `/blog/*`). */
export async function postBySlug(client: Client<HydrogenTypes>, channel: string, slug: string) {
  const { data } = await client.list('post', { locale: channel, where: { slug }, limit: 1 })
  return data.items[0]
}
