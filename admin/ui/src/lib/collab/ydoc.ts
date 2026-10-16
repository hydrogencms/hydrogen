// Pure mapping between a draft part document (the values of a type's fields) and Yjs structures.
//
//   text/string/slug fields        Y.Text (edits are minimal splices)
//   richtext                       Y.XmlFragment (driven by y-prosemirror; JSON via its conversion helpers)
//   blocks                         Y.Array of Y.Map {id, type, props: Y.Map, slots: Y.Map of Y.Array}; a move is
//                                  delete + insert, the block list is reconciled by id
//   group                          Y.Map
//   list of group                  Y.Array of Y.Map without ids; changes are index-based inserts/deletes from an
//                                  LCS of the items, an item that keeps its place is edited field by field
//   everything else                plain values (last writer wins), lists of scalars are replaced as a whole
//
// `sync` makes Y equal to a value, `project` makes a value equal to Y. Both only touch what differs, so they are
// idempotent: syncing what was just projected writes nothing.
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from 'y-prosemirror';
import * as Y from 'yjs';
import type { CompiledField } from '../types.svelte';
import { configOf } from '../richtext/config';
import { buildSchema, isEmptyDoc, toStored } from '../richtext/schema';

type Schema = ReturnType<typeof buildSchema>;
export type Doc = Record<string, unknown>;

export interface Env {
  /** The property definitions of a block type (undefined: unknown, props are then plain values). */
  blockProps?(type: string): CompiledField[] | undefined;
}

/** Callbacks run after the new Y content is integrated into the document (rich text is filled then). */
export type Post = (() => void)[];

export const SHARED = 'shared';
export const localePart = (locale: string) => `l:${locale}`;
export const partOf = (f: CompiledField, locale: string) => (f.translatable ? localePart(locale) : SHARED);

// ---- helpers

export const isObj = (v: unknown): v is Doc => typeof v === 'object' && v !== null && !Array.isArray(v);

export function clone<T>(v: T): T {
  if (Array.isArray(v)) return v.map(clone) as T;
  if (isObj(v)) {
    const o: Doc = {};
    for (const [k, x] of Object.entries(v)) if (x !== undefined) o[k] = clone(x);
    return o as T;
  }
  return v;
}

export function jsonEq(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a === 'number' && typeof b === 'number') return Number.isNaN(a) && Number.isNaN(b);
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((x, i) => jsonEq(x, b[i]));
  }
  const ka = Object.keys(a).filter((k) => (a as Doc)[k] !== undefined);
  const kb = Object.keys(b).filter((k) => (b as Doc)[k] !== undefined);
  return ka.length === kb.length && ka.every((k) => jsonEq((a as Doc)[k], (b as Doc)[k]));
}

// ---- rich text schemas and canonical JSON

const schemas = new Map<string, Schema>();
export function richSchema(def?: CompiledField): Schema {
  const c = configOf(def?.options);
  const key = JSON.stringify([c.marks ?? null, c.nodes ?? null]);
  let s = schemas.get(key);
  if (!s) {
    try {
      s = buildSchema(c);
    } catch {
      s = buildSchema({});
    }
    schemas.set(key, s);
  }
  return s;
}

/** The stored form of a rich text value as the editor emits it (mark order of the schema, no nulls); undefined when empty. */
export function canonRich(schema: Schema, v: unknown): unknown {
  if (v === undefined || v === null || isEmptyDoc(v)) return undefined;
  try {
    const n = schema.nodeFromJSON(v);
    return isEmptyDoc(n.toJSON()) ? undefined : toStored(n);
  } catch {
    return v;
  }
}

export function richOf(frag: Y.XmlFragment, schema: Schema): unknown {
  return canonRich(schema, yXmlFragmentToProsemirrorJSON(frag));
}

function fillRich(frag: Y.XmlFragment, schema: Schema, v: unknown) {
  const json = v === undefined || v === null || isEmptyDoc(v) ? schema.topNodeType.createAndFill()!.toJSON() : v;
  try {
    prosemirrorJSONToYXmlFragment(schema, json, frag);
  } catch {
    // An invalid document leaves the fragment as it is.
  }
}

// ---- kinds

type Kind = 'text' | 'rich' | 'blocks' | 'group' | 'objlist' | 'plain';

const docLike = (v: unknown) => isObj(v) && v.type === 'doc' && Array.isArray(v.content);

