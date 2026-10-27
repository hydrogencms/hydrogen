<script lang="ts">
  import '../../lib/messages/history';
  import { api } from '../../lib/api/client';
  import { blocksCatalog } from '../../lib/blocks/catalog.svelte';
  import { diffDocuments, refIds, type VersionDocs } from '../../lib/diff/diff';
  import DiffView from '../../lib/diff/DiffView.svelte';
  import { errorMessage } from '../../lib/errors';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { types, type CompiledType } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { isNotFound, itemApi, type ItemData } from './model';

  let { params }: { params?: Record<string, string> } = $props();
  const id = $derived(params?.id ?? '');

  interface Meta {
    version: number;
    createdAt: string;
    createdBy: string;
    locales: string[];
  }
  type Doc = Record<string, unknown>;
  interface VersionView {
    shared?: { fields?: Doc };
    locales?: Record<string, { fields?: Doc }>;
  }

  let item = $state<ItemData | null>(null);
  let type = $state<CompiledType | null>(null);
  let metas = $state<Meta[]>([]);
  let loadError = $state('');
  let ready = $state(false);

  $effect(() => {
    const itemId = id;
    const ctl = new AbortController();
    ready = false;
    loadError = '';
    (async () => {
      try {
        await types.load();
        const [it, list] = await Promise.all([itemApi.read({ kind: 'item', id: itemId }, '', ctl.signal), api.get('/items/{id}/versions', { params: { id: itemId }, signal: ctl.signal })]);
        if (ctl.signal.aborted) return;
        const ty = await types.detail(it.type);
        if (ty.fields.some((f) => f.shape.base === 'blocks')) await blocksCatalog.load().catch(() => {});
        if (ctl.signal.aborted) return;
        item = it;
        type = ty;
        metas = (list.items as Meta[]).slice().sort((x, y) => y.version - x.version);
        ready = true;
      } catch (e) {
        if (!ctl.signal.aborted) loadError = isNotFound(e) ? t('history.compare.notFound') : errorMessage(e);
      }
    })();
    return () => ctl.abort();
  });

  // ---- what is compared
  const q = $derived(router.query);
  const latest = $derived(metas[0]?.version);
  const pick = (v: string | null, fallback: string) => {
    if (v === 'draft') return 'draft';
    const n = Number(v);
    return v && metas.some((m) => m.version === n) ? String(n) : fallback;
  };
  const sideB = $derived(pick(q.get('b'), 'draft'));
  const sideA = $derived(pick(q.get('a'), latest !== undefined ? String(latest) : 'draft'));
  const localeOptions = $derived.by(() => {
    const set = new Set<string>(item?.locales ?? []);
    for (const m of metas) if ([sideA, sideB].includes(String(m.version))) for (const l of m.locales) set.add(l);
    if (item?.mainLocale) set.add(item.mainLocale);
    return [...set].sort((a, b) => (a === item?.mainLocale ? -1 : b === item?.mainLocale ? 1 : a.localeCompare(b)));
  });
  const locale = $derived(q.get('locale') && localeOptions.includes(q.get('locale')!) ? q.get('locale')! : (localeOptions[0] ?? ''));

  const sideOptions = $derived([
    { value: 'draft', label: t('history.compare.draft') },
    ...metas.map((m) => ({ value: String(m.version), label: `${t('history.compare.version', { n: m.version })} · ${formatDateTime(m.createdAt)}` })),
  ]);
  const set = (patch: Record<string, string>) => router.setQuery({ a: sideA, b: sideB, locale, ...patch });

  // ---- documents
  // Plain maps, not state: effects fill them while reading them.
  const views = new Map<string, Promise<VersionView>>();
  const versionDocs = (n: number): Promise<VersionDocs> => {
    const key = `${id}\u0000${n}`;
    if (!views.has(key)) {
      const p = api.get('/items/{id}/versions/{n}', { params: { id, n } }) as Promise<VersionView>;
      views.set(key, p);
      p.catch(() => views.delete(key));
    }
    return views.get(key)!.then((v) => ({
      shared: v.shared?.fields ?? {},
      locales: Object.fromEntries(Object.entries(v.locales ?? {}).map(([l, d]) => [l, d.fields ?? {}])),
    }));
  };
  const draftDocs = async (l: string, signal: AbortSignal): Promise<VersionDocs> => {
    const d = await itemApi.read({ kind: 'item', id }, l, signal);
    return { shared: d.fields, locales: d.locales.includes(l) ? { [l]: d.fields } : {} };
  };
  const docsOf = (side: string, l: string, signal: AbortSignal) => (side === 'draft' ? draftDocs(l, signal) : versionDocs(Number(side)));

  let docs = $state<{ a: VersionDocs; b: VersionDocs } | null>(null);
  let docsError = $state('');
  // Names of referenced items per language, fetched once each.
  const nameCache = new Map<string, string>();
  let names = $state.raw<Record<string, string>>({});

  $effect(() => {
    if (!ready || !type || !locale) return;
    const [a, b, l, ty] = [sideA, sideB, locale, type];
    const ctl = new AbortController();
    docs = null;
    docsError = '';
    (async () => {
      try {
        const [da, db] = await Promise.all([docsOf(a, l, ctl.signal), docsOf(b, l, ctl.signal)]);
        if (ctl.signal.aborted) return;
        // Names for referenced items, fetched once each (the draft view, in the compared language).
        const o = { block: (n: string) => blocksCatalog.get(n) };
        const cacheKey = (r: string) => `${id}\u0000${l}\u0000${r}`;
        const all = refIds(ty.fields, [da.shared, ...Object.values(da.locales), db.shared, ...Object.values(db.locales)], o);
        const want = all.filter((r) => !nameCache.has(cacheKey(r))).slice(0, 80);
        const got = await Promise.all(
          want.map((r) =>
            api
              .get('/items/{id}', { params: { id: r }, query: { draft: true, locale: l } as never, signal: ctl.signal })
              .then((v) => [r, v.name || r] as const)
              .catch(() => [r, ''] as const),
          ),
        );
        if (ctl.signal.aborted) return;
        for (const [r, n] of got) nameCache.set(cacheKey(r), n);
        names = Object.fromEntries(all.map((r) => [r, nameCache.get(cacheKey(r)) ?? '']));
        docs = { a: da, b: db };
      } catch (e) {
        if (!ctl.signal.aborted) docsError = errorMessage(e);
      }
    })();
    return () => ctl.abort();
  });

  const result = $derived(docs && type ? diffDocuments(type.fields, docs.a, docs.b, locale, { names, block: (n) => blocksCatalog.get(n) }) : null);
  const title = $derived(item?.name || t('editor.untitled'));
  const crumbs = $derived([
    { label: t('nav.content') },
    { label: type?.label ?? '', href: type ? `/collections/${type.name}` : undefined },
    { label: title, href: `/items/${id}` },
    { label: t('history.compare.title') },
  ]);
