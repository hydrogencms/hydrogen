<script lang="ts">
  import { untrack } from 'svelte';
  import { session } from '../auth/session.svelte';
  import { t } from '../i18n';
  import '../messages/media';
  import { types } from '../types.svelte';
  import Button from '../ui/Button.svelte';
  import Dialog from '../ui/Dialog.svelte';
  import EmptyState from '../ui/EmptyState.svelte';
  import Input from '../ui/Input.svelte';
  import Pagination from '../ui/Pagination.svelte';
  import Select from '../ui/Select.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import Tabs from '../ui/Tabs.svelte';
  import AssetGrid from './AssetGrid.svelte';
  import UploadQueue from './UploadQueue.svelte';
  import { Library } from './library.svelte';
  import type { FileValue } from './logic';
  import { Uploads } from './uploads.svelte';
  import type { AssetRow } from './upload';
  import './media.css';

  let {
    open = $bindable(false),
    names,
    current,
    onselect,
  }: {
    open?: boolean;
    /** Upload types the asset may come from. */
    names: string[];
    /** The asset already chosen (shown as selected). */
    current?: string;
    onselect: (asset: { id: string; name: string; file?: FileValue; status?: string }) => void;
  } = $props();

  const lib = new Library(untrack(() => [...names]), 12);
  const uploads = new Uploads();
  let tab = $state('library');
  let qText = $state('');
  let timer: ReturnType<typeof setTimeout> | undefined;
  let fileInput = $state<HTMLInputElement>();
  // Only the types the user may create assets of take uploads.
  const specs = $derived(names.filter((n) => session.can('content.create', { type: n })).map((n) => ({ name: n, upload: types.get(n)?.upload })));
  const canUpload = $derived(specs.length > 0);
  const accept = $derived(specs.every((s) => s.upload?.accept?.length) ? specs.flatMap((s) => s.upload!.accept!).join(',') : undefined);

  $effect(() => {
    if (open) return lib.load();
  });
  $effect(() => {
    if (open) return lib.loadFacets();
  });
  // Closing stops uploads still running (their completion must not choose anything) and drops the finished rows.
  $effect(() => {
    if (!open) {
      untrack(() => {
        tab = 'library';
        uploads.abort();
        uploads.clearFinished();
      });
    }
  });
  $effect(() => () => {
    clearTimeout(timer);
    uploads.destroy();
  });

  function typed(v: string) {
    qText = v;
    clearTimeout(timer);
    timer = setTimeout(() => {
      lib.q = v.trim();
      lib.page = 1;
    }, 250);
  }
  function choose(r: { id: string; name: string; file?: FileValue; status?: string }) {
    onselect({ id: r.id, name: r.name, file: r.file, status: r.status });
    open = false;
  }
  const pick = (r: AssetRow) => choose(r);

  const target = $derived({
    types: specs,
    preferred: lib.type,
    folder: lib.folder,
    known: async (sha: string) => lib.known(sha),
    ondone: (id: string, file: FileValue, _type: string, existing: boolean) => choose({ id, name: file.name, file, status: existing ? undefined : 'draft' }),
  });
  function picked(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    const files = [...(el.files ?? [])];
    el.value = '';
    if (files.length) void uploads.add(files.slice(0, 1), target);
  }
  const typeOptions = $derived(names.map((n) => ({ value: n, label: types.get(n)?.label ?? n })));
</script>

<Dialog bind:open title={t('media.chooseTitle')} size="lg">
  <Tabs
    label={t('media.pickTabs')}
    variant="segmented"
    bind:value={tab}
    tabs={canUpload
      ? [
          { id: 'library', label: t('media.pickLibrary') },
          { id: 'upload', label: t('media.pickUpload') },
        ]
      : [{ id: 'library', label: t('media.pickLibrary') }]}
  >
    {#snippet children(id)}
      {#if id === 'library'}
        <div class="h-picker__bar">
          <Input class="h-assets__search" icon="search" type="search" aria-label={t('media.search')} placeholder={t('media.searchPlaceholder')} autocomplete="off" value={qText} oninput={(e) => typed((e.currentTarget as HTMLInputElement).value)} />
          {#if names.length > 1}
            <Select compact aria-label={t('media.type')} value={lib.type} options={typeOptions} onchange={(e) => { lib.type = (e.currentTarget as HTMLSelectElement).value; lib.folder = ''; lib.tag = ''; lib.page = 1; }} />
          {/if}
          {#if lib.folderPaths.length}
            <Select compact aria-label={t('media.filterFolder')} value={lib.folder} placeholder={t('media.allFolders')} options={lib.folderPaths.map((p) => ({ value: p, label: p }))} onchange={(e) => { lib.folder = (e.currentTarget as HTMLSelectElement).value; lib.page = 1; }} />
          {/if}
          {#if lib.tags.length}
            <Select compact aria-label={t('media.filterTag')} value={lib.tag} placeholder={t('media.allTags')} options={lib.tags.map((x) => ({ value: x, label: x }))} onchange={(e) => { lib.tag = (e.currentTarget as HTMLSelectElement).value; lib.page = 1; }} />
          {/if}
        </div>
        {#if lib.failed}
          <p class="h-alert" role="alert">{t('media.loadFailed')} <Button size="sm" onclick={() => lib.reload()}>{t('app.retry')}</Button></p>
        {:else if lib.rows === null}
          <Skeleton lines={4} height="1.4rem" />
        {:else if lib.rows.length === 0}
          <EmptyState icon="image" title={t('media.pickNone')} />
        {:else}
          <AssetGrid rows={lib.rows} selected={current || undefined} onpick={pick} />
        {/if}
        {#if lib.rows && lib.total > lib.size}
          <Pagination page={lib.page} pageSize={lib.size} total={lib.total} onpage={(p) => (lib.page = p)} />
        {/if}
      {:else}
        <div class="h-picker__upload">
          <p class="h-muted">{t('media.pickUploadHint')}</p>
          <input bind:this={fileInput} hidden type="file" {accept} onchange={picked} />
          <Button icon="upload" variant="primary" onclick={() => fileInput?.click()}>{t('media.pickFiles')}</Button>
          <UploadQueue {uploads} />
        </div>
      {/if}
    {/snippet}
  </Tabs>
</Dialog>
