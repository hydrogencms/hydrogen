import { hydrogen } from '$lib/server/hydrogen'
import type { LayoutServerLoad } from './$types'

/** The channel name is the locale. Navigation from the page tree of the request's channel; the footer from the `siteSettings` singleton. */
export const load: LayoutServerLoad = async (event) => {
  const channel = await hydrogen.channel(event)
  const [nav, settings] = await Promise.all([
    hydrogen.nav(event, { depth: 2 }).catch(() => []),
    hydrogen.client(event, channel).singleton('siteSettings', { locale: channel }).then((r) => r.data.fields, () => undefined),
  ])
  return {
    channel,
    settings,
    nav: nav.filter((n) => n.url).map((n) => ({
      id: n.id, name: n.name, href: new URL(n.url!).pathname,
      kids: (n.nodes ?? []).filter((k) => k.url).map((k) => ({ id: k.id, name: k.name, href: new URL(k.url!).pathname })),
    })),
  }
}
