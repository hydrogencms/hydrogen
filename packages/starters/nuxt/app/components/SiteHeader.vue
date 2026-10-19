<script setup lang="ts">
const { data: site } = await useSite()
const path = (url?: string) => (url ? new URL(url).pathname : undefined)
</script>

<template>
  <header class="site-header">
    <div class="container bar">
      <a class="brand" :href="`/${site?.channel}`">{{ site?.siteName ?? 'Starter' }}</a>
      <nav aria-label="Main">
        <ul>
          <li v-for="n in site?.nodes" :key="n.id">
            <a :href="path(n.url)">{{ n.name }}</a>
            <ul v-if="n.nodes?.some((k) => k.url)">
              <li v-for="k in n.nodes.filter((k) => k.url)" :key="k.id"><a :href="path(k.url)">{{ k.name }}</a></li>
            </ul>
          </li>
        </ul>
      </nav>
    </div>
  </header>
</template>
