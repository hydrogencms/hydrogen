import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

export interface HydrogenIntegrationOptions {
  /** the module whose default export is `createHydrogen({...})`, relative to the project root (default `src/hydrogen.ts`) */
  config?: string
  /** where the preview and webhook routes live (default `/api/hydrogen`: `/preview` and `/revalidate`) */
  routes?: string
  /**
   * Static sites only: the page routes (paths relative to the project root) that render on demand in `astro dev`, so
   * draft preview and the bridge work while `astro build` stays fully static. Default: the scaffold's catch-all and
   * 404 page. List every route that shows CMS pages; leave out routes that rely on `getStaticPaths` props.
   */
  devRoutes?: string[]
}

const DEV_ROUTES = ['src/pages/[...path].astro', 'src/pages/404.astro']

interface Setup {
  command?: string
  config: { root: URL; base: string; site?: string; output: string; adapter?: unknown; trailingSlash?: string }
  updateConfig(c: Record<string, unknown>): unknown
  addMiddleware(m: { order: 'pre' | 'post'; entrypoint: URL }): void
  injectRoute(r: { pattern: string; entrypoint: URL; prerender: boolean }): void
}
interface RouteSetup {
  route: { component: string; prerender?: boolean }
}

const INSTANCE = 'virtual:hydrogen/instance'
const BRIDGE = 'virtual:hydrogen/bridge'
const ENV = 'virtual:hydrogen/env'

// a route component as Astro reports it (relative to the root, or absolute) in the form devRoutes uses
const normal = (c: string, root = '') => {
  const p = c.replaceAll('\\', '/')
  const r = root.replaceAll('\\', '/')
  return (r && p.startsWith(r) ? p.slice(r.length) : p).replace(/^\.?\/+/, '')
}

/**
 * The Astro integration: installs the route-resolving middleware, the preview and revalidation routes and the
 * preview bridge script (`/__hydrogen/bridge.js`), all driven by the `createHydrogen` instance in your config module.
 * With an SSR output and an adapter it installs all of them. A static build (`output: 'static'`, no adapter) gets the
 * middleware only, and in `astro dev` also the preview route and the bridge, with the CMS page routes (`devRoutes`)
 * rendered on demand so drafts can be previewed: preview and revalidation otherwise need a server.
 */
export default function hydrogen(o: HydrogenIntegrationOptions = {}) {
  const base = (o.routes ?? '/api/hydrogen').replace(/\/+$/, '')
  const here = (p: string) => new URL(p, import.meta.url)
  const devRoutes = new Set((o.devRoutes ?? DEV_ROUTES).map((c) => normal(c)))
  let devStatic = false
  let root = ''
  return {
    name: '@hydrogencms/astro',
    hooks: {
      'astro:route:setup'({ route }: RouteSetup) {
        if (devStatic && devRoutes.has(normal(route.component, root))) route.prerender = false
      },
      'astro:config:setup'({ command, config, updateConfig, addMiddleware, injectRoute }: Setup) {
        const module = resolve(fileURLToPath(config.root), o.config ?? 'src/hydrogen.ts')
        const isStatic = config.output === 'static' && !config.adapter
        devStatic = isStatic && command === 'dev'
        root = fileURLToPath(config.root)
        const bridge = isStatic && !devStatic ? '' : readFileSync(createRequire(import.meta.url).resolve('@hydrogencms/bridge/iife'), 'utf8')
        const env = JSON.stringify({ base: config.base, site: config.site, trailingSlash: config.trailingSlash, devStatic })
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
        if (isStatic && !devStatic) return
        injectRoute({ pattern: `${base}/preview`, entrypoint: here('./routes/preview.js'), prerender: false })
        if (!isStatic) injectRoute({ pattern: `${base}/revalidate`, entrypoint: here('./routes/revalidate.js'), prerender: false })
        injectRoute({ pattern: '/__hydrogen/bridge.js', entrypoint: here('./routes/bridge.js'), prerender: false })
      },
    },
  }
}
