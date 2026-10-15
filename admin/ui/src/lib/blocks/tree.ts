// Pure operations on a block tree (the stored value of a `blocks` field): ids, insert/move/duplicate/remove,
// allow/min/max rules, move targets, summaries and the routing of API error paths. Free of Svelte.
import type { CompiledField } from '../types.svelte';

export interface Block {
  id?: string;
  type: string;
  props?: Record<string, unknown>;
  slots?: Record<string, Block[]>;
  [k: string]: unknown;
}

export interface SlotDef {
  name: string;
  label: string;
  description?: string;
  allow: string[];
  min?: number;
  max?: number;
}

export interface BlockType {
  name: string;
  label: string;
  description?: string;
  icon?: string;
  props: CompiledField[];
  slots?: SlotDef[];
}

/** What a list of blocks (a blocks field or a slot) accepts. `max` 0 or undefined means no limit. */
export interface Rule {
  allow: string[];
  min?: number;
  max?: number;
}

/** A list of blocks: the field's root (`{}`) or one slot of a block (`{id, slot}`). */
export interface Container {
  id?: string;
  slot?: string;
}

export interface Schema {
  root: Rule;
  block(name: string): BlockType | undefined;
}

export const SHARED = 'shared';

// ---- ids

/** A UUIDv7 (time-ordered), the form the server assigns and validates. */
export function uuidv7(now = Date.now(), rnd: (n: number) => Uint8Array = (n) => crypto.getRandomValues(new Uint8Array(n))): string {
  const b = rnd(16);
  const ms = BigInt(now);
  for (let i = 0; i < 6; i++) b[i] = Number((ms >> BigInt(8 * (5 - i))) & 0xffn);
  b[6] = (b[6]! & 0x0f) | 0x70;
  b[8] = (b[8]! & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

const sameC = (a: Container, b: Container) => (a.id ?? '') === (b.id ?? '') && (a.slot ?? '') === (b.slot ?? '');

// ---- reading

export function listAt(root: Block[], c: Container): Block[] | undefined {
  if (!c.id) return root;
  const f = find(root, c.id);
  return f ? (f.block.slots?.[c.slot ?? ''] ?? []) : undefined;
}

export interface Found {
  block: Block;
  container: Container;
  index: number;
}

export function find(root: Block[], id: string, container: Container = {}): Found | undefined {
  for (let i = 0; i < root.length; i++) {
    const b = root[i]!;
    if (b.id === id) return { block: b, container, index: i };
    for (const [slot, list] of Object.entries(b.slots ?? {})) {
      if (!b.id) continue;
      const f = find(list, id, { id: b.id, slot });
      if (f) return f;
    }
  }
  return undefined;
}

/** Visits every block, depth first, with its container and index. */
export function walk(root: Block[], fn: (b: Block, c: Container, i: number, depth: number) => void, container: Container = {}, depth = 0) {
  root.forEach((b, i) => {
    fn(b, container, i, depth);
    for (const [slot, list] of Object.entries(b.slots ?? {})) walk(list, fn, { id: b.id, slot }, depth + 1);
  });
}

export const countBlocks = (root: Block[]): number => {
  let n = 0;
  walk(root, () => n++);
  return n;
};

export function descendantIds(b: Block): Set<string> {
  const out = new Set<string>();
  walk([b], (x) => x.id && out.add(x.id));
  return out;
}

export function ruleOf(s: Schema, c: Container, root: Block[]): Rule | undefined {
  if (!c.id) return s.root;
  const f = find(root, c.id);
  const slot = f && s.block(f.block.type)?.slots?.find((x) => x.name === c.slot);
  return slot ? { allow: slot.allow, min: slot.min, max: slot.max } : undefined;
}

// ---- writing (all return new trees; untouched subtrees are shared)

export function setList(root: Block[], c: Container, list: Block[]): Block[] {
  if (!c.id) return list;
  const map = (l: Block[]): Block[] =>
    l.map((b) => {
      if (b.id === c.id) return { ...b, slots: { ...b.slots, [c.slot ?? '']: list } };
      if (!b.slots) return b;
      let changed = false;
      const slots: Record<string, Block[]> = {};
      for (const [k, v] of Object.entries(b.slots)) {
        const n = map(v);
        if (n.some((x, i) => x !== v[i])) changed = true;
        slots[k] = n;
      }
      return changed ? { ...b, slots } : b;
    });
  return map(root);
}

export function insertAt(root: Block[], c: Container, index: number, ...blocks: Block[]): Block[] {
  const list = [...(listAt(root, c) ?? [])];
  list.splice(Math.max(0, Math.min(index, list.length)), 0, ...blocks);
  return setList(root, c, list);
}

export function removeById(root: Block[], id: string): Block[] {
  const f = find(root, id);
  if (!f) return root;
  return setList(
    root,
    f.container,
    (listAt(root, f.container) ?? []).filter((_, i) => i !== f.index),
  );
}

export function updateBlock(root: Block[], id: string, fn: (b: Block) => Block): Block[] {
  const f = find(root, id);
  if (!f) return root;
  return setList(
    root,
    f.container,
    (listAt(root, f.container) ?? []).map((b, i) => (i === f.index ? fn(b) : b)),
  );
}

/** Moves a block to `index` of `to` (an index in the list as it is before the move). Undefined when invalid. */
export function moveTo(root: Block[], id: string, to: Container, index: number): Block[] | undefined {
  const f = find(root, id);
  if (!f) return undefined;
  if (to.id && (to.id === id || descendantIds(f.block).has(to.id))) return undefined;
  const same = sameC(f.container, to);
  let at = index;
  if (same && f.index < index) at--;
  if (same && at === f.index) return root;
  const without = removeById(root, id);
  return insertAt(without, to, at, f.block);
}

export function reorder(root: Block[], id: string, delta: -1 | 1): Block[] | undefined {
  const f = find(root, id);
  if (!f) return undefined;
  const len = listAt(root, f.container)?.length ?? 0;
  const to = f.index + delta;
  if (to < 0 || to >= len) return undefined;
  return moveTo(root, id, f.container, delta < 0 ? to : to + 1);
}

/** A deep copy with new ids on every block. */
export function cloneBlock(b: Block, newId: () => string = uuidv7): Block {
  const out: Block = { ...(JSON.parse(JSON.stringify(b)) as Block), id: newId() };
  if (b.slots) {
    out.slots = {};
    for (const [k, list] of Object.entries(b.slots)) out.slots[k] = list.map((x) => cloneBlock(x, newId));
  }
  return out;
}

export function newBlock(def: BlockType, newId: () => string = uuidv7): Block {
  const props: Record<string, unknown> = {};
  for (const f of def.props) if (f.default !== undefined) props[f.name] = structuredClone(f.default);
  return { id: newId(), type: def.name, props };
}

// ---- rules

export const MAX_DEPTH = 8;
export const MAX_BLOCKS = 1000;

export type Refusal = { ok: true } | { ok: false; reason: 'type' | 'max' | 'min' | 'self' | 'depth' | 'count'; limit?: number };
const OK: Refusal = { ok: true };

/** Slot levels inside a block (0 for a block without children). */
export function slotDepth(b: Block): number {
  let d = 0;
  for (const list of Object.values(b.slots ?? {})) for (const c of list) d = Math.max(d, 1 + slotDepth(c));
  return d;
}

/** The slot level of a container: 0 for the field's root, 1 for a slot of a top-level block, and so on. */
export function containerLevel(root: Block[], c: Container): number {
  if (!c.id) return 0;
  let level = 0;
  walk(root, (b, _c, _i, depth) => {
    if (b.id === c.id) level = depth + 1;
  });
  return level;
}

/**
 * Whether blocks of `types` may go into `c`: allowed type, the list's maximum, the nesting depth and the block count
 * the server enforces. `added` are the blocks themselves when they have children (a paste, a duplicate, a move);
 * `moving` skips the count (the blocks are already in the tree).
 */
export function canInsert(s: Schema, root: Block[], c: Container, types: string[], extra = 0, added?: Block[], moving = false): Refusal {
  const rule = ruleOf(s, c, root);
  if (!rule) return { ok: false, reason: 'type' };
  if (types.some((t) => !rule.allow.includes(t))) return { ok: false, reason: 'type' };
  const len = (listAt(root, c)?.length ?? 0) + types.length - extra;
  if (rule.max && len > rule.max) return { ok: false, reason: 'max', limit: rule.max };
  const level = containerLevel(root, c);
  const inner = Math.max(0, ...(added ?? []).map(slotDepth));
  if (level + inner > MAX_DEPTH) return { ok: false, reason: 'depth', limit: MAX_DEPTH };
  if (!moving) {
    let n = 0;
    for (const b of added ?? []) walk([b], () => n++);
    if (!added) n = types.length;
    if (countBlocks(root) + n > MAX_BLOCKS) return { ok: false, reason: 'count', limit: MAX_BLOCKS };
  }
  return OK;
}

/** Whether `id` may be deleted (or moved out of its list) without going under the list's minimum. */
export function canRemove(s: Schema, root: Block[], id: string): Refusal {
  const f = find(root, id);
  if (!f) return { ok: false, reason: 'type' };
  const rule = ruleOf(s, f.container, root);
  const len = listAt(root, f.container)?.length ?? 0;
  if (rule?.min && len - 1 < rule.min) return { ok: false, reason: 'min', limit: rule.min };
  return OK;
}

export function canMove(s: Schema, root: Block[], id: string, to: Container): Refusal {
  const f = find(root, id);
  if (!f) return { ok: false, reason: 'type' };
  if (to.id && (to.id === id || descendantIds(f.block).has(to.id))) return { ok: false, reason: 'self' };
  const same = sameC(f.container, to);
  const ins = canInsert(s, root, to, [f.block.type], same ? 1 : 0, [f.block], true);
  if (!ins.ok) return ins;
  return same ? OK : canRemove(s, root, id);
}

export interface Target {
  container: Container;
  /** Names from the field down to the list: ['Body'] or ['Body', 'Cards', 'Cards']. */
  trail: string[];
  length: number;
}

/** Every list the block could be moved to (including its own, for a different position), with a readable trail. */
export function targets(s: Schema, root: Block[], id: string, fieldLabel: string): Target[] {
  const f = find(root, id);
  if (!f) return [];
  const out: Target[] = [];
  const consider = (c: Container, trail: string[], list: Block[]) => {
    if (canMove(s, root, id, c).ok) out.push({ container: c, trail, length: list.length });
  };
  consider({}, [fieldLabel], root);
  const rec = (list: Block[], trail: string[]) => {
    list.forEach((b, i) => {
      const def = s.block(b.type);
      for (const sl of def?.slots ?? []) {
        const t = [...trail, `${def!.label} ${i + 1}`, sl.label];
        const children = b.slots?.[sl.name] ?? [];
        if (b.id) consider({ id: b.id, slot: sl.name }, t, children);
        rec(children, t);
      }
    });
  };
  rec(root, [fieldLabel]);
  return out;
}

export function describeRefusal(r: Refusal, t: (key: string, v?: Record<string, string | number>) => string): string {
  if (r.ok) return '';
  if (r.reason === 'max') return t('blocks.reason.max', { n: r.limit ?? 0 });
  if (r.reason === 'min') return t('blocks.reason.min', { n: r.limit ?? 0 });
  if (r.reason === 'self') return t('blocks.reason.self');
  if (r.reason === 'depth') return t('blocks.reason.depth', { n: r.limit ?? 0 });
  if (r.reason === 'count') return t('blocks.reason.count', { n: r.limit ?? 0 });
  return t('blocks.reason.type');
}

// ---- summaries

/** Plain text of a stored rich text document. */
export function richText(v: unknown, max = 200): string {
  let out = '';
  const rec = (n: unknown) => {
    if (out.length >= max || !n || typeof n !== 'object') return;
    const o = n as { text?: unknown; content?: unknown[] };
    if (typeof o.text === 'string') out += o.text;
    if (Array.isArray(o.content)) {
      for (const c of o.content) {
        rec(c);
        if (out && !out.endsWith(' ')) out += ' ';
      }
    }
  };
  rec(v);
  return out.trim();
}

const textish = (f: CompiledField) => (f.shape.base === 'string' && !['token', 'enum', 'slug', 'url', 'email', 'date', 'datetime'].includes(f.shape.format ?? '')) || f.shape.base === 'richtext';

/** One line for the collapsed card: the first filled text-like prop. */
export function summary(b: Block, def: BlockType | undefined, max = 90): string {
  for (const f of def?.props ?? []) {
    if (!textish(f)) continue;
    const v = b.props?.[f.name];
    const s = f.shape.base === 'richtext' ? richText(v, max + 10) : typeof v === 'string' ? v : '';
    const one = s.replace(/\s+/g, ' ').trim();
    if (one) return one.length > max ? one.slice(0, max - 1) + '…' : one;
  }
  return '';
}

export { cleanBlocks, hydrateBlocks } from './clean';

// ---- API error paths

export const blockPath = (list: string, i: number) => `${list}[${i}]`;
export const propsPath = (block: string) => `${block}.props`;
export const slotPath = (block: string, slot: string) => `${block}.slots.${slot}`;

export interface Routed {
  /** Errors on the block itself or on something it shows no widget for, by block id. */
  block: Map<string, [string, string][]>;
  /** Errors on a slot as a whole (`body[1].slots.items`), by `${blockId}/${slot}`. */
  slot: Map<string, string[]>;
  /** The field's own error (`body`). */
  root: string[];
  /** Errors that name no block currently shown. */
  orphan: [string, string][];
  /** How many errors lie in or under each block, by id (prop errors included). */
  count: Map<string, number>;
}

/**
 * Sorts the form errors under `rootPath` (e.g. `body`) to where the editor shows them. Property errors
 * (`body[2].props.heading`) are shown by the property's own widget; the rest land on the block, the slot or
 * the field.
 */
export function routeErrors(s: Schema, root: Block[], rootPath: string, errors: Record<string, string>): Routed {
  const r: Routed = { block: new Map(), slot: new Map(), root: [], orphan: [], count: new Map() };
  const push = <K, V>(m: Map<K, V[]>, k: K, v: V) => m.set(k, [...(m.get(k) ?? []), v]);
  for (const [path, msg] of Object.entries(errors)) {
    if (path === rootPath) {
      r.root.push(msg);
      continue;
    }
    if (!path.startsWith(rootPath + '[')) continue;
    let list = root;
    let rest = path.slice(rootPath.length);
    let cur: Block | undefined;
    const chain: string[] = [];
    let done = false;
    for (;;) {
      const m = /^\[(\d+)\]/.exec(rest);
      if (!m) break;
      const b = list[Number(m[1])];
      if (!b?.id) {
        r.orphan.push([path, msg]);
        done = true;
        break;
      }
      cur = b;
      chain.push(b.id);
      rest = rest.slice(m[0].length);
      const sm = /^\.slots\.([^.[\]]+)/.exec(rest);
      if (sm && /^\.slots\.[^.[\]]+\[\d+\]/.test(rest)) {
        list = b.slots?.[sm[1]!] ?? [];
        rest = rest.slice(sm[0].length);
        continue;
      }
      break;
    }
    if (done || !cur) continue;
    for (const id of chain) r.count.set(id, (r.count.get(id) ?? 0) + 1);
    const def = s.block(cur.type);
    const sm = /^\.slots\.([^.[\]]+)$/.exec(rest);
    const pm = /^\.props\.([^.[\]]+)/.exec(rest);
    if (sm) push(r.slot, `${cur.id}/${sm[1]}`, msg);
    else if (pm && def?.props.some((f) => f.name === pm[1])) continue;
    else push(r.block, cur.id!, [path, msg]);
  }
  return r;
}

// ---- schema helpers

/** The rule of a blocks field: `allow` from its shape, `min`/`max` from its options. */
export function ruleOfField(f: CompiledField): Rule {
  const allow = (f.shape.allow as string[] | undefined) ?? (f.options?.allow as string[] | undefined) ?? [];
  const num = (v: unknown) => (typeof v === 'number' ? v : undefined);
  return { allow, min: num(f.options?.min), max: num(f.options?.max) };
}

export function schemaOf(blocks: readonly BlockType[], root: Rule): Schema {
  const by = new Map(blocks.map((b) => [b.name, b]));
  return { root, block: (n) => by.get(n) };
}

/** The blocks field of a (blockset) type: where a shared block's content lives. */
export function blocksFieldOf(fields: CompiledField[]): CompiledField | undefined {
  return fields.find((f) => f.shape.base === 'blocks');
}
