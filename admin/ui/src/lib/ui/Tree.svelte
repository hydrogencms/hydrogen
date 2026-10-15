<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { dropPosition, flatten, isDescendant, type DropPosition, type TreeNode } from './tree';
  import { t } from '../i18n';

  let {
    nodes,
    label,
    expanded = $bindable(new Set<string>()),
    selected = $bindable<string | undefined>(undefined),
    loadChildren,
    onactivate,
    onselect,
    onmove,
    canDrop,
    onmoverequest,
    row,
  }: {
    /** Root nodes. */
    nodes: TreeNode[];
    /** Accessible name of the tree. */
    label: string;
    expanded?: Set<string>;
    selected?: string;
    /** Called when a node with hasChildren and no children is expanded; the result is cached until reload(). */
    loadChildren?: (node: TreeNode) => Promise<TreeNode[]>;
    /** Enter or double click. */
    onactivate?: (node: TreeNode) => void;
    /** Click or Space. */
    onselect?: (node: TreeNode) => void;
    /** Dragging is enabled when set. Pointer drag; the keyboard alternative is onmoverequest. */
    onmove?: (move: { node: TreeNode; target: TreeNode; position: DropPosition }) => void;
    canDrop?: (node: TreeNode, target: TreeNode, position: DropPosition) => boolean;
    /** Key "m" (or the context menu key) on a row: open your "Move…" dialog. */
    onmoverequest?: (node: TreeNode) => void;
    /** Custom row content after the chevron. */
    row?: Snippet<[TreeNode]>;
  } = $props();

  let loaded = $state(new Map<string, TreeNode[]>());
  let loading = $state(new Set<string>());
  let focusId = $state<string>();
  let tree = $state<HTMLElement>();
  let drag = $state<{ id: string; x: number; y: number; active: boolean } | null>(null);
  let drop = $state<{ id: string; position: DropPosition } | null>(null);

  const childrenOf = (n: TreeNode) => n.children ?? loaded.get(n.id);
  const flat = $derived(flatten(nodes, expanded, childrenOf));
  const tabId = $derived(flat.some((f) => f.node.id === focusId) ? focusId : flat.some((f) => f.node.id === selected) ? selected : flat[0]?.node.id);

  /** Drop the cached children of a node (or all) and load them again if it is open. */
  export async function reload(id?: string) {
    const next = new Map(loaded);
    if (id) next.delete(id);
    else next.clear();
    loaded = next;
    const ids = id ? [id] : [...expanded];
    for (const i of ids) {
      const f = flat.find((x) => x.node.id === i);
      if (f && expanded.has(i)) await ensureLoaded(f.node);
    }
  }

  async function ensureLoaded(n: TreeNode) {
    if (n.children || loaded.has(n.id) || !n.hasChildren || !loadChildren) return;
    loading = new Set(loading).add(n.id);
    try {
      const kids = await loadChildren(n);
      loaded = new Map(loaded).set(n.id, kids);
    } finally {
      const l = new Set(loading);
      l.delete(n.id);
      loading = l;
    }
  }

  async function open(n: TreeNode) {
    await ensureLoaded(n);
    expanded = new Set(expanded).add(n.id);
  }
  function close(n: TreeNode) {
    const e = new Set(expanded);
    e.delete(n.id);
    expanded = e;
  }
  const toggle = (n: TreeNode) => (expanded.has(n.id) ? close(n) : open(n));

  function focusItem(id: string | undefined) {
    if (!id) return;
    focusId = id;
    queueMicrotask(() => {
      const el = [...(tree?.querySelectorAll<HTMLElement>('[role=treeitem]') ?? [])].find((e) => e.dataset.id === id);
      el?.focus();
    });
  }

  function keydown(e: KeyboardEvent) {
    const el = (e.target as HTMLElement).closest<HTMLElement>('[role=treeitem]');
    if (!el || e.altKey || e.ctrlKey || e.metaKey) return;
    const i = flat.findIndex((f) => f.node.id === el.dataset.id);
    if (i < 0) return;
    const cur = flat[i];
    let handled = true;
    switch (e.key) {
      case 'ArrowDown':
        focusItem(flat[i + 1]?.node.id);
        break;
      case 'ArrowUp':
        focusItem(flat[i - 1]?.node.id);
        break;
      case 'Home':
        focusItem(flat[0].node.id);
        break;
      case 'End':
        focusItem(flat[flat.length - 1].node.id);
        break;
      case 'ArrowRight':
        if (cur.expandable && !cur.open) void open(cur.node);
        else if (cur.open) focusItem(flat[i + 1]?.node.id);
        break;
      case 'ArrowLeft':
        if (cur.open) close(cur.node);
        else focusItem(cur.parent);
        break;
      case 'Enter':
        onactivate?.(cur.node);
        break;
      case ' ':
        selected = cur.node.id;
        onselect?.(cur.node);
        break;
      case 'm':
      case 'M':
        if (!onmoverequest) handled = false;
        else onmoverequest(cur.node);
        break;
      case 'ContextMenu':
        if (!onmoverequest) handled = false;
        else onmoverequest(cur.node);
        break;
      case 'Escape':
        if (drag) cancelDrag();
        else handled = false;
        break;
      default:
        handled = false;
    }
    if (handled) e.preventDefault();
  }

  function click(n: TreeNode) {
    if (suppress) return;
    selected = n.id;
    focusId = n.id;
    onselect?.(n);
  }

  // Pointer drag: starts after 4px of movement from a row that is not a button.
  let suppress = false;
  function down(e: PointerEvent, n: TreeNode) {
    if (!onmove || e.button !== 0 || n.disabled || (e.target as HTMLElement).closest('button,a,input')) return;
    drag = { id: n.id, x: e.clientX, y: e.clientY, active: false };
    addEventListener('pointermove', move);
    addEventListener('pointerup', up, { once: true });
  }
  function move(e: PointerEvent) {
    if (!drag) return;
    if (!drag.active && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 4) return;
    drag.active = true;
    const target = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('[role=treeitem]');
    const id = target?.dataset.id;
    drop = null;
    if (!target || !id || !tree?.contains(target) || id === drag.id || isDescendant(flat, id, drag.id)) return;
    const position = dropPosition(e.clientY, target.getBoundingClientRect());
    const from = flat.find((f) => f.node.id === drag!.id)!.node;
    const to = flat.find((f) => f.node.id === id)!.node;
    if (to.disabled || (canDrop && !canDrop(from, to, position))) return;
    drop = { id, position };
  }
  function up() {
    removeEventListener('pointermove', move);
    const d = drag;
    const target = drop;
    drag = null;
    drop = null;
    if (d?.active) {
      suppress = true;
      setTimeout(() => (suppress = false));
      if (target) {
        const node = flat.find((f) => f.node.id === d.id)!.node;
        const to = flat.find((f) => f.node.id === target.id)!.node;
        onmove?.({ node, target: to, position: target.position });
      }
    }
  }
  function cancelDrag() {
    removeEventListener('pointermove', move);
    drag = null;
    drop = null;
  }
