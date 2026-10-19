<script setup lang="ts">
import type { RouteMetadata } from '~/lib/route'
import type { Page } from '~/lib/types'

defineProps<{ item: { links?: Record<string, string>; fields: Page }; locale: string; preview: boolean; metadata: RouteMetadata }>()
</script>

<template>
  <PageBody :item="item" :locale="locale" :preview="preview" :alternates="metadata.alternates" />
</template>
