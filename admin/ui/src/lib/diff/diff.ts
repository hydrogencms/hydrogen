// Structural diff of two item documents against the type's schema. Pure: names of referenced items are passed in.
import type { CompiledField } from '../types.svelte';
import { SHARED, summary, type Block, type BlockType } from '../blocks/tree';
import { diffRich, isEmptyRich, type RichDiff } from './rich';
import { diffSeq, diffWordsEx, type Seg } from './seq';

export type Doc = Record<string, unknown>;

export interface DiffOptions {
  /** Item id to display name, for reference fields and shared blocks (see `refIds`). */
  names?: Record<string, string>;
  /** Block type definitions (`blocksCatalog.get`). Unknown types are compared from their values. */
  block?: (type: string) => BlockType | undefined;
}

export interface Ref {
  id: string;
  name?: string;
}

export type Change =
  | { kind: 'text'; segs: Seg[]; approx?: boolean }
  /** A scalar (number, boolean, enum, date, json...). `undefined` is absent. */
  | { kind: 'value'; before?: unknown; after?: unknown }
  | { kind: 'ref'; before?: Ref; after?: Ref }
  | ({ kind: 'rich' } & RichDiff)
  | { kind: 'list'; rows: ListRow[]; same: number }
  | { kind: 'group'; fields: FieldDiff[]; same: string[] }
  | { kind: 'blocks'; rows: BlockRow[]; same: number };

export interface FieldDiff {
  name: string;
  label: string;
  status: 'added' | 'removed' | 'changed';
  change: Change;
}

export interface ListRow {
  status: 'added' | 'removed' | 'changed';
  /** 1-based position (in the new list; in the old one when removed). */
  index: number;
  change: Change;
}

export interface BlockRow {
  id: string;
  type: string;
  /** The block type's label. */
  label: string;
  /** One line of the block's text, from its new side (old when removed). */
  summary: string;
  status: 'added' | 'removed' | 'changed' | 'moved' | 'context';
  /** Moved to another place (container or order); also set on changed blocks that moved. */
  moved: boolean;
  props: FieldDiff[];
  /** Props that did not change. */
  sameProps: string[];
  slots: { name: string; label: string; rows: BlockRow[]; same: number }[];
  /** Blocks inside an added or removed block. */
  nested: number;
}

export interface DocDiff {
  fields: FieldDiff[];
  /** Fields that are equal on both sides (collapsed). */
  same: { name: string; label: string }[];
}

// ---- values

const plain = (x: unknown): x is Doc => typeof x === 'object' && x !== null && !Array.isArray(x);

/** Absent as the server sees it: undefined, null, '', NaN, empty list or object. */
export function isEmpty(v: unknown): boolean {
  if (v === undefined || v === null || v === '') return true;
  if (typeof v === 'number') return Number.isNaN(v);
  if (Array.isArray(v)) return v.length === 0;
  if (plain(v)) return Object.values(v).every(isEmpty);
  return false;
}

export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (isEmpty(a) && isEmpty(b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => deepEqual(x, b[i]));
  if (plain(a) && plain(b)) {
    const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
    for (const k of keys) if (!deepEqual(a[k], b[k])) return false;
    return true;
  }
  return false;
}

/** A string equal for exactly the values `deepEqual` calls equal: empties are dropped, object keys sorted. Computed once per element. */
export function canonKey(v: unknown): string {
  const c = (x: unknown): unknown => {
    if (isEmpty(x)) return null;
    if (Array.isArray(x)) return x.map(c);
    if (plain(x)) {
      const o: Doc = {};
      for (const k of Object.keys(x).sort()) if (!isEmpty(x[k])) o[k] = c(x[k]);
      return o;
    }
    return x;
  };
  return JSON.stringify(c(v));
}

const STRING_SCALARS = new Set(['enum', 'date', 'datetime', 'slug', 'url', 'email', 'token', 'uuid']);

const ref = (id: unknown, o: DiffOptions): Ref | undefined => {
  if (typeof id !== 'string' || !id) return undefined;
  return { id, name: o.names?.[id] };
};

