<script lang="ts">
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { copyText } from '../../lib/errors';

  let { value, warning }: { value: string; warning: string } = $props();
  let copied = $state(false);

  async function copy() {
    if (await copyText(value)) {
      toast.success(t('common.copied'));
      copied = true;
    } else toast.error(t('settings.copyFailed'));
  }
</script>

<div class="h-stack">
  <p class="h-secret__note" role="status"><Icon name="alert-circle" size={16} />{warning}</p>
  <div class="h-secret__box">
    <code class="h-secret" data-testid="secret">{value}</code>
    <Button size="sm" icon={copied ? 'check' : 'copy'} onclick={copy}>{t('common.copy')}</Button>
  </div>
</div>

<style>
  .h-secret__note {
    display: flex;
    align-items: flex-start;
    gap: var(--h-space-2);
    margin: 0;
    padding: var(--h-space-3);
    border-radius: var(--h-radius);
    background: var(--h-warning-soft);
    color: var(--h-warning);
    font-size: var(--h-text-sm);
  }
  .h-secret__note :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .h-secret__box {
    display: flex;
    align-items: center;
    gap: var(--h-space-3);
    padding: var(--h-space-3);
    border: 1px solid var(--h-border-strong);
    border-radius: var(--h-radius);
    background: var(--h-surface-2);
  }
  .h-secret {
    flex: 1;
    min-width: 0;
    font-family: var(--h-mono);
    font-size: var(--h-text-sm);
    overflow-wrap: anywhere;
    user-select: all;
  }
</style>
