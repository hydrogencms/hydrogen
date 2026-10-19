import { defineConfig } from 'astro/config'
import node from '@astrojs/node'
import hydrogen from '@hydrogencms/astro'

// The adapter's integration: the middleware that asks the CMS about every page request, the preview and webhook
// routes and the preview bridge, all driven by src/hydrogen.ts.
export default defineConfig({
  output: 'server',
  adapter: node({ mode: 'standalone' }),
  integrations: [hydrogen()],
  devToolbar: { enabled: false },
})
