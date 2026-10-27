<script lang="ts">
  import '../../lib/messages/pages';
  import { onMount } from 'svelte';
  import { api } from '../../lib/api/client';
  import { channels } from '../../lib/channels.svelte';
  import { t } from '../../lib/i18n';
  import { siteName } from '../../lib/format';
  import Card from '../../lib/ui/Card.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import { placeTarget } from './logic';
  import ParentPicker from './ParentPicker.svelte';

  // The create screen's "where does this page go" card. `target` is what to place the new item at, or null.
  let {
    type,
    target = $bindable(null),
  }: {
    type: string;
    target?: { site: string; parent?: string } | null;
  } = $props();

  let sites = $state<string[]>([]);
  let loaded = $state(false);
  let on = $state(true);
  let site = $state('');
  let parent = $state('');
  let empty = $state(false);

  const languages = $derived(channels.active?.languages ?? [...new Set(channels.items.flatMap((c) => c.languages))]);
  const picked = $derived(on ? placeTarget(site, parent, empty) : null);
  $effect(() => {
    target = picked;
  });

  onMount(async () => {
    try {
      await channels.load();
      sites = (await api.get('/pages/sites')).items;
      site = sites[0] ?? '';
    } catch {
      sites = [];
    }
    loaded = true;
  });
</script>

{#if loaded && sites.length}
  <Card title={t('pages.placeCardTitle')}>
    <div class="h-stack">
      <Switch bind:checked={on} label={t('pages.placeNow')} />
      {#if on}
        {#if sites.length > 1}
          <Field label={t('pages.site')}>
            {#snippet children(c)}<Select {...c} bind:value={site} options={sites.map((s) => ({ value: s, label: siteName(s) }))} />{/snippet}
          </Field>
        {/if}
        <div>
          <p class="h-field__label">{t('pages.placeUnder')}</p>
          <ParentPicker {site} locale={languages[0] ?? ''} {type} bind:value={parent} bind:empty label={t('pages.placeUnder')} />
        </div>
      {:else}
        <p class="h-muted">{t('pages.placeLater')}</p>
      {/if}
    </div>
  </Card>
{/if}
