<script lang="ts">
  import Icon from '../ui/Icon.svelte';
  import { thumbPath, objectPosition, previewKind, type FileValue } from './logic';

  // A thumbnail: the image itself (cropped around its focal point) or a file icon (also when the image fails to load).
  let { file, cover = true }: { file: FileValue | undefined; cover?: boolean } = $props();
  const kind = $derived(previewKind(file?.mime));
  const ext = $derived(file ? (file.name.includes('.') ? file.name.slice(file.name.lastIndexOf('.') + 1).toUpperCase().slice(0, 5) : '') : '');
  let failed = $state('');
  const image = $derived(!!file && kind === 'image' && failed !== file.id);
</script>

<span class="h-thumb" class:h-thumb--contain={!cover}>
  {#if file && image}
    <img src={thumbPath(file)} alt="" loading="lazy" decoding="async" draggable="false" style:object-position={objectPosition(file.focal)} onerror={() => (failed = file?.id ?? '')} />
  {:else}
    <span class="h-thumb__icon">
      <Icon name={kind === 'pdf' ? 'file-text' : kind === 'video' || kind === 'audio' ? 'layout' : 'file'} size={28} />
      {#if ext}<span class="h-thumb__ext">{ext}</span>{/if}
    </span>
  {/if}
</span>
