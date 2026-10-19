import { h } from 'vue'
import type { FunctionalComponent } from 'vue'
import { imageProps } from '@hydrogencms/client'
import type { ImageAsset } from '@hydrogencms/client'

/** An `<img>` for a delivery asset: the preset's variant and srcset, lazy by default, positioned on the focal point. */
export const HydrogenImage: FunctionalComponent<{ asset: ImageAsset; preset?: string; alt?: string }> = (p, { attrs }) => {
  const i = imageProps(p.asset, p.preset)
  const f = p.asset.focal
  return h('img', {
    loading: 'lazy',
    decoding: 'async',
    ...i,
    style: f ? `object-position:${+(f.x * 100).toFixed(2)}% ${+(f.y * 100).toFixed(2)}%` : undefined,
    ...attrs,
    alt: p.alt ?? i.alt,
  })
}
HydrogenImage.props = ['asset', 'preset', 'alt']
