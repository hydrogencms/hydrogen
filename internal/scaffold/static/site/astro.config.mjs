import { defineConfig } from 'astro/config'
import hydrogen from '@hydrogencms/astro'

// SITE_URL is the origin the site is published at and SITE_BASE the path it lives under (/repo for a GitHub Pages project
// site). The CMS channel reads SITE_URL too (config/channels.yaml). The deploy workflow sets both.
export default defineConfig({
  output: 'static',
  site: process.env.SITE_URL ?? 'http://localhost:4321',
  base: process.env.SITE_BASE || undefined,
  integrations: [hydrogen()],
  devToolbar: { enabled: false },
})