</script>

<div class="h-page">
  <PageHeader title={t('history.compare.title')} breadcrumbs={crumbs} meta={title} />

  {#if loadError}
    <p class="h-alert" role="alert">{loadError}</p>
  {:else if !ready}
    <Skeleton lines={4} />
  {:else}
    <div class="h-cmp__bar" role="group" aria-label={t('history.compare.title')}>
      <Field label={t('history.compare.a')}>
        {#snippet children(c)}<Select {...c} value={sideA} options={sideOptions} onchange={(v) => set({ a: v })} />{/snippet}
      </Field>
      <Field label={t('history.compare.b')}>
        {#snippet children(c)}<Select {...c} value={sideB} options={sideOptions} onchange={(v) => set({ b: v })} />{/snippet}
      </Field>
      <Field label={t('history.compare.locale')}>
        {#snippet children(c)}<Select {...c} value={locale} options={localeOptions.map((l) => ({ value: l, label: l }))} onchange={(v) => set({ locale: v })} />{/snippet}
      </Field>
      <Button variant="ghost" icon="refresh" onclick={() => set({ a: sideB, b: sideA })}>{t('history.compare.swap')}</Button>
    </div>

    <p class="h-cmp__sum" aria-live="polite">
      {#if result && sideA !== sideB}
        {#if result.fields.length === 0}
          <span class="h-muted">{t('history.compare.identical', { locale })}</span>
        {:else}
          <Badge tone="warning">{result.fields.length === 1 ? t('history.compare.summaryOne') : t('history.compare.summary', { n: result.fields.length })}</Badge>
        {/if}
      {/if}
    </p>

    {#if sideA === sideB}
      <p class="h-muted">{t('history.compare.pickTwo')}</p>
    {:else if docsError}
      <p class="h-alert" role="alert">{docsError}</p>
    {:else if !result}
      <Skeleton lines={5} />
    {:else}
      <div class="h-cmp__out">
        {#if result.missing.a}<p class="h-alert" role="status">{t('history.compare.missingA', { locale })}</p>{/if}
        {#if result.missing.b}<p class="h-alert" role="status">{t('history.compare.missingB', { locale })}</p>{/if}
        {#if result.fields.length > 0}
          <div class="h-cmp__fields">
            {#each result.fields as f (f.name)}
              <section class="h-cmp__field" aria-label={f.label}>
                <header class="h-cmp__head">
                  <h2 class="h-cmp__label">{f.label}</h2>
                  <Badge tone={f.status === 'added' ? 'success' : f.status === 'removed' ? 'danger' : 'warning'}>
                    <span aria-hidden="true">{f.status === 'added' ? '+' : f.status === 'removed' ? '−' : '~'}</span>
                    {t(`diff.${f.status}`)}
                  </Badge>
                </header>
                <DiffView change={f.change} />
              </section>
            {/each}
          </div>
        {/if}
        {#if result.same.length}
          <details class="h-cmp__same">
            <summary>{t('history.compare.unchanged', { n: result.same.length })}</summary>
            <p class="h-muted">{result.same.map((s) => s.label).join(', ')}</p>
          </details>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  .h-cmp__bar {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--h-space-3);
  }
  .h-cmp__bar :global(.h-field) {
    min-width: 14rem;
  }
  .h-cmp__out,
  .h-cmp__fields {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
  }
  .h-cmp__sum {
    margin: 0;
  }
  .h-cmp__field {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    padding: var(--h-space-4);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-lg);
    background: var(--h-surface);
    min-width: 0;
  }
  .h-cmp__head {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-cmp__label {
    margin: 0;
    font-size: var(--h-text-md, 1rem);
    font-weight: 600;
  }
  .h-cmp__same summary {
    cursor: pointer;
    font-size: var(--h-text-sm);
  }
</style>
