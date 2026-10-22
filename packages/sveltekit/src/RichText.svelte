<script lang="ts">
  import RichNodes from './RichNodes.svelte'
  import { richTextNodes } from '@hydrogencms/client/richtext'
  import type { PMNode, RichTextOptions } from '@hydrogencms/client/richtext'

  /** A ProseMirror document as HTML; `item:<id>` links come from delivery's `links`. Unsafe URLs render as plain text; `schema`, `classes` and `render` as in `RichTextOptions`. */
  let { doc, ...options }: { doc: PMNode | null | undefined } & RichTextOptions = $props()
  const nodes = $derived(richTextNodes(doc, options))
</script>

<RichNodes {nodes} />
