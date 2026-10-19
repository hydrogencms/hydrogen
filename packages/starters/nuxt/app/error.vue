<script setup lang="ts">
const props = defineProps<{ error: { statusCode?: number } }>()
const missing = props.error.statusCode === 404
useHead({ title: missing ? 'Not found' : 'Unavailable', meta: [{ name: 'robots', content: 'noindex' }] })
</script>

<template>
  <section class="container notfound" data-template="not-found">
    <p class="code">{{ missing ? 404 : (error.statusCode ?? 500) }}</p>
    <h1>{{ missing ? 'Page not found' : 'Something went wrong' }}</h1>
    <p v-if="missing">The page you are looking for does not exist or has moved.</p>
    <a class="button" href="/">Back to the home page</a>
  </section>
</template>
