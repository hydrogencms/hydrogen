import { createHydrogen } from '@hydrogencms/astro/runtime'
import { channels } from './lib/channels'

/**
 * One deployment serves both web channels: the request's host and path prefix (/en, /de) pick the channel. Set
 * HYDROGEN_ORIGIN to locate URLs with a fixed public origin (a deployment on localhost).
 */
export default createHydrogen({
  baseUrl: process.env.HYDROGEN_URL ?? 'http://localhost:8080',
  channels,
  origin: process.env.HYDROGEN_ORIGIN,
  adminOrigin: process.env.HYDROGEN_ADMIN_ORIGIN,
  // the content types that have a template: src/pages/[...path].astro maps them to components
  templates: ['page', 'landing'],
  revalidateSecret: process.env.HYDROGEN_REVALIDATE_SECRET,
})
