<script lang="ts">
  import Button from '../ui/Button.svelte';
  import Combobox, { type ComboOption } from '../ui/Combobox.svelte';
  import Icon from '../ui/Icon.svelte';
  import Input from '../ui/Input.svelte';
  import Popover from '../ui/Popover.svelte';
  import { api } from '../api/client';
  import { t } from '../i18n';
  import { types } from '../types.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import type { BlocksModel } from './model.svelte';
  import { SHARED, type Container } from './tree';

  let {
    model,
    container,
    index,
    variant = 'end',
    label,
  }: {
    model: BlocksModel;
    container: Container;
    index: number;
    /** `end`: the labelled button after the last block; `between`: the small plus between blocks. */
    variant?: 'end' | 'between';
    /** What the button is about (names the block it sits before, or the slot). */
    label: string;
  } = $props();

  const refusal = $derived(model.addable(container));
  const rule = $derived(model.rules(container));
  const entries = $derived(
    (rule ? blocksCatalog.allowed(rule) : []).map((b) => ({ name: b.name, label: b.label, description: b.description ?? '', icon: b.icon || (b.name === SHARED ? 'link' : 'square') })),
  );
  let open = $state(false);
  let q = $state('');
  let active = $state(0);
  let step = $state<'pick' | 'shared'>('pick');
  let sharedValue = $state<ComboOption | null>(null);
  const uid = $props.id();

  const shown = $derived.by(() => {
    const s = q.trim().toLowerCase();
    return s ? entries.filter((e) => `${e.label} ${e.description} ${e.name}`.toLowerCase().includes(s)) : entries;
  });
  $effect(() => {
    void shown;
    active = 0;
  });
  $effect(() => {
    if (!open) {
      q = '';
      step = 'pick';
      sharedValue = null;
    }
  });

  function choose(name: string, close: () => void) {
    if (name === SHARED) {
      step = 'shared';
      return;
    }
    close();
    model.add(container, index, name);
  }
  function key(e: KeyboardEvent, close: () => void) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (shown.length) active = (active + (e.key === 'ArrowDown' ? 1 : -1) + shown.length) % shown.length;
      document.getElementById(`${uid}-o${active}`)?.scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const s = shown[active];
      if (s) choose(s.name, close);
    }
  }

  async function searchShared(text: string, signal: AbortSignal): Promise<ComboOption[]> {
    const per = await Promise.all(
      blocksCatalog.sharedTo.map((type) =>
        api
          .get('/collections/{type}/items', { params: { type }, query: { q: text, limit: 10, locale: model.locale || undefined } as never, signal })
          .then((r) => r.items.map((i) => ({ value: i.id, label: i.name || i.id, hint: types.get(i.type)?.label ?? i.type }))),
      ),
    );
    return per.flat();
  }
  function pickShared(o: ComboOption | null, close: () => void) {
    if (!o) return;
    close();
    model.addShared(container, index, o.value);
  }
</script>

{#if !refusal.ok && refusal.reason === 'max'}
  {#if variant === 'end'}
    <span title={model.reason(refusal)}>
      <Button size="sm" icon="plus" disabled>{t('blocks.add')}</Button>
      <span class="h-sr">{model.reason(refusal)}</span>
    </span>
  {/if}
{:else if refusal.ok}
  <Popover
    label={variant === 'end' ? t('blocks.add') : t('blocks.addBefore', { label })}
    triggerClass={variant === 'end' ? 'h-pill h-addblock h-addblock--end' : 'h-addblock h-addblock--between'}
    bind:open
  >
    {#snippet trigger()}
      {#if variant === 'end'}
        <Icon name="plus" size={16} />{t('blocks.add')}
      {:else}
        <Icon name="plus" size={14} /><span class="h-sr">{t('blocks.addBefore', { label })}</span>
      {/if}
    {/snippet}
    {#snippet children(close)}
      <div class="h-palette">
        {#if step === 'pick'}
          <Input
            icon="search"
            bind:value={q}
            placeholder={t('blocks.search')}
            aria-label={t('blocks.search')}
            role="combobox"
            aria-expanded="true"
            aria-controls="{uid}-list"
            aria-activedescendant={shown[active] ? `${uid}-o${active}` : undefined}
            autocomplete="off"
            onkeydown={(e: KeyboardEvent) => key(e, close)}
          />
          <ul class="h-palette__list" id="{uid}-list" role="listbox" aria-label={t('blocks.palette')}>
            {#each shown as e, i (e.name)}
              <li
                id="{uid}-o{i}"
                class="h-palette__opt"
                class:h-palette__opt--on={i === active}
                role="option"
                aria-selected={i === active}
                onpointermove={() => (active = i)}
                onclick={() => choose(e.name, close)}
                onkeydown={() => {}}
              >
                <span class="h-block__tile"><Icon name={e.icon} size={16} /></span>
                <span class="h-palette__text">
                  <span class="h-palette__name">{e.label}</span>
                  {#if e.description}<span class="h-palette__desc">{e.description}</span>{/if}
                </span>
              </li>
            {:else}
              <li class="h-palette__none h-muted">{t('blocks.noMatch', { q })}</li>
            {/each}
          </ul>
        {:else}
          <div class="h-palette__shared">
            <Button variant="ghost" size="sm" icon="arrow-left" onclick={() => (step = 'pick')}>{t('blocks.back')}</Button>
            <label class="h-sr" for="{uid}-sh">{t('blocks.sharedPick1')}</label>
            <Combobox
              id="{uid}-sh"
              bind:value={sharedValue}
              search={searchShared}
              placeholder={t('blocks.sharedSearch')}
              clearable={false}
              onselect={(o) => pickShared(o, close)}
            />
          </div>
        {/if}
      </div>
    {/snippet}
  </Popover>
{/if}
