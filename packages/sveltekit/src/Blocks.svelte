<script lang="ts">
  import Blocks from './Blocks.svelte'
  import { blockAttrs, componentOf } from './blocks.js'
  import type { BlockData, Registry } from './blocks.js'

  /** Renders a list of blocks; `path` is the list's path (`fields/body`), children get `<path>/<index>`. */
  let { blocks, registry, path, preview = false }: { blocks: BlockData[] | null | undefined; registry: Registry; path?: string; preview?: boolean } = $props()
</script>

{#each blocks ?? [] as block, i}
  {@const C = componentOf(registry, block.type)}
  {@const p = path === undefined ? undefined : `${path}/${i}`}
  {#snippet renderSlot(name: string)}
    <!-- inlined shared blocks are read-only here: no data-h-path inside -->
    {#if name === 'blocks'}
      <Blocks blocks={block.blocks} {registry} {preview} />
    {:else}
      <Blocks blocks={block.slots?.[name]} {registry} path={p && `${p}/slots/${name}`} {preview} />
    {/if}
  {/snippet}
  <C {block} props={block.props ?? {}} attrs={blockAttrs(block.type, p, preview)} path={p} {renderSlot} />
{/each}
