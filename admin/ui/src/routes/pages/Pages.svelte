<script lang="ts">
  import '../../lib/messages/pages';
  import { onMount } from 'svelte';
  import { api, ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Menu, { type MenuItem } from '../../lib/ui/Menu.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Tree from '../../lib/ui/Tree.svelte';
  import type { DropPosition, TreeNode } from '../../lib/ui/tree';
  import ItemRaw from '../item/Item.svelte';
  import AddDialog from './AddDialog.svelte';
  import LinkForm from './LinkForm.svelte';
  import {
    addableTypes, canHold, emptyFilter, filterActive, matchesFilter, placement, visibleIds, type PNode, type Placement,
  } from './logic';
  import { copyText, errorMessage } from '../../lib/errors';
  import MountList from './MountList.svelte';
  import MoveDialog from './MoveDialog.svelte';
  import { MOUNT_PAGE, SiteTree, type RowData } from './siteTree.svelte';

  let { params }: { params: Record<string, string> } = $props();

  const ItemEditor = ItemRaw;

  // svelte-ignore state_referenced_locally
  const site = params.site;
  const user = session.principalKey;
  const key = (what: string) => `h.pages.${what}.${user}.${site}`;
  const canEdit = $derived(session.can('pages.edit'));
  const canMount = $derived(session.can('pages.mount'));
  const optionsOf = (type: string) => types.get(type)?.options;

  const fresh = localStorage.getItem(key('expanded')) === null;
  let sites = $state<string[]>([]);
  let locale = $state('');
  let store = $state<SiteTree>();
  let ready = $state(false);
  let error = $state('');
  let expanded = $state(new Set<string>(readList(key('expanded'))));
  let fexp = $state(new Set<string>());
  let selected = $state<string | undefined>(router.query.get('node') ?? undefined);
  let extra = $state<PNode>();
  let filter = $state(emptyFilter());
  let filterBusy = $state(false);

  function readList(k: string): string[] {
    try {
      const v = JSON.parse(localStorage.getItem(k) ?? '[]');
      return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : [];
    } catch {
      return [];
    }
  }
  $effect(() => {
    localStorage.setItem(key('expanded'), JSON.stringify([...expanded].filter((i) => !i.includes(':'))));
  });

  const languages = $derived(
    channels.active?.languages ?? [...new Set(channels.items.flatMap((c) => c.languages))],
  );
  $effect(() => {
    if (ready && languages.length && !languages.includes(locale)) {
      locale = languages[0];
      void start();
    }
  });

  async function boot() {
    error = '';
    try {
      await Promise.all([types.load(), channels.load()]);
      sites = (await api.get('/pages/sites')).items;
      locale = languages[0] ?? '';
      await start();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function start() {
    store?.invalidate();
    const s = new SiteTree(site, locale);
    store = s;
    ready = false;
    try {
      await s.restore(expanded);
    } catch (e) {
      if (store === s) error = (e as Error).message;
      return;
    }
    if (store !== s) return;
    const root = s.kids.get('')?.[0];
    if (fresh && root && !expanded.has(root)) expanded = new Set(expanded).add(root);
    ready = true;
  }
  onMount(() => void boot());

  // Selection lives in the URL (?node=) so a reload or a shared link returns to the same node.
  $effect(() => {
    const id = selected && !selected.includes(':') ? selected : undefined;
    if (id !== (router.query.get('node') ?? undefined)) router.setQuery({ node: id });
  });
  $effect(() => {
    const id = selected;
    if (!store?.rootsLoaded || !id || id.includes(':') || store.byId.has(id)) {
      extra = undefined;
      return;
    }
    api.get('/pages/nodes/{id}', { params: { id } }).then((n) => (extra = { ...(n as unknown as PNode), name: '', children: 0 })).catch(() => (extra = undefined));
  });

  const sel = $derived.by(() => {
    if (!selected || !store) return undefined;
    if (selected.startsWith('item:')) return { item: selected.split(':')[2] };
    if (selected.startsWith('more:')) return undefined;
    const n = store.byId.get(selected) ?? extra;
    return n ? { node: n } : undefined;
  });

  // Filter: loads the whole tree once, then keeps matches with their ancestors.
  const filtering = $derived(filterActive(filter));
  $effect(() => {
    const s = store;
    if (!filtering || !s || !ready || s.full) return;
    const timer = setTimeout(async () => {
      filterBusy = true;
      try {
        await s.loadAll();
      } finally {
        filterBusy = false;
      }
    }, 200);
    return () => clearTimeout(timer);
  });
  const result = $derived(
    filtering && store?.full
      ? visibleIds(store.byId.values(), (n) => matchesFilter(n, filter, store!.locales.get(n.item ?? ''), languages), store.parentOf)
      : null,
  );
  const pruned = $derived(result && store ? store.pruned(result.visible) : []);
  $effect(() => {
    if (result) fexp = new Set(result.visible);
  });

  const dataOf = (n: TreeNode) => n.data as RowData;

  // The store owns the data and hands the Tree children through `nodes`; the returned list is only a fallback.
  async function loadKids(tn: TreeNode): Promise<TreeNode[]> {
    const d = dataOf(tn);
    if (d.row !== 'node') return [];
    if (d.n.kind === 'page') await store!.loadChildren(tn.id);
    else if (d.n.kind === 'mount') await store!.loadMount(tn.id, MOUNT_PAGE);
    return [];
  }

  async function lastChild(parent: string): Promise<string | undefined> {
    const res = await api.get('/pages/sites/{site}/tree', { params: { site }, query: { parent, depth: 1 } });
    const list = (res as unknown as { items: PNode[] }).items;
    return list[list.length - 1]?.id;
  }

  async function changed(parents: string[], select?: string) {
    if (!store) return;
    for (const p of new Set(parents)) await store.refresh(p).catch((e: Error) => toast.error(e.message));
    const e = new Set(expanded);
    for (const p of parents) if (p) e.add(p);
    expanded = e;
    if (select) selected = select;
  }

  function fail(e: unknown) {
    if (e instanceof ApiError && e.status < 500) toast.error(errorMessage(e));
  }

  async function toggleHidden(n: PNode) {
    try {
      await api.patch('/pages/nodes/{id}', { params: { id: n.id }, body: { hidden: !n.hidden } });
      await changed([n.parent ?? '']);
    } catch (e) {
      fail(e);
    }
  }

  async function copyUrl(n: PNode) {
    try {
      let url = n.target ?? n.path;
      if (n.kind === 'page' && n.item) {
        const rows = ((await api.get('/pages/items/{id}/paths', { params: { id: n.item } })) as unknown as { items: { locale: string; path: string; state: string }[] }).items;
        url = rows.find((r) => r.state === 'current' && r.locale === locale)?.path ?? rows.find((r) => r.state === 'current')?.path ?? url;
      }
      if (!url) {
        toast.info(t('pages.noUrl'));
        return;
      }
      if (!(await copyText(url))) throw new Error('copy');
      toast.success(t('pages.urlCopied', { url }));
    } catch {
      toast.error(t('pages.copyFailed'));
    }
  }

  // Dialogs
  let addDlg = $state<{ mode: 'child' | 'mount' | 'link' | 'mount-edit'; parent: PNode; node?: PNode } | null>(null);
  let addOpen = $state(false);
  let moveDlg = $state<{ node: PNode; initial?: Placement } | null>(null);
  let moveOpen = $state(false);
  let delNode = $state<PNode | null>(null);
  let delOpen = $state(false);
  let deleting = $state(false);

  const openAdd = (mode: 'child' | 'mount' | 'link' | 'mount-edit', parent: PNode, node?: PNode) => {
    addDlg = { mode, parent, node };
    addOpen = true;
  };
  const openMove = (node: PNode, initial?: Placement) => {
    moveDlg = { node, initial };
    moveOpen = true;
  };
  async function doDelete() {
    if (!delNode) return;
    deleting = true;
    try {
      await api.del('/pages/nodes/{id}', { params: { id: delNode.id } });
      const n = delNode;
      delOpen = false;
      if (selected === n.id) selected = undefined;
      toast.success(t('pages.deleted'));
      await changed([n.parent ?? '']);
    } catch (e) {
      fail(e);
    } finally {
      deleting = false;
    }
  }

  const childTypes = (n: PNode) => addableTypes(n, types.pageTypes.map((p) => p.name), optionsOf);

  function menuFor(n: PNode): MenuItem[] {
    if (n.restricted) return [];
    const items: MenuItem[] = [];
    if (n.kind === 'page' && n.item) items.push({ label: t('pages.open'), icon: 'edit', href: '/items/' + n.item });
    if (n.kind === 'mount') items.push({ label: t('pages.editMount'), icon: 'edit', disabled: !canMount, onselect: () => openAdd('mount-edit', store!.byId.get(n.parent ?? '') ?? n, n) });
    items.push({ label: t('pages.copyUrl'), icon: 'copy', onselect: () => void copyUrl(n) });
    if (canEdit) {
      if (n.kind === 'page') {
        items.push(
          { label: t('pages.addChild'), icon: 'plus', separator: true, disabled: !childTypes(n).length, onselect: () => openAdd('child', n) },
          ...(canMount ? [{ label: t('pages.addMount'), icon: 'layers', onselect: () => openAdd('mount', n) }] : []),
          { label: t('pages.addLink'), icon: 'link', onselect: () => openAdd('link', n) },
        );
      }
      items.push({ label: n.hidden ? t('pages.unhide') : t('pages.hide'), icon: 'eye', separator: n.kind !== 'page', onselect: () => void toggleHidden(n) });
      if (n.parent) items.push({ label: t('tree.move'), icon: 'grip', onselect: () => openMove(n) });
      if (n.kind !== 'page') items.push({ label: t('common.delete'), icon: 'trash', danger: true, separator: true, onselect: () => { delNode = n; delOpen = true; } });
    }
    return items;
  }

  // Drag and move
  function canDrop(node: TreeNode, target: TreeNode, position: DropPosition): boolean {
    const a = dataOf(node);
    const b = dataOf(target);
    if (!canEdit || !store || a.row !== 'node' || b.row !== 'node' || !a.n.parent || a.n.restricted) return false;
    const parent = position === 'inside' ? b.n : store.byId.get(b.n.parent ?? '');
    return canHold(parent, a.n, optionsOf);
  }

  async function onmove(m: { node: TreeNode; target: TreeNode; position: DropPosition }) {
    const a = dataOf(m.node);
    const b = dataOf(m.target);
    if (!store || a.row !== 'node' || b.row !== 'node') return;
    try {
      if (m.position === 'inside' && b.n.children > 0 && !store.kids.has(b.n.id)) await store.loadChildren(b.n.id);
      const p = placement(m.position, b.n, a.n.id, store.siblings);
      if (!p) return;
      if (p.parent === a.n.parent) {
        await api.patch('/pages/nodes/{id}', { params: { id: a.n.id }, body: { after: p.after } as never });
        await changed([p.parent]);
        return;
      }
      openMove(a.n, p);
    } catch (e) {
      fail(e);
    }
  }
  function moveRequest(tn: TreeNode) {
    const d = dataOf(tn);
    if (canEdit && d.row === 'node' && d.n.parent && !d.n.restricted && !filtering) openMove(d.n);
  }

  async function showMore(mount: string) {
    const m = store?.mounts.get(mount);
    if (m) await store!.loadMount(mount, m.limit + MOUNT_PAGE);
  }

  function activate(tn: TreeNode) {
    const d = dataOf(tn);
    if (d.row === 'more') void showMore(d.mount);
    else if (d.row === 'item') void router.navigate('/items/' + d.item.id);
    else if (d.n.kind === 'page' && d.n.item) void router.navigate('/items/' + d.n.item);
  }

  const statusTone = (s?: string) => (s === 'published' ? 'success' : s === 'changed' ? 'warning' : 'neutral');
  const complete = (ls: string[] | undefined) => (ls ? languages.filter((l) => ls.includes(l)) : []);

  // Split view
  const splitKey = `h.pages.split.${user}`;
  let left = $state(Number(localStorage.getItem(splitKey)) || 400);
  let box = $state<HTMLElement>();
  function resizeTo(x: number) {
    const w = box?.getBoundingClientRect().width ?? 1200;
    left = Math.round(Math.min(Math.max(x, 260), Math.max(w - 320, 260)));
  }
  function dragSplit(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    const move = (ev: PointerEvent) => resizeTo(ev.clientX - (box?.getBoundingClientRect().left ?? 0));
    const stop = () => {
      el.removeEventListener('pointermove', move);
      localStorage.setItem(splitKey, String(left));
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', stop, { once: true });
  }
  function splitKeys(e: KeyboardEvent) {
    const d = e.key === 'ArrowLeft' ? -24 : e.key === 'ArrowRight' ? 24 : 0;
    if (!d) return;
    e.preventDefault();
    resizeTo(left + d);
    localStorage.setItem(splitKey, String(left));
  }

  const matchCount = $derived(result?.matches.size ?? 0);
</script>

<div class="h-page h-pages">
  <div class="h-page__head">
    <h1>{t('pages.title', { site })}</h1>
    <span class="h-spacer"></span>
    {#if sites.length > 1}
      <Select
        aria-label={t('pages.site')}
        value={site}
        options={sites.map((s) => ({ value: s, label: s }))}
        onchange={(e) => void router.navigate('/pages/' + encodeURIComponent((e.currentTarget as HTMLSelectElement).value))}
      />
    {/if}
    {#if languages.length > 1}
      <Select aria-label={t('pages.locale')} bind:value={locale} options={languages.map((l) => ({ value: l, label: l }))} onchange={() => void start()} />
    {/if}
  </div>

  {#if error}
    <p class="h-alert" role="alert">{error} <Button size="sm" onclick={() => void boot()}>{t('app.retry')}</Button></p>
  {/if}

  <div class="h-pages__filter" role="search">
    <Input type="search" aria-label={t('pages.filterTitle')} placeholder={t('pages.filterTitle')} bind:value={filter.q} />
    <Select aria-label={t('pages.type')} bind:value={filter.type} placeholder={t('pages.anyType')} options={types.pageTypes.map((p) => ({ value: p.name, label: p.label }))} />
    <Select
      aria-label={t('pages.status')}
      bind:value={filter.status}
      placeholder={t('pages.anyStatus')}
      options={['draft', 'published', 'changed', 'hidden'].map((s) => ({ value: s, label: t('pages.status.' + s) }))}
    />
    {#if languages.length > 1}<Checkbox bind:checked={filter.gaps} label={t('pages.languageGaps')} />{/if}
    {#if filtering}
      <Button size="sm" variant="ghost" onclick={() => (filter = emptyFilter())}>{t('common.clear')}</Button>
      <span class="h-muted" role="status">{filterBusy || !result ? t('app.loading') : t('pages.matches', { n: matchCount })}</span>
    {/if}
  </div>

  <div class="h-pages__split" bind:this={box} style:--h-pages-left="{left}px">
    <div class="h-pages__tree">
      {#if !store || !ready}
        {#if !error}<Skeleton lines={8} />{/if}
      {:else if filtering}
        {#if result && !pruned.length}
          <p class="h-muted">{t('common.noResults')}</p>
        {:else}
          <Tree nodes={pruned} label={t('pages.treeLabel', { site })} bind:expanded={fexp} bind:selected onactivate={activate} {row} />
        {/if}
      {:else if !store.nodes.length}
        <EmptyState title={t('pages.empty')} icon="page" />
      {:else}
        <Tree
          nodes={store.nodes}
          label={t('pages.treeLabel', { site })}
          bind:expanded
          bind:selected
          loadChildren={loadKids}
          onactivate={activate}
          onmove={canEdit ? onmove : undefined}
          {canDrop}
          onmoverequest={canEdit ? moveRequest : undefined}
          {row}
        />
      {/if}
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions, a11y_no_static_element_interactions -->
    <div
      class="h-pages__sep"
      role="separator"
      aria-orientation="vertical"
      aria-label={t('pages.resize')}
      aria-valuenow={left}
      tabindex="0"
      onpointerdown={dragSplit}
      onkeydown={splitKeys}
    ></div>
    <section class="h-pages__pane" aria-label={t('pages.detail')}>
      {#if !sel}
        <EmptyState title={t('pages.selectNode')} icon="page" />
      {:else if 'item' in sel && sel.item}
        {#key sel.item}<ItemEditor id={sel.item} embedded />{/key}
      {:else if 'node' in sel && sel.node}
        {@const n = sel.node}
        {#key n.id}
          {#if n.restricted}
            <p class="h-muted">{t('pages.restricted')}</p>
          {:else if n.kind === 'page' && n.item}
            <ItemEditor id={n.item} embedded />
          {:else if n.kind === 'mount'}
            <MountList node={n} {locale} {languages} canEdit={canMount} onedit={() => openAdd('mount-edit', store!.byId.get(n.parent ?? '') ?? n, n)} />
          {:else if n.kind === 'link'}
            <LinkForm node={n} {languages} {canEdit} onsaved={() => void changed([n.parent ?? ''])} />
          {/if}
        {/key}
      {/if}
    </section>
  </div>
</div>

{#snippet row(tn: TreeNode)}
  {@const d = dataOf(tn)}
  {#if d.row === 'node'}
    {@const n = d.n}
    <span class="h-pages__row" class:h-pages__row--restricted={n.restricted}>
      <Icon name={n.kind === 'page' ? 'page' : n.kind === 'mount' ? 'layers' : 'link'} size={16} label={t('pages.kind.' + n.kind)} />
      <span class="h-tree__label">{n.name || '…'}</span>
      {#if !n.restricted}
        {#if n.kind === 'page'}
          {#if n.status}<Badge tone={statusTone(n.status)}>{t('pages.status.' + n.status)}</Badge>{/if}
          {#if languages.length > 1 && store?.locales.has(n.item ?? '')}
            {@const have = complete(store.locales.get(n.item ?? ''))}
            <span class="h-pages__dots" role="img" aria-label={t('pages.languagesHave', { list: have.join(', ') || t('common.none') })}>
              {#each languages as l (l)}<span class="h-pages__dot" class:h-pages__dot--on={have.includes(l)} title={l}></span>{/each}
            </span>
          {/if}
        {:else if n.kind === 'mount'}
          <span class="h-muted h-pages__meta">
            {n.mount?.collection} · {t('pages.itemCount', { n: store?.mounts.get(n.id)?.total ?? '…' })}
          </span>
        {:else}
          <span class="h-muted h-pages__meta">{n.target ?? ''}</span>
          {#if n.broken}<Badge tone="danger">{t('pages.broken')}</Badge>{/if}
        {/if}
        {#if n.hidden}<Badge>{t('pages.status.hidden')}</Badge>{/if}
        {#if selected === n.id}
          {@const items = menuFor(n)}
          {#if items.length}
            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions, a11y_no_noninteractive_element_interactions -->
            <span class="h-pages__menu" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} ondblclick={(e) => e.stopPropagation()}>
              <Menu {items} label={t('pages.actionsFor', { name: n.name })} />
            </span>
          {/if}
        {/if}
      {:else}
        <span class="h-muted">{t('pages.restrictedShort')}</span>
      {/if}
    </span>
  {:else if d.row === 'item'}
    <span class="h-pages__row">
      <Icon name="file" size={16} />
      <span class="h-tree__label">{d.item.name}</span>
      <Badge tone={statusTone(d.item.status)}>{t('pages.status.' + d.item.status)}</Badge>
    </span>
  {:else}
    <button type="button" class="h-pages__more" onclick={(e) => { e.stopPropagation(); void showMore(d.mount); }}>
      {t('pages.showMore', { n: Math.min(MOUNT_PAGE, d.left), left: d.left })}
    </button>
  {/if}
{/snippet}

{#if addDlg}
  {#key addDlg}
    <AddDialog
      bind:open={addOpen}
      mode={addDlg.mode}
      parent={addDlg.parent}
      node={addDlg.node}
      {locale}
      childTypes={childTypes(addDlg.parent)}
      {lastChild}
      ondone={(r) => void changed([r.parent], r.select)}
    />
  {/key}
{/if}
{#if moveDlg && store}
  {#key moveDlg}
    <MoveDialog
      bind:open={moveOpen}
      node={moveDlg.node}
      {store}
      locales={languages}
      initial={moveDlg.initial}
      ondone={(r) => void changed([r.from, r.to], moveDlg?.node.id)}
    />
  {/key}
{/if}
<Dialog bind:open={delOpen} title={t('pages.deleteTitle')} size="sm">
  <p>{t('pages.deleteText', { name: delNode?.name ?? '' })}</p>
  {#snippet footer()}
    <Button onclick={() => (delOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="danger" loading={deleting} onclick={() => void doDelete()}>{t('common.delete')}</Button>
  {/snippet}
</Dialog>

<style>
  .h-pages__filter > :global(.h-input) {
    flex: 0 1 12rem;
    width: auto;
  }
  .h-pages__filter > :global(input[type='search']) {
    flex: 1 1 14rem;
  }
  .h-page__head h1 {
    white-space: nowrap;
  }
  .h-pages__filter {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--h-space-2);
    margin-block-end: var(--h-space-3);
  }
  .h-pages__split {
    display: grid;
    grid-template-columns: var(--h-pages-left, 400px) 0.5rem minmax(0, 1fr);
    min-height: 60vh;
  }
  .h-pages__tree {
    min-width: 0;
    overflow: auto;
    max-height: 75vh;
  }
  .h-pages__pane {
    min-width: 0;
    padding-inline-start: var(--h-space-3);
  }
  .h-pages__sep {
    cursor: col-resize;
    background: var(--h-border);
    width: 2px;
    justify-self: center;
    border-radius: var(--h-radius);
  }
  .h-pages__sep:hover,
  .h-pages__sep:focus-visible {
    background: var(--h-accent, currentColor);
    width: 4px;
  }
  .h-pages__row {
    display: inline-flex;
    align-items: center;
    gap: var(--h-space-2);
    min-width: 0;
    flex: 1;
  }
  .h-pages__row--restricted {
    opacity: 0.6;
    font-style: italic;
  }
  .h-pages__meta {
    font-size: var(--h-text-xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .h-pages__dots {
    display: inline-flex;
    gap: 2px;
  }
  .h-pages__dot {
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    border: 1px solid currentColor;
    opacity: 0.5;
  }
  .h-pages__dot--on {
    background: currentColor;
    opacity: 1;
  }
  .h-pages__menu {
    margin-inline-start: auto;
  }
  .h-pages__more {
    background: none;
    border: 0;
    color: inherit;
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
    padding: 0;
  }
  :global(.h-pages__chooser) {
    max-height: 14rem;
    overflow: auto;
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
    padding: var(--h-space-1);
  }
  :global(.h-pages__changes) {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 12rem;
    overflow: auto;
    display: grid;
    gap: var(--h-space-1);
  }
  :global(.h-pages__from) {
    text-decoration: line-through;
    opacity: 0.7;
  }
  :global(.h-pages__to) {
    font-weight: 600;
  }
  @media (max-width: 800px) {
    .h-pages__split {
      grid-template-columns: 1fr;
    }
    .h-pages__sep {
      display: none;
    }
    .h-pages__pane {
      padding-inline-start: 0;
    }
  }
</style>