</script>

<ul bind:this={tree} class="h-tree" class:h-tree--dragging={drag?.active} role="tree" aria-label={label} onkeydown={keydown}>
  {#each flat as f (f.node.id)}
    {@const n = f.node}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <li
      class="h-tree__item"
      class:h-tree__item--sel={selected === n.id}
      class:h-tree__item--drag={drag?.active && drag.id === n.id}
      class:h-tree__item--before={drop?.id === n.id && drop.position === 'before'}
      class:h-tree__item--after={drop?.id === n.id && drop.position === 'after'}
      class:h-tree__item--inside={drop?.id === n.id && drop.position === 'inside'}
      role="treeitem"
      data-id={n.id}
      aria-level={f.level}
      aria-setsize={f.size}
      aria-posinset={f.pos}
      aria-expanded={f.expandable ? f.open : undefined}
      aria-selected={selected === n.id}
      aria-disabled={n.disabled || undefined}
      aria-busy={loading.has(n.id) || undefined}
      tabindex={tabId === n.id ? 0 : -1}
      onfocus={() => (focusId = n.id)}
      onclick={() => click(n)}
      ondblclick={() => onactivate?.(n)}
      onpointerdown={(e) => down(e, n)}
    >
      <span class="h-tree__row" style:padding-inline-start="{(f.level - 1) * 1.25}rem">
        {#if f.expandable}
          <button type="button" class="h-tree__twisty" tabindex="-1" aria-hidden="true" onclick={(e) => { e.stopPropagation(); void toggle(n); }}>
            <Icon name={f.open ? 'chevron-down' : 'chevron-right'} size={14} />
          </button>
        {:else}<span class="h-tree__twisty"></span>{/if}
        {#if row}{@render row(n)}{:else}
          {#if n.icon}<Icon name={n.icon} size={16} />{/if}
          <span class="h-tree__label">{n.label}</span>
        {/if}
        {#if loading.has(n.id)}<span class="h-spinner" aria-hidden="true"></span>{/if}
        {#if onmoverequest}
          <button type="button" class="h-tree__move" tabindex="-1" aria-hidden="true" title={t('tree.move')} onclick={(e) => { e.stopPropagation(); onmoverequest(n); }}>
            <Icon name="grip" size={14} />
          </button>
        {/if}
      </span>
    </li>
  {/each}
</ul>
