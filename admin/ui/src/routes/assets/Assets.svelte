<script lang="ts">
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import '../../lib/messages/media';
  import { types } from '../../lib/types.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import AssetsScreen from './AssetsScreen.svelte';

  void types.load();
  const names = $derived(types.all.filter((x) => x.upload && session.can('content.read', { type: x.name })).map((x) => x.name));
</script>

{#if !types.loaded}
  <div class="h-page"><Skeleton lines={5} height="1.4rem" /></div>
{:else if names.length === 0}
  <div class="h-page">
    <PageHeader title={t('media.title')} />
    <EmptyState icon="image" title={t('media.empty')} />
  </div>
{:else}
  <AssetsScreen {names} />
{/if}
