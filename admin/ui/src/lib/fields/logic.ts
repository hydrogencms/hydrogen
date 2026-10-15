// Pure logic behind the generated item form: slugs, `when`, values, patches, list operations and
// error paths. Kept free of Svelte so it can be tested directly.
import type { CompiledField } from '../types.svelte';
import { cleanBlocks, hydrateBlocks } from '../blocks/clean';

export type Doc = Record<string, unknown>;

// ---- slugs (mirrors schema.Slugify and isSlug on the server)

const FOLD: Record<string, string> = {
  æ: 'ae', œ: 'oe', ß: 'ss', ø: 'o', ł: 'l', đ: 'd', ð: 'd', ı: 'i', þ: 'th',
};
const SLUG_RUNE = /^[\p{Ll}\p{Lo}\p{Lm}\p{Nd}]$/u;
const SLUG = /^[\p{Ll}\p{Lo}\p{Lm}\p{Nd}]+(-[\p{Ll}\p{Lo}\p{Lm}\p{Nd}]+)*$/u;
const MAX_SLUG = 120;

export function isSlug(s: string): boolean {
  return SLUG.test(s);
}

export function slugify(s: string, max = MAX_SLUG): string {
  const out: string[] = [];
  let dash = true;
  for (const ch of s.toLowerCase()) {
    let piece = '';
    if (ch.charCodeAt(0) < 128) {
      if (SLUG_RUNE.test(ch)) piece = ch;
    } else if (FOLD[ch]) {
      piece = FOLD[ch];
    } else {
      const base = ch.normalize('NFD').replace(/\p{Mn}/gu, '');
      if (base === '' ) continue; // a bare combining mark
      if (base !== ch && base.charCodeAt(0) < 128 && SLUG_RUNE.test(base[0]!)) piece = base[0]!;
      else if (SLUG_RUNE.test(ch)) piece = ch;
    }
    if (piece) {
      out.push(piece);
      dash = false;
    } else if (!dash) {
      out.push('-');
      dash = true;
    }
    if (out.length > max) break;
  }
  return out.slice(0, max).join('').replace(/^-+|-+$/g, '');
}

/**
 * The slug after its source changed from `prev` to `next`: follows the source while it is empty or still the
 * generated value; an edited slug is left alone.
 */
export function autoSlug(current: string, prev: string, next: string): string {
  return current === '' || current === slugify(prev) ? slugify(next) : current;
}

/** Name of the sibling a slug is generated from: `options.from`, else the first `{placeholder}` of the pattern. */
export function slugSource(field: CompiledField, namePattern?: string): string | undefined {
  const from = field.options?.from;
  if (typeof from === 'string' && from) return from;
  const m = namePattern ? /\{(\w+)\}/.exec(namePattern) : null;
  return m?.[1];
}

// ---- conditions

export function jsonEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((x, i) => jsonEqual(x, b[i]));
  }
  const ka = Object.keys(a as object);
  const kb = Object.keys(b as object);
  return ka.length === kb.length && ka.every((k) => k in (b as object) && jsonEqual((a as Doc)[k], (b as Doc)[k]));
}

export function isEmptyValue(v: unknown): boolean {
  if (v === undefined || v === null || v === '') return true;
  if (Array.isArray(v)) return v.length === 0;
  if (typeof v === 'object') return Object.keys(v as object).length === 0;
  return false;
}

/** Whether a field is shown: its `when` against the sibling values (same rules as the server). */
export function evalWhen(when: CompiledField['when'], doc: Doc | undefined): boolean {
  if (!when) return true;
  const present = !!doc && when.field in doc && doc[when.field] !== undefined;
  const v = doc?.[when.field];
  switch (when.op) {
    case 'eq':
      return present && jsonEqual(v, when.value);
    case 'ne':
      return !present || !jsonEqual(v, when.value);
    case 'in':
      return present && Array.isArray(when.value) && when.value.some((x) => jsonEqual(v, x));
    case 'empty':
      return isEmptyValue(v) === (when.value === true);
  }
  return true;
}

// ---- values

export function baseOf(f: CompiledField): string {
  return f.shape.base;
}

/** Default for a new value of `f` (the type's `default`, else an empty value of its base). */
export function emptyValue(f: CompiledField): unknown {
  if (f.default !== undefined) return structuredClone(f.default);
  switch (baseOf(f)) {
    case 'boolean':
      return false;
    case 'list':
      return [];
    case 'group':
      return initialValues(f.fields ?? []);
    case 'number':
    case 'ref':
    case 'richtext':
    case 'json':
      return undefined;
  }
  return '';
}

