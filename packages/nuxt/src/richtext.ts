import { defineComponent, h } from 'vue'
import type { PropType, VNodeChild } from 'vue'
import { richTextNodes } from '@hydrogencms/client/richtext'
import type { PMNode, RichNode } from '@hydrogencms/client/richtext'

export type { PMNode }

/** Rich text (a ProseMirror document): `item:<id>` links become the URLs delivery put in `links`. */
export const HydrogenRichText = defineComponent({
  name: 'HydrogenRichText',
  props: {
    doc: { type: Object as PropType<PMNode | null> },
    links: { type: Object as PropType<Record<string, string>> },
  },
  setup(p) {
    const el = (n: RichNode, key: number): VNodeChild => (typeof n === 'string' ? n : h(n.tag, { key, ...n.attrs }, n.kids?.map(el)))
    return () => richTextNodes(p.doc, { links: p.links }).map(el)
  },
})
