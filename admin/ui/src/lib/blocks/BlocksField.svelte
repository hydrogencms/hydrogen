<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import '../messages/blocks';
  import Button from '../ui/Button.svelte';
  import Icon from '../ui/Icon.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import { t } from '../i18n';
  import type { CompiledField } from '../types.svelte';
  import { useFields } from '../fields/context';
  import BlockList from './BlockList.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import { BlocksModel } from './model.svelte';
  import MoveDialog from './MoveDialog.svelte';
  import SaveSharedDialog from './SaveSharedDialog.svelte';
  import { ruleOfField, type Block } from './tree';
  import './blocks.css';

  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
  }: {
    field: CompiledField;
    value: Block[] | undefined;
    path: string;
    readonly?: boolean;
  } = $props();

  const ctx = useFields();
  const model = new BlocksModel({
    get: () => value ?? [],
    set: (v) => (value = v),
    rule: () => ruleOfField(field),
    path: () => path,
    label: () => field.label,
    locale: () => ctx.locale,
    readonly: () => readonly || ctx.readonly,
    ctx,
  });

  let failed = $state(false);
  onMount(() => {
    blocksCatalog.load().catch(() => (failed = true));
  });

  // Blocks with problems open themselves, so the message is visible.
  $effect(() => {
    const ids = [...model.routed.count.keys()];
    untrack(() => {
      for (const id of ids) model.open[id] = true;
    });
  });

  const rootErrors = $derived([...model.routed.root, ...(ctx.error(path) && !model.routed.root.includes(ctx.error(path)!) ? [ctx.error(path)!] : [])]);
</script>

<fieldset class="h-fieldset h-blocks" disabled={readonly} data-blocks={path}>
  <legend class="h-fieldset__legend">
    {field.label}{#if field.required}<span class="h-field__req" aria-hidden="true">*</span>{/if}
  </legend>
  {#if field.description}<p class="h-field__desc">{field.description}</p>{/if}
  {#each rootErrors as m (m)}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{m}</p>{/each}
  {#if model.routed.orphan.length}
    <ul class="h-block__errors" role="alert" aria-label={t('blocks.orphanErrors')}>
      {#each model.routed.orphan as [p, m] (p)}<li><strong>{p}</strong>: {m}</li>{/each}
    </ul>
  {/if}
  {#if failed}
    <p class="h-alert" role="alert">{t('blocks.loadFailed')}</p>
  {:else if !blocksCatalog.loaded}
    <div role="status" aria-busy="true" aria-label={t('blocks.loading')}><Skeleton lines={3} /></div>
  {:else}
    <div class="h-blocks__bar">
      <span class="h-muted h-num">{model.total === 1 ? t('blocks.count1') : t('blocks.count', { n: model.total })}</span>
      <span class="h-spacer"></span>
      {#if model.total > 0}
        <Button size="sm" variant="ghost" onclick={() => model.setAll(!model.allOpen)}>{model.allOpen ? t('blocks.collapseAll') : t('blocks.expandAll')}</Button>
      {/if}
    </div>
    <BlockList {model} container={{}} list={value ?? []} {path} label={field.label} />
    <MoveDialog {model} />
    <SaveSharedDialog {model} />
  {/if}
  <p class="h-sr" role="status" aria-live="polite">{model.status}</p>
</fieldset>