/** `undefined` when both sides are equal. A missing side (undefined/empty) gives an all-added or all-removed change. */
export function diffValue(f: CompiledField, a: unknown, b: unknown, o: DiffOptions = {}): Change | null {
  if (deepEqual(a, b)) return null;
  const A = isEmpty(a) ? undefined : a;
  const B = isEmpty(b) ? undefined : b;
  switch (f.shape.base) {
    case 'string':
      if (!STRING_SCALARS.has(f.shape.format ?? '') && (A === undefined || typeof A === 'string') && (B === undefined || typeof B === 'string')) {
        const w = diffWordsEx(A as string | undefined, B as string | undefined);
        return { kind: 'text', segs: w.segs, approx: w.approx || undefined };
      }
      return { kind: 'value', before: A, after: B };
    case 'richtext': {
      if (isEmptyRich(A) && isEmptyRich(B)) return null;
      const r = diffRich(A, B);
      return r.rows.length ? { kind: 'rich', ...r } : null;
    }
    case 'ref':
      return { kind: 'ref', before: ref(A, o), after: ref(B, o) };
    case 'group': {
      const g = diffFields(f.fields ?? inferFields(A, B), plain(A) ? A : {}, plain(B) ? B : {}, o);
      return g.fields.length ? { kind: 'group', fields: g.fields, same: g.same.map((s) => s.label) } : null;
    }
    case 'list':
      return diffList(f, Array.isArray(A) ? A : [], Array.isArray(B) ? B : [], o);
    case 'blocks': {
      const r = diffBlocks(Array.isArray(A) ? (A as Block[]) : [], Array.isArray(B) ? (B as Block[]) : [], o);
      return r.rows.length ? { kind: 'blocks', ...r } : null;
    }
    default:
      return { kind: 'value', before: A, after: B };
  }
}

function diffList(f: CompiledField, a: unknown[], b: unknown[], o: DiffOptions): Change | null {
  const of: CompiledField = f.of ?? { name: f.name, label: f.label, type: 'json', shape: { base: 'json' } };
  const ops = diffSeq(a.map(canonKey), b.map(canonKey));
  const rows: ListRow[] = [];
  let same = 0;
  let dels: number[] = [];
  let inss: number[] = [];
  const flush = () => {
    const n = Math.max(dels.length, inss.length);
    for (let i = 0; i < n; i++) {
      const d = dels[i];
      const s = inss[i];
      if (d !== undefined && s !== undefined) {
        const c = diffValue(of, a[d], b[s], o);
        if (c) rows.push({ status: 'changed', index: s + 1, change: c });
      } else if (s !== undefined) {
        const c = diffValue(of, undefined, b[s], o);
        if (c) rows.push({ status: 'added', index: s + 1, change: c });
      } else if (d !== undefined) {
        const c = diffValue(of, a[d], undefined, o);
        if (c) rows.push({ status: 'removed', index: d + 1, change: c });
      }
    }
    dels = [];
    inss = [];
  };
  for (const op of ops) {
    if (op.k === 'eq') {
      flush();
      same++;
    } else if (op.k === 'del') dels.push(op.a!);
    else inss.push(op.b!);
  }
  flush();
  return rows.length ? { kind: 'list', rows, same } : null;
}

/** Fields for an object whose schema is unknown (a block prop bag): one per key, typed from the value. */
function inferFields(...docs: unknown[]): CompiledField[] {
  const keys: string[] = [];
  for (const d of docs) if (plain(d)) for (const k of Object.keys(d)) if (!keys.includes(k)) keys.push(k);
  keys.sort();
  return keys.map((k) => {
    const v = docs.map((d) => (plain(d) ? d[k] : undefined)).find((x) => !isEmpty(x));
    const base = typeof v === 'string' ? 'string' : typeof v === 'number' ? 'number' : typeof v === 'boolean' ? 'boolean' : 'json';
    return { name: k, label: k, type: base, shape: { base } };
  });
}

// ---- fields of a document

export function diffFields(fields: CompiledField[], a: Doc, b: Doc, o: DiffOptions = {}): DocDiff {
  const out: DocDiff = { fields: [], same: [] };
  for (const f of fields) {
    const av = a[f.name];
    const bv = b[f.name];
    const change = diffValue(f, av, bv, o);
    if (!change) {
      if (!isEmpty(av) || !isEmpty(bv)) out.same.push({ name: f.name, label: f.label });
      continue;
    }
    out.fields.push({ name: f.name, label: f.label, status: isEmpty(av) ? 'added' : isEmpty(bv) ? 'removed' : 'changed', change });
  }
  return out;
}

// ---- blocks

interface Entry {
  block: Block;
  container: string;
}

/** The blocks of both sides by id, with the container each sits in, and the options of the diff. */
interface BlockContext {
  before: Map<string, Entry>;
  after: Map<string, Entry>;
  options: DiffOptions;
}

type BlockList = { rows: BlockRow[]; same: number };

const keyOf = (c: string, id: string) => `${c}\u0000${id}`;
const rootKey = '';

function index(list: Block[], container: string, into: Map<string, Entry>) {
  for (const b of list) {
    if (!b.id) continue;
    into.set(b.id, { block: b, container });
    for (const [slot, kids] of Object.entries(b.slots ?? {})) index(kids, keyOf(b.id, slot), into);
  }
}

