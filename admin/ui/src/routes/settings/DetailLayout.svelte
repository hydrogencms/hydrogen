<script lang="ts">
  import type { Snippet } from 'svelte';

  let { main, side }: { main: Snippet; side?: Snippet } = $props();
</script>

<div class="h-detail" class:h-detail--single={!side}>
  <div class="h-detail__main">{@render main()}</div>
  {#if side}<div class="h-detail__side">{@render side()}</div>{/if}
</div>

<style>
  .h-detail {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--h-space-5);
    align-items: start;
  }
  .h-detail__main,
  .h-detail__side {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-5);
    min-width: 0;
  }
  @media (min-width: 1100px) {
    .h-detail:not(.h-detail--single) {
      grid-template-columns: minmax(0, 1fr) minmax(22rem, 28rem);
    }
  }
</style>
