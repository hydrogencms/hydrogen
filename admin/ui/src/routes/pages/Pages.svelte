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
  import Breadcrumbs from '../../lib/ui/Breadcrumbs.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Chip from '../../lib/ui/Chip.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Popover from '../../lib/ui/Popover.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Menu, { type MenuItem } from '../../lib/ui/Menu.svelte';
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
  import { siteName } from '../../lib/format';
  import { MOUNT_PAGE, SiteTree, type RowData } from './siteTree.svelte';

  let { params }: { params: Record<string, string> } = $props();

  const ItemEditor = ItemRaw;

  // svelte-ignore state_referenced_locally
  const site = params.site;
  const crumbs = [{ label: t('nav.pages') }, { label: siteName(site) }];
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
  const MIN = 280;
  const MAX = 560;
  const clamp = (x: number) => Math.round(Math.min(Math.max(x, MIN), MAX));
  let left = $state(clamp(Number(localStorage.getItem(splitKey)) || 360));
  let box = $state<HTMLElement>();
  function resizeTo(x: number) {
    const w = box?.getBoundingClientRect().width ?? 1200;
    left = clamp(Math.min(x, Math.max(w - 480, MIN)));
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
    const d = e.key === 'ArrowLeft' ? -24 : e.key === 'ArrowRight' ? 24 : e.key === 'Home' ? MIN - left : e.key === 'End' ? MAX - left : 0;
    if (!d) return;
    e.preventDefault();
    resizeTo(left + d);
    localStorage.setItem(splitKey, String(left));
  }

  const matchCount = $derived(result?.matches.size ?? 0);

  // "+ Add page": below the selected page, else below the site's first page (or as the first page of an empty site).
  const rootNode = $derived(store?.kids.has('') ? store.byId.get(store.kids.get('')?.[0] ?? '') : undefined);
  const addParent = $derived.by((): PNode | undefined => {
    const s = sel && 'node' in sel ? sel.node : undefined;
    if (s && s.kind === 'page' && !s.restricted && childTypes(s).length) return s;
    if (rootNode && !rootNode.restricted && childTypes(rootNode).length) return rootNode;
    return undefined;
  });
  const emptySite = $derived(!!store && ready && !store.nodes.length);
  function addPage() {
    if (emptySite) {
      openAdd('child', { id: '', site, kind: 'page', hidden: false, depth: -1, name: '', children: 0 });
      return;
    }
    if (addParent) openAdd('child', addParent);
  }
  const typeItems = $derived<MenuItem[]>([
    { label: t('pages.anyType'), checked: !filter.type, onselect: () => (filter.type = '') },
    ...types.pageTypes.map((p) => ({ label: p.label, checked: filter.type === p.name, onselect: () => (filter.type = p.name) })),
  ]);
  const statusItems = $derived<MenuItem[]>([
    { label: t('pages.anyStatus'), checked: !filter.status, onselect: () => (filter.status = '') },
    ...['draft', 'published', 'changed', 'hidden'].map((x) => ({ label: t('pages.status.' + x), checked: filter.status === x, onselect: () => (filter.status = x) })),
  ]);
  const filterCount = $derived((filter.type ? 1 : 0) + (filter.status ? 1 : 0) + (filter.gaps ? 1 : 0));
  const siteItems = $derived<MenuItem[]>(sites.map((x) => ({ label: siteName(x), icon: 'folder-tree', checked: x === site, href: '/pages/' + encodeURIComponent(x) })));
  const localeItems = $derived<MenuItem[]>(
    languages.map((l) => ({
      label: l,
      checked: l === locale,
      onselect: () => {
        locale = l;
        void start();
      },
    })),
  );
  let hover = $state('');
  const dotOf = (s?: string) => (s === 'published' ? 'filled' : s === 'changed' ? 'half' : 'hollow');
</script>

