<script lang="ts">
  import { toast } from './toast.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../i18n';
  const icon = { info: 'info', success: 'check-circle', error: 'alert-circle' } as const;
</script>

<div class="h-toasts" role="region" aria-label={t('toast.region')}>
  {#each toast.items as it (it.id)}
    <div class="h-toast h-toast--{it.kind}" role={it.kind === 'error' ? 'alert' : 'status'}>
      <span class="h-toast__icon"><Icon name={icon[it.kind]} size={18} /></span>
      <span class="h-toast__text">{it.text}</span>
      <button class="h-toast__close" type="button" aria-label={t('toast.dismiss')} onclick={() => toast.dismiss(it.id)}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/each}
</div>
