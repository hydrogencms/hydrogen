<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import ListBar from './ListBar.svelte';
  import SettingsHead from './SettingsHead.svelte';
  import { matchesText, PAGE, pageOf } from './util';

  type Role = Schemas['Role'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Role[] | null>(null);
  let failed = $state(false);
  let q = $state(router.query.get('q') ?? '');

  // Roles are few: load them all, filter and page here.
  async function load() {
    failed = false;
    try {
      rows = (await api.get('/roles', { query: { limit: 500 } })).items;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load();
  });
  const filtered = $derived((rows ?? []).filter((r) => matchesText(q, r.name, r.description, r.template)));
  function typing() {
    router.setQuery({ q: q.trim() || undefined, page: undefined });
  }
</script>

<div class="h-page">
  <SettingsHead section="roles">
    {#snippet primary()}
      {#if session.can('role.manage')}<Button variant="primary" icon="plus" onclick={() => router.navigate('/settings/roles/new')}>{t('settings.roles.new')}</Button>{/if}
    {/snippet}
  </SettingsHead>
  <ListBar bind:value={q} label={t('settings.roles.search')} placeholder={t('settings.roles.search')} oninput={typing} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.roles')}
      loading={!rows}
      rows={pageOf(filtered, page)}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.roles.name'), cell: nameCell },
        { key: 'template', label: t('settings.roles.template'), cell: templateCell, mobile: 'hide' },
        { key: 'policies', label: t('settings.roles.policies'), num: true, cell: countCell },
      ]}>
      {#snippet empty()}
        {#if q.trim()}
          <EmptyState icon="search" title={t('settings.roles.noMatch')} />
        {:else}
          <EmptyState icon="shield" title={t('settings.roles.empty')} description={t('settings.roles.emptyHint')} />
        {/if}
      {/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={PAGE} total={filtered.length} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
  <Slot at="settings.after" props={{section: 'roles'}} />
</div>

{#snippet nameCell(r: Role)}<NameCell name={r.name} sub={r.description || undefined} icon="shield" href={`/settings/roles/${r.id}`} />{/snippet}
{#snippet templateCell(r: Role)}{#if r.template}<Badge tone="info">{r.template}</Badge>{/if}{/snippet}
{#snippet countCell(r: Role)}{r.policies.length}{/snippet}
