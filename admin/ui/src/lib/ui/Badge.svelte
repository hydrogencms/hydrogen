<script lang="ts">
  import type { Snippet } from 'svelte';
  let {
    tone = 'neutral',
    dot = false,
    children,
  }: {
    tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info';
    /** A 6 px leading dot in the tone colour. */
    dot?: boolean;
    children: Snippet;
  } = $props();
</script>

<span class="h-badge h-badge--{tone}">{#if dot}<span class="h-badge__dot" aria-hidden="true"></span>{/if}{@render children()}</span>
