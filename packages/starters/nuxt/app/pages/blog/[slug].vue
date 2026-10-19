<script setup lang="ts">
import { channels } from '~/lib/channels'

// The CMS's preview URL of a post is `/blog/{slug}` (the channel's preview pattern, which has no language prefix).
// The preview route has set the cookie; the preview token belongs to one channel, so try each and continue at the
// post's real URL. Without a preview token there is nothing here.
definePageMeta({
  middleware: async (to) => {
    const h = useHydrogen()
    const posts = usePosts()
    const slug = String(to.params.slug)
    if (h.preview) {
      for (const channel of channels) {
        try {
          if (await posts.bySlug(channel, slug)) return navigateTo(`/${channel}/blog/${slug}`, { redirectCode: 302 })
        } catch {
          // a token of another channel is refused: try the next one
        }
      }
    }
    throw createError({ statusCode: 404, statusMessage: 'Page not found', fatal: true })
  },
})
</script>

<template><div /></template>
