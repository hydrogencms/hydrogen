import { createHydrogen } from '@hydrogencms/next'
import type { HydrogenTypes } from './types'
import Landing from '@/templates/landing'
import Page from '@/templates/page'
import { channels } from './channels'

const baseUrl = process.env.HYDROGEN_URL ?? 'http://localhost:8080'

/**
 * One deployment serves both web channels: the request's host and path prefix (/en, /de) pick the channel. Set
 * HYDROGEN_ORIGIN to locate URLs with a fixed public origin (a deployment on localhost).
 *
 * Templates are client components ('use client'): in draft mode the adapter renders them from the admin's
 * working document, so the page updates while the editor types.
 */
export const hydrogen = createHydrogen<HydrogenTypes>({
  baseUrl,
  channels,
  origin: process.env.HYDROGEN_ORIGIN,
  adminOrigin: process.env.HYDROGEN_ADMIN_ORIGIN,
  templates: { page: Page, landing: Landing },
  revalidateSecret: process.env.HYDROGEN_REVALIDATE_SECRET,
})