function kindOf(def: CompiledField | undefined, v?: unknown): Kind {
  if (!def) return docLike(v) ? 'rich' : 'plain';
  switch (def.shape.base) {
    case 'string': {
      const fm = def.shape.format ?? '';
      return fm === 'enum' || fm === 'token' || fm === 'date' || fm === 'datetime' ? 'plain' : 'text';
    }
    case 'richtext':
      return 'rich';
    case 'blocks':
      return 'blocks';
    case 'group':
      return def.fields ? 'group' : 'plain';
    case 'list':
      return def.of?.shape.base === 'group' && def.of.fields ? 'objlist' : 'plain';
  }
  return 'plain';
}

const defOf = (defs: CompiledField[] | undefined, name: string) => defs?.find((d) => d.name === name);

function validBlocks(v: unknown): v is Doc[] {
  if (!Array.isArray(v)) return false;
  const seen = new Set<string>();
  for (const b of v) {
    if (!isObj(b) || typeof b.id !== 'string' || typeof b.type !== 'string' || seen.has(b.id)) return false;
    seen.add(b.id);
  }
  return true;
}

// ---- build: value -> new Y content

export function build(def: CompiledField | undefined, v: unknown, env: Env, post: Post): unknown {
  const kind = kindOf(def, v);
  if (kind === 'rich') {
    const frag = new Y.XmlFragment();
    const schema = richSchema(def);
    post.push(() => fillRich(frag, schema, v));
    return frag;
  }
  // An absent list of groups still gets its (empty) array, so two clients adding the first items at the same
  // time merge instead of each setting the key (last writer wins) and one losing its item.
  if ((v === undefined || v === null) && kind === 'objlist') return new Y.Array<Y.Map<unknown>>();
  if (v === undefined) return undefined;
  switch (kind) {
    case 'text':
      if (typeof v === 'string') return new Y.Text(v);
      break;
    case 'blocks':
      if (validBlocks(v)) {
        const a = new Y.Array<Y.Map<unknown>>();
        a.push(v.map((b) => blockMap(b, env, post)));
        return a;
      }
      break;
    case 'group':
      if (isObj(v)) return buildMap(def!.fields, v, env, post);
      break;
    case 'objlist':
      if (Array.isArray(v) && v.every(isObj)) {
        const a = new Y.Array<Y.Map<unknown>>();
        a.push(v.map((x) => buildMap(def!.of!.fields, x as Doc, env, post)));
        return a;
      }
      break;
  }
  return clone(v);
}

function buildMap(defs: CompiledField[] | undefined, v: Doc, env: Env, post: Post): Y.Map<unknown> {
  const m = new Y.Map<unknown>();
  const names = new Set([...(defs?.map((d) => d.name) ?? []), ...Object.keys(v)]);
  for (const n of names) {
    const y = build(defOf(defs, n), v[n], env, post);
    if (y !== undefined) m.set(n, y);
  }
  return m;
}

const BLOCK_KEYS = new Set(['id', 'type', 'props', 'slots']);

export function blockMap(b: Doc, env: Env, post: Post): Y.Map<unknown> {
  const m = new Y.Map<unknown>();
  m.set('id', b.id);
  m.set('type', b.type);
  m.set('props', buildMap(env.blockProps?.(b.type as string), isObj(b.props) ? b.props : {}, env, post));
  if (isObj(b.slots)) {
    const sm = new Y.Map<unknown>();
    for (const [k, list] of Object.entries(b.slots)) {
      if (!Array.isArray(list)) continue;
      const a = new Y.Array<Y.Map<unknown>>();
      a.push(list.filter(isObj).map((x) => blockMap(x, env, post)));
      sm.set(k, a);
    }
    m.set('slots', sm);
  }
  for (const [k, x] of Object.entries(b)) if (!BLOCK_KEYS.has(k) && x !== undefined) m.set(k, clone(x));
  return m;
}

// ---- read: Y -> plain value

export function toPlain(y: unknown): unknown {
  if (y instanceof Y.Text) return y.toString();
  if (y instanceof Y.XmlFragment) return richOf(y, richSchema());
  if (y instanceof Y.Map) {
    const o: Doc = {};
    y.forEach((x, k) => {
      const p = toPlain(x);
      if (p !== undefined) o[k] = p;
    });
    return o;
  }
  if (y instanceof Y.Array) return y.toArray().map(toPlain);
  return clone(y);
}

