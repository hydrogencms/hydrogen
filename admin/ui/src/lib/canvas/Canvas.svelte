<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import '../messages/canvas';
  import './canvas.css';
  import { t } from '../i18n';
  import type { CompiledField } from '../types.svelte';
  import Icon from '../ui/Icon.svelte';
  import IconButton from '../ui/IconButton.svelte';
  import Menu, { type MenuItem } from '../ui/Menu.svelte';
  import { href, link } from '../router.svelte';
  import { blocksCatalog } from '../blocks/catalog.svelte';
  import Palette from '../blocks/Palette.svelte';
  import { blocksModel } from '../blocks/registry';
  import { canMove, canRemove, find, listAt, SHARED, updateBlock, type Block } from '../blocks/tree';
  import type { PreviewHost } from '../preview/host';
  import { dropAt, type CanvasDrop, type LayoutNode } from './drop';
  import { blockPathOf, chainOf, containerOf, locate, parsePath } from './paths';
  import { editableText } from './texts';
  import EmptyAdd from './EmptyAdd.svelte';
  import Toolbar from './Toolbar.svelte';

  let {
    host,
    fields,
    docFields,
    setField,
    readonly = false,
    scale,
    nodes,
    hover = null,
  }: {
    host: PreviewHost;
    fields: CompiledField[];
    /** The working document's top-level values (what the frame shows). */
    docFields: Record<string, unknown>;
    setField: (name: string, value: unknown) => void;
    readonly?: boolean;
    scale: number;
    nodes: LayoutNode[];
    hover?: string | null;
  } = $props();

  interface Sel {
    field: string;
    id?: string;
    prop?: string;
  }
  const TB_H = 36;

  let root = $state<HTMLElement>();
  let rw = $state(0);
  let rh = $state(0);
  let tbw = $state(0);
  let tb = $state<Toolbar>();
  let sel = $state.raw<Sel | null>(null);
  let editing = $state('');
  /** The block an inline edit belongs to: the commit goes to it even if blocks moved meanwhile. */
  let editId: string | undefined;
  /** A prop path the layout has shown for the selection: kept while the frame re-maps it after a change. */
  let stickyProp = $state('');
  let stopDrag: (() => void) | null = null;
  onDestroy(() => stopDrag?.());
  let drag = $state.raw<{ moved: boolean; drop: CanvasDrop | null } | null>(null);
  let scrollSel = false;
  let acting = false;
  let lastRect = $state.raw<[number, number, number, number] | undefined>();

  let insertOpen = $state(false);
  let slotOpen = $state(false);
  let slotName = $state('');
  $effect(() => {
    if (insertOpen || slotOpen) acting = true;
  });

  const blockType = (n: string) => blocksCatalog.get(n);
  const blockFields = $derived(fields.filter((f) => (f.type === 'blocks' || f.shape?.base === 'blocks') && !f.admin?.hidden));
  const byPath = $derived(new Map(nodes.map((n) => [n.path, n])));
  const model = $derived(sel ? blocksModel(sel.field) : undefined);
  const found = $derived(sel?.id && model ? find(model.root, sel.id) : undefined);
  const blockPath = $derived(sel ? (sel.id ? (model ? blockPathOf(sel.field, model.root, sel.id) : undefined) : `fields/${sel.field}`) : undefined);
  const propPath = $derived(blockPath && sel?.prop ? `${blockPath}/props/${sel.prop}` : undefined);
  const selPath = $derived(propPath && (byPath.has(propPath) || propPath === stickyProp) ? propPath : blockPath);
  $effect(() => {
    if (propPath && byPath.has(propPath)) stickyProp = propPath;
  });
  const selNode = $derived(selPath ? byPath.get(selPath) : undefined);
  const isBlock = $derived(!!found && selPath === blockPath);
  const typeDef = $derived(found ? blockType(found.block.type) : undefined);
  const selLabel = $derived.by(() => {
    if (!sel) return '';
    if (isBlock) return blocksCatalog.label(found!.block.type);
    if (sel.prop && typeDef) return typeDef.props.find((p) => p.name === sel!.prop)?.label ?? sel.prop;
    return fields.find((f) => f.name === sel!.field)?.label ?? sel.field;
  });
  const textInfo = $derived(selPath && !isBlock && !readonly ? editableText(fields, blockType, docFields, selPath) : null);
  const isShared = $derived(isBlock && found!.block.type === SHARED);

  // A deleted (or undone) block ends the selection.
  $effect(() => {
    if (sel?.id && model && !found) sel = null;
  });
  $effect(() => {
    if (selNode) lastRect = selNode.rect;
    else if (!sel) lastRect = undefined;
  });
  $effect(() => {
    if (scrollSel && selPath && selNode) {
      scrollSel = false;
      host.scrollTo(selPath);
    }
  });

  // The block a form card selects (or a canvas action adds) becomes the canvas selection.
  const selectedKey = $derived(blockFields.map((f) => `${f.name}=${blocksModel(f.name)?.selected ?? ''}`).join('|'));
  let seen = untrack(() => selectedKey);
  $effect(() => {
    const key = selectedKey;
    const prev = new Map(seen.split('|').map((s) => s.split('=') as [string, string]));
    seen = key;
    for (const s of key.split('|')) {
      const [name, id] = s.split('=') as [string, string];
      if (!id || prev.get(name) === id || !blocksModel(name)?.root.length) continue;
      if (sel?.field === name && sel.id === id) continue;
      sel = { field: name, id };
      scrollSel = true;
      if (acting) {
        acting = false;
        void focusToolbar();
      }
    }
  });

  async function focusToolbar() {
    await tick();
    requestAnimationFrame(() => {
      // Not while editing inline (the double click after a click), nor when the user went elsewhere (a form field).
      const a = document.activeElement;
      if (editing || (a && a !== document.body && a.tagName !== 'IFRAME' && !root?.parentElement?.contains(a))) return;
      tb?.focus();
    });
  }
  const formRow = (name: string) => document.querySelector<HTMLElement>(`.h-item__form [data-field="${CSS.escape(name)}"]`);

  // ---- frame -> form

  export function select(path: string | null, edit: boolean) {
    if (!path) {
      sel = null;
      return;
    }
    const fp = parsePath(path);
    if (!fp || !fields.some((f) => f.name === fp.field)) return;
    if (fp.chain.length) {
      const m = blocksModel(fp.field);
      const id = m && locate(m.root, fp.chain)?.block.id;
      if (!m || !id) return;
      sel = { field: fp.field, id, prop: fp.prop };
      void m.reveal(id, fp.prop);
      if (edit && fp.prop) {
        if (!startEdit(path)) void m.focusProp(id, fp.prop);
        return;
      }
    } else {
      sel = { field: fp.field };
      formRow(fp.field)?.scrollIntoView?.({ block: 'nearest' });
      if (edit && !startEdit(path)) formRow(fp.field)?.querySelector<HTMLElement>('input,textarea,select,button')?.focus();
      if (edit) return;
    }
    acting = true;
    void focusToolbar();
  }

  function startEdit(path: string): boolean {
    const e = readonly ? null : editableText(fields, blockType, docFields, path);
    if (!e) return false;
    editing = path;
    const fp = parsePath(path);
    const m = fp?.chain.length ? blocksModel(fp.field) : undefined;
    editId = m && fp ? locate(m.root, fp.chain)?.block.id : undefined;
    host.editText(path, e.multiline);
    return true;
  }

  export function inlineEdit(path: string, value: string | null) {
    // Only the edit the canvas asked for, written where it was asked (by block id).
    if (!editing || path !== editing) return;
    editing = '';
    const id = editId;
    editId = undefined;
    if (value !== null && !readonly) {
      const fp = parsePath(path);
      if (fp?.chain.length && fp.prop && id) {
        const m = blocksModel(fp.field);
        const b = m && find(m.root, id)?.block;
        if (m && b && b.props?.[fp.prop] !== value) m.commit(updateBlock(m.root, id, (x) => ({ ...x, props: { ...x.props, [fp.prop!]: value } })));
      } else if (fp && !fp.chain.length && docFields[fp.field] !== value) {
        setField(fp.field, value);
      }
    }
    if (sel) {
      acting = true;
      void focusToolbar();
    }
  }

  // ---- form -> frame

  onMount(() => {
    const focusin = (e: FocusEvent) => {
      const target = e.target as HTMLElement | null;
      if (!target?.closest?.('.h-item__form')) return;
      acting = false;
      const card = target.closest<HTMLElement>('[data-block-id]');
      if (card) {
        const field = card.closest<HTMLElement>('[data-blocks]')?.dataset.blocks;
        const id = card.dataset.blockId;
        if (!field || !id || !fields.some((f) => f.name === field)) return;
        let prop: string | undefined;
        for (let n: HTMLElement | null = target.closest<HTMLElement>('[data-field]'); n && card.contains(n); n = n.parentElement?.closest<HTMLElement>('[data-field]') ?? null) {
          const m = /\.props\.([^.[\]]+)$/.exec(n.dataset.field ?? '');
          if (m) {
            prop = m[1];
            break;
          }
        }
        sel = { field, id, prop };
        scrollSel = true;
      } else {
        const name = target.closest<HTMLElement>('[data-field]')?.dataset.field;
        if (name && fields.some((f) => f.name === name) && byPath.has(`fields/${name}`)) {
          sel = { field: name };
          scrollSel = true;
        }
      }
    };
    document.addEventListener('focusin', focusin);
    return () => document.removeEventListener('focusin', focusin);
  });

  // ---- actions

  const announce = (msg: string) => model?.announce(msg);
  const reasonOf = (r: { ok: boolean }) => (r.ok ? undefined : model!.reason(r as never));
  const tip = (reason: string | undefined) => (reason ? { title: reason } : {});

  const parentId = $derived.by(() => {
    if (!sel?.id || !model) return undefined;
    if (!isBlock) return sel.id;
    const chain = chainOf(model.root, sel.id);
    return chain && chain.length > 2 ? locate(model.root, chain.slice(0, -2))?.block.id : undefined;
  });
  function selectParent() {
    if (!sel || !parentId) return;
    sel = { field: sel.field, id: parentId };
    scrollSel = true;
    announce(t('canvas.selected', { label: blocksCatalog.label(find(model!.root, parentId)?.block.type ?? '') }));
  }
  function nudge(d: -1 | 1) {
    if (!isBlock || readonly || !model || !sel?.id) return;
    acting = true;
    const m = model;
    const id = sel.id;
    m.quietly(() => m.nudge(id, d));
    scrollSel = true;
    void focusToolbar();
  }
  const sibs = $derived(found && model ? (listAt(model.root, found.container) ?? []) : []);
  const upReason = $derived(found && found.index === 0 ? t('blocks.reason.first') : undefined);
  const downReason = $derived(found && found.index === sibs.length - 1 ? t('blocks.reason.last') : undefined);
  const dupRefusal = $derived(found && model ? model.addable(found.container, [found.block.type], [found.block]) : { ok: true as const });
  const delRefusal = $derived(found && model && sel?.id ? canRemove(model.schema, model.root, sel.id) : { ok: true as const });

  function duplicate() {
    if (!isBlock || !model || !sel?.id) return;
    acting = true;
    const m = model;
    const id = sel.id;
    m.quietly(() => m.duplicate(id));
  }
  function remove() {
    if (!isBlock || !model || !sel?.id) return;
    const next = parentId ? { field: sel.field, id: parentId } : null;
    model.remove(sel.id);
    if (!find(model.root, sel.id)) {
      sel = next;
      acting = true;
      if (next) void focusToolbar();
      else root?.parentElement?.focus();
    }
  }
  function showInForm() {
    if (!sel) return;
    if (sel.id && model) {
      if (sel.prop && !isBlock) void model.focusProp(sel.id, sel.prop);
      else {
        void model.reveal(sel.id);
        void model.focusBlock(sel.id);
      }
    } else formRow(sel.field)?.querySelector<HTMLElement>('input,textarea,select,button')?.focus();
  }

  const blockNodes = $derived(nodes.filter((n) => n.block !== undefined && parsePath(n.path)?.prop === undefined && !!parsePath(n.path)?.chain.length));
  function step(d: -1 | 1) {
    const i = blockNodes.findIndex((n) => n.path === blockPath);
    const to = blockNodes[i < 0 ? (d > 0 ? 0 : blockNodes.length - 1) : i + d];
    if (to) {
      select(to.path, false);
      scrollSel = true;
    }
  }

  function escape() {
    sel = null;
    acting = false;
    model?.announce(t('canvas.cleared'));
    root?.parentElement?.focus();
  }

  function onkey(e: KeyboardEvent, el: HTMLElement) {
    const mod = e.metaKey || e.ctrlKey;
    if (e.altKey && !mod) {
      const d = e.key === 'ArrowUp' || e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : 0;
      if (d) {
        e.preventDefault();
        nudge(d);
      }
      return;
    }
    if (e.altKey) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      escape();
    } else if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && !e.defaultPrevented && !mod) {
      e.preventDefault();
      step(e.key === 'ArrowUp' ? -1 : 1);
    } else if (e.key === 'Enter' && e.target === el) {
      e.preventDefault();
      showInForm();
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && isBlock && !readonly) {
      e.preventDefault();
      remove();
    } else if (mod && e.key.toLowerCase() === 'd' && isBlock && !readonly) {
      e.preventDefault();
      duplicate();
    }
  }

  const slotItems = $derived.by<MenuItem[]>(() => {
    if (!found || !model || !typeDef?.slots?.length) return [];
    return typeDef.slots.map((s) => {
      const r = model!.addable({ id: found!.block.id, slot: s.name });
      return { label: t('canvas.slotItem', { label: s.label }), disabled: !r.ok, hint: reasonOf(r), onselect: () => ((slotName = s.name), (slotOpen = true)) };
    });
  });
  const slotContainer = $derived({ id: found?.block.id, slot: slotName || typeDef?.slots?.[0]?.name });
  const slotIndex = $derived(model ? (listAt(model.root, slotContainer)?.length ?? 0) : 0);

  // ---- drag

  function startDrag(e: PointerEvent) {
    if (readonly || !isBlock || !model || !sel?.id || !root || !blockPath || e.button !== 0) return;
    e.preventDefault();
    const id = sel.id;
    const field = sel.field;
    const m = model;
    const path = blockPath;
    const x0 = e.clientX;
    const y0 = e.clientY;
    let cx = x0;
    let cy = y0;
    let raf = 0;
    drag = { moved: false, drop: null };
    const allowed = (list: string) => {
      const c = containerOf(m.root, list);
      return !!c && c.field === field && canMove(m.schema, m.root, id, c.container).ok;
    };
    const update = () => {
      if (!drag?.moved || !root) return;
      const r = root.getBoundingClientRect();
      const drop = dropAt(nodes, path, (cx - r.left) / scale, (cy - r.top) / scale, allowed);
      if (JSON.stringify(drop) !== JSON.stringify(drag.drop)) drag = { moved: true, drop };
    };
    const loop = () => {
      if (!root) return;
      const r = root.getBoundingClientRect();
      const ly = cy - r.top;
      if (ly < 48) host.scrollTo({ by: -16 });
      else if (ly > r.height - 48) host.scrollTo({ by: 16 });
      update();
      raf = requestAnimationFrame(loop);
    };
    const move = (ev: PointerEvent) => {
      // Released outside the window: the up event never came.
      if (ev.buttons === 0) return end(false);
      cx = ev.clientX;
      cy = ev.clientY;
      if (drag && !drag.moved && Math.hypot(cx - x0, cy - y0) >= 4) {
        drag = { moved: true, drop: null };
        raf = requestAnimationFrame(loop);
      }
      update();
    };
    const end = (commit: boolean) => {
      stopDrag = null;
      removeEventListener('pointermove', move);
      removeEventListener('pointerup', up);
      removeEventListener('pointercancel', cancel);
      removeEventListener('keydown', key, true);
      cancelAnimationFrame(raf);
      const d = drag;
      drag = null;
      const c = commit && d?.moved && d.drop ? containerOf(m.root, d.drop.list) : undefined;
      if (c && d?.drop) m.quietly(() => m.moveToContainer(id, c.container, d.drop!.index, m.label));
      acting = true;
      void focusToolbar();
    };
    const up = () => end(true);
    const cancel = () => end(false);
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') {
        ev.stopPropagation();
        end(false);
      }
    };
    stopDrag = () => end(false);
    addEventListener('pointermove', move);
    addEventListener('pointerup', up);
    addEventListener('pointercancel', cancel);
    addEventListener('keydown', key, true);
  }

  // ---- drawing

  const box = (r: [number, number, number, number]) => ({ x: r[0] * scale, y: r[1] * scale, w: r[2] * scale, h: r[3] * scale });
  const hoverNode = $derived(hover && hover !== selPath && !drag ? byPath.get(hover) : undefined);
  const hoverLabel = $derived.by(() => {
    if (!hover) return '';
    const fp = parsePath(hover);
    if (!fp) return '';
    const m = fp.chain.length ? blocksModel(fp.field) : undefined;
    const b = m && fp.chain.length ? locate(m.root, fp.chain)?.block : undefined;
    if (b && fp.prop) return blockType(b.type)?.props.find((p) => p.name === fp.prop)?.label ?? fp.prop;
    if (b) return blocksCatalog.label(b.type);
    return fields.find((f) => f.name === fp.field)?.label ?? fp.field;
  });
  const tbRect = $derived(selNode?.rect ?? lastRect);
  const tbPos = $derived.by(() => {
    if (!tbRect) return null;
    const b = box(tbRect);
    let y = b.y - TB_H - 4 >= 0 ? b.y - TB_H - 4 : b.y + 4;
    y = Math.max(0, Math.min(y, Math.max(0, rh - TB_H)));
    const x = Math.max(0, Math.min(b.x, Math.max(0, rw - tbw)));
    return { x, y };
  });
  const emptyFields = $derived(readonly ? [] : blockFields.filter((f) => blocksModel(f.name) && !blocksModel(f.name)!.root.length));
