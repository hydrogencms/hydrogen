<script setup lang="ts">
import type { Item } from '@hydrogencms/client'
import type { Post } from '~/lib/types'

const props = defineProps<{ item: Item<Post>; locale: string; preview: boolean }>()
const date = computed(() => {
  const d = new Date(props.item.publishedAt)
  return isNaN(+d) ? '' : d.toLocaleDateString(props.locale, { dateStyle: 'long', timeZone: 'UTC' })
})
</script>

<template>
  <PageBody :item="item" :locale="locale" :preview="preview">
    <template #lead>
      <p class="meta">
        <time :datetime="item.publishedAt">{{ date }}</time>
        <template v-if="item.fields.excerpt"> · <HydrogenField path="fields/excerpt" :preview="preview">{{ item.fields.excerpt }}</HydrogenField></template>
      </p>
    </template>
  </PageBody>
</template>
