import { createElement } from 'react'
import type { ImgHTMLAttributes } from 'react'
import { imageProps } from '@hydrogencms/client'
import type { ImageAsset } from '@hydrogencms/client'

export interface ImageProps extends Omit<ImgHTMLAttributes<HTMLImageElement>, 'src' | 'srcSet' | 'alt'> {
  asset: ImageAsset
  /** image preset name; without it (or without the variant) the original file is used */
  preset?: string
  /** overrides the asset's `fields.alt` */
  alt?: string
}

/** An `<img>` for a delivery asset: the preset's variant and srcset, lazy by default, positioned on the focal point. */
export function Image({ asset, preset, alt, style, ...rest }: ImageProps) {
  const p = imageProps(asset, preset)
  const f = asset.focal
  return createElement('img', {
    loading: 'lazy',
    decoding: 'async',
    ...p,
    ...rest,
    alt: alt ?? p.alt,
    style: f ? { objectPosition: `${+(f.x * 100).toFixed(2)}% ${+(f.y * 100).toFixed(2)}%`, ...style } : style,
  })
}
