<script lang="ts">
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { copyText } from '../../lib/errors';

  let { value, warning }: { value: string; warning: string } = $props();

  async function copy() {
    if (await copyText(value)) toast.success(t('common.copied'));
    else toast.error(t('settings.copyFailed'));
  }
</script>

<div class="h-stack">
  <p class="h-alert" role="status">{warning}</p>
  <div class="h-row">
    <code class="h-secret" data-testid="secret">{value}</code>
    <Button icon="copy" onclick={copy}>{t('common.copy')}</Button>
  </div>
</div>

<style>
  .h-secret {
    flex: 1;
    padding: var(--h-space-2) var(--h-space-3);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
    overflow-wrap: anywhere;
    user-select: all;
  }
</style>
