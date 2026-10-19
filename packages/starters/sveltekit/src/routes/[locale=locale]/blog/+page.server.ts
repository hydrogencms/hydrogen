import { posts } from '$lib/server/posts'
import type { PageServerLoad } from './$types'

export const load: PageServerLoad = async (event) => {
  const items = await posts(event, event.params.locale)
  return {
    locale: event.params.locale,
    posts: items.map((p) => ({ id: p.id, slug: p.fields.slug, title: p.fields.title, excerpt: p.fields.excerpt })),
  }
}
