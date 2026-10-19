<script lang="ts">
  import type { Snippet } from 'svelte'

  /**
   * Marks inline content as editable at `path` (`fields/title`) in preview; outside it renders the content alone.
   * A plain string is passed as `value`: the span then holds exactly one text node (Svelte's own markers would add
   * comment nodes to a dynamic element), which inline editing on the canvas needs.
   */
  let { path, preview = false, value, children }: { path: string; preview?: boolean; value?: string | null; children?: Snippet } = $props()
</script>

{#if !preview}
  {#if children}{@render children()}{:else}{value}{/if}
{:else if children}
  <span data-h-path={path}>{@render children()}</span>
{:else}
  <span data-h-path={path}>{value}</span>
{/if}
