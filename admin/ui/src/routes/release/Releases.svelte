<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import '../../lib/messages/releases';
  import { session } from '../../lib/auth/session.svelte';
  import { formatInZone, inTab, parseTab, statusTone, tabQuery, TABS, type Release, type ReleaseTab } from './logic';
  import { t } from '../../lib/i18n';
  import { reportError } from '../../lib/errors';
  import { router } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { PAGE } from '../settings/util';
  import { releaseApi } from './api';
  import NewReleaseDialog from './NewReleaseDialog.svelte';

  const tab = $derived<ReleaseTab>(parseTab(router.query.get('tab')));
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Release[] | null>(null);
  let total = $state<number | undefined>();
  let hasMore = $state(false);
  let failed = $state(false);
  let creating = $state(false);
  const canManage = $derived(session.can('release.manage'));

  $effect(() => {
    const current = tab;
    const offset = (page - 1) * PAGE;
    const ctl = new AbortController();
    rows = null;
    failed = false;
    releaseApi
      .list({ ...tabQuery(current), limit: PAGE, offset }, ctl.signal)
      .then((r) => {
        rows = r.items.filter((x) => inTab(x, current));
        total = r.total;
        hasMore = !!r.hasMore;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        failed = true;
        reportError(e);
      });
    return () => ctl.abort();
  });

  const tabs = $derived(TABS.map((id) => ({ id, label: t(`releases.tab.${id}`) })));
  const label = (s: string) => t(`releases.status.${s}`);
  const when = (r: Release) => (r.status === 'published' ? r.publishedAt : r.scheduledAt);
</script>

<div class="h-page">
  <Slot at="releases.before" />
  <PageHeader title={t('nav.releases')} breadcrumbs={[{ label: t('nav.publishing') }, { label: t('nav.releases') }]} meta={t('releases.meta')}>
    {#snippet primary()}
      {#if canManage}<Button variant="primary" icon="plus" onclick={() => (creating = true)}>{t('releases.new')}</Button>{/if}
    {/snippet}
  </PageHeader>
  <Tabs
    variant="underline"
    label={t('releases.tabs')}
    {tabs}
    value={tab}
    onchange={(id) => router.setQuery({ tab: id === 'open' ? undefined : id, page: undefined })} />

  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => router.retry()}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t(`releases.tab.${tab}`)}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('releases.name'), cell: nameCell },
        { key: 'status', label: t('releases.statusCol'), cell: statusCell },
        { key: 'items', label: t('releases.items'), num: true, cell: itemsCell, mobile: 'hide' },
        { key: 'when', label: t('releases.when'), cell: whenCell },
        { key: 'by', label: t('releases.by'), cell: byCell, mobile: 'hide' },
        { key: 'updated', label: t('collections.col.updated'), cell: updatedCell, mobile: 'hide' },
      ]}>
      {#snippet empty()}
        <EmptyState icon="send" title={t(`releases.empty.${tab}`)} description={tab === 'open' && canManage ? t('releases.emptyHint') : undefined}>
          {#snippet actions()}
            {#if tab === 'open' && canManage}<Button variant="primary" icon="plus" onclick={() => (creating = true)}>{t('releases.new')}</Button>{/if}
          {/snippet}
        </EmptyState>
      {/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={PAGE} {total} {hasMore} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
  <Slot at="releases.after" />
</div>

{#snippet nameCell(r: Release)}<NameCell name={r.name || r.id} icon={r.kind === 'item' ? 'clock' : 'send'} href={`/releases/${r.id}`} sub={r.description || undefined} />{/snippet}
{#snippet statusCell(r: Release)}<Badge tone={statusTone(r.status)} dot>{label(r.status)}</Badge>{/snippet}
{#snippet itemsCell(r: Release)}{r.itemCount}{/snippet}
{#snippet whenCell(r: Release)}
  {#if when(r)}<span title={when(r)}>{formatInZone(when(r))}</span>{:else}<span class="h-muted">—</span>{/if}
{/snippet}
{#snippet byCell(r: Release)}{r.createdByName}{/snippet}
{#snippet updatedCell(r: Release)}<Time at={r.updatedAt} />{/snippet}

<NewReleaseDialog bind:open={creating} oncreated={(r) => router.navigate(`/releases/${r.id}`)} />
