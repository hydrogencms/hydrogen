<script setup lang="ts">
import type { RichTextProps } from '~/lib/types'

defineProps<{ props: RichTextProps }>()
const links = useLinks()
</script>

<template>
  <div class="container prose"><HydrogenRichText :doc="props.body as any" :links="links" /></div>
</template>
