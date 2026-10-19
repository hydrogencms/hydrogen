import type { Component, Snippet } from 'svelte'

export interface BlockData {
  id: string
  type: string
  props?: Record<string, any>
  slots?: Record<string, BlockData[]>
  /** `shared` blocks: the referenced item's blocks, inlined by delivery */
  blocks?: BlockData[]
}

/** Spread on the component's root element: `data-h-block` always, `data-h-path` in preview. */
export type BlockAttrs = { 'data-h-block': string; 'data-h-path'?: string }

/** What a block component receives; `renderSlot('items')` renders a slot through the same registry (`'blocks'`: a shared block's inlined blocks). */
export interface BlockProps<P = Record<string, any>> {
  block: BlockData
  props: P
  attrs: BlockAttrs
  path?: string
  renderSlot: Snippet<[string]>
}

type Comp = Component<BlockProps<any>>
export interface Registry {
  components: Readonly<Record<string, Comp>>
  fallback: Comp
  warned: Set<string>
}

/** `data-h-path` for editing overlays; only in preview. */
export const pathAttrs = (path: string | undefined, preview?: boolean): { 'data-h-path'?: string } =>
  preview && path ? { 'data-h-path': path } : {}

declare const process: { env?: Record<string, string | undefined> } | undefined
const dev = () => typeof process !== 'undefined' && process.env?.NODE_ENV !== 'production'

/** The component for a block type; an unregistered type gets the fallback (and one warning in development). */
export function componentOf(registry: Registry, type: string): Comp {
  const c = registry.components[type]
  if (c) return c
  if (dev() && !registry.warned.has(type)) {
    registry.warned.add(type)
    console.warn(`@hydrogencms/sveltekit: no component registered for block type "${type}"`)
  }
  return registry.fallback
}

export const blockAttrs = (type: string, path: string | undefined, preview?: boolean): BlockAttrs => ({ 'data-h-block': type, ...pathAttrs(path, preview) })
