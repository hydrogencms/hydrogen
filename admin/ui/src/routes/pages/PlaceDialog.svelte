<script lang="ts">
  import '../../lib/messages/pages';
  import { t } from '../../lib/i18n';
  import { errorMessage } from '../../lib/errors';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import { siteName } from '../../lib/format';
  import { placeTarget } from './logic';
  import ParentPicker from './ParentPicker.svelte';
  import { placeItem } from './place';

  let {
    open = $bindable(false),
    item,
    sites,
    site,
    locale,
    ondone,
  }: {
    open?: boolean;
    item: { id: string; type: string; name: string };
    sites: string[];
    /** The site offered first. */
    site: string;
    locale: string;
    ondone: (r: { site: string; parent: string; node: string }) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let chosen = $state(site || sites[0] || '');
  let parent = $state('');
  let empty = $state(false);
  let busy = $state(false);
  let error = $state('');
  const target = $derived(placeTarget(chosen, parent, empty));

  async function place() {
    if (!target) return;
    busy = true;
    error = '';
    try {
      const node = await placeItem(target.site, item.id, target.parent);
      open = false;
      ondone({ site: target.site, parent: target.parent ?? '', node });
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open title={t('pages.placeTitle', { name: item.name })} size="md">
  <div class="h-stack">
    {#if sites.length > 1}
      <Field label={t('pages.site')}>
        {#snippet children(c)}<Select {...c} bind:value={chosen} options={sites.map((s) => ({ value: s, label: siteName(s) }))} />{/snippet}
      </Field>
    {/if}
    <div>
      <p class="h-field__label">{t('pages.placeUnder')}</p>
      <ParentPicker site={chosen} {locale} type={item.type} bind:value={parent} bind:empty label={t('pages.placeUnder')} />
    </div>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
  </div>
  {#snippet footer()}
    <Button onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" loading={busy} disabled={!target} onclick={() => void place()}>{t('pages.place')}</Button>
  {/snippet}
</Dialog>
