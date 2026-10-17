import type { Client } from '@hydrogencms/next'
import { hydrogen } from './hydrogen'
import type { HydrogenTypes } from './types'

/** Posts are a plain collection: the pages module does not route them, so the app does (the CMS reserves `/blog/*`). */
export async function postBySlug(client: Client<HydrogenTypes>, channel: string, slug: string) {
  const { data } = await client.list('post', { locale: channel, where: { slug }, limit: 1 })
  return data.items[0]
}

export async function posts(channel: string) {
  return (await (await hydrogen.clientFor(channel)).list('post', { locale: channel, sort: '-publishedAt', limit: 50 })).data.items
}
