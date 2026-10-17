import { notFound, redirect } from 'next/navigation'
import { channels } from '@/lib/channels'
import { hydrogen } from '@/lib/hydrogen'
import { postBySlug } from '@/lib/posts'

export const instant = false

/**
 * The CMS's preview URL of a post is `/blog/{slug}` (the channel's preview pattern, which has no language
 * prefix). The preview route has set the draft cookie; the preview token belongs to one channel, so try each and
 * continue at the post's real URL.
 */
export default async function PreviewPost({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  if (await hydrogen.previewToken()) {
    for (const channel of channels) {
      try {
        if (await postBySlug(await hydrogen.clientFor(channel), channel, slug)) redirect(`/${channel}/blog/${slug}`)
      } catch (e) {
        if ((e as { digest?: string }).digest?.startsWith('NEXT_REDIRECT')) throw e
        // a token of another channel is refused: try the next one
      }
    }
  }
  notFound()
}
