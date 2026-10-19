<script setup lang="ts">
import { channels } from '~/lib/channels'

const locale = String(useRoute().params.locale)
if (!channels.includes(locale)) throw createError({ statusCode: 404, statusMessage: 'Page not found', fatal: true })
const posts = usePosts()
useHydrogenPreview()
useHead({ title: 'Blog' })
const { data: items } = await useAsyncData(`posts:${locale}`, () => posts.all(locale))
</script>

<template>
  <section class="container prose" :lang="locale">
    <h1>Blog</h1>
    <ul class="posts">
      <li v-for="p in items" :key="p.id">
        <h2><a :href="`/${locale}/blog/${p.fields.slug}`">{{ p.fields.title }}</a></h2>
        <p v-if="p.fields.excerpt">{{ p.fields.excerpt }}</p>
      </li>
    </ul>
  </section>
</template>
