<script lang="ts">
  import { t } from '../i18n';
  import '../messages/media';
  import Button from '../ui/Button.svelte';
  import type { Uploads } from './uploads.svelte';
  import './media.css';

  // The upload list of one queue. The status line is the only live region: it stays in the page (empty until
  // something is queued) and says "2 of 5 uploaded, 1 failed"; the rows and progress bars are not announced.
  let { uploads, clearable = false, label = t('media.uploads') }: { uploads: Uploads; clearable?: boolean; label?: string } = $props();
</script>

<div class="h-uploads" role="group" aria-label={label}>
  <p class="h-uploads__status" role="status" aria-live="polite">{uploads.summary}</p>
  {#if uploads.entries.length}
    <ul>
      {#each uploads.entries as u (u.id)}
        <li class="h-uploads__item h-uploads__item--{u.state}">
          <span class="h-uploads__name">{u.name}</span>
          <progress value={u.progress} max="1" aria-label={t('media.progress', { name: u.name })}></progress>
          <span class="h-uploads__msg">{u.message}</span>
          {#if u.state === 'queued' || u.state === 'uploading'}
            <Button size="sm" variant="ghost" aria-label={t('media.cancelUpload', { name: u.name })} onclick={() => uploads.cancel(u.id)}>{t('media.cancel')}</Button>
          {/if}
        </li>
      {/each}
    </ul>
    {#if clearable && !uploads.busy}<Button size="sm" variant="ghost" onclick={() => uploads.clearFinished()}>{t('media.clearFinished')}</Button>{/if}
  {/if}
</div>
