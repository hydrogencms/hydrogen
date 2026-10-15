<script lang="ts">
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';

  let {
    open = $bindable(false),
    title,
    initial = '',
    onsubmit,
  }: { open?: boolean; title: string; initial?: string; onsubmit: (name: string) => void } = $props();

  let name = $state('');
  $effect(() => {
    if (open) name = initial;
  });
  function submit() {
    const n = name.trim();
    if (!n) return;
    open = false;
    onsubmit(n);
  }
</script>

<Dialog bind:open {title} size="sm">
  <form
    id="h-viewname"
    onsubmit={(e) => {
      e.preventDefault();
      submit();
    }}
  >
    <Field label={t('collections.viewName')} description={t('collections.viewNameHelp')}>
      {#snippet children(c)}<Input {...c} bind:value={name} />{/snippet}
    </Field>
  </form>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="h-viewname" variant="primary" disabled={!name.trim()}>{t('common.save')}</Button>
  {/snippet}
</Dialog>