/** Blocks of a removed block that are gone for good: children that live on elsewhere (moved) are not counted, nor what they carry. */
function countGone(list: Block[], after: Map<string, Entry>): number {
  let gone = 0;
  for (const b of list) {
    if (b.id && after.has(b.id)) continue;
    gone += 1 + countGone(Object.values(b.slots ?? {}).flat(), after);
  }
  return gone;
}

/** Copies of the blocks where every block has an id: those without one get a positional pseudo-id (parent, slot, index). */
function withIds(list: Block[], parent: string): Block[] {
  return list.map((b, i) => {
    const id = b.id || `~${parent}/${i}`;
    const slots = b.slots ? mapValues(b.slots, (kids, name) => withIds(kids, `${id}/${name}`)) : undefined;
    return { ...b, id, ...(slots ? { slots } : {}) };
  });
}

const mapValues = <T, U>(o: Record<string, T>, fn: (v: T, k: string) => U): Record<string, U> =>
  Object.fromEntries(Object.entries(o).map(([k, v]) => [k, fn(v, k)]));

const defOf = (type: string, o: DiffOptions): BlockType | undefined => {
  if (type === SHARED) {
    return { name: SHARED, label: 'Shared block', props: [{ name: 'item', label: 'Shared block', type: 'ref', shape: { base: 'ref' } }] };
  }
  return o.block?.(type);
};

export function diffBlocks(before: Block[], after: Block[], options: DiffOptions = {}): BlockList {
  const a = withIds(before, '');
  const b = withIds(after, '');
  const ctx: BlockContext = { before: new Map(), after: new Map(), options };
  index(a, rootKey, ctx.before);
  index(b, rootKey, ctx.after);
  return diffBlockList(a, b, rootKey, ctx);
}

function diffBlockList(before: Block[], after: Block[], container: string, ctx: BlockContext): BlockList {
  // Reordered: among blocks that stayed in this container, the ones outside the longest unchanged sequence.
  const beforeIds = before.filter((x) => x.id && ctx.after.get(x.id)?.container === container).map((x) => x.id!);
  const afterIds = after.filter((x) => x.id && ctx.before.get(x.id)?.container === container).map((x) => x.id!);
  const keep = new Set(diffSeq(beforeIds, afterIds).filter((op) => op.k === 'eq').map((op) => afterIds[op.b!]!));
  const rows: BlockRow[] = [];
  let same = 0;
  for (const blk of after) {
    const previous = blk.id ? ctx.before.get(blk.id) : undefined;
    if (!previous) {
      rows.push(addedOrRemovedRow(blk, 'added', ctx));
      continue;
    }
    const moved = previous.container !== container || !keep.has(blk.id!);
    const row = changedRow(blk, previous.block, moved, ctx);
    if (row.status === 'context') same++;
    else rows.push(row);
  }
  for (const blk of before) if (blk.id && !ctx.after.has(blk.id)) rows.push(addedOrRemovedRow(blk, 'removed', ctx));
  return { rows, same };
}

function newRow(blk: Block, status: BlockRow['status'], moved: boolean, def: BlockType | undefined, options: DiffOptions): BlockRow {
  const sharedName = blk.type === SHARED ? (options.names?.[String(blk.props?.item ?? '')] ?? '') : '';
  return {
    id: blk.id ?? '',
    type: blk.type,
    label: def?.label ?? blk.type,
    summary: summary(blk, def) || sharedName,
    status,
    moved,
    props: [],
    sameProps: [],
    slots: [],
    nested: 0,
  };
}

const slotLabel = (def: BlockType | undefined, name: string) => def?.slots?.find((s) => s.name === name)?.label ?? name;

function addedOrRemovedRow(blk: Block, status: 'added' | 'removed', ctx: BlockContext): BlockRow {
  const def = defOf(blk.type, ctx.options);
  const row = newRow(blk, status, false, def, ctx.options);
  const props = blk.props ?? {};
  const added = status === 'added';
  row.props = diffFields(propFields(def, blk.props), added ? {} : props, added ? props : {}, ctx.options).fields;
  if (!added) {
    row.nested = countGone(Object.values(blk.slots ?? {}).flat(), ctx.after);
    return row;
  }
  // Children that already existed elsewhere show up as moved into the new block.
  for (const [name, kids] of Object.entries(blk.slots ?? {})) {
    const list = diffBlockList([], kids, keyOf(blk.id ?? '', name), ctx);
    if (list.rows.length) row.slots.push({ name, label: slotLabel(def, name), ...list });
  }
  return row;
}

