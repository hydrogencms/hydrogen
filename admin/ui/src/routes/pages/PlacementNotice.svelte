<script lang="ts">
  import '../../lib/messages/pages';
  import { onMount } from 'svelte';
  import { channels } from '../../lib/channels.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import { api } from '../../lib/api/client';
  import Button from '../../lib/ui/Button.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { listUnplaced } from './place';
  import PlaceDialog from './PlaceDialog.svelte';

  // Shown above an unplaced page item in the editor: one request, made only for page types.
  let { id, type, name }: { id: string; type: string; name: string } = $props();

  let unplaced = $state(false);
  let sites = $state<string[]>([]);
  let open = $state(false);
  const canEdit = session.can('pages.edit');

  onMount(async () => {
    try {
      const res = await listUnplaced({ item: id, limit: 1, offset: 0 });
      unplaced = res.total > 0;
      if (unplaced && canEdit) {
        await channels.load();
        sites = (await api.get('/pages/sites')).items;
      }
    } catch {
      unplaced = false;
    }
  });
  const locale = $derived(channels.active?.languages?.[0] ?? channels.items[0]?.languages?.[0] ?? '');
</script>

{#if unplaced}
  <div class="h-placenotice" role="status">
    <span class="h-muted">{t('pages.notInTree')}</span>
    {#if canEdit && sites.length}
      <Button size="sm" icon="sitemap" onclick={() => (open = true)}>{t('pages.placeAction')}</Button>
    {/if}
  </div>
  {#if sites.length}
    <PlaceDialog
      bind:open
      item={{ id, type, name }}
      {sites}
      site={sites[0]}
      {locale}
      ondone={() => {
        unplaced = false;
        toast.success(t('pages.placed'));
      }}
    />
  {/if}
{/if}

<style>
  .h-placenotice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-3);
    padding: var(--h-space-2) var(--h-space-3);
    border-radius: var(--h-radius-md);
    background: var(--h-surface-2);
    font-size: var(--h-text-sm);
  }
</style>