/** Plain value of Y content read with the field definitions, so rich text uses its own field's schema. */
export function plainOf(y: unknown, def: CompiledField | undefined, env: Env): unknown {
  if (y instanceof Y.XmlFragment) return richOf(y, richSchema(def));
  if (y instanceof Y.Text) return y.toString();
  if (y instanceof Y.Map) return plainMap(y, def?.fields, env);
  if (y instanceof Y.Array) {
    const kind = kindOf(def);
    if (kind === 'blocks') return y.toArray().map((m) => plainBlock(m as Y.Map<unknown>, env));
    return y.toArray().map((x) => plainOf(x, kind === 'objlist' ? def!.of : def?.of, env));
  }
  return clone(y);
}

function plainMap(y: Y.Map<unknown>, defs: CompiledField[] | undefined, env: Env): Doc {
  const o: Doc = {};
  y.forEach((x, k) => {
    const p = plainOf(x, defOf(defs, k), env);
    if (p !== undefined) o[k] = p;
  });
  return o;
}

export function plainBlock(m: Y.Map<unknown>, env: Env): Doc {
  const o: Doc = {};
  const defs = env.blockProps?.(m.get('type') as string);
  m.forEach((x, k) => {
    let p: unknown;
    if (k === 'props' && x instanceof Y.Map) p = plainMap(x, defs, env);
    else if (k === 'slots' && x instanceof Y.Map) {
      const so: Doc = {};
      x.forEach((a, sk) => {
        if (a instanceof Y.Array) so[sk] = a.toArray().map((b) => plainBlock(b as Y.Map<unknown>, env));
      });
      p = so;
    } else p = plainOf(x, undefined, env);
    if (p !== undefined) o[k] = p;
  });
  return o;
}

// ---- text

/** Replaces the differing middle of the text (common prefix and suffix are kept), without splitting surrogate pairs. */
export function spliceText(t: Y.Text, next: string) {
  const cur = t.toString();
  if (cur === next) return;
  const [at, del, ins] = diffText(cur, next);
  if (del) t.delete(at, del);
  if (ins) t.insert(at, ins);
}

export function diffText(cur: string, next: string): [at: number, del: number, ins: string] {
  const max = Math.min(cur.length, next.length);
  let p = 0;
  while (p < max && cur.charCodeAt(p) === next.charCodeAt(p)) p++;
  const hi = (c: number) => c >= 0xd800 && c <= 0xdbff;
  if (p > 0 && p < max + 1 && hi(cur.charCodeAt(p - 1))) p--;
  let s = 0;
  while (s < max - p && cur.charCodeAt(cur.length - 1 - s) === next.charCodeAt(next.length - 1 - s)) s++;
  const lo = (c: number) => c >= 0xdc00 && c <= 0xdfff;
  if (s > 0 && lo(cur.charCodeAt(cur.length - s))) s--;
  return [p, cur.length - p - s, next.slice(p, next.length - s)];
}

// ---- sequence diff

/** Gaps between the anchors of a longest common subsequence: [i0, i1) of `a` against [j0, j1) of `b`. */
export function lcsGaps(a: number, b: number, eq: (i: number, j: number) => boolean): { i0: number; i1: number; j0: number; j1: number }[] {
  const w = b + 1;
  const t = new Uint32Array((a + 1) * w);
  for (let i = a - 1; i >= 0; i--) {
    for (let j = b - 1; j >= 0; j--) {
      t[i * w + j] = eq(i, j) ? t[(i + 1) * w + j + 1]! + 1 : Math.max(t[(i + 1) * w + j]!, t[i * w + j + 1]!);
    }
  }
  const out: { i0: number; i1: number; j0: number; j1: number }[] = [];
  let i = 0;
  let j = 0;
  let gi = 0;
  let gj = 0;
  while (i < a && j < b) {
    if (eq(i, j)) {
      if (i > gi || j > gj) out.push({ i0: gi, i1: i, j0: gj, j1: j });
      i++;
      j++;
      gi = i;
      gj = j;
    } else if (t[(i + 1) * w + j]! >= t[i * w + j + 1]!) i++;
    else j++;
  }
  if (a > gi || b > gj) out.push({ i0: gi, i1: a, j0: gj, j1: b });
  return out;
}

/** Indexes of a longest increasing subsequence of `seq`. */
function lis(seq: number[]): Set<number> {
  const n = seq.length;
  const len = new Array<number>(n).fill(1);
  const prev = new Array<number>(n).fill(-1);
  let best = -1;
  for (let i = 0; i < n; i++) {
    for (let j = 0; j < i; j++) if (seq[j]! < seq[i]! && len[j]! + 1 > len[i]!) (len[i] = len[j]! + 1), (prev[i] = j);
    if (best < 0 || len[i]! > len[best]!) best = i;
  }
  const out = new Set<number>();
  for (let i = best; i >= 0; i = prev[i]!) out.add(i);
  return out;
}

