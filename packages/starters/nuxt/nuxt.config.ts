// Runtime settings come from the environment: NUXT_HYDROGEN_BASE_URL (the CMS), NUXT_HYDROGEN_ORIGIN (a fixed
// public origin, e.g. on localhost), NUXT_HYDROGEN_REVALIDATE_SECRET (the webhook secret) and
// NUXT_HYDROGEN_ADMIN_ORIGIN (the admin that frames previews; default: the CMS's origin).
export default defineNuxtConfig({
  compatibilityDate: '2026-10-01',
  modules: ['@hydrogencms/nuxt'],
  css: ['~/assets/main.css'],
  devtools: { enabled: false },
  telemetry: false,
  nitro: { preset: 'node-server' },
  app: { head: { htmlAttrs: { lang: 'en' }, title: 'Starter' } },
  // One deployment serves both web channels: the request's host and path prefix (/en, /de) pick the channel.
  hydrogen: {
    baseUrl: 'http://localhost:8080',
    channels: ['en', 'de'],
    templates: ['page', 'landing'],
  },
})
