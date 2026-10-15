import { createElement, Fragment } from 'react'
import type { ComponentType, ReactNode } from 'react'

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

export interface BlockComponentProps<P = Record<string, any>> {
  block: BlockData
  props: P
  /** each slot rendered through the same registry; `blocks` holds a shared block's inlined blocks */
  slots: Record<string, ReactNode>
  attrs: BlockAttrs
  path?: string
}

type Comp = ComponentType<BlockComponentProps<any>>
export interface RegistryOptions { fallback?: Comp }
export interface Registry {
  components: Readonly<Record<string, Comp>>
  fallback: Comp
  warned: Set<string>
}

declare const process: { env?: Record<string, string | undefined> } | undefined
const dev = () => typeof process !== 'undefined' && process.env?.NODE_ENV !== 'production'

const Unknown: Comp = ({ block, attrs }) =>
  attrs['data-h-path'] === undefined ? null : createElement('div', attrs, `Unknown block "${block.type}"`)

const Shared: Comp = ({ attrs, slots }) => createElement('div', { ...attrs, style: { display: 'contents' } }, slots.blocks)

export function createRegistry(components: Record<string, Comp>, o: RegistryOptions = {}): Registry {
  return { components: { shared: Shared, ...components }, fallback: o.fallback ?? Unknown, warned: new Set() }
}

/** `data-h-path` for editing overlays; only in preview. */
export const pathAttrs = (path: string | undefined, preview?: boolean): { 'data-h-path'?: string } =>
  preview && path ? { 'data-h-path': path } : {}

interface Common { registry: Registry; path?: string; preview?: boolean }

/** Renders a block through the registry. `path` is the block's own path, e.g. `fields/body/2`. */
export function Block({ block, registry, path, preview }: Common & { block: BlockData }): ReactNode {
  let C = registry.components[block.type]
  if (!C) {
    C = registry.fallback
    if (dev() && !registry.warned.has(block.type)) {
      registry.warned.add(block.type)
      console.warn(`@hydrogencms/react: no component registered for block type "${block.type}"`)
    }
  }
  const slots: Record<string, ReactNode> = {}
  for (const k of Object.keys(block.slots ?? {})) {
    slots[k] = createElement(Blocks, { blocks: block.slots![k]!, registry, path: path && `${path}/slots/${k}`, preview })
  }
  // inlined shared blocks are read-only here: no data-h-path inside
  if (block.blocks) slots.blocks = createElement(Blocks, { blocks: block.blocks, registry, preview })
  const attrs: BlockAttrs = { 'data-h-block': block.type, ...pathAttrs(path, preview) }
  return createElement(C, { block, props: block.props ?? {}, slots, attrs, path })
}

/** Renders a list of blocks; `path` is the list's path (`fields/body`), children get `<path>/<index>`. */
export function Blocks({ blocks, registry, path, preview }: Common & { blocks: BlockData[] | null | undefined }): ReactNode {
  return createElement(Fragment, null, (blocks ?? []).map((b, i) =>
    createElement(Block, { key: b.id, block: b, registry, path: path === undefined ? undefined : `${path}/${i}`, preview })))
}

/** Marks inline content as editable at `path` (`fields/title`); renders just the children outside preview. */
export function Field({ path, preview, as = 'span', children }: { path: string; preview?: boolean; as?: string; children?: ReactNode }): ReactNode {
  return preview ? createElement(as, { 'data-h-path': path }, children) : createElement(Fragment, null, children)
}
