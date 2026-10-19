<script setup lang="ts">
import type { RouteMetadata } from '~/lib/route'
import type { Landing } from '~/lib/types'

defineProps<{ item: { links?: Record<string, string>; fields: Landing }; locale: string; preview: boolean; metadata: RouteMetadata }>()
</script>

<template>
  <PageBody :item="item" :locale="locale" :preview="preview" :alternates="metadata.alternates" :hidden="item.fields.body?.[0]?.type === 'hero'" />
</template>
