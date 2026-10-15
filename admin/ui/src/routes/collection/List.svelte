<script lang="ts">
  import '../../lib/messages/collections';
  import { tick } from 'svelte';
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { errorMessage } from '../../lib/errors';
  import { cellText, runPool } from '../../lib/fields/logic';
  import { knownLocales } from '../../lib/fields/locales';
    import { t } from '../../lib/i18n';
  import { link, href, router } from '../../lib/router.svelte';
  import { types, type CompiledField } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import BulkBar from '../../lib/ui/BulkBar.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Menu from '../../lib/ui/Menu.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Popover from '../../lib/ui/Popover.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import type { Column } from '../../lib/ui/table';
  import { ext } from '../../lib/extensions.svelte';
  import Extension from '../../lib/ext/Extension.svelte';
  import Slot from '../../lib/ext/Slot.svelte';
  import LocaleDots from './LocaleDots.svelte';
  import StatusBadge from './StatusBadge.svelte';
  import FilterBar from './FilterBar.svelte';
  import ViewNameDialog from './ViewNameDialog.svelte';
  import { canonicalQuery, emptyQuery, nextSort, parseQuery, parseViews, renameView, sortState, toApiQuery, toUrlPatch, upsertView, type ListQuery, type SavedView } from './query';

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
  // `admin.list` entries are `field` or `field:cell` (a cell element of the extension manifest).
  const MAX_COLS = 10;
  const listed = $derived((type?.admin?.list ?? []).map((n) => n.split(':') as [string, string?]).filter(([n]) => n !== 'name'));
  const defaultCols = $derived(listed.map(([n]) => n).filter((n) => type?.fields.some((f) => f.name === n)));
  const colsKey = $derived(`h.cols.${session.principalKey}.${typeName}`);
  let chosen = $state<string[] | null>(null);
  $effect(() => {
    try {
      const v = JSON.parse(localStorage.getItem(colsKey) ?? 'null');
      chosen = Array.isArray(v) && v.every((x) => typeof x === 'string') ? v : null;
    } catch {
      chosen = null;
    }
  });
  const colNames = $derived(chosen ?? defaultCols);
  const fieldCols = $derived(colNames.slice(0, MAX_COLS).map((n) => type?.fields.find((f) => f.name === n)).filter((f): f is CompiledField => !!f));
  const cellTags = $derived(new Map(listed.filter(([, c]) => c).map(([n, c]) => [n, ext.cell(c!)])));
  // Offered in the Columns menu: the admin.list fields first, then the other top-level fields that fit a cell.
  const candidates = $derived.by(() => {
    const flat = ['string', 'number', 'boolean', 'ref', 'list'];
    const all = type?.fields ?? [];
    const first = defaultCols.map((n) => all.find((f) => f.name === n)!);
    return [...first, ...all.filter((f) => !defaultCols.includes(f.name) && f.name !== 'name' && flat.includes(f.shape.base))];
  });
  function setCols(next: string[] | null) {
    chosen = next;
    if (next) localStorage.setItem(colsKey, JSON.stringify(next));
    else localStorage.removeItem(colsKey);
  }
  function toggleCol(name: string, on: boolean) {
    const cur = colNames.filter((n) => candidates.some((f) => f.name === n));
    if (on && cur.length >= MAX_COLS) return;
    setCols(on ? [...cur, name] : cur.filter((n) => n !== name));
  }

  // ---- URL state
  function update(patch: Partial<ListQuery>, opts: { push?: boolean } = {}) {
    const next: ListQuery = { ...q, page: 1, ...patch };
    router.setQuery(toUrlPatch(next, router.query), opts);
  }

  // ---- table
  const rowsView = $derived(
    rows.map((r) => {
      const row: Row = { ...r };
      const d = (r as { fields?: Record<string, unknown> }).fields ?? {};
      for (const f of fieldCols) {
        row[`f:${f.name}`] = f.shape.base === 'boolean' ? (d[f.name] === true ? t('common.yes') : '') : cellText(f, d[f.name]);
      }
      return row;
    }),
  );
  const columns = $derived<Column<Row>[]>([
    { key: 'name', label: t('collections.col.name'), sortable: true, cell: nameCell },
    ...fieldCols.map((f) => ({ key: `f:${f.name}`, label: f.label, sortable: !!f.index, mobile: 'hide', cell: cellTags.get(f.name) ? extCell : undefined }) as Column<Row>),
    { key: 'status', label: t('collections.col.status'), cell: statusCell },
    { key: 'locales', label: t('collections.col.languages'), mobile: 'hide', cell: localesCell },
    { key: 'updated', label: t('collections.col.updated'), sortable: true, cell: updatedCell },
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

  // ---- views (saved queries, per user and type, in localStorage; the sidebar lists them too)
  const viewsKey = $derived(`h.views.${session.principalKey}.${typeName}`);
  let views = $state<SavedView[]>([]);
  $effect(() => {
    const key = viewsKey;
    const read = () => (views = parseViews(localStorage.getItem(key)));
    read();
    addEventListener('h:views', read);
    return () => removeEventListener('h:views', read);
  });
  function persistViews(next: SavedView[]) {
    views = next;
    localStorage.setItem(viewsKey, JSON.stringify(next));
    dispatchEvent(new Event('h:views'));
  }
  const activeView = $derived(views.find((v) => canonicalQuery(v.query) === canonicalQuery(router.query))?.name);
  const tabValue = $derived(activeView === undefined ? 'all' : `v:${activeView}`);
  const viewTabs = $derived([
    { id: 'all', label: t('collections.viewAll') },
    ...views.map((v) => ({ id: `v:${v.name}`, label: v.name, icon: 'star' })),
  ]);
  function applyQuery(qs: string) {
    router.setQuery(toUrlPatch(parseQuery(new URLSearchParams(qs)), router.query));
  }
  function pickView(id: string) {
    if (id === 'all') router.setQuery(toUrlPatch({ ...emptyQuery(), locale: q.locale }, router.query));
    else {
      const v = views.find((x) => `v:${x.name}` === id);
      if (v) applyQuery(v.query);
    }
  }
  let saveOpen = $state(false);
  let renameOpen = $state(false);
  function saveView(name: string) {
    const qs = new URLSearchParams(router.query);
    qs.delete('page');
    persistViews(upsertView(views, { name, query: qs.toString() }));
  }
  const viewMenu = $derived([
    { label: t('collections.renameView'), icon: 'edit', onselect: () => (renameOpen = true) },
    { label: t('collections.deleteView'), icon: 'trash', danger: true, onselect: () => persistViews(views.filter((v) => v.name !== activeView)) },
  ]);

  // ---- page size, layout
  const SIZES = [25, 50, 100];
  const sizeOptions = $derived((SIZES.includes(q.pageSize) ? SIZES : [...SIZES, q.pageSize].sort((a, b) => a - b)).map((n) => ({ value: String(n), label: String(n) })));
  let narrow = $state(false);
  $effect(() => {
    const m = matchMedia('(max-width: 767px)');
    const set = () => (narrow = m.matches);
    set();
    m.addEventListener('change', set);
    return () => m.removeEventListener('change', set);
  });
  const filtered = $derived(!!(q.q || q.filters.length || q.status));
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
    <Slot at="list.before" props={{ type: typeName }} />
    <PageHeader title={type.label} breadcrumbs={[{ label: t('nav.content') }, { label: type.label }]} meta={type.description}>
      {#snippet badge()}{#if !error && (!loading || total > 0)}<Badge>{total}</Badge>{/if}{/snippet}
      {#snippet actions()}
        {#if candidates.length}
          <Popover label={t('collections.columns')} triggerClass="h-pill" align="end">
            {#snippet trigger()}<Icon name="sliders" size={16} />{t('collections.columns')}{/snippet}
            {#snippet children()}
              <div class="h-cols">
                <p class="h-muted h-cols__help">{t('collections.columnsHelp', { max: MAX_COLS })}</p>
                {#each candidates as f (f.name)}
                  {@const on = colNames.includes(f.name)}
                  <Checkbox checked={on} label={f.label} disabled={!on && colNames.length >= MAX_COLS} onchange={(e) => toggleCol(f.name, e.currentTarget.checked)} />
                {/each}
                {#if chosen}<Button variant="ghost" size="sm" onclick={() => setCols(null)}>{t('collections.columnsReset')}</Button>{/if}
              </div>
            {/snippet}
          </Popover>
        {/if}
      {/snippet}
      {#snippet primary()}{#if canCreate}<Button variant="primary" icon="plus" onclick={create}>{t('collections.newType', { type: type.label })}</Button>{/if}{/snippet}
    </PageHeader>

    <div class="h-list-bar">
      <Tabs variant="underline" tabs={viewTabs} value={tabValue} label={t('collections.views')} onchange={pickView}>
        {#snippet trailing()}
          {#if activeView !== undefined}<Menu items={viewMenu} label={t('collections.viewMenu', { name: activeView })} />{/if}
          <Button variant="ghost" size="sm" icon="plus" onclick={() => (saveOpen = true)}>{t('collections.saveView')}</Button>
        {/snippet}
      </Tabs>
      <FilterBar {type} {q} {offered} {search} {onsearch} onchange={(patch) => update(patch)} />
    </div>

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
        maxHeight={narrow ? undefined : 'max(22rem, calc(100dvh - 22rem))'}
        onrowclick={(r) => void router.navigate(`/items/${r.id}${q.locale ? `?locale=${q.locale}` : ''}`)}
      >
        {#snippet empty()}
          {#if filtered}
            <EmptyState icon="filter" title={t('collections.noMatch')} description={t('collections.emptyFiltered')}>
              {#snippet actions()}<Button onclick={() => update({ q: '', status: '', filters: [] })}>{t('collections.clearFilters')}</Button>{/snippet}
            </EmptyState>
          {:else}
            <EmptyState icon="file" title={t('collections.emptyTitle', { type: type.label.toLowerCase() })} description={t('collections.emptyText')}>
              {#snippet actions()}{#if canCreate}<Button variant="primary" icon="plus" onclick={create}>{t('collections.newType', { type: type.label })}</Button>{/if}{/snippet}
            </EmptyState>
          {/if}
        {/snippet}
        {#snippet footer()}
          {#if total > 0}
            <span class="h-rows"><span class="h-muted" aria-hidden="true">{t('collections.rows')}</span><Select compact aria-label={t('collections.pageSize')} value={String(q.pageSize)} options={sizeOptions} onchange={(e) => update({ pageSize: Number((e.currentTarget as HTMLSelectElement).value) })} /></span>
            <Pagination page={q.page} pageSize={q.pageSize} {total} onpage={(p) => update({ page: p }, { push: true })} />
          {/if}
        {/snippet}
      </Table>
    {/if}
    <BulkBar count={canPublish || canDelete ? selected.size : 0} onclear={() => (selected = new Set())}>
      {#if canPublish}<Button size="sm" variant="ghost" onclick={() => startBulk('publish')}>{t('collections.bulk.publish')}</Button>{/if}
      {#if canDelete}<Button size="sm" variant="ghost" onclick={() => startBulk('delete')}>{t('common.delete')}</Button>{/if}
    </BulkBar>
    <Slot at="list.after" props={{ type: typeName }} />
  {/if}
</div>

{#snippet nameCell(row: Row)}
  <NameCell name={row.name || t('collections.untitled')} sub={row.key} icon={type?.admin?.icon || 'file-text'} href={`/items/${row.id}${q.locale ? `?locale=${q.locale}` : ''}`} />
{/snippet}
{#snippet extCell(row: Row, col: Column<Row>)}
  {@const f = fieldCols.find((x) => `f:${x.name}` === col.key)}
  {@const tag = f && cellTags.get(f.name)}
  {#if f && tag}
    <Extension {tag} props={{ value: (row.fields as Record<string, unknown> | undefined)?.[f.name], item: row, field: f }} />
  {/if}
{/snippet}
{#snippet statusCell(row: Row)}<StatusBadge status={row.status} />{/snippet}
{#snippet updatedCell(row: Row)}<Time at={row.updatedAt} />{/snippet}
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

<ViewNameDialog bind:open={saveOpen} title={t('collections.saveViewTitle')} onsubmit={saveView} />
<ViewNameDialog
  bind:open={renameOpen}
  title={t('collections.renameViewTitle')}
  initial={activeView ?? ''}
  onsubmit={(n) => {
    const from = activeView;
    if (from !== undefined) persistViews(renameView(views, from, n));
  }}
/>

<style>
  .h-list-bar {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
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
  .h-cols {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    max-height: 20rem;
    overflow-y: auto;
  }
  .h-cols__help {
    margin: 0;
    font-size: var(--h-text-xs);
  }
  .h-rows {
    display: inline-flex;
    align-items: center;
    gap: var(--h-space-2);
    font-size: var(--h-text-sm);
  }
</style>
