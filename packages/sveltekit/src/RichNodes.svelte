<script lang="ts">
  import RichNodes from './RichNodes.svelte'
  import type { RichNode } from '@hydrogencms/client/richtext'
  let { nodes }: { nodes: RichNode[] } = $props()
</script>

{#each nodes as n}{#if typeof n === 'string'}{n}{:else if n.kids}<svelte:element this={n.tag} {...n.attrs}><RichNodes nodes={n.kids} /></svelte:element>{:else}<svelte:element this={n.tag} {...n.attrs} />{/if}{/each}
