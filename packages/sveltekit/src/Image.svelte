<script lang="ts">
  import { imageProps } from '@hydrogencms/client'
  import type { ImageAsset } from '@hydrogencms/client'
  import type { HTMLImgAttributes } from 'svelte/elements'

  /** An `<img>` for a delivery asset: the preset's variant and srcset, lazy by default, positioned on the focal point. */
  let { asset, preset, alt, style, ...rest }: {
    asset: ImageAsset
    /** image preset name; without it (or without the variant) the original file is used */
    preset?: string
    /** overrides the asset's `fields.alt` */
    alt?: string
  } & Omit<HTMLImgAttributes, 'src' | 'srcset' | 'alt'> = $props()
  const p = $derived(imageProps(asset, preset))
  const f = $derived(asset.focal)
  const pos = $derived(f ? `object-position: ${+(f.x * 100).toFixed(2)}% ${+(f.y * 100).toFixed(2)}%;` : '')
</script>

<img loading="lazy" decoding="async" src={p.src} srcset={p.srcSet} width={p.width} height={p.height} {...rest} alt={alt ?? p.alt} style={pos + (style ?? '')} />
