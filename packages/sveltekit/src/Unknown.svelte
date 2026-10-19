<script lang="ts">
  import type { BlockProps } from './blocks.js'
  let { block, attrs }: BlockProps = $props()
</script>

{#if attrs['data-h-path'] !== undefined}<div {...attrs}>Unknown block "{block.type}"</div>{/if}
