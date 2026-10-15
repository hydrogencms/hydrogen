import { createHydrogen } from '@hydrogencms/next'
import type { HydrogenTypes } from './types'
import Article from '@/templates/article'
import Generic from '@/templates/generic'
import Landing from '@/templates/landing'
import Page from '@/templates/page'
import { channels } from './channels'

/**
 * One deployment serves every web channel of examples/multisite: the request's host and path prefix pick the
 * channel (the `match` options the CMS publishes). Set HYDROGEN_ORIGIN to locate URLs with a fixed public origin
 * instead of the request's Host (a single-channel deployment on localhost).
 */
export const hydrogen = createHydrogen<HydrogenTypes>({
  baseUrl: process.env.HYDROGEN_URL ?? 'http://localhost:8080',
  channels,
  origin: process.env.HYDROGEN_ORIGIN,
  templates: { page: Page, landing: Landing, article: Article },
  fallback: Generic,
  revalidateSecret: process.env.HYDROGEN_REVALIDATE_SECRET,
})
