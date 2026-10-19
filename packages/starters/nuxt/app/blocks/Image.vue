<script setup lang="ts">
import type { ImageProps } from '~/lib/types'

defineProps<{ props: ImageProps }>()
</script>

<template>
  <div class="container prose">
    <figure class="figure">
      <img :src="props.src" :alt="props.alt ?? ''" loading="lazy">
      <figcaption v-if="props.caption">{{ props.caption }}</figcaption>
    </figure>
  </div>
</template>