// ---- sync: value -> Y (in place where possible)

/**
 * Makes `existing` (the Y content currently held for this value) equal to `v`. Returns `existing` when it was
 * updated in place (or already equal), else the content to hold instead (undefined: remove it).
 */
export function sync(existing: unknown, def: CompiledField | undefined, v: unknown, env: Env, post: Post): unknown {
  const kind = kindOf(def, v);
  if (existing instanceof Y.XmlFragment) {
    if (kind !== 'rich' && v !== undefined) return build(def, v, env, post);
    const schema = richSchema(def);
    if (!jsonEq(richOf(existing, schema), canonRich(schema, v))) post.push(() => fillRich(existing, schema, v));
    return existing;
  }
  // Emptying a string field empties its Y.Text (the key stays), so a concurrent edit of it is merged, not lost.
  if ((v === undefined || v === null) && existing instanceof Y.Text) {
    spliceText(existing, '');
    return existing;
  }
  if (v === undefined && existing instanceof Y.Array && kind === 'objlist') {
    syncObjList(existing, def!, [], env, post);
    return existing;
  }
  if (v === undefined) return kind === 'rich' ? build(def, v, env, post) : undefined;
  if (existing instanceof Y.Text) {
    if (typeof v === 'string') {
      spliceText(existing, v);
      return existing;
    }
  } else if (existing instanceof Y.Map) {
    if (kind === 'group' && isObj(v)) {
      syncMap(existing, def!.fields, v, env, post);
      return existing;
    }
  } else if (existing instanceof Y.Array) {
    // Duplicate ids can only be a transient state; keep what Y holds rather than replacing the array.
    if (kind === 'blocks' && !validBlocks(v)) return existing;
    if (kind === 'blocks' && validBlocks(v)) {
      syncBlocks(existing, v, env, post);
      return existing;
    }
    if (kind === 'objlist' && Array.isArray(v) && v.every(isObj)) {
      syncObjList(existing, def!, v as Doc[], env, post);
      return existing;
    }
  } else if (existing !== undefined && kind !== 'text' && kind !== 'rich' && kind !== 'blocks' && kind !== 'group' && kind !== 'objlist') {
    return jsonEq(existing, v) ? existing : clone(v);
  }
  return build(def, v, env, post);
}

function put(m: Y.Map<unknown>, key: string, existing: unknown, next: unknown) {
  if (next === existing) return;
  if (next === undefined) m.delete(key);
  else m.set(key, next);
}

export function syncMap(m: Y.Map<unknown>, defs: CompiledField[] | undefined, v: Doc, env: Env, post: Post, skip?: Set<string>) {
  const names = new Set([...(defs?.map((d) => d.name) ?? []), ...Object.keys(v), ...m.keys()]);
  for (const n of names) {
    if (skip?.has(n)) continue;
    const ex = m.get(n);
    put(m, n, ex, sync(ex, defOf(defs, n), v[n], env, post));
  }
}

function syncBlock(m: Y.Map<unknown>, b: Doc, env: Env, post: Post) {
  const props = m.get('props');
  const defs = env.blockProps?.(b.type as string);
  const pv = isObj(b.props) ? b.props : {};
  if (props instanceof Y.Map) syncMap(props, defs, pv, env, post);
  else m.set('props', buildMap(defs, pv, env, post));
  const slots = m.get('slots');
  if (!isObj(b.slots)) {
    if (slots !== undefined) m.delete('slots');
  } else {
    const sm: Y.Map<unknown> = slots instanceof Y.Map ? (slots as Y.Map<unknown>) : new Y.Map<unknown>();
    if (sm !== slots) m.set('slots', sm);
    for (const k of new Set([...Object.keys(b.slots), ...sm.keys()])) {
      const list = b.slots[k];
      const ex = sm.get(k);
      if (!Array.isArray(list)) {
        if (ex !== undefined) sm.delete(k);
      } else if (ex instanceof Y.Array && validBlocks(list)) syncBlocks(ex as Y.Array<Y.Map<unknown>>, list, env, post);
      else {
        const a = new Y.Array<Y.Map<unknown>>();
        sm.set(k, a);
        a.push(list.filter(isObj).map((x) => blockMap(x, env, post)));
      }
    }
  }
  const extra = new Set([...Object.keys(b), ...m.keys()].filter((k) => !BLOCK_KEYS.has(k)));
  for (const k of extra) {
    const ex = m.get(k);
    put(m, k, ex, sync(ex, undefined, b[k], env, post));
  }
}

