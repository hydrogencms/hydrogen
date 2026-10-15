<script lang="ts">
  import type { Snippet } from 'svelte';
  import Input from '../../lib/ui/Input.svelte';

  let {
    value = $bindable(''),
    label,
    placeholder,
    oninput,
    children,
  }: {
    value?: string;
    label: string;
    placeholder?: string;
    oninput?: () => void;
    /** Filter chips and other controls after the search box. */
    children?: Snippet;
  } = $props();
</script>

<div class="h-listbar" role="search" aria-label={label}>
  <div class="h-listbar__search">
    <Input icon="search" bind:value aria-label={label} {placeholder} {oninput} autocomplete="off" />
  </div>
  {#if children}{@render children()}{/if}
</div>

<style>
  .h-listbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-listbar__search {
    width: min(100%, 20rem);
  }
</style>
