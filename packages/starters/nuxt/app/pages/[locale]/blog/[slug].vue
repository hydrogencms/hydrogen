<script setup lang="ts">
import { channels } from '~/lib/channels'
import PostTemplate from '~/templates/Post.vue'

const { locale, slug } = useRoute().params as { locale: string; slug: string }
const notFound = () => createError({ statusCode: 404, statusMessage: 'Page not found', fatal: true })
if (!channels.includes(locale)) throw notFound()
const posts = usePosts()
const preview = useHydrogenPreview()
const { data: item } = await useAsyncData(`post:${locale}:${slug}`, async () => (await posts.bySlug(locale, slug)) ?? null)
if (!item.value) throw notFound()
useHead({ title: item.value.fields.title, meta: item.value.fields.excerpt ? [{ name: 'description', content: item.value.fields.excerpt }] : [] })
</script>

<template>
  <PostTemplate :item="item!" :locale="locale" :preview="preview" />
</template>
