<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../lib/i18n';
  import { types } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import { UNPLACED_PAGE, type UnplacedItem } from './logic';
  import { listUnplaced } from './place';
  import PlaceDialog from './PlaceDialog.svelte';

  // Page items that are in no site's tree, below the tree. Selecting a row opens the item; "Place…" puts it
  // into a tree.
  let {
    site,
    sites,
    locale,
    canEdit,
    selected = $bindable<string | undefined>(undefined),
    onplaced,
  }: {
    site: string;
    sites: string[];
    locale: string;
    canEdit: boolean;
    selected?: string;
    onplaced: (r: { site: string; parent: string; node: string }) => void;
  } = $props();

  const KEY = 'item:unplaced:';
  let items = $state<UnplacedItem[]>([]);
  let total = $state(0);
  let limit = UNPLACED_PAGE;
  let busy = $state(false);
  let error = $state('');
  let target = $state<UnplacedItem | null>(null);
  let open = $state(false);
  let seq = 0;

  async function load(n: number) {
    const mine = ++seq;
    busy = true;
    error = '';
    try {
      const res = await listUnplaced({ locale, limit: n, offset: 0 });
      if (mine !== seq) return;
      items = res.items;
      total = res.total;
      limit = n;
    } catch (e) {
      if (mine === seq) error = (e as Error).message;
    } finally {
      if (mine === seq) busy = false;
    }
  }
  $effect(() => {
    void locale;
    untrack(() => void load(limit));
  });

  const statusLabel = (s: string) => t('pages.status.' + s);
  const dotOf = (s?: string) => (s === 'published' ? 'filled' : s === 'changed' ? 'half' : 'hollow');
</script>

{#if total > 0 || error}
  <section class="h-unplaced" aria-label={t('pages.unplacedTitle', { n: total })}>
    <h2 class="h-unplaced__title">{t('pages.unplacedTitle', { n: total })}</h2>
    <p class="h-muted h-unplaced__hint">{t('pages.unplacedHint')}</p>
    {#if error}<p class="h-alert" role="alert">{error} <Button size="sm" onclick={() => void load(limit)}>{t('app.retry')}</Button></p>{/if}
    <ul class="h-plain h-unplaced__list">
      {#each items as it (it.id)}
        <li class="h-unplaced__row" class:h-unplaced__row--sel={selected === KEY + it.id}>
          <button type="button" class="h-unplaced__open" onclick={() => (selected = KEY + it.id)} aria-current={selected === KEY + it.id ? 'true' : undefined}>
            <span class="h-unplaced__name">{it.name || '…'}</span>
            <span class="h-muted h-unplaced__type">{types.get(it.type)?.label ?? it.type}</span>
            <span class="h-dot h-dot--{dotOf(it.status)}" role="img" aria-label={statusLabel(it.status)} title={statusLabel(it.status)}></span>
          </button>
          {#if canEdit}
            <Button
              size="sm"
              onclick={() => {
                target = it;
                open = true;
              }}>{t('pages.placeAction')}</Button
            >
          {/if}
        </li>
      {/each}
    </ul>
    {#if total > items.length}
      <Button size="sm" variant="ghost" loading={busy} onclick={() => void load(limit + UNPLACED_PAGE)}>
        {t('pages.showMore', { n: Math.min(UNPLACED_PAGE, total - items.length), left: total - items.length })}
      </Button>
    {/if}
  </section>
{/if}
{#if target}
  {#key target.id}
    <PlaceDialog
      bind:open
      item={target}
      {sites}
      {site}
      {locale}
      ondone={(r) => {
        if (selected === KEY + (target?.id ?? '')) selected = undefined;
        void load(limit);
        onplaced(r);
      }}
    />
  {/key}
{/if}

<style>
  .h-unplaced {
    margin-top: var(--h-space-4);
    padding-top: var(--h-space-3);
    border-top: 1px solid var(--h-border);
  }
  .h-unplaced__title {
    font-size: var(--h-text-sm);
    font-weight: 650;
    padding: 0 var(--h-space-2);
  }
  .h-unplaced__hint {
    padding: 0 var(--h-space-2);
    font-size: var(--h-text-sm);
  }
  .h-unplaced__list {
    margin: var(--h-space-2) 0;
  }
  .h-unplaced__row {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
    padding: 0 var(--h-space-2);
    border-radius: var(--h-radius);
  }
  .h-unplaced__row--sel {
    background: var(--h-surface-2);
  }
  .h-unplaced__open {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
    padding: var(--h-space-2) 0;
    text-align: start;
    background: none;
    border: 0;
    color: inherit;
    cursor: pointer;
  }
  .h-unplaced__name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .h-unplaced__type {
    margin-inline-start: auto;
    font-size: var(--h-text-sm);
  }
</style>
