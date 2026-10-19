<script setup lang="ts">
import type { CardsProps } from '~/lib/types'

defineProps<{ props: CardsProps }>()
</script>

<template>
  <section :class="['container cards', `space-${props.space ?? 'md'}`]">
    <h2 v-if="props.heading">{{ props.heading }}</h2>
    <div :class="`grid cols-${props.columns ?? '3'}`"><slot name="items" /></div>
  </section>
</template>
