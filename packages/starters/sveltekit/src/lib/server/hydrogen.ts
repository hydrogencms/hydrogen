import { env } from '$env/dynamic/private'
import { createHydrogen } from '@hydrogencms/sveltekit'
import { channels } from '$lib/channels'
import type { HydrogenTypes } from '$lib/types'

/**
 * One deployment serves both web channels: the request's host and path prefix (/en, /de) pick the channel. Set
 * HYDROGEN_ORIGIN to locate URLs with a fixed public origin (a deployment on localhost).
 */
export const hydrogen = createHydrogen<HydrogenTypes>({
  baseUrl: env.HYDROGEN_URL ?? 'http://localhost:8080',
  channels,
  origin: env.HYDROGEN_ORIGIN,
  adminOrigin: env.HYDROGEN_ADMIN_ORIGIN,
  templates: ['page', 'landing'],
  revalidateSecret: env.HYDROGEN_REVALIDATE_SECRET,
})
