import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { addComponent, addImports, addServerHandler, addServerPlugin, addServerTemplate, createResolver, defineNuxtModule } from '@nuxt/kit'
import type { HydrogenConfig } from './delivery.js'

export interface ModuleOptions {
  /** the CMS, e.g. "https://cms.acme.com" */
  baseUrl: string
  /** the one channel this deployment serves ... */
  channel?: string
  /** ... or several, told apart by the host and prefix matchers in their options */
  channels?: string[]
  /** token of a token-protected channel (server side only) */
  token?: string
  /** the content types that have a template (a fallback template: `fallback`) */
  templates: string[]
  fallback?: boolean
  /** public origin to locate URLs with instead of the request's (dev on localhost, or another host than the channel's) */
  origin?: string
  /** use X-Forwarded-Host/-Proto for the public origin (default false; turn on only behind a proxy that overwrites them) */
  trustForwarded?: boolean
  /** path of the preview URLs for items without a route (default "/preview": `/preview/<collection>/<id>`) */
  previewPath?: string
  /** seconds a cached CMS response stays fresh when no webhook purges it (default 3600) */
  ttl?: number
  /** milliseconds the CMS may take (default 3000) */
  timeoutMs?: number
  /** webhook secret for `/api/hydrogen/revalidate`; at least 16 characters */
  revalidateSecret?: string
  /** origin of the admin that embeds preview pages (default: the origin of `baseUrl`) */
  adminOrigin?: string
}

declare module '@nuxt/schema' {
  interface NuxtConfig { hydrogen?: Partial<ModuleOptions> }
  interface NuxtOptions { hydrogen?: Partial<ModuleOptions> }
}

export default defineNuxtModule<ModuleOptions>({
  meta: { name: '@hydrogencms/nuxt', configKey: 'hydrogen', compatibility: { nuxt: '>=4.0.0' } },
  defaults: {
    baseUrl: 'http://localhost:8080', channel: '', channels: [], token: '', templates: [], fallback: false, origin: '',
    trustForwarded: false, previewPath: '/preview', ttl: 3600, timeoutMs: 3000, revalidateSecret: '', adminOrigin: '',
  },
  setup(o, nuxt) {
    const many = o.channels!.length > 0
    if (!many && !o.channel) throw new Error('@hydrogencms/nuxt: set hydrogen.channel or hydrogen.channels')
    const cfg = { ...o, baseUrl: o.baseUrl.replace(/\/+$/, ''), channels: many ? o.channels : [o.channel], channel: many ? '' : o.channel } as HydrogenConfig
    const rc = nuxt.options.runtimeConfig
    rc.hydrogen = { ...cfg, ...(rc.hydrogen as object) }

    const { resolve } = createResolver(import.meta.url)
    nuxt.options.build.transpile.push(resolve('.'))
    addServerHandler({ middleware: true, handler: resolve('./middleware.js') })
    addServerHandler({ route: '/api/hydrogen/preview', method: 'get', handler: resolve('./route-preview.js') })
    addServerHandler({ route: '/api/hydrogen/revalidate', method: 'post', handler: resolve('./route-revalidate.js') })
    addServerPlugin(resolve('./nitro-plugin.js'))
    // the bridge's IIFE build, served to preview pages
    const bridge = JSON.stringify(readFileSync(createRequire(import.meta.url).resolve('@hydrogencms/bridge/iife'), 'utf8'))
    addServerTemplate({
      filename: '#hydrogen/bridge',
      getContents: () => `import { defineEventHandler, setResponseHeader } from 'h3'\nexport default defineEventHandler((e) => (setResponseHeader(e, 'content-type', 'text/javascript'), ${bridge}))`,
    })
    addServerHandler({ route: '/__hydrogen/bridge.js', method: 'get', handler: '#hydrogen/bridge' })

    addImports(['useHydrogen', 'useHydrogenPage', 'useHydrogenPreview', 'createRegistry', 'useLinks'].map((name) => ({ name, from: resolve('./runtime.js') })))
    for (const [file, ...names] of [['blocks', 'Blocks', 'Field'], ['richtext', 'RichText'], ['image', 'Image']] as const)
      for (const n of names) addComponent({ name: 'Hydrogen' + n, export: 'Hydrogen' + n, filePath: resolve(`./${file}.js`) })
  },
})
