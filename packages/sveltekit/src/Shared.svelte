<script lang="ts">
  import type { BlockProps } from './blocks.js'
  let { attrs, renderSlot }: BlockProps = $props()
</script>

<div {...attrs} style="display: contents">{@render renderSlot('blocks')}</div>
