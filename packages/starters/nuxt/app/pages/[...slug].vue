<script setup lang="ts">
import type { Component } from 'vue'
import Landing from '~/templates/Landing.vue'
import Page from '~/templates/Page.vue'

// Pages: the CMS decides which page lives at which URL; the module answers redirects and 404s.
const templates: Record<string, Component> = { page: Page, landing: Landing }
const page = useHydrogenPage()
</script>

<template>
  <component :is="templates[page.template]" :item="page.item" :locale="page.locale" :preview="page.preview" :metadata="page.metadata" />
</template>
