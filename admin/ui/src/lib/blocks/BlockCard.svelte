<script lang="ts">
  import Fields from '../fields/Fields.svelte';
  import Icon from '../ui/Icon.svelte';
  import Menu, { type MenuItem } from '../ui/Menu.svelte';
  import { t } from '../i18n';
  import BlockList from './BlockList.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import { clipboard, type BlocksModel } from './model.svelte';
  import SharedBody from './SharedBody.svelte';
  import { types } from '../types.svelte';
  import { blockPath, blocksFieldOf, canRemove, listAt, propsPath, ruleOfField, slotPath, summary, SHARED, targets, type Block, type Container } from './tree';

  let {
    model,
    block,
    container,
    index,
    path,
  }: {
    model: BlocksModel;
    block: Block;
    container: Container;
    index: number;
    /** Form path of the list this block is in. */
    path: string;
  } = $props();

  const def = $derived(blocksCatalog.get(block.type));
  const id = $derived(block.id ?? '');
  const mine = $derived(blockPath(path, index));
  const open = $derived(!!model.open[id]);
  const label = $derived(def?.label ?? block.type);
  const sum = $derived(summary(block, def));
  const sibs = $derived(listAt(model.root, container) ?? []);
  const isShared = $derived(block.type === SHARED);
  const errs = $derived(model.routed.block.get(id) ?? []);
  const errCount = $derived(model.routed.count.get(id) ?? 0);
  const uid = $props.id();
  const dragging = $derived(model.drag?.id === id);

  const menu = $derived.by<MenuItem[]>(() => {
    const rm = canRemove(model.schema, model.root, id);
    const dup = model.addable(container, [block.type], [block]);
    const moves = targets(model.schema, model.root, id, model.label).filter((x) => !(x.container.id === container.id && (x.container.slot ?? '') === (container.slot ?? '') && x.length <= 1));
    const paste = model.pasteState(container);
    const sharedTypes = isShared ? [] : sharedOptions();
    const items: MenuItem[] = [
      { label: t('blocks.moveUp'), icon: 'arrow-up', disabled: index === 0, hint: index === 0 ? t('blocks.reason.first') : 'Alt+↑', onselect: () => model.nudge(id, -1) },
      { label: t('blocks.moveDown'), icon: 'arrow-down', disabled: index === sibs.length - 1, hint: index === sibs.length - 1 ? t('blocks.reason.last') : 'Alt+↓', onselect: () => model.nudge(id, 1) },
      { label: t('blocks.moveTo'), icon: 'folder-tree', disabled: moves.length === 0, hint: moves.length === 0 ? t('blocks.reason.noTarget') : undefined, onselect: () => (model.moving = id) },
      { label: t('blocks.duplicate'), icon: 'copy', separator: true, disabled: !dup.ok, hint: dup.ok ? undefined : model.reason(dup), onselect: () => model.duplicate(id) },
      { label: t('blocks.copy'), icon: 'copy', onselect: () => model.copy(id) },
      { label: t('blocks.pasteAfter'), icon: 'copy', disabled: !paste.ok, hint: clipboard.blocks.length ? (paste.ok ? undefined : model.reason(paste)) : t('blocks.reason.empty'), onselect: () => model.paste(container, index + 1) },
    ];
    if (!isShared) {
      items.push({
        label: t('blocks.saveShared'),
        icon: 'link',
        separator: true,
        disabled: sharedTypes.length === 0,
        hint: sharedTypes.length === 0 ? t('blocks.reason.noShared') : undefined,
        onselect: () => (model.sharing = id),
      });
    }
    items.push({ label: t('blocks.delete'), icon: 'trash', danger: true, separator: true, disabled: !rm.ok, hint: rm.ok ? 'Del' : model.reason(rm), onselect: () => model.remove(id) });
    return items;
  });

  /** Blockset types whose blocks field accepts this block (the dialog lists them). */
  function sharedOptions(): string[] {
    return blocksCatalog.sharedTo.length ? sharedTypesFor(block.type) : [];
  }
  function sharedTypesFor(blockType: string): string[] {
    return blocksCatalog.sharedTo.filter((tn) => {
      const f = blocksFieldOf(types.get(tn)?.fields ?? []);
      return !!f && ruleOfField(f).allow.includes(blockType);
    });
  }

  function key(e: KeyboardEvent) {
    if (model.readonly) return;
    if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
      e.preventDefault();
      model.nudge(id, e.key === 'ArrowUp' ? -1 : 1);
    } else if (e.key === 'Delete' && !e.altKey && !e.ctrlKey && !e.metaKey) {
      e.preventDefault();
      model.remove(id);
    }
  }
