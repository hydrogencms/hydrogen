<script lang="ts">
  import IconButton from './IconButton.svelte';
  import { t } from '../i18n';

  let {
    page,
    pageSize,
    total,
    hasMore,
    onpage,
  }: {
    /** 1-based. */
    page: number;
    pageSize: number;
    /** Known total (when the API returned it). */
    total?: number;
    /** Used when total is unknown. */
    hasMore?: boolean;
    onpage: (page: number) => void;
  } = $props();

  const from = $derived((page - 1) * pageSize + 1);
  const last = $derived(total !== undefined ? Math.max(1, Math.ceil(total / pageSize)) : undefined);
  const more = $derived(last !== undefined ? page < last : !!hasMore);
  const to = $derived(total !== undefined ? Math.min(total, page * pageSize) : page * pageSize);
</script>

<nav class="h-pagination" aria-label={t('pagination.label')}>
  <span class="h-pagination__range h-num" aria-live="polite">
    {#if total !== undefined}{total === 0 ? '0' : t('pagination.range', { from, to, total })}{:else}{t('pagination.page', { page })}{/if}
  </span>
  <IconButton icon="chevron-left" label={t('common.previous')} size="sm" variant="secondary" disabled={page <= 1} onclick={() => onpage(page - 1)} />
  <IconButton icon="chevron-right" label={t('common.next')} size="sm" variant="secondary" disabled={!more} onclick={() => onpage(page + 1)} />
</nav>
