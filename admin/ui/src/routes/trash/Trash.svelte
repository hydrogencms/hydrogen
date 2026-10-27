<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import { tick as settle } from 'svelte';
  import '../../lib/messages/releases';
  import { ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { reportError } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from '../settings/Confirm.svelte';
  import { trashApi, type TrashRow } from '../release/api';

  const SIZE = 25;
  const type = $derived(router.query.get('type') ?? '');
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<TrashRow[] | null>(null);
  let total = $state<number | undefined>();
  let hasMore = $state(false);
  let failed = $state(false);
  let tick = $state(0);
  const canDelete = $derived(session.can('content.delete'));

  let shownType: string | undefined;
  // Row index to focus after a restore/purge removed the row that had focus.
  let focusRow = -1;

  async function refocus() {
    const at = focusRow;
    focusRow = -1;
    if (at < 0) return;
    await settle();
    const btns = document.querySelectorAll<HTMLElement>('[data-trash-restore]');
    (btns[Math.min(at, btns.length - 1)] ?? document.querySelector<HTMLElement>('.h-trash__bar select'))?.focus();
  }

  $effect(() => {
    void tick;
    const ctl = new AbortController();
    failed = false;
    if (shownType !== type) {
      if (shownType !== undefined) rows = null;
      shownType = type;
    }
    trashApi
      .list({ type: type || undefined, limit: SIZE, offset: (page - 1) * SIZE }, ctl.signal)
      .then((r) => {
        if (!r.items.length && page > 1) {
          // The last row of this page went: step back to the previous one.
          router.setQuery({ page: page - 1 === 1 ? undefined : page - 1 });
          return;
        }
        rows = r.items;
        total = r.total;
        hasMore = !!r.hasMore;
        void refocus();
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        failed = true;
        reportError(e);
      });
    return () => ctl.abort();
  });

  const typeOptions = $derived(types.listable.map((x) => ({ value: x.name, label: x.label })));

  async function restore(r: TrashRow) {
    focusRow = rows?.findIndex((x) => x.id === r.id) ?? -1;
    try {
      await trashApi.restore(r.id);
      toast.success(t('trash.restored', { name: r.name || r.id }));
      tick++;
    } catch (e) {
      focusRow = -1;
      if (e instanceof ApiError) reportError(e);
      else throw e;
    }
  }

  let purging = $state<TrashRow | null>(null);
  let purgeOpen = $state(false);
  async function purge() {
    if (!purging) return;
    focusRow = rows?.findIndex((x) => x.id === purging!.id) ?? -1;
    try {
      await trashApi.purge(purging.id);
      toast.success(t('trash.purged'));
      tick++;
    } catch (e) {
      focusRow = -1;
      reportError(e);
      throw e;
    }
  }
</script>

<div class="h-page">
  <Slot at="trash.before" />
  <PageHeader title={t('nav.trash')} breadcrumbs={[{ label: t('nav.publishing') }, { label: t('nav.trash') }]} meta={t('trash.meta')} />
  <div class="h-trash__bar">
    <Select
      compact
      aria-label={t('trash.filterType')}
      value={type}
      placeholder={t('trash.allTypes')}
      options={typeOptions}
      onchange={(v) => router.setQuery({ type: v || undefined, page: undefined })} />
  </div>

  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => tick++}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.trash')}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('releases.name'), cell: nameCell },
        { key: 'type', label: t('releases.itemType'), cell: typeCell, mobile: 'hide' },
        { key: 'deletedAt', label: t('trash.deleted'), cell: deletedCell },
        { key: 'by', label: t('trash.by'), cell: byCell, mobile: 'hide' },
        ...(canDelete ? [{ key: 'actions', label: t('common.actions'), align: 'end' as const, cell: actionCell }] : []),
      ]}>
      {#snippet empty()}<EmptyState icon="trash" title={type ? t('trash.emptyType') : t('trash.empty')} description={t('trash.emptyHint')} />{/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={SIZE} {total} {hasMore} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
  <Slot at="trash.after" />
</div>

{#snippet nameCell(r: TrashRow)}<NameCell name={r.name || r.id} icon={types.get(r.type)?.admin?.icon ?? 'file'} href={`/trash/${r.id}`} />{/snippet}
{#snippet typeCell(r: TrashRow)}{types.get(r.type)?.label ?? r.type}{/snippet}
{#snippet deletedCell(r: TrashRow)}<Time at={r.deletedAt} />{/snippet}
{#snippet byCell(r: TrashRow)}{r.deletedByName || r.deletedBy}{/snippet}
{#snippet actionCell(r: TrashRow)}
  <span class="h-trash__acts">
    <Button size="sm" icon="refresh" data-trash-restore onclick={() => void restore(r)}>{t('trash.restore')}</Button>
    <Button
      size="sm"
      variant="danger"
      onclick={() => {
        purging = r;
        purgeOpen = true;
      }}>{t('trash.purge')}</Button>
  </span>
{/snippet}

<Confirm
  bind:open={purgeOpen}
  title={t('trash.purge')}
  message={t('trash.purgeConfirm', { name: purging?.name || purging?.id || '' })}
  confirm={t('trash.purge')}
  onconfirm={purge} />

<style>
  .h-trash__bar {
    display: flex;
    gap: var(--h-space-3);
    align-items: center;
  }
  .h-trash__acts {
    display: inline-flex;
    gap: var(--h-space-2);
  }
</style>