export function syncBlocks(arr: Y.Array<Y.Map<unknown>>, target: Doc[], env: Env, post: Post) {
  const byId = new Map(target.map((b, i) => [b.id as string, i]));
  // Drop blocks that are gone, changed type or appear twice.
  const seen = new Set<string>();
  for (let i = arr.length - 1; i >= 0; i--) {
    const m = arr.get(i);
    const id = m.get('id') as string;
    const ti = byId.get(id);
    if (ti === undefined || target[ti]!.type !== m.get('type')) arr.delete(i);
  }
  for (let i = 0; i < arr.length; i++) {
    const id = arr.get(i).get('id') as string;
    if (seen.has(id)) arr.delete(i--);
    else seen.add(id);
  }
  // Blocks outside a longest increasing run of positions moved: delete here, re-insert below.
  const order = arr.toArray().map((m) => byId.get(m.get('id') as string)!);
  const keep = lis(order);
  for (let i = arr.length - 1; i >= 0; i--) if (!keep.has(i)) arr.delete(i);
  for (let i = 0; i < target.length; i++) {
    const b = target[i]!;
    if (i < arr.length && arr.get(i).get('id') === b.id) syncBlock(arr.get(i), b, env, post);
    else arr.insert(i, [blockMap(b, env, post)]);
  }
}

function syncObjList(arr: Y.Array<Y.Map<unknown>>, def: CompiledField, target: Doc[], env: Env, post: Post) {
  const defs = def.of!.fields;
  const old = arr.toArray().map((m) => plainOf(m, def.of, env));
  const want = target.map((x) => clone(x));
  const gaps = lcsGaps(old.length, want.length, (i, j) => jsonEq(old[i], want[j]));
  for (const g of gaps.reverse()) {
    const pair = Math.min(g.i1 - g.i0, g.j1 - g.j0);
    for (let k = 0; k < pair; k++) syncMap(arr.get(g.i0 + k), defs, target[g.j0 + k]!, env, post);
    if (g.i1 - g.i0 > pair) arr.delete(g.i0 + pair, g.i1 - g.i0 - pair);
    if (g.j1 - g.j0 > pair) {
      arr.insert(g.i0 + pair, target.slice(g.j0 + pair, g.j1).map((x) => buildMap(defs, x, env, post)));
    }
  }
}

// ---- project: Y -> value (in place where possible)

export interface Hooks {
  /** A string value changes from `old` to `next` (to keep the caret of a focused input). */
  text?(path: string, old: string, next: string): void;
}

const join = (p: string, k: string | number) => (typeof k === 'number' ? `${p}[${k}]` : p ? `${p}.${k}` : k);

/** Returns `cur` when it was updated in place (or equal), else the value to hold instead (undefined: remove it). */
export function project(y: unknown, cur: unknown, def: CompiledField | undefined, env: Env, hooks: Hooks, path: string): unknown {
  if (y === undefined) return undefined;
  if (y instanceof Y.Text) {
    const s = y.toString();
    if (cur === s) return cur;
    // An absent value and an empty text are the same to the form.
    if (s === '' && (cur === undefined || cur === null)) return cur;
    if (typeof cur === 'string') hooks.text?.(path, cur, s);
    return s;
  }
  if (y instanceof Y.XmlFragment) {
    const schema = richSchema(def);
    const j = richOf(y, schema);
    return jsonEq(canonRich(schema, cur), j) ? cur : j;
  }
  if (y instanceof Y.Map) {
    const o = isObj(cur) ? cur : {};
    projectMap(y, o, def?.fields, env, hooks, path);
    return o;
  }
  if (y instanceof Y.Array) {
    const kind = kindOf(def);
    if (kind === 'blocks') {
      const a = Array.isArray(cur) ? cur : [];
      projectBlocks(y as Y.Array<Y.Map<unknown>>, a, env, hooks, path);
      return a;
    }
    if (kind === 'objlist') {
      // An empty list and an absent one are the same to the form.
      if (y.length === 0 && (cur === undefined || cur === null)) return cur;
      const a = Array.isArray(cur) ? cur : [];
      projectObjList(y as Y.Array<Y.Map<unknown>>, a, def!, env, hooks, path);
      return a;
    }
    const p = plainOf(y, def, env);
    return jsonEq(cur, p) ? cur : p;
  }
  return jsonEq(cur, y) ? cur : clone(y);
}

