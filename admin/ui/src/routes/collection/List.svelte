<script lang="ts">
  import '../../lib/messages/collections';
  import { tick } from 'svelte';
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { errorMessage } from '../../lib/errors';
  import { cellText, runPool } from '../../lib/fields/logic';
  import { knownLocales } from '../../lib/fields/locales';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { link, href, router } from '../../lib/router.svelte';
  import { types, type CompiledField } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import type { Column } from '../../lib/ui/table';
  import LocaleDots from './LocaleDots.svelte';
  import StatusBadge from './StatusBadge.svelte';
  import FilterBar from './FilterBar.svelte';
  import ViewsDialog from './ViewsDialog.svelte';
  import { nextSort, parseQuery, sortState, toApiQuery, toUrlPatch, type ListQuery } from './query';

  type Summary = Schemas['Summary'];
  type Row = Summary & Record<string, unknown>;

  let { params }: { params: Record<string, string> } = $props();

  const typeName = $derived(params.type!);
  const type = $derived(types.get(typeName));
  const q = $derived(parseQuery(router.query));
  const locales = $derived(knownLocales());
  const apiQuery = $derived(toApiQuery(q, channels.languages?.[0] ?? ''));
  const shown = $derived(channels.showsType(typeName));
  const canPublish = $derived(session.can('content.publish', { type: typeName }));
  const canDelete = $derived(session.can('content.delete', { type: typeName }));
  const canCreate = $derived(session.can('content.create', { type: typeName }));
  const sort = $derived(sortState(q));

  let rows = $state.raw<Summary[]>([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state<Error | null>(null);
  let reload = $state(0);
  let selected = $state(new Set<string>());

  // ---- load
  $effect(() => {
    const query = apiQuery;
    const name = typeName;
    void reload;
    if (!types.get(name) || !channels.showsType(name)) return;
    const ctl = new AbortController();
    loading = true;
    error = null;
    api
      .get('/collections/{type}/items', {
        params: { type: name },
        query: { ...query, fields: fieldCols.length ? fieldCols.slice(0, 10).map((f) => f.name).join(',') : undefined } as never,
        signal: ctl.signal,
      })
      .then((r) => {
        rows = r.items;
        total = r.total;
        loading = false;
        const ids = new Set(r.items.map((i) => i.id));
        selected = new Set([...selected].filter((id) => ids.has(id)));
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        error = e;
        loading = false;
      });
    return () => ctl.abort();
  });

  // Columns for fields come from the list response (`fields=a,b`, at most 10 names).
  const fieldCols = $derived(
    (type?.admin?.list ?? []).filter((n) => n !== 'name').map((n) => type!.fields.find((f) => f.name === n)).filter((f): f is CompiledField => !!f),
  );

  // ---- URL state
  function update(patch: Partial<ListQuery>, opts: { push?: boolean } = {}) {
    const next: ListQuery = { ...q, page: 1, ...patch };
    router.setQuery(toUrlPatch(next, router.query), opts);
  }

  // ---- table
  const rowsView = $derived(
    rows.map((r) => {
      const row: Row = { ...r, updated: formatDateTime(r.updatedAt) };
      const d = (r as { fields?: Record<string, unknown> }).fields ?? {};
      for (const f of fieldCols) {
        row[`f:${f.name}`] = f.shape.base === 'boolean' ? (d[f.name] === true ? t('common.yes') : '') : cellText(f, d[f.name]);
      }
      return row;
    }),
  );
  const columns = $derived<Column<Row>[]>([
    { key: 'name', label: t('collections.col.name'), sortable: true, cell: nameCell },
    ...fieldCols.map((f) => ({ key: `f:${f.name}`, label: f.label, sortable: !!f.index }) as Column<Row>),
    { key: 'status', label: t('collections.col.status'), cell: statusCell },
    { key: 'locales', label: t('collections.col.languages'), cell: localesCell },
    { key: 'updated', label: t('collections.col.updated'), sortable: true },
  ]);
  const tableSort = $derived({
    key: sort.key === 'updatedAt' ? 'updated' : fieldCols.some((f) => f.name === sort.key) ? `f:${sort.key}` : sort.key,
    dir: sort.dir,
  });
  function onsort(key: string) {
    const name = key === 'updated' ? 'updatedAt' : key.startsWith('f:') ? key.slice(2) : key;
    update({ sort: nextSort(q, name, name === 'updatedAt') });
  }
  const offered = $derived(locales.length ? locales : []);

  // ---- search (debounced into the URL)
  let search = $state('');
  $effect(() => {
    search = q.q;
  });
  let timer: ReturnType<typeof setTimeout> | undefined;
  function onsearch(v: string) {
    search = v;
    clearTimeout(timer);
    timer = setTimeout(() => update({ q: v }), 300);
  }

  // ---- create
  let pickLocale = $state(false);
  let newLocale = $state('');
  function create() {
    if (offered.length > 1) {
      newLocale = q.locale || offered[0]!;
      pickLocale = true;
    } else void router.navigate(`/collections/${typeName}/new${offered[0] ? `?locale=${offered[0]}` : ''}`);
  }
  function confirmCreate() {
    pickLocale = false;
    void router.navigate(`/collections/${typeName}/new?locale=${encodeURIComponent(newLocale)}`);
  }

  // ---- bulk actions
  interface Result {
    id: string;
    name: string;
    ok: boolean;
    message: string;
  }
  let bulk = $state<{ kind: 'publish' | 'delete'; phase: 'confirm' | 'running' | 'done'; results: Result[] } | null>(null);
  let bulkOpen = $state(false);
  const selectedRows = $derived(rows.filter((r) => selected.has(r.id)));

  function startBulk(kind: 'publish' | 'delete') {
    bulk = { kind, phase: 'confirm', results: [] };
    bulkOpen = true;
  }
  function errText(e: unknown): string {
    if (e instanceof ApiError) {
      if (e.code === 'referenced') return t('collections.bulk.referenced', { n: String((e.body?.referrers as unknown[] | undefined)?.length ?? 0) });
      if (e.status === 403) return t('app.forbidden');
      return e.message;
    }
    return e instanceof Error ? e.message : String(e);
  }
  async function runBulk() {
    if (!bulk) return;
    const kind = bulk.kind;
    const targets = [...selectedRows];
    bulk = { kind, phase: 'running', results: [] };
    const res = await runPool(targets, 4, async (r) => {
      if (kind === 'publish') {
        if (!r.draftParts.length) throw new Error(t('collections.bulk.nothing'));
        await api.post('/items/{id}/publish', { params: { id: r.id }, body: {} as never });
      } else await api.del('/items/{id}', { params: { id: r.id } });
    });
    const results = targets.map((r, i) => ({ id: r.id, name: r.name || r.id, ok: res[i]!.ok, message: res[i]!.ok ? '' : errText((res[i] as { error: unknown }).error) }));
    bulk = { kind, phase: 'done', results };
    await tick();
    document.getElementById('h-bulk-close')?.focus();
    selected = new Set(results.filter((r) => !r.ok).map((r) => r.id));
    reload++;
  }

  // ---- views
  let viewsOpen = $state(false);
  const clearChannel = () => channels.select('');
</script>

<div class="h-page">
  {#if !type}
    <EmptyState icon="alert" title={t('collections.unknownType')} description={typeName} />
  {:else if !shown}
    <EmptyState icon="filter" title={t('collections.notInChannel', { type: type.label })} description={t('collections.notInChannelText')}>
      {#snippet actions()}<Button onclick={clearChannel}>{t('collections.allChannels')}</Button>{/snippet}
    </EmptyState>
  {:else}
    <div class="h-page__head">
      <div>
        <h1>{type.label}</h1>
        {#if type.description}<p class="h-muted">{type.description}</p>{/if}
      </div>
      <div class="h-row">
        <Button icon="star" onclick={() => (viewsOpen = true)}>{t('collections.views')}</Button>
        {#if canCreate}<Button variant="primary" icon="plus" onclick={create}>{t('collections.new')}</Button>{/if}
      </div>
    </div>

    <FilterBar {type} {q} {offered} {search} {onsearch} onchange={(patch) => update(patch)} />

    {#if selected.size > 0 && (canPublish || canDelete)}
      <div class="h-list-bulk h-row" role="region" aria-label={t('collections.bulk.label')}>
        <strong>{t('collections.bulk.selected', { n: selected.size })}</strong>
        {#if canPublish}<Button size="sm" onclick={() => startBulk('publish')}>{t('collections.bulk.publish')}</Button>{/if}
        {#if canDelete}<Button size="sm" variant="danger" onclick={() => startBulk('delete')}>{t('common.delete')}</Button>{/if}
        <Button size="sm" variant="ghost" onclick={() => (selected = new Set())}>{t('common.clear')}</Button>
      </div>
    {/if}

    {#if error}
      <div class="h-alert" role="alert">
        <span>{errorMessage(error)}</span>
        <Button size="sm" onclick={() => reload++}>{t('app.retry')}</Button>
      </div>
    {:else}
      <Table
        columns={columns}
        rows={rowsView}
        rowKey={(r) => r.id}
        caption={t('collections.caption', { type: type.label })}
        sort={tableSort}
        {onsort}
        selectable={canPublish || canDelete}
        bind:selected
        {loading}
        onrowclick={(r) => void router.navigate(`/items/${r.id}${q.locale ? `?locale=${q.locale}` : ''}`)}
      >
        {#snippet empty()}
          <EmptyState icon="file" title={t('collections.empty')} description={q.q || q.filters.length || q.status ? t('collections.emptyFiltered') : t('collections.emptyText')}>
            {#snippet actions()}{#if canCreate}<Button variant="primary" onclick={create}>{t('collections.new')}</Button>{/if}{/snippet}
          </EmptyState>
        {/snippet}
      </Table>
      <Pagination page={q.page} pageSize={q.pageSize} {total} onpage={(p) => update({ page: p }, { push: true })} />
    {/if}
  {/if}
</div>

{#snippet nameCell(row: Row)}
  <a class="h-list-name" use:link href={href(`/items/${row.id}`, q.locale ? { locale: q.locale } : undefined)}>{row.name || t('collections.untitled')}</a>
  {#if row.key}<span class="h-muted h-list-key">{row.key}</span>{/if}
{/snippet}
{#snippet statusCell(row: Row)}<StatusBadge status={row.status} />{/snippet}
{#snippet localesCell(row: Row)}
  <LocaleDots locales={offered.length ? offered : row.locales} item={row} />
{/snippet}

<Dialog bind:open={pickLocale} title={t('collections.pickLocale')} size="sm">
  <p>{t('collections.pickLocaleText')}</p>
  <Select bind:value={newLocale} aria-label={t('collections.mainLocale')} options={offered.map((l) => ({ value: l, label: l }))} />
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (pickLocale = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" onclick={confirmCreate}>{t('collections.continue')}</Button>
  {/snippet}
</Dialog>

{#if bulk}
  <Dialog bind:open={bulkOpen} title={bulk.kind === 'publish' ? t('collections.bulk.publishTitle') : t('collections.bulk.deleteTitle')} dismissible={bulk.phase !== 'running'} onclose={() => (bulk = null)}>
    {#if bulk.phase === 'confirm'}
      <p>{bulk.kind === 'publish' ? t('collections.bulk.publishText', { n: selectedRows.length }) : t('collections.bulk.deleteText', { n: selectedRows.length })}</p>
      <ul class="h-list-names">
        {#each selectedRows.slice(0, 10) as r (r.id)}<li>{r.name || r.id}</li>{/each}
        {#if selectedRows.length > 10}<li class="h-muted">{t('collections.bulk.more', { n: selectedRows.length - 10 })}</li>{/if}
      </ul>
    {:else if bulk.phase === 'running'}
      <p role="status">{t('collections.bulk.running')}</p>
    {:else}
      <p role="status">{t('collections.bulk.done', { ok: bulk.results.filter((r) => r.ok).length, failed: bulk.results.filter((r) => !r.ok).length })}</p>
      <ul class="h-list-names">
        {#each bulk.results as r (r.id)}
          <li class="h-row">
            <Badge tone={r.ok ? 'success' : 'danger'}>{r.ok ? t('collections.bulk.ok') : t('collections.bulk.failed')}</Badge>
            <span>{r.name}</span>
            {#if r.message}<span class="h-muted">{r.message}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#snippet footer()}
      {#if bulk?.phase === 'confirm'}
        <Button variant="ghost" onclick={() => (bulkOpen = false)}>{t('common.cancel')}</Button>
        <Button variant={bulk.kind === 'delete' ? 'danger' : 'primary'} onclick={runBulk}>{bulk.kind === 'delete' ? t('common.delete') : t('collections.bulk.publish')}</Button>
      {:else if bulk?.phase === 'done'}
        <Button id="h-bulk-close" variant="primary" onclick={() => (bulkOpen = false)}>{t('common.close')}</Button>
      {/if}
    {/snippet}
  </Dialog>
{/if}

<ViewsDialog bind:open={viewsOpen} type={typeName} onapply={(qs) => router.setQuery(toUrlPatch(parseQuery(new URLSearchParams(qs)), router.query))} current={router.query.toString()} />

<style>
  .h-list-bulk {
    padding: var(--h-space-2) var(--h-space-3);
    border-radius: var(--h-radius);
    background: var(--h-accent-soft);
  }
  .h-list-name {
    font-weight: 500;
  }
  .h-list-key {
    margin-left: var(--h-space-2);
    font-size: var(--h-text-xs);
  }
  .h-list-names {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-1);
    margin: var(--h-space-2) 0 0;
    padding: 0;
    list-style: none;
    font-size: var(--h-text-sm);
  }
</style>
