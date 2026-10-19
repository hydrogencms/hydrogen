import { createHydrogen } from '@hydrogencms/astro/runtime'

/** The CMS channel this site renders (config/channels.yaml). */
export const channel = 'web'

export default createHydrogen({
  baseUrl: process.env.HYDROGEN_URL ?? 'http://127.0.0.1:8080',
  channel,
  base: import.meta.env.BASE_URL,
  // the content types that have a template: src/pages/[...path].astro maps each to a component
  templates: ['page'],
})
