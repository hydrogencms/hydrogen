import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

export interface HydrogenIntegrationOptions {
  /** the module whose default export is `createHydrogen({...})`, relative to the project root (default `src/hydrogen.ts`) */
  config?: string
  /** where the preview and webhook routes live (default `/api/hydrogen`: `/preview` and `/revalidate`) */
  routes?: string
}

interface Setup {
  config: { root: URL; base: string; site?: string; output: string; adapter?: unknown }
  updateConfig(c: Record<string, unknown>): unknown
  addMiddleware(m: { order: 'pre' | 'post'; entrypoint: URL }): void
  injectRoute(r: { pattern: string; entrypoint: URL; prerender: boolean }): void
}

const INSTANCE = 'virtual:hydrogen/instance'
const BRIDGE = 'virtual:hydrogen/bridge'
const ENV = 'virtual:hydrogen/env'

/**
 * The Astro integration: installs the route-resolving middleware, the preview and revalidation routes and the
 * preview bridge script (`/__hydrogen/bridge.js`), all driven by the `createHydrogen` instance in your config module.
 * With an SSR output and an adapter it installs all of them. A static build (`output: 'static'`, no adapter) gets the
 * middleware only: preview, revalidation and the bridge need a server.
 */
export default function hydrogen(o: HydrogenIntegrationOptions = {}) {
  const base = (o.routes ?? '/api/hydrogen').replace(/\/+$/, '')
  const here = (p: string) => new URL(p, import.meta.url)
  return {
    name: '@hydrogencms/astro',
    hooks: {
      'astro:config:setup'({ config, updateConfig, addMiddleware, injectRoute }: Setup) {
        const module = resolve(fileURLToPath(config.root), o.config ?? 'src/hydrogen.ts')
        const isStatic = config.output === 'static' && !config.adapter
        const bridge = isStatic ? '' : readFileSync(createRequire(import.meta.url).resolve('@hydrogencms/bridge/iife'), 'utf8')
        const env = JSON.stringify({ base: config.base, site: config.site })
        updateConfig({
          vite: {
            plugins: [
              {
                name: 'hydrogen-virtual',
                resolveId: (id: string) => (id === INSTANCE || id === BRIDGE || id === ENV ? '\0' + id : undefined),
                load: (id: string) =>
                  id === '\0' + INSTANCE ? `export { default } from ${JSON.stringify(module)}`
                    : id === '\0' + ENV ? `export default ${env}`
                    : id === '\0' + BRIDGE ? `export default ${JSON.stringify(bridge)}`
                    : undefined,
              },
            ],
          },
        })
        addMiddleware({ order: 'pre', entrypoint: here('./middleware.js') })
        if (isStatic) return
        injectRoute({ pattern: `${base}/preview`, entrypoint: here('./routes/preview.js'), prerender: false })
        injectRoute({ pattern: `${base}/revalidate`, entrypoint: here('./routes/revalidate.js'), prerender: false })
        injectRoute({ pattern: '/__hydrogen/bridge.js', entrypoint: here('./routes/bridge.js'), prerender: false })
      },
    },
  }
}
