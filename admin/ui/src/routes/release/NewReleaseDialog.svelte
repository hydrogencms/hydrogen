<script lang="ts">
  import '../../lib/messages/releases';
  import { ApiError } from '../../lib/api/client';
  import { errorMessage } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Textarea from '../../lib/ui/Textarea.svelte';
  import { releaseApi } from './api';
  import type { Release } from './logic';

  let { open = $bindable(false), oncreated }: { open?: boolean; oncreated: (r: Release) => void | Promise<void> } = $props();

  let name = $state('');
  let description = $state('');
  let busy = $state(false);
  let error = $state('');
  let nameError = $state('');

  $effect(() => {
    if (open) {
      name = '';
      description = '';
      error = '';
      nameError = '';
    }
  });

  async function create() {
    nameError = '';
    error = '';
    if (!name.trim()) {
      nameError = t('releases.nameRequired');
      return;
    }
    busy = true;
    try {
      const r = await releaseApi.create({ name: name.trim(), description: description.trim() || undefined });
      open = false;
      await oncreated(r);
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      const f = e.fields?.find((x) => x.path === 'name');
      if (f) nameError = f.message;
      else error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open title={t('releases.new')} size="sm" dismissible={!busy}>
  <form
    id="release-new"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}
  >
    <Field label={t('releases.name')} error={nameError} required>
      {#snippet children(c)}<Input {...c} bind:value={name} />{/snippet}
    </Field>
    <Field label={t('releases.description')}>
      {#snippet children(c)}<Textarea {...c} rows={3} bind:value={description} />{/snippet}
    </Field>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" disabled={busy} onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="release-new" variant="primary" loading={busy}>{t('common.create')}</Button>
  {/snippet}
</Dialog>