</script>

<div bind:this={root} bind:clientWidth={rw} bind:clientHeight={rh} class="h-canvas" data-canvas-on>
  {#if hoverNode}
    {@const b = box(hoverNode.rect)}
    <div class="h-canvas__hover" style:left="{b.x}px" style:top="{b.y}px" style:width="{b.w}px" style:height="{b.h}px">
      {#if hoverLabel}<span class="h-canvas__tag">{hoverLabel}</span>{/if}
    </div>
  {/if}
  {#if selPath && (selNode || (lastRect && selPath === stickyProp))}
    {@const b = box(selNode?.rect ?? lastRect!)}
    <div class="h-canvas__sel" data-path={selPath} style:left="{b.x}px" style:top="{b.y}px" style:width="{b.w}px" style:height="{b.h}px"></div>
  {/if}
  {#if drag?.drop}
    {@const b = box(drag.drop.line)}
    <div class="h-canvas__drop" style:left="{b.x}px" style:top="{b.y}px" style:width="{b.w}px" style:height="{b.h}px"></div>
  {/if}

  {#if sel && tbPos}
    <Toolbar
      bind:this={tb}
      bind:width={tbw}
      label={isBlock ? t('canvas.block', { label: selLabel }) : t('canvas.field', { label: selLabel })}
      x={tbPos.x}
      y={tbPos.y}
      hidden={!!editing || !!drag?.moved}
      onkeydown={onkey}
    >
      <span class="h-canvas__tblabel" aria-hidden="true">{selLabel}</span>
      {#if isBlock && !readonly}
        <IconButton class="h-canvas__handle" icon="grip-vertical" size="sm" label={t('canvas.drag')} aria-describedby="h-canvas-draghint" onpointerdown={startDrag} onclick={() => announce(t('canvas.dragHint'))} />
        <span id="h-canvas-draghint" class="h-sr">{t('canvas.dragHint')}</span>
      {/if}
      <IconButton icon="arrow-up-right" size="sm" label={t('canvas.parent')} disabled={!parentId} {...tip(parentId ? undefined : t('canvas.noParent'))} onclick={selectParent} />
      {#if isBlock && !readonly && model && found}
        <IconButton icon="arrow-up" size="sm" label={t('blocks.moveUp')} disabled={!!upReason} {...tip(upReason)} onclick={() => nudge(-1)} />
        <IconButton icon="arrow-down" size="sm" label={t('blocks.moveDown')} disabled={!!downReason} {...tip(downReason)} onclick={() => nudge(1)} />
        <Palette {model} container={found.container} index={found.index + 1} variant="canvas" label={t('canvas.insertAfter')} bind:open={insertOpen} />
        {#if slotItems.length}
          <Menu label={t('canvas.addInside')} items={slotItems} align="start" triggerClass="h-iconbutton h-iconbutton--ghost h-iconbutton--sm">
            {#snippet trigger()}<Icon name="folder-tree" size={16} /><span class="h-sr">{t('canvas.addInside')}</span>{/snippet}
          </Menu>
          <Palette {model} container={slotContainer} index={slotIndex} variant="canvas" anchor label={t('canvas.addInside')} bind:open={slotOpen} />
        {/if}
        <IconButton icon="copy" size="sm" label={t('blocks.duplicate')} disabled={!dupRefusal.ok} {...tip(reasonOf(dupRefusal))} onclick={duplicate} />
        <IconButton icon="trash" size="sm" variant="danger" label={t('blocks.delete')} disabled={!delRefusal.ok} {...tip(reasonOf(delRefusal))} onclick={remove} />
        {#if isShared}
          <a class="h-iconbutton h-iconbutton--ghost h-iconbutton--sm" use:link href={href(`/items/${String(found.block.props?.item ?? '')}`)} aria-label={t('canvas.editShared')} title={t('canvas.editShared')}>
            <Icon name="external-link" size={16} />
          </a>
          <IconButton icon="layers" size="sm" label={t('canvas.detach')} onclick={() => void model.detach(found.block.id!)} />
        {/if}
      {/if}
      {#if textInfo}
        <IconButton icon="edit" size="sm" label={t('canvas.editText')} onclick={() => selPath && startEdit(selPath)} />
      {/if}
      <IconButton icon="panel-left" size="sm" label={t('canvas.showInForm')} onclick={showInForm} />
    </Toolbar>
  {/if}

  {#if emptyFields.length}
    <div class="h-canvas__empties">
      {#each emptyFields as f (f.name)}
        {@const m = blocksModel(f.name)}
        {#if m}
          <EmptyAdd model={m} label={t('canvas.emptyAdd', { label: f.label })} onopen={() => (acting = true)} />
        {/if}
      {/each}
    </div>
  {/if}

  {#if drag}<div class="h-canvas__glass"></div>{/if}
</div>
