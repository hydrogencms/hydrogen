import { defineNitroPlugin } from 'nitropack/runtime'
import type { NuxtRenderHTMLContext } from 'nuxt/app'
import type { H3Event } from 'h3'

export const BRIDGE_PATH = '/__hydrogen/bridge.js'

/** In preview the page loads the bridge (reload mode: the admin saves the draft and the bridge swaps `[data-h-root]`). */
export default defineNitroPlugin((nitro) => {
  const onHtml = (html: NuxtRenderHTMLContext, { event }: { event: H3Event }) => {
    const c = event.context.hydrogen
    if (c?.preview) html.bodyAppend.push(`<script src="${BRIDGE_PATH}" data-auto data-admin-origin="${c.adminOrigin}" data-mode="reload"></script>`)
  }
  ;(nitro.hooks.hook as (name: string, fn: typeof onHtml) => void)('render:html', onHtml)
})
