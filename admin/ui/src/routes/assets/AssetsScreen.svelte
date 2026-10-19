<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import { untrack } from 'svelte';
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import '../../lib/messages/media';
  import { router } from '../../lib/router.svelte';
  import Confirm from '../settings/Confirm.svelte';
  import { types } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Segmented from '../../lib/ui/Segmented.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import Tree from '../../lib/ui/Tree.svelte';
  import type { TreeNode } from '../../lib/ui/tree';
  import AssetDetails from '../../lib/media/AssetDetails.svelte';
  import AssetGrid from '../../lib/media/AssetGrid.svelte';
  import UploadQueue from '../../lib/media/UploadQueue.svelte';
  import Thumb from '../../lib/media/Thumb.svelte';
  import { Library } from '../../lib/media/library.svelte';
  import { ancestorsOf, dimensions, droppedFiles, formatSize, type FileValue, type FolderNode } from '../../lib/media/logic';
  import { Uploads } from '../../lib/media/uploads.svelte';
  import type { AssetRow } from '../../lib/media/upload';
  import '../../lib/media/media.css';

  let { names }: { names: string[] } = $props();

  const lib = new Library(untrack(() => [...names]), 24);
  const uploads = new Uploads();
  const ALL = '*';
  // Uploading needs content.create on a type; the others take no files.
  const specs = $derived(names.filter((n) => session.can('content.create', { type: n })).map((n) => ({ name: n, upload: types.get(n)?.upload })));
  const canUpload = $derived(specs.some((s) => s.name === lib.type));
  const here = router.path;
  const accept = $derived(specs.every((s) => s.upload?.accept?.length) ? specs.flatMap((s) => s.upload!.accept!).join(',') : undefined);

  const q = $derived(router.query);
  const view = $derived(q.get('view') === 'list' ? 'list' : 'grid');
  const itemId = $derived(q.get('item') ?? '');

  $effect(() => {
    const t = q.get('type') ?? '';
    lib.type = names.includes(t) ? t : (names[0] ?? '');
    lib.folder = q.get('folder') ?? '';
    lib.tag = q.get('tag') ?? '';
    lib.q = q.get('q') ?? '';
    lib.page = Math.max(1, Number(q.get('page')) || 1);
  });
  $effect(() => lib.load());
  $effect(() => lib.loadFacets());
  // Leaving the screen stops the uploads still running: their completion must not touch the address.
  $effect(() => () => uploads.destroy());

  // ---- search (debounced into the address)
  let qText = $state(untrack(() => router.query.get('q') ?? ''));
  let timer: ReturnType<typeof setTimeout> | undefined;
  function typed(v: string) {
    qText = v;
    clearTimeout(timer);
    timer = setTimeout(() => router.setQuery({ q: v.trim() || undefined, page: undefined }), 250);
  }
  $effect(() => () => clearTimeout(timer));

  // ---- folders
  const toNode = (n: FolderNode): TreeNode => ({ id: n.id, label: n.label, icon: 'folder', children: n.children.map(toNode) });
  const nodes = $derived<TreeNode[]>([{ id: ALL, label: t('media.allAssets'), icon: 'folder-open', children: lib.folders.map(toNode) }]);
  let expanded = $state(new Set<string>([ALL]));
  let sel = $state<string | undefined>(ALL);
  $effect(() => {
    sel = lib.folder || ALL;
    const more = ancestorsOf(lib.folder).filter((a) => !expanded.has(a));
    if (more.length) expanded = new Set([...expanded, ...more]);
  });
  const chooseFolder = (n: TreeNode) => guarded(() => router.setQuery({ folder: n.id === ALL ? undefined : n.id, page: undefined, item: undefined }));

  let folderOpen = $state(false);
  let folderPath = $state('');
  function addFolder(e: Event) {
    e.preventDefault();
    if (!folderPath.trim()) return;
    const p = lib.addFolder(folderPath);
    folderOpen = false;
    folderPath = '';
    router.setQuery({ folder: p === '/' ? undefined : p, page: undefined });
  }

  // ---- uploads
  let fileInput = $state<HTMLInputElement>();
  let over = $state(false);
  let depth = 0;
  const target = $derived({
    types: specs,
    preferred: lib.type,
    folder: lib.folder,
    known: async (sha: string) => lib.known(sha),
    ondone: (id: string, _file: FileValue, _type: string, existing: boolean) => {
      // A new asset can bring a new folder; the list shows it either way.
      if (existing) lib.reload();
      else lib.invalidateFacets();
      if (router.path === here) router.setQuery({ item: id });
    },
  });
  function addFiles(files: Iterable<File>) {
    if (canUpload) void uploads.add(files, target);
  }
  function picked(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    const files = [...(el.files ?? [])];
    el.value = '';
    if (files.length) addFiles(files);
  }
  const hasFiles = (e: DragEvent) => !!e.dataTransfer && [...e.dataTransfer.types].includes('Files');
  function enter(e: DragEvent) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth++;
    over = canUpload;
  }
  function overEv(e: DragEvent) {
    if (hasFiles(e)) e.preventDefault();
  }
  function leave() {
    depth = Math.max(0, depth - 1);
    if (!depth) over = false;
  }
  function drop(e: DragEvent) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    over = false;
    addFiles(droppedFiles(e.dataTransfer!));
  }

  // Leaving an asset with unsaved changes asks first.
  let detailsDirty = $state(false);
  let discardOpen = $state(false);
  let afterDiscard: (() => void) | undefined;
  function guarded(go: () => void) {
    if (!detailsDirty) return go();
    afterDiscard = go;
    discardOpen = true;
  }
  const select = (r: AssetRow) => {
    if (r.id !== itemId) guarded(() => router.setQuery({ item: r.id }));
  };
  const closeDetails = () => guarded(() => router.setQuery({ item: undefined }));
  const typeOptions = $derived(names.map((n) => ({ value: n, label: types.get(n)?.label ?? n })));
  const filtered = $derived(!!(lib.q || lib.folder || lib.tag));
