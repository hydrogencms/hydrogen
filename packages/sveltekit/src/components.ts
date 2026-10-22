import type { Component } from 'svelte'
import type { BlockProps, Registry } from './blocks.js'
import Shared from './Shared.svelte'
import Unknown from './Unknown.svelte'

export { default as Blocks } from './Blocks.svelte'
export { default as Field } from './Field.svelte'
export { default as RichText } from './RichText.svelte'
export { default as Image } from './Image.svelte'
export { default as Page } from './Page.svelte'
export { default as Live } from './Live.svelte'
export { pathAttrs, blockAttrs } from './blocks.js'
export { liveDoc, mergeDoc } from '@hydrogencms/bridge/merge'
export { richTextNodes, safeHref } from '@hydrogencms/client/richtext'
export type { BlockData, BlockAttrs, BlockProps, Registry } from './blocks.js'
export type { PMNode, PMMark, RichNode, RichTextOptions, RichTextSchema } from '@hydrogencms/client/richtext'

type Comp = Component<BlockProps<any>>

/** A registry from block type to component; `shared` is built in, an unknown type renders nothing (a marker in preview). */
export function createRegistry(components: Record<string, Comp>, o: { fallback?: Comp } = {}): Registry {
  return { components: { shared: Shared as Comp, ...components }, fallback: o.fallback ?? (Unknown as Comp), warned: new Set() }
}
