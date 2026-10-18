<script lang="ts">
  import { parseSnippet } from '../snippet';

  let { html }: { html: string } = $props();
  const segments = $derived(parseSnippet(html));
</script>

{#each segments as s}{#if s.mark}<mark class="h-mark">{s.text}</mark>{:else}{s.text}{/if}{/each}