</script>

<div class="h-page h-assets">
  <Slot at="assets.before" />
  <input bind:this={fileInput} hidden type="file" multiple {accept} onchange={picked} />
  <PageHeader title={t('media.title')} breadcrumbs={[{ label: t('nav.content') }, { label: t('media.title') }]} meta={t('media.meta')}>
    {#snippet primary()}
      {#if canUpload}<Button variant="primary" icon="upload" onclick={() => fileInput?.click()}>{t('media.upload')}</Button>{/if}
    {/snippet}
  </PageHeader>

  <div class="h-assets__layout" class:h-assets__layout--panel={!!itemId}>
    <nav class="h-assets__side" aria-label={t('media.sideNav')}>
      <Tree label={t('media.library')} {nodes} bind:expanded bind:selected={sel} onselect={chooseFolder} />
      <Button size="sm" variant="ghost" icon="plus" onclick={() => (folderOpen = true)}>{t('media.newFolder')}</Button>
      {#if lib.tags.length}
        <Select compact aria-label={t('media.filterTag')} value={lib.tag} placeholder={t('media.allTags')} options={lib.tags.map((x) => ({ value: x, label: x }))} onchange={(e) => router.setQuery({ tag: (e.currentTarget as HTMLSelectElement).value || undefined, page: undefined })} />
      {/if}
    </nav>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="h-assets__main" class:h-assets__main--over={over} ondragenter={enter} ondragover={overEv} ondragleave={leave} ondrop={drop}>
      <div class="h-assets__bar">
        <Input
          class="h-assets__search"
          icon="search"
          type="search"
          aria-label={t('media.search')}
          placeholder={t('media.searchPlaceholder')}
          autocomplete="off"
          value={qText}
          oninput={(e) => typed((e.currentTarget as HTMLInputElement).value)}
        />
        {#if names.length > 1}
          <Select compact aria-label={t('media.type')} value={lib.type} options={typeOptions} onchange={(e) => router.setQuery({ type: (e.currentTarget as HTMLSelectElement).value, folder: undefined, tag: undefined, page: undefined, item: undefined })} />
        {/if}
        <Segmented
          size="sm"
          label={t('media.view')}
          value={view}
          options={[
            { value: 'grid', label: t('media.viewGrid'), icon: 'grid' },
            { value: 'list', label: t('media.viewList'), icon: 'list' },
          ]}
          onchange={(v) => router.setQuery({ view: v === 'grid' ? undefined : v })}
        />
      </div>

      <UploadQueue {uploads} clearable />

      {#if lib.failed}
        <p class="h-alert" role="alert">{t('media.loadFailed')} <Button size="sm" onclick={() => lib.reload()}>{t('app.retry')}</Button></p>
      {:else if lib.rows === null}
        <Skeleton lines={5} height="1.4rem" />
      {:else if lib.rows.length === 0}
        <EmptyState icon="image" title={filtered ? t('media.emptyFiltered') : t('media.empty')} description={filtered ? t('media.emptyFilteredHint') : `${t('media.emptyHint')} ${t('media.dropHint')}`} />
      {:else if view === 'grid'}
        <AssetGrid rows={lib.rows} selected={itemId} onpick={select} />
      {:else}
        <Table
          caption={t('media.assets')}
          rows={lib.rows}
          rowKey={(r) => r.id}
          onrowclick={select}
          columns={[
            { key: 'name', label: t('media.col.name'), cell: nameCell },
            { key: 'mime', label: t('media.col.type'), cell: typeCell, mobile: 'hide' },
            { key: 'size', label: t('media.col.size'), cell: sizeCell, mobile: 'hide' },
            { key: 'dim', label: t('media.col.dimensions'), cell: dimCell, mobile: 'hide' },
            { key: 'folder', label: t('media.col.folder'), cell: folderCell, mobile: 'hide' },
            { key: 'updatedAt', label: t('media.col.updated'), cell: updatedCell },
          ]}>
          {#snippet footer()}<span></span>{/snippet}
        </Table>
      {/if}

      {#if lib.rows && lib.total > lib.size}
        <Pagination page={lib.page} pageSize={lib.size} total={lib.total} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/if}

      {#if over}<div class="h-assets__drop" aria-hidden="true"><Icon name="upload" size={28} />{t('media.drop')}</div>{/if}
    </div>

    {#if itemId}
      <div class="h-assets__panel">
        <AssetDetails
          id={itemId}
          folders={lib.folderPaths}
          bind:dirty={detailsDirty}
          onclose={closeDetails}
          onchanged={(c) => (c?.facets ? lib.invalidateFacets() : lib.reload())}
          onremoved={() => { router.setQuery({ item: undefined }); lib.invalidateFacets(); }} />
      </div>
    {/if}
  </div>
  <Slot at="assets.after" />
</div>

<Confirm
  bind:open={discardOpen}
  title={t('media.discardTitle')}
  message={t('media.discardText')}
  confirm={t('media.discard')}
  onconfirm={async () => {
    const go = afterDiscard;
    afterDiscard = undefined;
    detailsDirty = false;
    go?.();
  }} />

<Dialog bind:open={folderOpen} title={t('media.newFolderTitle')} size="sm">
  <form id="h-newfolder" onsubmit={addFolder}>
    <label class="h-field__label" for="h-newfolder-path">{t('media.newFolderPath')}</label>
    <Input id="h-newfolder-path" bind:value={folderPath} placeholder="/marketing/2026" autocomplete="off" />
    <p class="h-muted">{t('media.newFolderHint')}</p>
  </form>
  {#snippet footer()}
    <Button onclick={() => (folderOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" type="submit" form="h-newfolder">{t('media.newFolderAdd')}</Button>
  {/snippet}
</Dialog>

{#snippet nameCell(r: AssetRow)}
  <button type="button" class="h-assetrow" onclick={(e) => { e.stopPropagation(); select(r); }}>
    <Thumb file={r.file} /><span class="h-assetrow__name">{r.name}</span>
  </button>
{/snippet}
{#snippet typeCell(r: AssetRow)}{r.file?.mime ?? ''}{/snippet}
{#snippet sizeCell(r: AssetRow)}{r.file ? formatSize(r.file.size) : ''}{/snippet}
{#snippet dimCell(r: AssetRow)}{r.file ? dimensions(r.file) : ''}{/snippet}
{#snippet folderCell(r: AssetRow)}{r.folder}{/snippet}
{#snippet updatedCell(r: AssetRow)}<Time at={r.updatedAt} />{/snippet}
