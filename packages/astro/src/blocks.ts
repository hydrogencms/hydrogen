export interface BlockData {
  id: string
  type: string
  props?: Record<string, any>
  slots?: Record<string, BlockData[]>
  /** `shared` blocks: the referenced item's blocks, inlined by delivery */
  blocks?: BlockData[]
}

/** Spread on a component's root element: `data-h-block` always, `data-h-path` in preview. */
export type BlockAttrs = { 'data-h-block': string; 'data-h-path'?: string }

/** The props of `<Blocks>`: a block component renders a slot with `<Blocks {...slots.items} />`. */
export interface BlocksProps {
  blocks: BlockData[]
  registry: Registry
  path?: string
  preview?: boolean
  links?: Record<string, string>
}

/** What a block component (an `.astro` file) receives. */
export interface BlockComponentProps<P = Record<string, any>> {
  block: BlockData
  props: P
  attrs: BlockAttrs
  path?: string
  preview?: boolean
  /** the item's `links` (item id → URL), for `ref` props and rich text */
  links?: Record<string, string>
  registry: Registry
  /** each slot of the block as `<Blocks>` props; a shared block's inlined blocks are `slots.blocks` */
  slots: Record<string, BlocksProps>
}

type Comp = unknown
export interface Registry {
  components: Readonly<Record<string, Comp>>
  /** rendered for unknown block types (default: nothing, a labelled box in preview) */
  fallback?: Comp
  warned: Set<string>
}

/** `{ hero: Hero, … }` from block type to Astro component; `shared` is built in. */
export const createRegistry = (components: Record<string, Comp>, o: { fallback?: Comp } = {}): Registry => ({ components, fallback: o.fallback, warned: new Set() })

/** `data-h-path` for editing overlays; only in preview. */
export const blockAttrs = (type: string, path: string | undefined, preview?: boolean): BlockAttrs =>
  preview && path ? { 'data-h-block': type, 'data-h-path': path } : { 'data-h-block': type }

declare const process: { env?: Record<string, string | undefined> } | undefined

/** The component for a block type: the registry's, the built-in `shared`, the fallback, else `unknown`'s (preview only). */
export function pickBlock(r: Registry, type: string, builtin: { shared: Comp; unknown: Comp }, preview?: boolean): Comp {
  const c = r.components[type] ?? (type === 'shared' ? builtin.shared : undefined)
  if (c) return c
  if (!r.warned.has(type) && typeof process !== 'undefined' && process.env?.NODE_ENV !== 'production') {
    r.warned.add(type)
    console.warn(`@hydrogencms/astro: no component registered for block type "${type}"`)
  }
  return r.fallback ?? (preview ? builtin.unknown : undefined)
}

/** The slots of a block as `<Blocks>` props; inlined shared blocks are read-only, so they carry no path. */
export function blockSlots(b: BlockData, own: string | undefined, registry: Registry, preview?: boolean, links?: Record<string, string>): Record<string, BlocksProps> {
  const out: Record<string, BlocksProps> = {}
  for (const k of Object.keys(b.slots ?? {})) out[k] = { blocks: b.slots![k]!, registry, path: own && `${own}/slots/${k}`, preview, links }
  if (b.blocks) out.blocks = { blocks: b.blocks, registry, preview, links }
  return out
}