/** Starting values of a new item: only fields with a declared default (the server applies them too). */
export function initialValues(fields: CompiledField[]): Doc {
  const out: Doc = {};
  for (const f of fields) {
    if (f.default !== undefined) out[f.name] = structuredClone(f.default);
    else if (baseOf(f) === 'group' && f.fields) {
      const g = initialValues(f.fields);
      if (Object.keys(g).length) out[f.name] = g;
    }
  }
  return out;
}

/** Drops values the server treats as absent ('', null, NaN, empty lists and groups), recursively. */
export function cleanValue(f: CompiledField, v: unknown): unknown {
  if (v === undefined || v === null || v === '') return undefined;
  if (typeof v === 'number' && Number.isNaN(v)) return undefined;
  switch (baseOf(f)) {
    case 'blocks':
      return Array.isArray(v) && v.length ? cleanBlocks(v) : undefined;
    case 'group': {
      if (typeof v !== 'object' || Array.isArray(v)) return v;
      const out = cleanDoc(f.fields ?? [], v as Doc);
      return Object.keys(out).length ? out : undefined;
    }
    case 'list': {
      if (!Array.isArray(v)) return v;
      const items = v.map((x) => (f.of ? cleanValue(f.of, x) : x)).filter((x) => x !== undefined);
      return items.length ? items : undefined;
    }
  }
  return v;
}

export function cleanDoc(fields: CompiledField[], doc: Doc): Doc {
  const out: Doc = {};
  const known = new Set(fields.map((f) => f.name));
  for (const f of fields) {
    const c = cleanValue(f, doc[f.name]);
    if (c !== undefined) out[f.name] = c;
  }
  for (const k of Object.keys(doc)) if (!known.has(k) && doc[k] !== undefined) out[k] = doc[k];
  return out;
}

/** JSON merge patch from `base` to `cur` (null removes). Objects merge key by key; lists and scalars replace. */
export function mergePatch(base: unknown, cur: unknown): unknown {
  const plain = (x: unknown): x is Doc => typeof x === 'object' && x !== null && !Array.isArray(x);
  if (!plain(base) || !plain(cur)) return cur === undefined ? null : cur;
  const out: Doc = {};
  for (const k of Object.keys(cur)) {
    if (!jsonEqual(base[k], cur[k])) out[k] = k in base ? mergePatch(base[k], cur[k]) : cur[k];
  }
  for (const k of Object.keys(base)) if (!(k in cur)) out[k] = null;
  return out;
}

/** The PATCH `fields` body: only what changed between the saved and the current values. Empty when nothing did. */
export function fieldsPatch(fields: CompiledField[], saved: Doc, current: Doc): Doc {
  const a = cleanDoc(fields, saved);
  const b = cleanDoc(fields, current);
  return (mergePatch(a, b) as Doc) ?? {};
}

/** Top-level names of translatable fields. */
export function translatableNames(fields: CompiledField[]): string[] {
  return fields.filter((f) => f.translatable).map((f) => f.name);
}

/** Fills translatable fields that are empty in `target` from `source` (copy from the main locale). */
export function copyTranslatable(fields: CompiledField[], target: Doc, source: Doc): Doc {
  const out = { ...target };
  for (const f of fields) {
    if (f.translatable && isEmptyValue(out[f.name]) && !isEmptyValue(source[f.name])) out[f.name] = structuredClone(source[f.name]);
  }
  return out;
}

// ---- lists

export function listAdd<T>(list: readonly T[] | undefined, item: T, at?: number): T[] {
  const a = [...(list ?? [])];
  a.splice(at ?? a.length, 0, item);
  return a;
}

export function listRemove<T>(list: readonly T[], i: number): T[] {
  return list.filter((_, n) => n !== i);
}

/** Moves item `i` by `delta` places; out-of-range moves return the list unchanged. */
export function listMove<T>(list: readonly T[], i: number, delta: number): T[] {
  const to = i + delta;
  if (i < 0 || i >= list.length || to < 0 || to >= list.length || delta === 0) return [...list];
  const a = [...list];
  const [x] = a.splice(i, 1);
  a.splice(to, 0, x!);
  return a;
}

// ---- error paths

export function joinPath(parent: string, seg: string | number): string {
  return typeof seg === 'number' ? `${parent}[${seg}]` : parent ? `${parent}.${seg}` : seg;
}

export function parsePath(path: string): (string | number)[] {
  const out: (string | number)[] = [];
  for (const m of path.matchAll(/([^.[\]]+)|\[(\d+)\]/g)) out.push(m[2] !== undefined ? Number(m[2]) : m[1]!);
  return out;
}

