// A small JSON Patch generator (RFC 6902 add/remove/replace) over plain JSON, for the preview bridge.
export type PatchOp = { op: 'add' | 'replace'; path: string; value: unknown } | { op: 'remove'; path: string };

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const UNSAFE = new Set(['__proto__', 'constructor', 'prototype']);
const esc = (k: string) => k.replace(/~/g, '~0').replace(/\//g, '~1');

export function equal(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => equal(x, b[i]));
  if (isObj(a) && isObj(b)) {
    const ka = Object.keys(a);
    return ka.length === Object.keys(b).length && ka.every((k) => k in b && equal(a[k], b[k]));
  }
  return false;
}

/** A deep copy through JSON: drops undefined, so what is diffed is what is sent. */
export const plain = <T>(v: T): T => JSON.parse(JSON.stringify(v ?? null)) as T;

/**
 * Operations that turn `a` into `b`. Objects diff key by key. Arrays of the same length diff element by
 * element (an element that changed identity, i.e. another `id`, is replaced whole); a pure insertion or
 * removal becomes adds at the position or removes from the end of the run; any other change replaces the array.
 */
export function diff(a: unknown, b: unknown, path = '', out: PatchOp[] = []): PatchOp[] {
  if (equal(a, b)) return out;
  if (isObj(a) && isObj(b)) {
    // The receiver refuses these keys in a path, so an object that has one is replaced whole.
    if ([...Object.keys(a), ...Object.keys(b)].some((k) => UNSAFE.has(k))) return out.push({ op: 'replace', path, value: b }), out;
    for (const k of Object.keys(a)) if (!(k in b)) out.push({ op: 'remove', path: `${path}/${esc(k)}` });
    for (const k of Object.keys(b)) {
      if (!(k in a)) out.push({ op: 'add', path: `${path}/${esc(k)}`, value: b[k] });
      else diff(a[k], b[k], `${path}/${esc(k)}`, out);
    }
    return out;
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length === b.length) {
      a.forEach((x, i) => {
        const y = b[i];
        if (equal(x, y)) return;
        if (isObj(x) && isObj(y) && x.id !== y.id) out.push({ op: 'replace', path: `${path}/${i}`, value: y });
        else diff(x, y, `${path}/${i}`, out);
      });
      return out;
    }
    let p = 0;
    while (p < a.length && p < b.length && equal(a[p], b[p])) p++;
    let s = 0;
    while (s < a.length - p && s < b.length - p && equal(a[a.length - 1 - s], b[b.length - 1 - s])) s++;
    const midA = a.length - p - s;
    const midB = b.length - p - s;
    if (midA === 0) {
      for (let k = 0; k < midB; k++) out.push({ op: 'add', path: `${path}/${p + k}`, value: b[p + k] });
      return out;
    }
    if (midB === 0) {
      for (let k = midA - 1; k >= 0; k--) out.push({ op: 'remove', path: `${path}/${p + k}` });
      return out;
    }
  }
  out.push({ op: 'replace', path, value: b });
  return out;
}

/** Operations for a patch message: the diff, or one whole-document replace when that is smaller. */
export function patchOps(a: unknown, b: unknown): PatchOp[] {
  const ops = diff(a, b);
  if (ops.length > 1 && JSON.stringify(ops).length > JSON.stringify(b).length) return [{ op: 'replace', path: '', value: b }];
  return ops;
}
