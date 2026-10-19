import { sveltekit } from '@sveltejs/kit/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [sveltekit()],
  // One copy of Svelte, whichever package imports it.
  resolve: { dedupe: ['svelte'] },
})