/** The field definition a path points at (list items resolve to the list's `of`), or undefined. */
export function fieldAtPath(fields: CompiledField[], path: string): CompiledField | undefined {
  let level: CompiledField[] | undefined = fields;
  let f: CompiledField | undefined;
  for (const seg of parsePath(path)) {
    if (typeof seg === 'number') {
      if (!f?.of) return undefined;
      f = f.of;
    } else {
      f = level?.find((x) => x.name === seg);
      if (!f) return undefined;
    }
    level = f.fields;
  }
  return f;
}

/**
 * Splits form errors into those a widget will show (the path names a visible field) and the rest, which the
 * editor lists above the form so none is lost (hidden by `when`, or not a field of the type).
 */
export function unroutedErrors(fields: CompiledField[], errors: Record<string, string>, doc: Doc): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [path, msg] of Object.entries(errors)) {
    if (!pathVisible(fields, path, doc)) out[path] = msg;
  }
  return out;
}

function pathVisible(fields: CompiledField[], path: string, doc: Doc): boolean {
  let level: CompiledField[] | undefined = fields;
  let cur: unknown = doc;
  let f: CompiledField | undefined;
  for (const seg of parsePath(path)) {
    if (f && baseOf(f) === 'blocks') return true;
    if (typeof seg === 'number') {
      if (!f?.of) return false;
      f = f.of;
      cur = Array.isArray(cur) ? cur[seg] : undefined;
      level = f.fields;
    } else {
      f = level?.find((x) => x.name === seg);
      if (!f || f.admin?.hidden || !evalWhen(f.when, cur as Doc)) return false;
      cur = (cur as Doc | undefined)?.[seg];
      level = f.fields;
    }
  }
  return !!f;
}

/** Top-level field names that carry an error (to mark them in the form). */
export function errorFieldNames(errors: Record<string, string>): Set<string> {
  return new Set(Object.keys(errors).map((p) => String(parsePath(p)[0] ?? '')));
}

// ---- dates

const pad = (n: number) => String(n).padStart(2, '0');

/** RFC 3339 instant to the `YYYY-MM-DDTHH:mm` a datetime-local input shows (local time). */
export function toLocalInput(iso: unknown): string {
  if (typeof iso !== 'string' || !iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** A datetime-local value (local time) to the RFC 3339 UTC instant the API wants; '' stays ''. */
export function fromLocalInput(local: string): string {
  if (!local) return '';
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString().replace('.000Z', 'Z');
}

// ---- helpers for lists and bulk work

/** Runs `fn` over `items` with at most `n` in flight; results keep the input order. Never rejects: failures are returned. */
export async function runPool<T, R>(items: readonly T[], n: number, fn: (item: T, i: number) => Promise<R>): Promise<(
  | { ok: true; value: R }
  | { ok: false; error: unknown }
)[]> {
  const out: ({ ok: true; value: R } | { ok: false; error: unknown })[] = new Array(items.length);
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const i = next++;
      try {
        out[i] = { ok: true, value: await fn(items[i]!, i) };
      } catch (error) {
        out[i] = { ok: false, error };
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(n, items.length) }, worker));
  return out;
}

/** Plain text for a field value in a table cell. */
export function cellText(f: CompiledField, v: unknown): string {
  if (v === undefined || v === null || v === '') return '';
  if (Array.isArray(v)) return v.map((x) => (typeof x === 'object' ? '…' : String(x))).join(', ');
  if (typeof v === 'object') return '…';
  return String(v);
}

/** A copy of `doc` where unset groups are empty objects, so group widgets always have something to write into. */
export function hydrate(fields: CompiledField[], doc: Doc | undefined | null): Doc {
  const out: Doc = structuredClone(doc ?? {});
  const walk = (f: CompiledField, v: unknown): unknown => {
    if (baseOf(f) === 'group' && f.fields) {
      const g = (v && typeof v === 'object' && !Array.isArray(v) ? v : {}) as Doc;
      for (const c of f.fields) {
        const r = walk(c, g[c.name]);
        if (r !== undefined) g[c.name] = r;
      }
      return g;
    }
    if (baseOf(f) === 'list' && f.of && Array.isArray(v)) return v.map((x) => walk(f.of!, x));
    if (baseOf(f) === 'blocks') return hydrateBlocks(v);
    return v;
  };
  for (const f of fields) {
    const r = walk(f, out[f.name]);
    if (r !== undefined) out[f.name] = r;
  }
  return out;
}
