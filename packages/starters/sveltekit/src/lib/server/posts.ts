import type { RequestEvent } from '@sveltejs/kit'
import { hydrogen } from './hydrogen'

/** Posts are a plain collection: the pages module does not route them, so the app does (the CMS reserves `/blog/*`). */
export async function postBySlug(event: RequestEvent, channel: string, slug: string) {
  const { data } = await hydrogen.client(event, channel).list('post', { locale: channel, where: { slug }, limit: 1 })
  return data.items[0]
}

export async function posts(event: RequestEvent, channel: string) {
  return (await hydrogen.client(event, channel).list('post', { locale: channel, sort: '-publishedAt', limit: 50 })).data.items
}