</script>

<li
  class="h-block"
  class:h-block--sel={model.selected === id}
  class:h-block--drag={dragging}
  class:h-block--err={errCount > 0}
  data-block-id={id}
  data-block-type={block.type}
  data-cont-id={container.id ?? ''}
  data-cont-slot={container.slot ?? ''}
  data-index={index}
  aria-label={t('blocks.block', { n: index + 1, label })}
>
  <div class="h-block__head">
    {#if !model.readonly}
      <button
        type="button"
        class="h-block__grip"
        aria-label={t('blocks.drag', { label })}
        title={t('blocks.dragHint')}
        onpointerdown={(e) => model.startDrag(e, id)}
        onkeydown={key}
      >
        <Icon name="grip-vertical" size={16} />
      </button>
    {/if}
    <span class="h-block__tile"><Icon name={blocksCatalog.icon(block.type)} size={16} /></span>
    <button
      type="button"
      class="h-block__toggle"
      aria-expanded={open}
      aria-controls="{uid}-body"
      onclick={() => {
        model.select(id);
        model.toggle(id);
      }}
      onkeydown={key}
    >
      <span class="h-block__label">{label}</span>
      {#if sum}<span class="h-block__sum">{sum}</span>{/if}
      <Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} />
    </button>
    {#if errCount > 0}<span class="h-block__errbadge" role="status">{errCount === 1 ? t('blocks.error1') : t('blocks.errors', { n: errCount })}</span>{/if}
    {#if !model.readonly}
      <Menu items={menu} label={t('blocks.menu', { label })} />
    {/if}
  </div>
  {#if open}
    <div class="h-block__body" id="{uid}-body">
      {#if !def}
        <p class="h-alert" role="alert">{t('blocks.unknown', { type: block.type })}</p>
      {:else if isShared}
        <SharedBody {model} {block} {errCount} />
      {:else}
        {#if errs.length}
          <ul class="h-block__errors" role="alert" aria-label={t('blocks.blockErrors')}>
            {#each errs as [p, m] (p)}<li><Icon name="alert-circle" size={14} />{m}</li>{/each}
          </ul>
        {/if}
        {#if def.props.length && block.props}
          <div class="h-fieldcard">
            <div class="h-fieldgrid">
              <Fields fields={def.props} bind:doc={block.props} path={propsPath(mine)} />
            </div>
          </div>
        {/if}
        {#each def.slots ?? [] as sl (sl.name)}
          {@const sp = slotPath(mine, sl.name)}
          {@const list = block.slots?.[sl.name] ?? []}
          {@const serr = model.routed.slot.get(`${id}/${sl.name}`) ?? []}
          <section class="h-block__slot" aria-label={sl.label}>
            <header class="h-block__slothead">
              <h4>{sl.label}</h4>
              <span class="h-muted">
                {#if sl.min && sl.max}{t('blocks.slotRange', { min: sl.min, max: sl.max })}{:else if sl.min}{t('blocks.slotMin', { min: sl.min })}{:else if sl.max}{t('blocks.slotMax', { max: sl.max })}{/if}
              </span>
            </header>
            {#if sl.description}<p class="h-field__desc">{sl.description}</p>{/if}
            {#each serr as m (m)}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{m}</p>{/each}
            {#if sl.min && list.length < sl.min && !serr.length}<p class="h-muted h-block__need">{t('blocks.slotNeeds', { min: sl.min })}</p>{/if}
            <BlockList {model} container={{ id, slot: sl.name }} {list} path={sp} label={`${label}: ${sl.label}`} />
          </section>
        {/each}
      {/if}
    </div>
  {/if}
</li>