function assign(o: Doc, k: string, cur: unknown, next: unknown) {
  if (next === cur) return;
  if (next === undefined) delete o[k];
  else o[k] = next;
}

export function projectMap(y: Y.Map<unknown>, o: Doc, defs: CompiledField[] | undefined, env: Env, hooks: Hooks, path: string, skip?: Set<string>) {
  const names = new Set([...y.keys(), ...Object.keys(o)]);
  for (const n of names) {
    if (skip?.has(n)) continue;
    const cur = o[n];
    assign(o, n, cur, project(y.get(n), cur, defOf(defs, n), env, hooks, join(path, n)));
  }
}

function projectBlock(m: Y.Map<unknown>, b: Doc, env: Env, hooks: Hooks, path: string) {
  const defs = env.blockProps?.(b.type as string);
  const props = m.get('props');
  if (props instanceof Y.Map) {
    if (!isObj(b.props)) b.props = {};
    projectMap(props, b.props as Doc, defs, env, hooks, join(path, 'props'));
  }
  const slots = m.get('slots');
  if (slots instanceof Y.Map) {
    if (!isObj(b.slots)) b.slots = {};
    const so = b.slots as Doc;
    for (const k of new Set([...slots.keys(), ...Object.keys(so)])) {
      const ex = slots.get(k);
      if (!(ex instanceof Y.Array)) delete so[k];
      else {
        if (!Array.isArray(so[k])) so[k] = [];
        projectBlocks(ex as Y.Array<Y.Map<unknown>>, so[k] as Doc[], env, hooks, join(join(path, 'slots'), k));
      }
    }
  } else if (b.slots !== undefined) delete b.slots;
  projectMap(m, b, undefined, env, hooks, path, BLOCK_KEYS);
}

export function projectBlocks(y: Y.Array<Y.Map<unknown>>, arr: Doc[], env: Env, hooks: Hooks, path: string) {
  const items = y.toArray();
  const ids = items.map((m) => m.get('id') as string);
  const want = new Map(items.map((m) => [m.get('id') as string, m.get('type')]));
  for (let i = arr.length - 1; i >= 0; i--) {
    const b = arr[i]!;
    if (!isObj(b) || want.get(b.id as string) !== b.type) arr.splice(i, 1);
  }
  for (let i = 0; i < items.length; i++) {
    const m = items[i]!;
    if (arr[i]?.id === ids[i]) {
      projectBlock(m, arr[i]!, env, hooks, join(path, i));
      continue;
    }
    let j = -1;
    for (let k = i + 1; k < arr.length; k++) if (arr[k]!.id === ids[i]) j = k;
    if (j >= 0) {
      const [b] = arr.splice(j, 1);
      arr.splice(i, 0, b!);
      projectBlock(m, b!, env, hooks, join(path, i));
    } else arr.splice(i, 0, plainBlock(m, env));
  }
  if (arr.length > items.length) arr.splice(items.length);
}

function projectObjList(y: Y.Array<Y.Map<unknown>>, arr: unknown[], def: CompiledField, env: Env, hooks: Hooks, path: string) {
  const items = y.toArray();
  const want = items.map((m) => plainOf(m, def.of, env));
  const have = arr.map((x) => clone(x));
  const defs = def.of!.fields;
  for (const g of lcsGaps(have.length, want.length, (i, j) => jsonEq(have[i], want[j])).reverse()) {
    const pair = Math.min(g.i1 - g.i0, g.j1 - g.j0);
    for (let k = 0; k < pair; k++) {
      const cur = arr[g.i0 + k];
      if (isObj(cur)) projectMap(items[g.j0 + k]!, cur, defs, env, hooks, join(path, g.i0 + k));
      else arr[g.i0 + k] = want[g.j0 + k];
    }
    if (g.i1 - g.i0 > pair) arr.splice(g.i0 + pair, g.i1 - g.i0 - pair);
    if (g.j1 - g.j0 > pair) arr.splice(g.i0 + pair, 0, ...want.slice(g.j0 + pair, g.j1));
  }
}

// ---- duplicate block ids (two clients moved the same block)

/**
 * Deletes later copies of a block id in a blocks array tree, in document order, so concurrent moves of one block
 * never leave two. Every client makes the same choice from the same state, and the first copy is never removed.
 */
