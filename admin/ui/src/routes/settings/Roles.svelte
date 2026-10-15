<script lang="ts">
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { PAGE } from './util';

  type Role = Schemas['Role'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Role[] | null>(null);
  let total = $state(0);
  let failed = $state(false);

  async function load(p: number) {
    failed = false;
    try {
      const r = await api.get('/roles', { query: { limit: PAGE, offset: (p - 1) * PAGE } });
      rows = r.items;
      total = r.total;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load(page);
  });
</script>

<div class="h-page">
  <div class="h-page__head">
    <h1>{t('nav.roles')}</h1>
    {#if session.can('role.manage')}<Button variant="primary" icon="plus" onclick={() => router.navigate('/settings/roles/new')}>{t('settings.roles.new')}</Button>{/if}
  </div>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(page)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.roles')}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.roles.name'), cell: nameCell },
        { key: 'description', label: t('settings.description') },
        { key: 'template', label: t('settings.roles.template'), cell: templateCell },
        { key: 'policies', label: t('settings.roles.policies'), align: 'end', cell: countCell },
      ]}>
      {#snippet empty()}<p class="h-muted">{t('settings.roles.empty')}</p>{/snippet}
    </Table>
    <Pagination {page} pageSize={PAGE} {total} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
  {/if}
</div>

{#snippet nameCell(r: Role)}<a use:link href={href(`/settings/roles/${r.id}`)}>{r.name}</a>{/snippet}
{#snippet templateCell(r: Role)}{#if r.template}<Badge tone="info">{r.template}</Badge>{/if}{/snippet}
{#snippet countCell(r: Role)}{r.policies.length}{/snippet}