function changedRow(blk: Block, previous: Block, moved: boolean, ctx: BlockContext): BlockRow {
  const def = defOf(blk.type, ctx.options);
  const row = newRow(blk, 'context', moved, def, ctx.options);
  const props = diffFields(propFields(def, previous.props, blk.props), previous.props ?? {}, blk.props ?? {}, ctx.options);
  row.props = props.fields;
  row.sameProps = props.same.map((s) => s.label);
  const slotNames = new Set([...Object.keys(previous.slots ?? {}), ...Object.keys(blk.slots ?? {})]);
  for (const name of [...slotNames].sort()) {
    const list = diffBlockList(previous.slots?.[name] ?? [], blk.slots?.[name] ?? [], keyOf(blk.id!, name), ctx);
    if (list.rows.length) row.slots.push({ name, label: slotLabel(def, name), ...list });
  }
  row.status = row.props.length || row.slots.length ? 'changed' : moved ? 'moved' : 'context';
  return row;
}

function propFields(def: BlockType | undefined, ...bags: (Doc | undefined)[]): CompiledField[] {
  const known = def?.props ?? [];
  const names = new Set(known.map((p) => p.name));
  const unknownProps = (bag: Doc | undefined) => (bag ? Object.fromEntries(Object.entries(bag).filter(([k]) => !names.has(k))) : undefined);
  return [...known, ...inferFields(...bags.map(unknownProps))];
}

// ---- versions and locales

/** The stored documents of a version: the non-translatable fields once, the translatable ones per locale. */
export interface VersionDocs {
  shared: Doc;
  locales: Record<string, Doc>;
}

/** The document a locale sees: shared fields plus that locale's translatable fields. `present` is false when the locale has no content. */
export function mergeLocale(fields: CompiledField[], docs: VersionDocs, locale: string): { doc: Doc; present: boolean } {
  const loc = docs.locales[locale];
  const doc: Doc = {};
  for (const f of fields) {
    const src = f.translatable ? loc : docs.shared;
    if (src && src[f.name] !== undefined) doc[f.name] = src[f.name];
  }
  return { doc, present: !!loc || !fields.some((f) => f.translatable) };
}

export interface ItemDiff extends DocDiff {
  /** A side without content in the locale. */
  missing: { a: boolean; b: boolean };
}

export function diffDocuments(fields: CompiledField[], a: VersionDocs, b: VersionDocs, locale: string, o: DiffOptions = {}): ItemDiff {
  const A = mergeLocale(fields, a, locale);
  const B = mergeLocale(fields, b, locale);
  return { ...diffFields(fields, A.doc, B.doc, o), missing: { a: !A.present, b: !B.present } };
}

/** Locales (of the union of both sides) whose translatable fields differ. Shared fields are not locale-specific and are not counted. */
export function changedLocales(fields: CompiledField[], a: VersionDocs, b: VersionDocs): string[] {
  const tr = fields.filter((f) => f.translatable);
  return [...new Set([...Object.keys(a.locales), ...Object.keys(b.locales)])].filter((l) => tr.some((f) => !deepEqual(a.locales[l]?.[f.name], b.locales[l]?.[f.name]))).sort();
}

/** True when any non-translatable field differs. */
export const sharedChanged = (fields: CompiledField[], a: VersionDocs, b: VersionDocs): boolean =>
  fields.some((f) => !f.translatable && !deepEqual(a.shared[f.name], b.shared[f.name]));

// ---- references

/** Ids of the items the documents reference (ref fields, in lists, groups and block props, shared blocks), to resolve into names. */
export function refIds(fields: CompiledField[], docs: Doc[], o: DiffOptions = {}): string[] {
  const out = new Set<string>();
  const value = (f: CompiledField, v: unknown) => {
    if (isEmpty(v)) return;
    switch (f.shape.base) {
      case 'ref':
        if (typeof v === 'string') out.add(v);
        break;
      case 'group':
        if (plain(v)) doc(f.fields ?? [], v);
        break;
      case 'list':
        if (Array.isArray(v) && f.of) for (const x of v) value(f.of, x);
        break;
      case 'blocks':
        if (Array.isArray(v)) blocks(v as Block[]);
        break;
    }
  };
  const doc = (fs: CompiledField[], d: Doc) => {
    for (const f of fs) value(f, d[f.name]);
  };
  const blocks = (list: Block[]) => {
    for (const b of list) {
      const def = defOf(b.type, o);
      if (def && b.props) doc(def.props, b.props);
      for (const kids of Object.values(b.slots ?? {})) blocks(kids);
    }
  };
  for (const d of docs) doc(fields, d);
  return [...out];
}
