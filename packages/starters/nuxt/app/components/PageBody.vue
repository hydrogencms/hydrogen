<script setup lang="ts">
import { registry } from '~/lib/blocks'
import { channels } from '~/lib/channels'
import type { Block } from '~/lib/types'

const props = defineProps<{
  item: { links?: Record<string, string>; fields: { title: string; body?: Block[] } }
  locale: string
  preview: boolean
  /** the page opens with a hero that carries the visible heading */
  hidden?: boolean
  alternates?: { hreflang: string; href: string }[]
}>()

// The same page in the other languages, from the route's alternates, in the channels' configuration order.
const pathOf = (href: string) => new URL(href).pathname
const order = (l: string) => (channels.includes(l) ? channels.indexOf(l) : channels.length)
const languages = computed(() => [...(props.alternates ?? [])].sort((a, b) => order(a.hreflang) - order(b.hreflang)))
</script>

<template>
  <nav v-if="languages.length > 1" class="container langs" aria-label="Language">
    <a v-for="a in languages" :key="a.hreflang" :href="pathOf(a.href)" :hreflang="a.hreflang" :aria-current="a.hreflang === locale ? 'true' : undefined">
      {{ a.hreflang.toUpperCase() }}
    </a>
  </nav>
  <article :lang="locale">
    <header :class="hidden ? 'sr-only' : 'page-head'">
      <div class="container">
        <h1><HydrogenField path="fields/title" :preview="preview">{{ item.fields.title }}</HydrogenField></h1>
        <slot name="lead" />
      </div>
    </header>
    <HydrogenBlocks :blocks="item.fields.body" :registry="registry" path="fields/body" :preview="preview" :links="item.links ?? {}" />
    <slot />
  </article>
</template>
