import { error, isHttpError, redirect } from '@sveltejs/kit'
import { HydrogenError } from '@hydrogencms/client'
import { hydrogen } from '$lib/server/hydrogen'
import { postBySlug } from '$lib/server/posts'
import type { PageServerLoad } from './$types'

export const load: PageServerLoad = async (event) => {
  const { locale, slug } = event.params
  let item
  try {
    item = await postBySlug(event, locale, slug)
  } catch (e) {
    if (e instanceof HydrogenError && e.status === 401 && hydrogen.previewToken(event))
      redirect(307, `/api/hydrogen/preview?exit=1&path=${encodeURIComponent(event.url.pathname)}`)
    if (isHttpError(e)) throw e
    error(e instanceof HydrogenError && e.status < 500 ? 404 : 502)
  }
  if (!item) error(404, 'Not found')
  const title = String(item.fields.title)
  return hydrogen.render(event, { template: 'post', item, locale, metadata: { title, alternates: [], locale } })
}
