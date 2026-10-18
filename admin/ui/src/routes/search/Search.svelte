<script lang="ts">
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Snippet from '../../lib/ui/Snippet.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import { api } from '../../lib/api/client';
  import { types } from '../../lib/types.svelte';
  import { router, link, href } from '../../lib/router.svelte';
  import { t } from '../../lib/i18n';

  type Result = { items: { id: string; name: string; type: string }[]; snippets: Record<string, string> };

  const q = $derived((router.query.get('q') ?? '').trim());
  let result = $state<Result | null>(null);
  let busy = $state(false);
  let failed = $state(false);

  $effect(() => {
    const text = q;
    result = null;
    failed = false;
    if (!text) {
      busy = false;
      return;
    }
    busy = true;
    const c = new AbortController();
    api
      .get('/search', { query: { q: text.slice(0, 200), limit: 50 }, signal: c.signal })
      .then((r) => {
        if (c.signal.aborted) return;
        result = r;
        busy = false;
      })
      .catch(() => {
        if (c.signal.aborted) return;
        failed = true;
        busy = false;
      });
    return () => c.abort();
  });

  const typeLabel = (name: string) => types.get(name)?.label ?? name;
</script>

<div class="h-page">
  <h1>{t('search.title')}</h1>
  {#if !q}
    <EmptyState icon="search" title={t('search.title')} description={t('search.placeholder')} />
  {:else if busy}
    <p class="h-muted" role="status">{t('combobox.searching')}</p>
  {:else if failed}
    <p class="h-muted" role="alert">{t('search.failed')}</p>
  {:else if result && result.items.length === 0}
    <EmptyState icon="search" title={t('search.none', { q })} description={t('search.placeholder')} />
  {:else if result}
    <h2 class="h-search__heading">{t('search.results', { q })}</h2>
    <ul class="h-search">
      {#each result.items as it (it.id)}
        <li class="h-search__item">
          <a class="h-search__name" use:link href={href(`/items/${it.id}`)}>
            <Icon name={types.get(it.type)?.admin?.icon ?? 'file'} size={16} /><span>{it.name || it.id}</span>
          </a>
          <span class="h-muted h-search__type">{typeLabel(it.type)}</span>
          {#if result.snippets[it.id]}<p class="h-search__snippet"><Snippet html={result.snippets[it.id]!} /></p>{/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .h-search__heading {
    font-size: var(--h-text-lg);
  }
  .h-search {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
  }
  .h-search__item {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: var(--h-space-1) var(--h-space-3);
  }
  .h-search__name {
    display: inline-flex;
    align-items: center;
    gap: var(--h-space-2);
    font-weight: 600;
  }
  .h-search__type {
    font-size: var(--h-text-xs);
  }
  .h-search__snippet {
    grid-column: 1 / -1;
    margin: 0;
    color: var(--h-text-muted);
    font-size: var(--h-text-sm);
    overflow-wrap: anywhere;
  }
</style>
