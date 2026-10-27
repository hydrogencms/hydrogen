<script lang="ts">
  import '../../lib/messages/collections';
  import { config } from '../../lib/config';
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import { types } from '../../lib/types.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { errorMessage } from '../../lib/errors';
  import Editor from '../item/Editor.svelte';

  let { params }: { params: Record<string, string> } = $props();

  // A page type created here is also offered a place in the site tree; the picker loads only then.
  const place = $derived(!!types.get(params.type)?.page && config().pages && session.can('pages.edit'));
  const loadPlace = () => Promise.all([import('../pages/PlaceControls.svelte'), import('../pages/place')]);
  let target = $state<{ site: string; parent?: string } | null>(null);

  async function oncreated(item: { id: string }) {
    const tgt = target;
    if (!place || !tgt) return;
    try {
      const { placeItem } = await import('../pages/place');
      await placeItem(tgt.site, item.id, tgt.parent);
    } catch (e) {
      toast.error(t('pages.createdNotPlaced', { id: item.id, reason: errorMessage(e) }));
    }
  }
</script>

{#snippet aside()}
  {#if place}
    {#await loadPlace() then [{ default: PlaceControls }]}
      <PlaceControls type={params.type} bind:target />
    {/await}
  {/if}
{/snippet}

<Editor mode="new" typeName={params.type} {aside} {oncreated} />