export function dedupeBlocks(root: Y.Array<Y.Map<unknown>>): boolean {
  const seen = new Set<string>();
  const dels: [Y.Array<Y.Map<unknown>>, number][] = [];
  const walk = (arr: Y.Array<Y.Map<unknown>>) => {
    arr.forEach((m, i) => {
      const id = m.get('id') as string;
      if (seen.has(id)) return void dels.push([arr, i]);
      seen.add(id);
      const sm = m.get('slots');
      if (sm instanceof Y.Map) sm.forEach((a) => a instanceof Y.Array && walk(a as Y.Array<Y.Map<unknown>>));
    });
  };
  walk(root);
  for (const [arr, i] of dels.reverse()) arr.delete(i);
  return dels.length > 0;
}

// ---- parts

/** The top-level fields of a type that live in `part`. */
export function partFields(fields: CompiledField[], locale: string, part: string): CompiledField[] {
  return fields.filter((f) => partOf(f, locale) === part);
}

/** Seeds a part's map from field values. Call inside a transaction. */
export function seedPart(m: Y.Map<unknown>, defs: CompiledField[], values: Doc, env: Env) {
  const post: Post = [];
  for (const d of defs) {
    const y = build(d, values[d.name], env, post);
    if (y !== undefined) m.set(d.name, y);
  }
  for (const f of post) f();
}

/** Makes a part's map equal to the values of its fields. Call inside a transaction. */
export function syncPart(m: Y.Map<unknown>, defs: CompiledField[], values: Doc, env: Env, only?: Set<string>) {
  const post: Post = [];
  for (const d of defs) {
    if (only && !only.has(d.name)) continue;
    const ex = m.get(d.name);
    put(m, d.name, ex, sync(ex, d, values[d.name], env, post));
  }
  for (const f of post) f();
}

/** Where each boundary and character of `a` lands in `b` (a three-way merge needs to place an edit made against `a`). */
function align(a: string, b: string): { match: Int32Array; bnd: Int32Array } | null {
  if (a.length * b.length > 4_000_000) return null;
  const match = new Int32Array(a.length).fill(-1);
  const bnd = new Int32Array(a.length + 1);
  let i = 0;
  let j = 0;
  const run = (n: number) => {
    for (let k = 0; k < n; k++, i++, j++) {
      match[i] = j;
      bnd[i] = j;
    }
  };
  for (const g of lcsGaps(a.length, b.length, (x, y) => a.charCodeAt(x) === b.charCodeAt(y))) {
    run(g.i0 - i);
    for (let k = g.i0; k < g.i1; k++) bnd[k] = k === g.i0 ? g.j0 : g.j1;
    i = g.i1;
    j = g.j1;
  }
  run(a.length - i);
  bnd[a.length] = b.length;
  return { match, bnd };
}

/** Applies the edit that turned `base` into `cur` to a text that others changed meanwhile (`t`). */
function mergeText(t: Y.Text, base: string, cur: string) {
  const now = t.toString();
  if (now === base) return spliceText(t, cur);
  if (now === cur) return;
  const [la, ld, li] = diffText(base, cur);
  const al = align(base, now);
  if (!al) {
    const [ra, rd, ri] = diffText(base, now);
    let at: number;
    if (la + ld <= ra) at = la;
    else if (ra + rd <= la) at = la + ri.length - rd;
    else return;
    if (ld) t.delete(at, ld);
    if (li) t.insert(at, li);
    return;
  }
  let pos = al.bnd[la]!;
  const dels: number[] = [];
  for (let k = la; k < la + ld; k++) if (al.match[k]! >= 0) dels.push(al.match[k]!);
  // Characters that are still there go, from the end (runs in one delete).
  for (let q = dels.length - 1; q >= 0; ) {
    let lo = q;
    while (lo > 0 && dels[lo - 1] === dels[lo]! - 1) lo--;
    t.delete(dels[lo]!, q - lo + 1);
    q = lo - 1;
  }
  pos -= dels.filter((x) => x < pos).length;
  if (li) t.insert(pos, li);
}

