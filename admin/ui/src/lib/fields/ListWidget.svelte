<script lang="ts">
  import { tick } from 'svelte';
  import Button from '../ui/Button.svelte';
  import Icon from '../ui/Icon.svelte';
  import IconButton from '../ui/IconButton.svelte';
  import type { CompiledField } from '../types.svelte';
  import { t } from '../i18n';
  import GroupWidget from './GroupWidget.svelte';
  import Scalar from './Scalar.svelte';
  import { useFields } from './context';
  import { emptyValue, joinPath, listAdd, listMove, listRemove } from './logic';

  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
  } = $props();

  const ctx = useFields();
  const of = $derived(field.of!);
  const items = $derived(Array.isArray(value) ? (value as unknown[]) : []);
  const isGroup = $derived(of.shape.base === 'group' && !!of.fields);
  const error = $derived(ctx.error(path));
  const uid = $props.id();
  let status = $state('');

  // A group item is named by its first filled-in text value ("Why join"), else "Item N".
  function itemLabel(i: number): string {
    const v = items[i];
    if (isGroup && v && typeof v === 'object') {
      for (const f of of.fields ?? []) {
        const s = (v as Record<string, unknown>)[f.name];
        if (typeof s === 'string' && s.trim()) return s.trim().length > 60 ? s.trim().slice(0, 59) + '…' : s.trim();
      }
    }
    return t('collections.itemN', { n: i + 1 });
  }

  function add() {
    value = listAdd(value, emptyValue(of));
    ctx.clearError(path);
    void focusItem(items.length);
  }
  function remove(i: number) {
    value = listRemove(items, i);
    status = t('collections.removed', { label: itemLabel(i) });
    void focusItem(Math.min(i, items.length - 1), 'add');
  }
  function move(i: number, d: number) {
    const to = i + d;
    if (to < 0 || to >= items.length) return;
    value = listMove(items, i, d);
    status = t('collections.moved', { label: field.label, n: to + 1, total: items.length });
    void focusItem(to, d < 0 ? 'up' : 'down');
  }
  async function focusItem(i: number, what: 'up' | 'down' | 'add' | 'first' = 'first') {
    await tick();
    const row = document.getElementById(`${uid}-${i}`);
    const sel = what === 'up' ? '[data-act=up]' : what === 'down' ? '[data-act=down]' : what === 'add' ? '[data-act=add]' : 'input,select,textarea';
    const found = row?.querySelector<HTMLElement>(sel) ?? (what === 'first' ? row?.querySelector<HTMLElement>('button') : null);
    const el = (what === 'add' && !row ? document.getElementById(`${uid}-add`) : found) ?? document.getElementById(`${uid}-add`);
    el?.focus();
  }
  function key(e: KeyboardEvent, i: number) {
    if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    e.preventDefault();
    move(i, e.key === 'ArrowUp' ? -1 : 1);
  }
</script>

<fieldset class="h-fieldset h-list-widget" disabled={readonly}>
  <legend class="h-fieldset__legend">
    {field.label}{#if field.required}<span class="h-field__req" aria-hidden="true">*</span>{/if}
  </legend>
  {#if field.description}<p class="h-field__desc">{field.description}</p>{/if}
  {#if error}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{error}</p>{/if}
  <ol class="h-list-widget__items">
    {#each items as _, i (i)}
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <li class="h-list-widget__item" class:h-list-widget__item--scalar={!isGroup} id="{uid}-{i}" role="group" aria-label={itemLabel(i)} onkeydown={(e) => key(e, i)}>
        {#if isGroup}
          <div class="h-list-widget__bar">
            {#if !readonly}<span class="h-list-widget__grip" aria-hidden="true" title={t('collections.reorderHint')}><Icon name="grip-vertical" size={16} /></span>{/if}
            <span class="h-list-widget__n">{itemLabel(i)}</span>
            {@render ops(i)}
          </div>
          <div class="h-list-widget__body">
            <GroupWidget field={of} bind:value={value[i]} path={joinPath(path, i)} {readonly} bare />
          </div>
        {:else}
          {#if !readonly}<span class="h-list-widget__grip" aria-hidden="true" title={t('collections.reorderHint')}><Icon name="grip-vertical" size={16} /></span>{/if}
          <div class="h-list-widget__body">
            <Scalar field={of} bind:value={value[i]} path={joinPath(path, i)} {readonly} ariaLabel={itemLabel(i)} />
            {#if ctx.error(joinPath(path, i))}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{ctx.error(joinPath(path, i))}</p>{/if}
          </div>
          {@render ops(i)}
        {/if}
      </li>
    {/each}
  </ol>
  {#if !readonly}
    <div class="h-list-widget__foot">
      <Button data-act="add" id="{uid}-add" size="sm" icon="plus" onclick={add}>{t('collections.addItem', { label: field.label })}</Button>
      <span class="h-muted h-list-widget__hint">{t('collections.reorderHint')}</span>
    </div>
  {/if}
  <p class="h-sr" role="status" aria-live="polite">{status}</p>
</fieldset>

{#snippet ops(i: number)}
  {#if !readonly}
    <div class="h-list-widget__ops">
      <IconButton data-act="up" icon="arrow-up" label={t('collections.moveUp', { label: itemLabel(i) })} size="sm" disabled={i === 0} onclick={() => move(i, -1)} />
      <IconButton data-act="down" icon="arrow-down" label={t('collections.moveDown', { label: itemLabel(i) })} size="sm" disabled={i === items.length - 1} onclick={() => move(i, 1)} />
      <IconButton icon="trash" label={t('collections.removeItem', { label: itemLabel(i) })} size="sm" onclick={() => remove(i)} />
    </div>
  {/if}
{/snippet}
