<script setup lang="ts">
import type { HeroProps } from '~/lib/types'

const p = defineProps<{ props: HeroProps }>()
const links = useLinks()
const href = computed(() => (p.props.ctaLink ? links[p.props.ctaLink] : p.props.ctaUrl))
</script>

<!-- Every block component has one root: `data-h-block` (and `data-h-path` in preview) fall through to it. -->
<template>
  <section :class="['hero', `tone-${props.tone ?? 'light'}`]">
    <div class="container">
      <h2 class="hero-title">{{ props.heading }}</h2>
      <p v-if="props.subheading" class="hero-sub">{{ props.subheading }}</p>
      <a v-if="props.ctaLabel && href" class="button" :href="href">{{ props.ctaLabel }}</a>
    </div>
  </section>
</template>
