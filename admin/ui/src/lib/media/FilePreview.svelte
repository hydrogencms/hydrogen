<script lang="ts">
  import { t } from '../i18n';
  import '../messages/media';
  import Icon from '../ui/Icon.svelte';
  import FocalPicker from './FocalPicker.svelte';
  import { assetPath, previewKind, type FileValue, type Focal } from './logic';

  // A file by type: images carry the focal point picker, video and audio play, a PDF links, the rest shows an icon.
  let { file, readonly = false, onfocal }: { file: FileValue | undefined; readonly?: boolean; onfocal?: (f: Focal | undefined) => void } = $props();
  const kind = $derived(previewKind(file?.mime));
</script>

<div class="h-filepreview">
  {#if file && kind === 'image'}
    <FocalPicker {file} value={file.focal} {readonly} onchange={onfocal} />
  {:else if file && kind === 'video'}
    <!-- svelte-ignore a11y_media_has_caption -->
    <video controls preload="metadata" src={assetPath(file)}></video>
  {:else if file && kind === 'audio'}
    <audio controls preload="metadata" src={assetPath(file)}></audio>
  {:else if file && kind === 'pdf'}
    <a class="h-filepreview__file" href={assetPath(file)} target="_blank" rel="noopener"><Icon name="file-text" size={28} />{t('media.openPdf')}</a>
  {:else}
    <p class="h-filepreview__file h-muted"><Icon name="file" size={28} />{file ? t('media.noPreview') : t('media.noFile')}</p>
  {/if}
</div>
