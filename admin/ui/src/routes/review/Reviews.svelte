<script lang="ts">
  import '../../lib/messages/review';
  import { reportError } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
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
  import { reviewApi } from './api';
  import { parseTab, stateTone, tabQuery, TABS, type QueueRow, type QueueTab } from './logic';

  const tab = $derived<QueueTab>(parseTab(router.query.get('tab')));
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<QueueRow[] | null>(null);
  let total = $state<number | undefined>();
  let hasMore = $state(false);
  let failed = $state(false);

  $effect(() => {
    const current = tab;
    const offset = (page - 1) * PAGE;
    const ctl = new AbortController();
    rows = null;
    failed = false;
    reviewApi
      .queue({ ...tabQuery(current), limit: PAGE, offset }, ctl.signal)
      .then((r) => {
        if (ctl.signal.aborted) return;
        rows = r.items;
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

  const tabs = $derived(TABS.map((id) => ({ id, label: t(`reviews.tab.${id}`) })));
  const typeLabel = (n: string) => types.get(n)?.label ?? n;
</script>

<div class="h-page">
  <PageHeader title={t('nav.reviews')} breadcrumbs={[{ label: t('nav.publishing') }, { label: t('nav.reviews') }]} meta={t('reviews.meta')} />
  <Tabs variant="underline" label={t('reviews.tabs')} {tabs} value={tab} onchange={(id) => router.setQuery({ tab: id === 'assigned' ? undefined : id, page: undefined })} />

  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => router.retry()}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t(`reviews.tab.${tab}`)}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('reviews.col.item'), cell: nameCell },
        { key: 'type', label: t('reviews.col.type'), cell: typeCell, mobile: 'hide' },
        { key: 'state', label: t('reviews.col.state'), cell: stateCell },
        { key: 'by', label: t('reviews.col.submittedBy'), cell: byCell, mobile: 'hide' },
        { key: 'at', label: t('reviews.col.submitted'), cell: atCell },
        { key: 'reviewers', label: t('reviews.col.reviewers'), cell: reviewersCell, mobile: 'hide' },
      ]}>
      {#snippet empty()}
        <EmptyState icon="check" title={t(`reviews.empty.${tab}`)} />
      {/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={PAGE} {total} {hasMore} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
</div>

{#snippet nameCell(r: QueueRow)}<NameCell name={r.name || r.id} icon={types.get(r.type)?.admin?.icon ?? 'file'} href={`/items/${r.id}`} />{/snippet}
{#snippet typeCell(r: QueueRow)}{typeLabel(r.type)}{/snippet}
{#snippet stateCell(r: QueueRow)}<Badge tone={stateTone(r.state)} dot>{t(`review.state.${r.state}`)}</Badge>{/snippet}
{#snippet byCell(r: QueueRow)}{r.submittedByName || t('review.someone')}{/snippet}
{#snippet atCell(r: QueueRow)}{#if r.submittedAt}<Time at={r.submittedAt} />{:else}<span class="h-muted">—</span>{/if}{/snippet}
{#snippet reviewersCell(r: QueueRow)}
  {#if r.reviewers.length}{r.reviewers.map((x) => x.name || t('review.someone')).join(', ')}{:else}<span class="h-muted">—</span>{/if}
{/snippet}
