<script setup lang="ts">
const { data: site } = await useSite()
</script>

<template>
  <footer class="site-footer">
    <div class="container">
      <strong>{{ site?.siteName }}</strong>
      <p v-if="site?.footerText">{{ site.footerText }}</p>
    </div>
  </footer>
</template>
