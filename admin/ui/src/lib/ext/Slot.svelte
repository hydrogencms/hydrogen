<script lang="ts">
  import { ext } from '../extensions.svelte';
  import Extension from './Extension.svelte';

  let { at, props = {} }: { at: string; props?: Record<string, unknown> } = $props();
</script>

{#each ext.slots(at) as s, i (s.tag + i)}
  <div class="h-slot" data-slot={at}><Extension tag={s.tag} props={{ at, ...props }} /></div>
{/each}
