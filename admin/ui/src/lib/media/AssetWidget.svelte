<script lang="ts">
  import { t } from '../i18n';
  import '../messages/media';
  import { useFields } from '../fields/context';
  import type { CompiledField } from '../types.svelte';
  import { itemApi } from '../../routes/item/model';
  import { href, link } from '../router.svelte';
  import Button from '../ui/Button.svelte';
  import Badge from '../ui/Badge.svelte';
  import AssetPicker from './AssetPicker.svelte';
  import Thumb from './Thumb.svelte';
  import { dimensions, fileOf, formatSize, type FileValue } from './logic';
  import './media.css';

  // The `asset` field: a reference to an item of an upload type, with a thumbnail and the library as its picker.
  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
    attrs = {},
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
    attrs?: Record<string, unknown>;
  } = $props();

  const ctx = useFields();
  const id = $derived(typeof value === 'string' ? value : typeof value?.id === 'string' ? (value.id as string) : '');
  const names = $derived(((field.shape.to ?? field.options?.to) as string[] | undefined) ?? []);
  let open = $state(false);
  let info = $state<{ id: string; name: string; file?: FileValue; status: string } | null>(null);
  let missing = $state(false);

  $effect(() => {
    const want = id;
    const loc = ctx.locale;
    if (!want) {
      info = null;
      missing = false;
      return;
    }
    const ctl = new AbortController();
    itemApi
      .read({ kind: 'item', id: want }, loc, ctl.signal)
      .then((v) => {
        info = { id: want, name: v.name || want, file: fileOf(v.fields.file), status: v.status };
        missing = false;
      })
      .catch(() => {
        if (!ctl.signal.aborted) missing = true;
      });
    return () => ctl.abort();
  });

  function chosen(a: { id: string; name: string; file?: FileValue; status?: string }) {
    value = a.id;
    info = { id: a.id, name: a.name, file: a.file, status: a.status ?? 'draft' };
    ctx.clearError(path);
  }
  const shown = $derived(info && info.id === id ? info : null);
</script>

<div class="h-assetfield" role="group" aria-label={(attrs['aria-label'] as string | undefined) ?? field.label}>
  {#if id}
    <a class="h-assetfield__chip" use:link href={href(`/items/${id}`)}>
      <Thumb file={shown?.file} />
      <span class="h-assetfield__text">
        <span class="h-assetfield__name">{shown?.name ?? id}</span>
        {#if shown?.file}<span class="h-muted">{[dimensions(shown.file), formatSize(shown.file.size)].filter(Boolean).join(' · ')}</span>{/if}
        {#if missing}<span class="h-field__error">{t('media.assetGone')}</span>{/if}
      </span>
      {#if shown && shown.status !== 'published' && shown.status !== 'changed'}<Badge tone="warning">{t('media.unpublishedAsset')}</Badge>{/if}
    </a>
  {:else}
    <span class="h-muted">{t('media.none')}</span>
  {/if}
  {#if !readonly}
    <span class="h-assetfield__ops">
      <Button size="sm" icon="image" onclick={() => (open = true)}>{t('media.choose')}</Button>
      {#if id}<Button size="sm" variant="ghost" onclick={() => { value = undefined; ctx.clearError(path); }}>{t('media.remove')}</Button>{/if}
    </span>
  {/if}
</div>

{#if !readonly}<AssetPicker bind:open {names} current={id} onselect={chosen} />{/if}
