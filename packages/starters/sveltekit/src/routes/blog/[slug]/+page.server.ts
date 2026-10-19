import { error, isRedirect, redirect } from '@sveltejs/kit'
import { channels } from '$lib/channels'
import { hydrogen } from '$lib/server/hydrogen'
import { postBySlug } from '$lib/server/posts'
import type { PageServerLoad } from './$types'

/**
 * The CMS's preview URL of a post is `/blog/{slug}` (the channel's preview pattern, which has no language
 * prefix). The preview route has set the cookie; the preview token belongs to one channel, so try each and
 * continue at the post's real URL.
 */
export const load: PageServerLoad = async (event) => {
  if (hydrogen.previewToken(event)) {
    for (const channel of channels) {
      try {
        if (await postBySlug(event, channel, event.params.slug)) redirect(307, `/${channel}/blog/${event.params.slug}`)
      } catch (e) {
        if (isRedirect(e)) throw e
        // a token of another channel is refused: try the next one
      }
    }
  }
  error(404, 'Not found')
}
