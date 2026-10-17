<script lang="ts">
  import { t } from '../i18n';
  import '../messages/media';
  import { useFields } from '../fields/context';
  import { types, type CompiledField } from '../types.svelte';
  import Button from '../ui/Button.svelte';
  import Icon from '../ui/Icon.svelte';
  import FilePreview from './FilePreview.svelte';
  import { assetPath, checkFile, dimensions, fileOf, formatSize, maxUpload, parseSize, type Focal } from './logic';
  import { uploadFile } from './upload';
  import './media.css';

  // The built-in `file` field of an upload type: preview, focal point, download and replace.
  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
  } = $props();

  const ctx = useFields();
  const file = $derived(fileOf(value));
  let input = $state<HTMLInputElement>();
  let busy = $state(false);
  let problem = $state('');
  const spec = $derived(ctx.type ? types.get(ctx.type)?.upload : undefined);

  async function chosen(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    const f = el.files?.[0];
    el.value = '';
    if (!f) return;
    problem = '';
    const bad = checkFile(spec, f, maxUpload());
    if (bad) {
      problem = bad === 'size' ? t('media.fileTooBig', { max: formatSize(Math.min(parseSize(spec?.max_size) ?? Infinity, maxUpload() ?? Infinity)) }) : t('media.fileNotAccepted');
      return;
    }
    busy = true;
    try {
      const up = await uploadFile(f, { type: ctx.type });
      value = up.file;
      ctx.clearError(path);
    } catch (err) {
      problem = err instanceof Error ? err.message : t('media.failed');
    } finally {
      busy = false;
    }
  }
  function focal(f: Focal | undefined) {
    if (file) value = { ...file, focal: f };
  }
</script>

<div class="h-filefield" role="group" aria-label={field.label}>
  {#if file}
    <FilePreview {file} {readonly} onfocal={focal} />
    <p class="h-filefield__meta">
      <strong>{file.name}</strong>
      <span class="h-muted">{[file.mime, formatSize(file.size), dimensions(file)].filter(Boolean).join(' · ')}</span>
    </p>
  {:else}
    <p class="h-muted">{t('media.noFile')}</p>
  {/if}
  <div class="h-filefield__ops">
    {#if !readonly}
      <Button size="sm" icon="upload" loading={busy} onclick={() => input?.click()}>{file ? t('media.replace') : t('media.uploadFile')}</Button>
      <input bind:this={input} hidden type="file" accept={spec?.accept?.join(',')} onchange={chosen} />
    {/if}
    {#if file}<a class="h-button h-button--secondary h-button--sm" href={assetPath(file)} download={file.name}><Icon name="download" size={14} />{t('media.download')}</a>{/if}
  </div>
  {#if problem}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{problem}</p>{/if}
</div>
