<script setup lang="ts">
import type { CardProps } from '~/lib/types'

const p = defineProps<{ props: CardProps }>()
const links = useLinks()
const href = computed(() => (p.props.link ? links[p.props.link] : undefined))
</script>

<template>
  <article class="card">
    <h3><a v-if="href" :href="href">{{ props.title }}</a><template v-else>{{ props.title }}</template></h3>
    <p v-if="props.text">{{ props.text }}</p>
  </article>
</template>
