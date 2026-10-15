<script lang="ts">
  import Button from '../ui/Button.svelte';
  import { t } from '../i18n';
  import BlockCard from './BlockCard.svelte';
  import { clipboard, type BlocksModel } from './model.svelte';
  import Palette from './Palette.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import type { Block, Container } from './tree';

  let {
    model,
    container,
    list,
    path,
    label,
  }: {
    model: BlocksModel;
    container: Container;
    list: Block[];
    /** Form path of this list (`body`, `body[1].slots.items`). */
    path: string;
    label: string;
  } = $props();

  const dropIndex = $derived(model.drag?.drop && (model.drag.drop.container.id ?? '') === (container.id ?? '') && (model.drag.drop.container.slot ?? '') === (container.slot ?? '') ? model.drag.drop.index : -1);
  const paste = $derived(model.pasteState(container));
  const pasteLabel = $derived(clipboard.blocks.length ? blocksCatalog.label(clipboard.blocks[0]!.type) : '');
</script>

<div class="h-blocklist" data-drop-list data-cont-id={container.id ?? ''} data-cont-slot={container.slot ?? ''} data-length={list.length}>
  {#if list.length === 0}
    <div class="h-blocklist__empty" class:h-blocklist__empty--drop={dropIndex === 0}>
      <span class="h-muted">{container.id ? t('blocks.slotEmpty') : t('blocks.empty')}</span>
    </div>
  {/if}
  <ol class="h-blocklist__items" aria-label={label}>
    {#each list as block, i (block.id ?? i)}
      {#if !model.readonly && model.drag === null}
        <li class="h-blocklist__gap">
          <Palette {model} {container} index={i} variant="between" label={blocksCatalog.label(block.type)} />
        </li>
      {:else if dropIndex === i}
        <li class="h-blocklist__drop" aria-hidden="true"></li>
      {/if}
      <BlockCard {model} {block} {container} index={i} {path} />
    {/each}
    {#if dropIndex === list.length && list.length > 0}
      <li class="h-blocklist__drop" aria-hidden="true"></li>
    {/if}
  </ol>
  {#if !model.readonly}
    <div class="h-blocklist__foot">
      <Palette {model} {container} index={list.length} variant="end" {label} />
      {#if clipboard.blocks.length}
        <span title={paste.ok ? '' : model.reason(paste)}>
          <Button size="sm" variant="ghost" icon="copy" disabled={!paste.ok} onclick={() => model.paste(container, list.length)}>{t('blocks.pasteHere', { label: pasteLabel })}</Button>
          {#if !paste.ok}<span class="h-sr">{model.reason(paste)}</span>{/if}
        </span>
      {/if}
    </div>
  {/if}
</div>
