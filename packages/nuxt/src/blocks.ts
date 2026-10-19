import { defineComponent, h, inject, provide } from 'vue'
import type { Component, PropType, VNodeChild } from 'vue'

export interface BlockData {
  id: string
  type: string
  props?: Record<string, any>
  slots?: Record<string, BlockData[]>
  /** `shared` blocks: the referenced item's blocks, inlined by delivery */
  blocks?: BlockData[]
}

export interface Registry {
  components: Readonly<Record<string, Component>>
  fallback: Component
  warned: Set<string>
}

const LINKS = Symbol('hydrogen:links')
const PREVIEW = Symbol('hydrogen:preview')

/** The `links` (item id → URL) of the page being rendered, for `ref` props: `useLinks()[props.link]`. */
export const useLinks = (): Record<string, string> => inject<Record<string, string>>(LINKS, {})

/** `shared` is built in: it renders the blocks delivery inlined (its slot `blocks`). */
const Shared = defineComponent({ setup: (_, { slots }) => () => h('div', { style: 'display:contents' }, slots.blocks?.()) })

/** Unknown block types render nothing, except a marker in preview. */
const Unknown = defineComponent({
  props: { type: String },
  setup: (p, { attrs }) => () => (attrs['data-h-path'] === undefined ? null : h('div', attrs, `Unknown block "${p.type}"`)),
})

/**
 * A map from block type to component. A component receives the block's props as its prop `props` and the block's
 * slots as Vue slots of the same name; the attributes `data-h-block` (and `data-h-path` in preview) fall through to
 * its root element (a component with several roots binds `$attrs` itself).
 */
export function createRegistry(components: Record<string, Component>, o: { fallback?: Component } = {}): Registry {
  return { components: { shared: Shared, ...components }, fallback: o.fallback ?? Unknown, warned: new Set() }
}

const dev = () => typeof process !== 'undefined' && process.env?.NODE_ENV !== 'production'

/**
 * Renders a list of blocks through the registry; `path` is the list's path (`fields/body`), the children get
 * `<path>/<index>`. `links` and `preview` are available to the block components below (`useLinks`).
 */
export const HydrogenBlocks = defineComponent({
  name: 'HydrogenBlocks',
  props: {
    blocks: { type: Array as PropType<BlockData[] | null>, default: null },
    registry: { type: Object as PropType<Registry>, required: true },
    path: String,
    preview: Boolean,
    links: Object as PropType<Record<string, string>>,
  },
  setup(p) {
    if (p.links) provide(LINKS, p.links)
    provide(PREVIEW, p.preview)
    const render = (b: BlockData, path: string | undefined): VNodeChild => {
      const known = p.registry.components[b.type]
      if (!known && dev() && !p.registry.warned.has(b.type)) {
        p.registry.warned.add(b.type)
        console.warn(`@hydrogencms/nuxt: no component for block "${b.type}"`)
      }
      const nested = (blocks: BlockData[], to?: string) => () => h(HydrogenBlocks, { blocks, registry: p.registry, path: to, preview: p.preview })
      const slots: Record<string, () => VNodeChild> = {}
      for (const k of Object.keys(b.slots ?? {})) slots[k] = nested(b.slots![k]!, path && `${path}/slots/${k}`)
      // inlined shared blocks are read-only here: no data-h-path inside
      if (b.blocks) slots.blocks = nested(b.blocks)
      const attrs = { 'data-h-block': b.type, ...(p.preview && path ? { 'data-h-path': path } : {}) }
      return h(known ?? p.registry.fallback, known ? { key: b.id, props: b.props ?? {}, ...attrs } : { key: b.id, type: b.type, ...attrs }, slots)
    }
    return () => (p.blocks ?? []).map((b, i) => render(b, p.path === undefined ? undefined : `${p.path}/${i}`))
  },
})

/** Marks inline content as editable at `path` (`fields/title`); renders just the slot outside preview. */
export const HydrogenField = defineComponent({
  name: 'HydrogenField',
  props: { path: { type: String, required: true }, preview: { type: Boolean, default: undefined }, as: { type: String, default: 'span' } },
  setup(p, { slots }) {
    const inherited = inject<boolean>(PREVIEW, false)
    return () => ((p.preview ?? inherited) ? h(p.as, { 'data-h-path': p.path }, slots.default?.()) : slots.default?.())
  },
})