/** Blocks added, removed or edited locally (by id) go onto the blocks others have now; nested lists stay as they are. */
function mergeBlocks(arr: Y.Array<Y.Map<unknown>>, base: unknown, cur: unknown, env: Env, post: Post) {
  const bl = (Array.isArray(base) ? base : []).filter(isObj) as Doc[];
  const cl = (Array.isArray(cur) ? cur : []).filter(isObj) as Doc[];
  const baseById = new Map(bl.map((b) => [b.id as string, b]));
  const curIds = new Set(cl.map((b) => b.id as string));
  const idx = (id: string) => arr.toArray().findIndex((m) => m.get('id') === id);
  for (const b of bl) {
    if (curIds.has(b.id as string)) continue;
    const i = idx(b.id as string);
    if (i >= 0) arr.delete(i);
  }
  cl.forEach((b, k) => {
    const id = b.id as string;
    const before = baseById.get(id);
    if (!before) {
      if (idx(id) >= 0) return;
      const prev = k > 0 ? idx(cl[k - 1]!.id as string) : -1;
      arr.insert(prev + 1, [blockMap(b, env, post)]);
    } else if (!jsonEq(before, b)) {
      const m = arr.get(idx(id) >= 0 ? idx(id) : 0);
      if (!m || m.get('id') !== id) return;
      const props = m.get('props');
      const bp = isObj(before.props) ? before.props : {};
      const cp = isObj(b.props) ? b.props : {};
      if (!(props instanceof Y.Map)) return;
      const defs = env.blockProps?.(b.type as string);
      for (const n of new Set([...Object.keys(bp), ...Object.keys(cp)])) {
        if (jsonEq(bp[n], cp[n])) continue;
        const ex = props.get(n);
        if (ex instanceof Y.Text && typeof cp[n] === 'string') mergeText(ex, typeof bp[n] === 'string' ? bp[n] : '', cp[n]);
        else if (jsonEq(ex === undefined ? undefined : plainOf(ex, defOf(defs, n), env), bp[n])) put(props, n, ex, sync(ex, defOf(defs, n), cp[n], env, post));
      }
    }
  });
}

/**
 * Applies local edits made before the session was joined (`cur` against the draft `base` they started from) to
 * the fields in `only`: text as a splice relative to what others wrote meanwhile, other fields only when nobody
 * else changed them. It never writes the pre-join value back over remote changes. Call inside a transaction.
 */
export function mergeLocal(m: Y.Map<unknown>, defs: CompiledField[] | undefined, cur: Doc, base: Doc, env: Env, only?: Set<string>) {
  const post: Post = [];
  mergeInto(m, defs, cur, base, env, post, only);
  for (const f of post) f();
}

function mergeInto(m: Y.Map<unknown>, defs: CompiledField[] | undefined, cur: Doc, base: Doc, env: Env, post: Post, only?: Set<string>) {
  const names = only ?? new Set([...Object.keys(cur), ...Object.keys(base)]);
  for (const n of names) {
    const d = defOf(defs, n);
    const ex = m.get(n);
    const c = cur[n];
    const b = base[n];
    if (jsonEq(c, b)) continue;
    if (ex instanceof Y.Text) {
      const c2 = typeof c === 'string' ? c : c == null ? '' : null;
      if (c2 !== null) mergeText(ex, typeof b === 'string' ? b : '', c2);
      continue;
    }
    if (ex instanceof Y.Map && kindOf(d, c) === 'group' && isObj(c) && (b === undefined || isObj(b))) {
      mergeInto(ex, d?.fields, c, (b ?? {}) as Doc, env, post);
      continue;
    }
    if (ex instanceof Y.Array && kindOf(d) === 'blocks') {
      mergeBlocks(ex as Y.Array<Y.Map<unknown>>, b, c, env, post);
      continue;
    }
    const yv = ex === undefined ? undefined : plainOf(ex, d, env);
    if (jsonEq(yv, c)) continue;
    // Anything else: the local edit applies only where nobody else changed the field meanwhile.
    if (jsonEq(yv === '' ? undefined : yv, b === '' ? undefined : b)) put(m, n, ex, sync(ex, d, c, env, post));
  }
}

/** Writes a part's content into the values object (only the fields in `only`, else all). */
export function projectPart(m: Y.Map<unknown>, defs: CompiledField[], values: Doc, env: Env, hooks: Hooks, only?: Set<string>) {
  for (const d of defs) {
    if (only && !only.has(d.name)) continue;
    const cur = values[d.name];
    assign(values, d.name, cur, project(m.get(d.name), cur, d, env, hooks, d.name));
  }
}

/** The Y content of a field path (`body[2].props.heading`) inside a part map, or undefined. */
export function yAtPath(m: Y.Map<unknown>, path: string): unknown {
  let cur: unknown = m;
  for (const mm of path.matchAll(/([^.[\]]+)|\[(\d+)\]/g)) {
    if (cur instanceof Y.Map && mm[1] !== undefined) cur = cur.get(mm[1]);
    else if (cur instanceof Y.Array && mm[2] !== undefined) cur = cur.get(Number(mm[2]));
    else return undefined;
  }
  return cur;
}
