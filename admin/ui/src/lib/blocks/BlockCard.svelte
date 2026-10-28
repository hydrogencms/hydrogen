<script lang="ts">
  import Fields from '../fields/Fields.svelte';
  import Icon from '../ui/Icon.svelte';
  import Menu, { type MenuItem } from '../ui/Menu.svelte';
  import { ext } from '../extensions.svelte';
  import Preview from '../ext/Preview.svelte';
  import { t } from '../i18n';
  import BlockList from './BlockList.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import { clipboard, type BlocksModel } from './model.svelte';
  import SharedBody from './SharedBody.svelte';
  import { types } from '../types.svelte';
  import { blockPath, blocksFieldOf, canRemove, listAt, propsPath, ruleOfField, sameContainer, slotPath, summary, SHARED, targets, type Block, type Container } from './tree';

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
  const pv = $derived(ext.preview(block.type));
  const sibs = $derived(listAt(model.root, container) ?? []);
  const isShared = $derived(block.type === SHARED);
  const errs = $derived(model.routed.block.get(id) ?? []);
  const errCount = $derived(model.routed.count.get(id) ?? 0);
  const uid = $props.id();
  const dragging = $derived(model.drag?.id === id);

  const item = (key: string, icon: string, rest: Omit<MenuItem, 'label' | 'icon'>): MenuItem => ({ label: t(key), icon, ...rest });

  const menu = $derived.by<MenuItem[]>(() => {
    const removal = canRemove(model.schema, model.root, id);
    const duplicate = model.addable(container, [block.type], [block]);
    const paste = model.pasteState(container);
    const isFirst = index === 0;
    const isLast = index === sibs.length - 1;
    const moveTargets = targets(model.schema, model.root, id, model.label).filter(
      (target) => !(sameContainer(target.container, container) && target.length <= 1),
    );
    const noSharedTypes = isShared || sharedOptions().length === 0;
    const pasteHint = !clipboard.blocks.length ? t('blocks.reason.empty') : paste.ok ? undefined : model.reason(paste);
    const items: MenuItem[] = [
      item('blocks.moveUp', 'arrow-up', { disabled: isFirst, hint: isFirst ? t('blocks.reason.first') : 'Alt+↑', onselect: () => model.nudge(id, -1) }),
      item('blocks.moveDown', 'arrow-down', { disabled: isLast, hint: isLast ? t('blocks.reason.last') : 'Alt+↓', onselect: () => model.nudge(id, 1) }),
      item('blocks.moveTo', 'folder-tree', {
        disabled: moveTargets.length === 0,
        hint: moveTargets.length === 0 ? t('blocks.reason.noTarget') : undefined,
        onselect: () => (model.moving = id),
      }),
      item('blocks.duplicate', 'copy', {
        separator: true,
        disabled: !duplicate.ok,
        hint: duplicate.ok ? undefined : model.reason(duplicate),
        onselect: () => model.duplicate(id),
      }),
      item('blocks.copy', 'copy', { onselect: () => model.copy(id) }),
      item('blocks.pasteAfter', 'copy', { disabled: !paste.ok, hint: pasteHint, onselect: () => model.paste(container, index + 1) }),
    ];
    if (!isShared) {
      items.push(
        item('blocks.saveShared', 'link', {
          separator: true,
          disabled: noSharedTypes,
          hint: noSharedTypes ? t('blocks.reason.noShared') : undefined,
          onselect: () => (model.sharing = id),
        }),
      );
    }
    items.push(
      item('blocks.delete', 'trash', {
        danger: true,
        separator: true,
        disabled: !removal.ok,
        hint: removal.ok ? 'Del' : model.reason(removal),
        onselect: () => model.remove(id),
      }),
    );
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
      {#if pv && !open}
        <span class="h-block__sum h-block__sum--pv" data-block-preview={block.type}>{sum}<Preview tag={pv} type={block.type} props={$state.snapshot(block.props ?? {})} context="card" /></span>
      {:else if sum}<span class="h-block__sum">{sum}</span>{/if}
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
