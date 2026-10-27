<script lang="ts">
  import { config } from '../../lib/config';
  import { session } from '../../lib/auth/session.svelte';
  import { types } from '../../lib/types.svelte';
  import Editor from './Editor.svelte';

  let { params, id, embedded = false }: { params?: Record<string, string>; id?: string; embedded?: boolean } = $props();
</script>

{#snippet aside(d: { id: string; type: string; name?: string } | null)}
  {#if d && !embedded && config().pages && types.get(d.type)?.page && session.can('pages.read')}
    {#await import('../pages/PlacementNotice.svelte') then { default: PlacementNotice }}
      {#key d.id}<PlacementNotice id={d.id} type={d.type} name={d.name ?? ''} />{/key}
    {/await}
  {/if}
{/snippet}

<Editor mode="item" id={id ?? params?.id ?? ''} {embedded} {aside} />
