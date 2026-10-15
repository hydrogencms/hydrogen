<svelte:options customElement={{ tag: 'x-color-swatch', props: { ctx: { type: 'Object' }, value: { type: 'String' } } }} />

<script lang="ts">
  let { value = '' }: { value?: string | null } = $props()
</script>

{#if value}
  <span class="swatch" data-swatch={value}>
    <i class="chip" style:background-color={value}></i>
    <code>{value}</code>
  </span>
{/if}

<style>
  .swatch { display: inline-flex; gap: var(--h-space-2); align-items: center; }
  .chip { display: inline-block; inline-size: 1rem; block-size: 1rem; border-radius: 50%; border: 1px solid var(--h-border-strong); }
</style>
