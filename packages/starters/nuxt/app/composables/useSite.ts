import type { TreeNode } from '@hydrogencms/client'
import type { HydrogenTypes } from '~/lib/types'

/** The request's channel, its navigation (page tree, two levels) and site settings; shared by header and footer. */
export function useSite() {
  const h = useHydrogen()
  return useAsyncData('hydrogen:site', async () => {
    const channel = h.channel
    const c = h.client<HydrogenTypes>(channel)
    const [tree, s] = await Promise.all([
      c.tree({ depth: 2 }),
      c.singleton('siteSettings', { locale: channel }).then((r) => r.data.fields, () => undefined),
    ])
    return { channel, nodes: tree.data.filter((n) => n.url) as TreeNode[], siteName: s?.siteName, footerText: s?.footerText }
  })
}
