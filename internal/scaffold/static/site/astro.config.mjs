import { defineConfig } from 'astro/config'
import hydrogen from '@hydrogencms/astro'

// SITE_URL is the origin the site is published at and SITE_BASE the path it lives under (/repo for a GitHub Pages project
// site). The CMS channel reads SITE_URL too (config/channels.yaml). The deploy workflow sets both.
// The dev server listens on 127.0.0.1 like hydrogen dev: the admin's preview frame is then the same site, so the browser
// keeps the preview cookie (localhost and 127.0.0.1 are different sites).
export default defineConfig({
  output: 'static',
  site: process.env.SITE_URL ?? 'http://127.0.0.1:4321',
  server: { host: '127.0.0.1' },
  base: process.env.SITE_BASE || undefined,
  integrations: [hydrogen()],
  devToolbar: { enabled: false },
})
