<script lang="ts">
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import { t } from '../../lib/i18n';

  let {
    open = $bindable(false),
    title,
    message,
    confirm,
    danger = true,
    onconfirm,
  }: {
    open?: boolean;
    title: string;
    message: string;
    confirm: string;
    danger?: boolean;
    /** Runs on confirm; the dialog closes when it resolves (and stays open when it throws). */
    onconfirm: () => Promise<void>;
  } = $props();
  let busy = $state(false);

  async function go() {
    busy = true;
    try {
      await onconfirm();
      open = false;
    } catch {
      // the caller reported it
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open {title} size="sm" dismissible={!busy}>
  <p>{message}</p>
  {#snippet footer()}
    <Button onclick={() => (open = false)} disabled={busy}>{t('common.cancel')}</Button>
    <Button variant={danger ? 'danger' : 'primary'} loading={busy} onclick={go}>{confirm}</Button>
  {/snippet}
</Dialog>
