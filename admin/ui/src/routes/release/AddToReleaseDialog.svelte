<script lang="ts">
  import '../../lib/messages/releases';
  import { ApiError } from '../../lib/api/client';
  import { errorMessage } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import { href, link } from '../../lib/router.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { releaseApi } from './api';
  import { editable, type Release } from './logic';

  let {
    open = $bindable(false),
    item,
    itemName,
    locales = [],
    onadded,
  }: {
    open?: boolean;
    item: string;
    itemName: string;
    /** Locales to publish with (empty: the server's default). */
    locales?: string[];
    onadded?: () => void;
  } = $props();

  const NEW = '__new';
  let releases = $state<Release[] | null>(null);
  let choice = $state('');
  let newName = $state('');
  let busy = $state(false);
  let error = $state('');
  let failed = $state(false);

  $effect(() => {
    if (!open) return;
    error = '';
    failed = false;
    releases = null;
    newName = '';
    const ctl = new AbortController();
    releaseApi
      .list({ status: 'open,failed', kind: 'release', limit: 200 }, ctl.signal)
      .then((r) => {
        releases = r.items.filter((x) => x.kind !== 'item' && editable(x.status));
        choice = releases[0]?.id ?? NEW;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        if (e instanceof ApiError && e.status >= 400 && e.status < 500) error = errorMessage(e);
        failed = true;
        releases = [];
        choice = NEW;
      });
    return () => ctl.abort();
  });

  async function add() {
    error = '';
    if (choice === NEW && !newName.trim()) {
      error = t('releases.nameRequired');
      return;
    }
    busy = true;
    try {
      let id = choice;
      const created = choice === NEW;
      if (created) id = (await releaseApi.create({ name: newName.trim() })).id;
      try {
        await releaseApi.putItem(id, item, { action: 'publish', ...(locales.length ? { locales } : {}) });
      } catch (e) {
        // Don't leave an empty release behind.
        if (created) await releaseApi.remove(id).catch(() => {});
        throw e;
      }
      open = false;
      toast.success(t('releases.added', { name: itemName }));
      onadded?.();
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open title={t('releases.addTitle')} size="sm" dismissible={!busy}>
  <form
    id="release-add"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void add();
    }}
  >
    <p>{t('releases.addText', { name: itemName })}</p>
    {#if releases === null}
      <p class="h-muted">{t('app.loading')}</p>
    {:else}
      <Field label={t('releases.release')}>
        {#snippet children(c)}
          <Select
            {...c}
            bind:value={choice}
            options={[...(releases ?? []).map((r) => ({ value: r.id, label: r.name })), { value: NEW, label: t('releases.newOption') }]}
          />
        {/snippet}
      </Field>
      {#if choice === NEW}
        <Field label={t('releases.name')} required>
          {#snippet children(c)}<Input {...c} bind:value={newName} />{/snippet}
        </Field>
      {/if}
    {/if}
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
    {#if failed}<p class="h-muted"><a use:link href={href('/releases')}>{t('nav.releases')}</a></p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" disabled={busy} onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="release-add" variant="primary" loading={busy} disabled={releases === null}>{t('releases.add')}</Button>
  {/snippet}
</Dialog>
