<script setup lang="ts">
import type { CalloutProps } from '~/lib/types'

defineProps<{ props: CalloutProps }>()
</script>

<template>
  <div class="container prose">
    <aside :class="['callout', `tone-${props.tone ?? 'brand'}`]"><p>{{ props.text }}</p></aside>
  </div>
</template>