<div class="h-page h-pages" class:h-pages--detail={!!sel}>
  <Breadcrumbs items={crumbs} />
  <h1 class="h-sr">{t('pages.title', { site: siteName(site) })}</h1>

  <div class="h-pages__split" bind:this={box} style:--h-pages-left="{left}px">
    <section class="h-card h-card--none h-pages__tree" aria-label={t('pages.treeLabel', { site })}>
      <div class="h-pages__head">
        <div class="h-pages__top">
          {#if sites.length > 1}
            <Menu items={siteItems} label={t('pages.site')} align="start" triggerClass="h-pill h-pages__site">
              {#snippet trigger()}<Icon name="folder-tree" size={16} /><span class="h-pages__sitename">{siteName(site)}</span><Icon name="chevron-down" size={14} />{/snippet}
            </Menu>
          {:else}
            <span class="h-pill h-pages__site"><Icon name="folder-tree" size={16} /><span class="h-pages__sitename">{siteName(site)}</span></span>
          {/if}
          {#if languages.length > 1}
            <Menu items={localeItems} label={t('pages.locale')} triggerClass="h-pill">
              {#snippet trigger()}<Icon name="globe" size={16} /><span class="h-pill__text">{locale}</span><Icon name="chevron-down" size={14} />{/snippet}
            </Menu>
          {/if}
          <span class="h-spacer"></span>
          {#if canEdit && ready && (emptySite || addParent)}
            <Button size="sm" variant="primary" icon="plus" class="h-pages__add" title={t('pages.addPage')} onclick={addPage}><span class="h-pages__addtext">{t('pages.addPage')}</span></Button>
          {/if}
        </div>
        <div role="search">
          <Input type="search" icon="search" aria-label={t('pages.filterTitle')} placeholder={t('pages.filterTitle')} bind:value={filter.q} />
        </div>
        <div class="h-pages__chips">
          <div class="h-pages__wide">
          <Menu items={typeItems} label={t('pages.type')} align="start" triggerClass="h-pages__chipbtn {filter.type ? 'h-pages__chipbtn--on' : ''}">
            {#snippet trigger()}{filter.type ? (types.get(filter.type)?.label ?? filter.type) : t('pages.type')}<Icon name="chevron-down" size={14} />{/snippet}
          </Menu>
          <Menu items={statusItems} label={t('pages.status')} align="start" triggerClass="h-pages__chipbtn {filter.status ? 'h-pages__chipbtn--on' : ''}">
            {#snippet trigger()}{filter.status ? t('pages.status.' + filter.status) : t('pages.status')}<Icon name="chevron-down" size={14} />{/snippet}
          </Menu>
          {#if languages.length > 1}
            <Chip label={t('pages.languageGaps')} variant={filter.gaps ? 'applied' : 'neutral'} aria-pressed={filter.gaps} onclick={() => (filter.gaps = !filter.gaps)} />
          {/if}
          </div>
          <div class="h-pages__narrow">
            <Popover label={t('pages.filters')} align="start" triggerClass="h-pages__chipbtn {filterCount ? 'h-pages__chipbtn--on' : ''}">
              {#snippet trigger()}<Icon name="sliders" size={14} />{t('pages.filters')}{#if filterCount}<span class="h-pages__n">{filterCount}</span>{/if}{/snippet}
              {#snippet children()}
                <div class="h-pages__fpop">
                  <Select aria-label={t('pages.type')} bind:value={filter.type} placeholder={t('pages.anyType')} options={types.pageTypes.map((p) => ({ value: p.name, label: p.label }))} />
                  <Select aria-label={t('pages.status')} bind:value={filter.status} placeholder={t('pages.anyStatus')} options={['draft', 'published', 'changed', 'hidden'].map((x) => ({ value: x, label: t('pages.status.' + x) }))} />
                  {#if languages.length > 1}<Checkbox bind:checked={filter.gaps} label={t('pages.languageGaps')} />{/if}
                </div>
              {/snippet}
            </Popover>
          </div>
          {#if filtering}
            <Button size="sm" variant="ghost" onclick={() => (filter = emptyFilter())}>{t('common.clear')}</Button>
            <span class="h-muted h-pages__count" role="status">{filterBusy || !result ? t('app.loading') : t('pages.matches', { n: matchCount })}</span>
          {/if}
        </div>
      </div>

      {#if error}
        <p class="h-alert h-pages__alert" role="alert">{error} <Button size="sm" onclick={() => void boot()}>{t('app.retry')}</Button></p>
      {/if}

      <div class="h-pages__scroll">
        {#if !store || !ready}
          {#if !error}<div class="h-pages__skeleton"><Skeleton lines={8} /></div>{/if}
        {:else if filtering}
          {#if result && !pruned.length}
            <p class="h-muted h-pages__none">{t('common.noResults')}</p>
          {:else}
            <Tree nodes={pruned} label={t('pages.treeLabel', { site })} bind:expanded={fexp} bind:selected onactivate={activate} {row} />
          {/if}
        {:else if !store.nodes.length}
          <EmptyState title={t('pages.empty')} description={t('pages.emptyText')} icon="file">
            {#snippet actions()}
              {#if canEdit}<Button variant="primary" icon="plus" onclick={addPage}>{t('pages.addFirst')}</Button>{/if}
            {/snippet}
          </EmptyState>
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
    </section>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions, a11y_no_static_element_interactions -->
    <div
      class="h-split__sep h-pages__sep"
      role="separator"
      aria-orientation="vertical"
      aria-label={t('pages.resize')}
      aria-valuemin={MIN}
      aria-valuemax={MAX}
      aria-valuenow={left}
      tabindex="0"
      onpointerdown={dragSplit}
      onkeydown={splitKeys}
    ></div>
    <section class="h-pages__pane" aria-label={t('pages.detail')}>
      <div class="h-pages__back">
        <Button size="sm" icon="arrow-left" onclick={() => (selected = undefined)}>{t('pages.back')}</Button>
      </div>
      {#if !sel}
        <EmptyState title={t('pages.selectNode')} icon="file" />
      {:else if 'item' in sel && sel.item}
        {#key sel.item}<ItemEditor id={sel.item} embedded />{/key}
      {:else if 'node' in sel && sel.node}
        {@const n = sel.node}
        {#key n.id}
          {#if n.restricted}
            <p class="h-muted h-pages__none">{t('pages.restricted')}</p>
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
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <span class="h-pages__row" class:h-pages__row--restricted={n.restricted} onpointerenter={() => (hover = n.id)}>
      <span class="h-pages__kind"><Icon name={n.kind === 'page' ? 'file' : n.kind === 'mount' ? 'folder-tree' : 'link'} size={16} label={t('pages.kind.' + n.kind)} /></span>
      <span class="h-tree__label">{n.name || '…'}</span>
      {#if !n.restricted}
        {#if n.kind === 'mount'}
          <span class="h-muted h-pages__meta">
            {n.mount?.collection} · {t('pages.itemCount', { n: store?.mounts.get(n.id)?.total ?? '…' })}
          </span>
        {:else if n.kind === 'link'}
          <span class="h-muted h-pages__meta">{n.target ?? ''}</span>
          {#if n.broken}<Badge tone="danger">{t('pages.broken')}</Badge>{/if}
        {/if}
        {#if n.hidden}<Badge>{t('pages.status.hidden')}</Badge>{/if}
        {#if n.kind === 'page'}
          {#if languages.length > 1 && store?.locales.has(n.item ?? '')}
            {@const have = complete(store.locales.get(n.item ?? ''))}
            <span class="h-pages__dots" role="img" aria-label={t('pages.languagesHave', { list: have.join(', ') || t('common.none') })}>
              {#each languages as l (l)}<span class="h-pages__dot" class:h-pages__dot--on={have.includes(l)} title={l}></span>{/each}
            </span>
          {/if}
          {#if n.status}
            <span class="h-dot h-dot--{dotOf(n.status)}" role="img" aria-label={t('pages.status.' + n.status)} title={t('pages.status.' + n.status)}></span>
          {/if}
        {/if}
        {#if selected === n.id || hover === n.id}
          {@const items = menuFor(n)}
          {#if items.length}
            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions, a11y_no_noninteractive_element_interactions -->
            <span class="h-pages__menu" class:h-pages__menu--sel={selected === n.id} onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} ondblclick={(e) => e.stopPropagation()}>
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
      <span class="h-pages__kind"><Icon name="file-text" size={16} /></span>
      <span class="h-tree__label">{d.item.name}</span>
      <span class="h-dot h-dot--{dotOf(d.item.status)}" role="img" aria-label={t('pages.status.' + d.item.status)} title={t('pages.status.' + d.item.status)}></span>
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
  .h-pages {
    height: calc(100vh - var(--h-topbar-height));
    padding-bottom: var(--h-space-4);
    gap: 0;
  }
  .h-pages__split {
    display: grid;
    grid-template-columns: var(--h-pages-left, 360px) 0.75rem minmax(0, 1fr);
    flex: 1;
    min-height: 0;
  }
  .h-pages__tree {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    container-type: inline-size;
  }
  .h-pages__top :global(.h-pages__site) {
    flex: 0 1 auto;
    min-width: 0;
  }
  .h-pages__sitename {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .h-pages__top :global(.h-pages__add) {
    flex: none;
  }
  .h-pages__wide {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-pages__narrow {
    display: none;
  }
  .h-pages__n {
    display: inline-grid;
    place-items: center;
    min-width: 1.125rem;
    height: 1.125rem;
    border-radius: var(--h-radius-pill);
    background: var(--h-accent-contrast);
    color: var(--h-accent);
    font-size: var(--h-text-xs);
  }
  .h-pages__fpop {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
    min-width: 14rem;
  }
  .h-pages__back {
    display: none;
  }
  @container (max-width: 419px) {
    .h-pages__wide {
      display: none;
    }
    .h-pages__narrow {
      display: block;
    }
    .h-pages__top :global(.h-pages__add) {
      width: var(--h-control-height-sm);
      padding: 0;
    }
    .h-pages__addtext {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }
  }
  .h-pages__head {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    padding: var(--h-space-3);
    border-bottom: 1px solid var(--h-border);
  }
  .h-pages__top {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-pages__top :global(.h-pill) {
    height: var(--h-control-height-sm);
    max-width: 11rem;
  }
  .h-pages__chips {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    gap: var(--h-space-2);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .h-pages__chips > * {
    flex: none;
  }
  .h-pages__chips :global(.h-pages__chipbtn) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: var(--h-control-height-sm);
    padding: 0 var(--h-space-3);
    border: 1px solid var(--h-border-strong);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface);
    color: var(--h-text);
    font-size: var(--h-text-sm);
    font-weight: 500;
    white-space: nowrap;
  }
  .h-pages__chips :global(.h-pages__chipbtn:hover) {
    background: var(--h-surface-2);
  }
  .h-pages__chips :global(.h-pages__chipbtn--on),
  .h-pages__chips :global(.h-pages__chipbtn--on:hover) {
    border-color: var(--h-accent);
    background: var(--h-accent);
    color: var(--h-accent-contrast);
  }
  .h-pages__count {
    font-size: var(--h-text-xs);
  }
  .h-pages__scroll {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    overflow: auto;
    padding: var(--h-space-2);
  }
  .h-pages__scroll > :global(.h-empty) {
    flex: 1;
    justify-content: center;
  }
  .h-pages__scroll > :global(.h-tree),
  .h-pages__scroll > :global(ul) {
    flex: none;
  }
  .h-pages__scroll :global(.h-empty__text),
  .h-pages__scroll :global(.h-empty__title) {
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .h-pages__skeleton,
  .h-pages__none,
  .h-pages__alert {
    margin: var(--h-space-3);
  }
  .h-pages__pane {
    min-width: 0;
    min-height: 0;
    overflow: auto;
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-lg);
    background: var(--h-bg);
  }
  .h-pages__pane > :global(.h-page) {
    padding: var(--h-space-4);
    gap: var(--h-space-4);
  }
  .h-pages__sep {
    position: relative;
  }
  .h-pages__sep::after {
    content: '';
    position: absolute;
    inset: 0 auto 0 50%;
    width: 2px;
    margin-left: -1px;
    border-radius: 1px;
    background: transparent;
    transition: background var(--h-dur-fast);
  }
  .h-pages__sep:hover::after,
  .h-pages__sep:focus-visible::after {
    background: var(--h-accent);
  }
  .h-pages__sep:focus-visible {
    outline: none;
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
  .h-pages__kind {
    display: inline-grid;
    flex: none;
    color: var(--h-text-muted);
  }
  .h-pages__meta {
    max-width: 8rem;
    font-size: var(--h-text-xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .h-pages__dots {
    display: inline-flex;
    gap: 3px;
  }
  .h-pages__dot {
    width: 6px;
    height: 6px;
    border: 1px solid var(--h-text-faint);
    border-radius: 50%;
  }
  .h-pages__dot--on {
    border-color: var(--h-success);
    background: var(--h-success);
  }
  .h-pages__menu {
    display: inline-flex;
    margin-inline-start: auto;
    opacity: 0;
  }
  .h-pages__menu--sel,
  .h-pages__menu:focus-within,
  :global(.h-tree__item:hover) .h-pages__menu {
    opacity: 1;
  }
  .h-pages__more {
    padding: 0;
    border: 0;
    background: none;
    color: var(--h-text-muted);
    font: inherit;
    font-size: var(--h-text-sm);
    cursor: pointer;
  }
  .h-pages__more:hover {
    color: var(--h-text);
    text-decoration: underline;
  }
  :global(.h-pages__chooser) {
    max-height: 16rem;
    overflow: auto;
    padding: var(--h-space-1);
    border-radius: var(--h-radius);
    background: var(--h-surface-2);
  }
  :global(.h-pages__changes) {
    display: grid;
    gap: var(--h-space-1);
    max-height: 12rem;
    margin: 0;
    padding: 0;
    overflow: auto;
    list-style: none;
  }
  :global(.h-pages__from) {
    text-decoration: line-through;
    opacity: 0.7;
  }
  :global(.h-pages__to) {
    font-weight: 600;
  }
  @media (max-width: 1023px) {
    .h-pages {
      height: auto;
    }
    .h-pages__split {
      grid-template-columns: minmax(0, 1fr);
    }
    .h-pages__sep {
      display: none;
    }
    .h-pages--detail .h-pages__tree {
      display: none;
    }
    .h-pages:not(.h-pages--detail) .h-pages__pane {
      display: none;
    }
    .h-pages__back {
      display: block;
      padding: var(--h-space-3) var(--h-space-4) 0;
    }
  }
</style>
