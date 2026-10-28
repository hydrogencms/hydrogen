import { tick } from 'svelte';
import { t } from '../i18n';
import { toast } from '../ui/toast.svelte';
import type { FieldCtx } from '../fields/context';
import { chainOf, formPath } from '../canvas/paths';
import { blocksCatalog } from './catalog.svelte';
import { readShared } from './shared';
import {
  canInsert,
  canMove,
  canRemove,
  cloneBlock,
  countBlocks,
  describeRefusal,
  find,
  insertAt,
  listAt,
  moveTo,
  newBlock,
  removeById,
  reorder,
  routeErrors,
  SHARED,
  walk,
  type Block,
  type Container,
  type Refusal,
  type Rule,
  type Schema,
} from './tree';

/** Copied blocks, kept in memory for the session (pasting makes copies with new ids). */
class Clipboard {
  blocks = $state.raw<Block[]>([]);
}
export const clipboard = new Clipboard();

export interface ModelOptions {
  get(): Block[];
  set(v: Block[]): void;
  rule(): Rule;
  /** The form path of the field (`body`). */
  path(): string;
  label(): string;
  locale(): string;
  readonly(): boolean;
  ctx: FieldCtx;
}

export interface Drop {
  container: Container;
  index: number;
}

const SCROLL_EDGE = 64;
const FOCUSABLE =
  'input:not([type=hidden]):not(:disabled), textarea:not(:disabled), select:not(:disabled), [contenteditable=""], ' +
  '[contenteditable="true"], button:not(:disabled), [tabindex]:not([tabindex="-1"])';

const typeRefusal: Refusal = { ok: false, reason: 'type' };

const containerOf = (id: string | undefined, slot: string | undefined): Container => (id ? { id, slot } : {});

const dropOfCard = (card: HTMLElement, y: number): Drop => {
  const rect = card.getBoundingClientRect();
  const index = Number(card.dataset.index);
  return { container: containerOf(card.dataset.contId, card.dataset.contSlot), index: y < rect.top + rect.height / 2 ? index : index + 1 };
};

const dropOfZone = (zone: HTMLElement): Drop => ({
  container: containerOf(zone.dataset.contId, zone.dataset.contSlot),
  index: Number(zone.dataset.length ?? 0),
});

/** State and operations of one blocks field: selection, open cards, drag and drop, clipboard, undo. */
export class BlocksModel {
  open = $state<Record<string, boolean>>({});
  selected = $state('');
  status = $state('');
  drag = $state<{ id: string; drop: Drop | null } | null>(null);
  moving = $state<string | null>(null);
  sharing = $state<string | null>(null);

  #o: ModelOptions;
  #shared = new Map<string, Promise<Awaited<ReturnType<typeof readShared>>>>();

  constructor(o: ModelOptions) {
    this.#o = o;
  }

