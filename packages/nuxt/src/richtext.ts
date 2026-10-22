import { defineComponent, h } from 'vue'
import type { PropType, VNodeChild } from 'vue'
import { richTextNodes } from '@hydrogencms/client/richtext'
import type { PMNode, RichNode, RichTextOptions } from '@hydrogencms/client/richtext'

export type { PMNode }

/** Rich text (a ProseMirror document): `item:<id>` links become the URLs delivery put in `links`; `schema`, `classes` and `render` as in `RichTextOptions`. */
export const HydrogenRichText = defineComponent({
  name: 'HydrogenRichText',
  props: {
    doc: { type: Object as PropType<PMNode | null> },
    links: { type: Object as PropType<RichTextOptions['links']> },
    resolveLink: { type: Function as PropType<RichTextOptions['resolveLink']> },
    schema: { type: Object as PropType<RichTextOptions['schema']> },
    classes: { type: Object as PropType<RichTextOptions['classes']> },
    render: { type: Object as PropType<RichTextOptions['render']> },
  },
  setup(p) {
    const el = (n: RichNode, key: number): VNodeChild => (typeof n === 'string' ? n : h(n.tag, { key, ...Object.fromEntries(Object.entries(n.attrs).filter(([, v]) => v !== undefined)) }, n.kids?.map(el)))
    return () => richTextNodes(p.doc, p).map(el)
  },
})