  get root(): Block[] {
    const v = this.#o.get();
    return Array.isArray(v) ? v : [];
  }
  get path() {
    return this.#o.path();
  }
  get label() {
    return this.#o.label();
  }
  get locale() {
    return this.#o.locale();
  }
  get readonly() {
    return this.#o.readonly();
  }
  get ctx() {
    return this.#o.ctx;
  }
  get rule(): Rule {
    return this.#o.rule();
  }
  schema: Schema = $derived.by(() => blocksCatalog.schema(this.#o.rule()));
  routed = $derived.by(() => routeErrors(this.schema, this.root, this.path, this.#o.ctx.errors?.() ?? {}));
  total = $derived(countBlocks(this.root));
  allOpen = $derived.by(() => {
    let all = true;
    walk(this.root, (b) => {
      if (b.id && !this.open[b.id]) all = false;
    });
    return all;
  });

  // ---- reading

  rules(c: Container): Rule | undefined {
    return !c.id ? this.rule : this.schema.block(find(this.root, c.id)?.block.type ?? '')?.slots?.find((s) => s.name === c.slot);
  }
  reason(r: Refusal) {
    return describeRefusal(r, t);
  }

  // ---- state

  commit(next: Block[]) {
    this.#o.set(next);
    this.#o.ctx.clearError(this.path);
  }
  announce(msg: string) {
    this.status = msg;
  }
  toggle(id: string) {
    this.open[id] = !this.open[id];
  }
  select(id: string) {
    this.selected = id;
  }
  setAll(open: boolean) {
    const next: Record<string, boolean> = {};
    if (open) walk(this.root, (b) => b.id && (next[b.id] = true));
    this.open = next;
  }
  #quiet = false;
  /** Runs an operation without moving the focus into the form (canvas actions keep it on the canvas). */
  quietly<T>(fn: () => T): T {
    this.#quiet = true;
    try {
      return fn();
    } finally {
      this.#quiet = false;
    }
  }
  async focusBlock(id: string, sel = '.h-block__toggle') {
    if (this.#quiet) return;
    await tick();
    document.querySelector<HTMLElement>(`[data-block-id="${id}"] > .h-block__head ${sel}`)?.focus();
  }

  /** Opens the card and its ancestors, selects it and scrolls it (or the prop's row) into view. Focus stays put. */
  async reveal(id: string, prop?: string): Promise<void> {
    const f = find(this.root, id);
    if (!f) return;
    this.open[id] = true;
    for (let c = f.container; c.id; c = find(this.root, c.id)?.container ?? {}) this.open[c.id] = true;
    this.selected = id;
    await tick();
    const card = document.querySelector<HTMLElement>(`[data-block-id="${id}"]`);
    const selector = prop ? this.#propSelector(id, prop) : undefined;
    const target = selector ? (card?.querySelector<HTMLElement>(selector) ?? card) : card;
    target?.scrollIntoView?.({ block: 'nearest' });
  }

  /** The attribute selector of a prop's form row, or undefined when the block is not in the tree. */
  #propSelector(id: string, prop: string): string | undefined {
    const chain = chainOf(this.root, id);
    if (!chain) return undefined;
    const path = formPath({ field: this.path, chain, prop }).replace(/(["\\])/g, '\\$1');
    return `[data-field="${path}"]`;
  }

  /** Reveals the block and focuses the first control of the prop's row. */
  async focusProp(id: string, prop: string): Promise<void> {
    await this.reveal(id, prop);
    const selector = this.#propSelector(id, prop);
    if (!selector) return;
    const row = document.querySelector<HTMLElement>(`[data-block-id="${id}"] ${selector}`);
    row?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
  }

  // ---- insert / delete / copy

  addable(c: Container, extra: string[] = [], added?: Block[]): Refusal {
    return canInsert(this.schema, this.root, c, extra, { added });
  }

  add(c: Container, index: number, type: string, props?: Record<string, unknown>): Block | undefined {
    const def = blocksCatalog.get(type);
    if (!def) return;
    const ok = canInsert(this.schema, this.root, c, [type]);
    if (!ok.ok) {
      toast.error(this.reason(ok));
      return;
    }
    const b = newBlock(def);
    if (props) b.props = { ...b.props, ...props };
    this.commit(insertAt(this.root, c, index, b));
    this.open[b.id!] = true;
    this.selected = b.id!;
    this.announce(t('blocks.added', { label: def.label }));
    void this.focusBlock(b.id!);
    return b;
  }

  remove(id: string) {
    const f = find(this.root, id);
    if (!f) return;
    const ok = canRemove(this.schema, this.root, id);
    if (!ok.ok) {
      toast.error(this.reason(ok));
      return;
    }
    const label = blocksCatalog.label(f.block.type);
    const copy = JSON.parse(JSON.stringify(f.block)) as Block;
    this.commit(removeById(this.root, id));
    this.announce(t('blocks.deleted', { label }));
    toast.push('info', t('blocks.deleted', { label }), 8000, {
      label: t('blocks.undo'),
      run: () => {
        const home = listAt(this.root, f.container) ? f.container : undefined;
        const ok = home ? canInsert(this.schema, this.root, home, [copy.type], { added: [copy] }) : typeRefusal;
        if (!home || !ok.ok) {
          toast.error(t('blocks.restoreFailed', { label, reason: this.reason(ok) }));
          return;
        }
        this.commit(insertAt(this.root, home, f.index, copy));
        this.selected = copy.id ?? '';
        this.announce(t('blocks.restored', { label }));
      },
    });
  }

  duplicate(id: string) {
    const f = find(this.root, id);
    if (!f) return;
    const ok = canInsert(this.schema, this.root, f.container, [f.block.type], { added: [f.block] });
    if (!ok.ok) {
      toast.error(this.reason(ok));
      return;
    }
    const c = cloneBlock(f.block);
    this.commit(insertAt(this.root, f.container, f.index + 1, c));
    this.open[c.id!] = true;
    this.selected = c.id!;
    this.announce(t('blocks.duplicated', { label: blocksCatalog.label(f.block.type) }));
    void this.focusBlock(c.id!);
  }

  copy(id: string) {
    const f = find(this.root, id);
    if (!f) return;
    clipboard.blocks = [JSON.parse(JSON.stringify(f.block))];
    const label = blocksCatalog.label(f.block.type);
    toast.info(t('blocks.copied', { label }));
    this.announce(t('blocks.copied', { label }));
  }

  pasteState(c: Container): Refusal {
    if (!clipboard.blocks.length) return typeRefusal;
    const types = clipboard.blocks.map((b) => b.type);
    return canInsert(this.schema, this.root, c, types, { added: clipboard.blocks });
  }

  paste(c: Container, index: number) {
    const ok = this.pasteState(c);
    if (!ok.ok) {
      toast.error(this.reason(ok));
      return;
    }
    const copies = clipboard.blocks.map((b) => cloneBlock(b));
    this.commit(insertAt(this.root, c, index, ...copies));
    for (const b of copies) this.open[b.id!] = true;
    this.selected = copies[0]?.id ?? '';
    this.announce(t('blocks.pasted', { label: blocksCatalog.label(copies[0]!.type) }));
    void this.focusBlock(copies[0]!.id!);
  }

  // ---- move

  nudge(id: string, delta: -1 | 1) {
    const f = find(this.root, id);
    const next = f && reorder(this.root, id, delta);
    if (!f || !next) return;
    this.commit(next);
    const len = listAt(next, f.container)?.length ?? 0;
    this.announce(t('blocks.moved', { label: blocksCatalog.label(f.block.type), n: f.index + delta + 1, total: len }));
    void this.focusBlock(id);
  }

  moveToContainer(id: string, c: Container, index?: number, target = '') {
    const f = find(this.root, id);
    if (!f) return false;
    const ok = canMove(this.schema, this.root, id, c);
    if (!ok.ok) {
      toast.error(this.reason(ok));
      return false;
    }
    const next = moveTo(this.root, id, c, index ?? listAt(this.root, c)?.length ?? 0);
    if (!next) return false;
    this.commit(next);
    this.selected = id;
    this.announce(t('blocks.movedTo', { label: blocksCatalog.label(f.block.type), target }));
    void this.focusBlock(id);
    return true;
  }

  // ---- drag and drop (pointer events; the keyboard has Alt+arrows and "Move to…")

  startDrag(e: PointerEvent, id: string) {
    if (this.readonly || e.button !== 0) return;
    e.preventDefault();
    const x0 = e.clientX;
    const y0 = e.clientY;
    let y = y0;
    let raf = 0;
    const handle = e.currentTarget as HTMLElement;
    const pid = e.pointerId;
    try {
      handle.setPointerCapture(pid);
    } catch {
      /* not capturable (tests) */
    }
    const scroller = scrollParent(handle);
    const tick_ = () => {
      if (!this.drag?.id) return;
      const r = scroller === document.scrollingElement ? { top: 0, bottom: innerHeight } : scroller.getBoundingClientRect();
      if (y < r.top + SCROLL_EDGE) scroller.scrollBy(0, -12);
      else if (y > r.bottom - SCROLL_EDGE) scroller.scrollBy(0, 12);
      raf = requestAnimationFrame(tick_);
    };
    const move = (ev: PointerEvent) => {
      y = ev.clientY;
      if (!this.drag) {
        if (Math.hypot(ev.clientX - x0, ev.clientY - y0) < 4) return;
        this.drag = { id, drop: null };
        raf = requestAnimationFrame(tick_);
      }
      this.drag.drop = this.dropAt(id, ev.clientX, ev.clientY);
    };
    const end = (commit: boolean) => {
      removeEventListener('pointermove', move);
      removeEventListener('pointerup', up);
      removeEventListener('pointercancel', cancel);
      removeEventListener('keydown', key, true);
      cancelAnimationFrame(raf);
      const d = this.drag;
      this.drag = null;
      try {
        handle.releasePointerCapture(pid);
      } catch {
        /* ignore */
      }
      if (commit && d?.drop) {
        const label = blocksCatalog.label(find(this.root, id)?.block.type ?? '');
        const next = moveTo(this.root, id, d.drop.container, d.drop.index);
        if (next && next !== this.root) {
          this.commit(next);
          this.selected = id;
          this.announce(t('blocks.movedTo', { label, target: this.label }));
          void this.focusBlock(id);
        }
      }
    };
    const up = () => end(true);
    const cancel = () => end(false);
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') {
        ev.stopPropagation();
        end(false);
      }
    };
    addEventListener('pointermove', move);
    addEventListener('pointerup', up);
    addEventListener('pointercancel', cancel);
    addEventListener('keydown', key, true);
  }

  /** The drop position under the pointer, or null when the block may not go there. */
  dropAt(id: string, x: number, y: number): Drop | null {
    const el = document.elementFromPoint(x, y);
    const card = el?.closest<HTMLElement>('[data-block-id]');
    const zone = el?.closest<HTMLElement>('[data-drop-list]');
    let drop: Drop | null = null;
    if (card && card.dataset.blockId !== id) drop = dropOfCard(card, y);
    else if (!card && zone) drop = dropOfZone(zone);
    else if (card) return null;
    if (!drop || !canMove(this.schema, this.root, id, drop.container).ok) return null;
    return drop;
  }

  // ---- shared blocks

  shared(id: string) {
    let p = this.#shared.get(id + '|' + this.locale);
    if (!p) {
      p = readShared(id, this.locale);
      p.catch(() => this.#shared.delete(id + '|' + this.locale));
      this.#shared.set(id + '|' + this.locale, p);
    }
    return p;
  }

  addShared(c: Container, index: number, item: string) {
    const b = this.add(c, index, SHARED, { item });
    if (b) this.open[b.id!] = true;
  }

  /** Replaces a shared block by copies of the blocks it shows. */
  async detach(id: string) {
    const f = find(this.root, id);
    const item = f && typeof f.block.props?.item === 'string' ? f.block.props.item : '';
    if (!f || !item) return;
    try {
      const info = await readShared(item, this.locale);
      const copies = info.blocks.map((b) => cloneBlock(b));
      const types = copies.map((b) => b.type);
      const rule = this.rules(f.container);
      const base = removeById(this.root, id);
      const allowed = !!rule && types.every((type) => rule.allow.includes(type));
      const ok = allowed ? canInsert(this.schema, base, f.container, types, { added: copies }) : typeRefusal;
      if (!ok.ok) {
        toast.error(t('blocks.sharedDetachFail', { reason: this.reason(ok) }));
        return;
      }
      this.commit(insertAt(base, f.container, f.index, ...copies));
      for (const b of copies) this.open[b.id!] = true;
      this.selected = copies[0]?.id ?? '';
      toast.success(t('blocks.sharedDetached'));
    } catch {
      toast.error(t('blocks.sharedMissing'));
    }
  }

  /** Swaps a block for a shared block that shows the item created from it. */
  replaceWithShared(id: string, item: string) {
    const f = find(this.root, id);
    if (!f) return;
    const b: Block = { id: f.block.id, type: SHARED, props: { item } };
    const next = removeById(this.root, id);
    this.commit(insertAt(next, f.container, f.index, b));
    this.selected = id;
  }
}

function scrollParent(el: HTMLElement): Element {
  for (let n = el.parentElement; n; n = n.parentElement) {
    const o = getComputedStyle(n).overflowY;
    if ((o === 'auto' || o === 'scroll') && n.scrollHeight > n.clientHeight) return n;
  }
  return document.scrollingElement ?? document.documentElement;
}
