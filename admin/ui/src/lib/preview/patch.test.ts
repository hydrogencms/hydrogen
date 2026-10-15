import { describe, expect, it } from 'vitest';
import { diff, equal, patchOps, plain, type PatchOp } from './patch';

// The semantics of @hydrogencms/bridge's applyPatch (RFC 6902 add/remove/replace), reduced to what is needed.
function apply(doc: any, ops: PatchOp[]): any {
  let d = structuredClone(doc);
  const tok = (p: string) => (p === '' ? [] : p.slice(1).split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~')));
  for (const o of ops) {
    const t = tok(o.path);
    if (!t.length) {
      d = (o as any).value;
      continue;
    }
    let n = d;
    for (const k of t.slice(0, -1)) {
      n = Array.isArray(n) ? n[+k] : n[k];
      if (n === undefined) throw new Error('no path ' + o.path);
    }
    const k = t[t.length - 1]!;
    if (Array.isArray(n)) {
      if (!/^(0|[1-9]\d*)$/.test(k) && k !== '-') throw new Error('bad index ' + o.path);
      if (o.op === 'add') n.splice(k === '-' ? n.length : +k, 0, (o as any).value);
      else if (+k >= n.length) throw new Error('index ' + o.path);
      else if (o.op === 'remove') n.splice(+k, 1);
      else n[+k] = (o as any).value;
    } else if (o.op === 'add') n[k] = (o as any).value;
    else if (!(k in n)) throw new Error('missing ' + o.path);
    else if (o.op === 'remove') delete n[k];
    else n[k] = (o as any).value;
  }
  return d;
}

const blk = (id: string, heading = id, extra: object = {}) => ({ id, type: 'hero', props: { heading }, ...extra });
const doc = (...body: any[]) => ({ fields: { title: 'T', body } });

function roundTrip(a: any, b: any) {
  const ops = patchOps(a, b);
  expect(apply(a, ops)).toEqual(b);
  return ops;
}

describe('diff', () => {
  it('is empty for equal documents', () => {
    expect(diff(doc(blk('a')), doc(blk('a')))).toEqual([]);
    expect(equal({ a: [1, { b: 2 }] }, { a: [1, { b: 2 }] })).toBe(true);
  });

  it('addresses a changed property by pointer', () => {
    const a = doc(blk('a'), blk('b'));
    const b = doc(blk('a'), blk('b', 'changed'));
    expect(apply(a, diff(a, b))).toEqual(b);
    expect(diff(a, b)).toEqual([{ op: 'replace', path: '/fields/body/1/props/heading', value: 'changed' }]);
  });

  it('adds and removes object members, escaping ~ and /', () => {
    const a = { fields: { 'a/b': 1, 'c~d': 2, gone: 3 } };
    const b = { fields: { 'a/b': 2, 'c~d': 2, fresh: 4 } };
    const ops = diff(a, b);
    expect(apply(a, ops)).toEqual(b);
    expect(ops).toContainEqual({ op: 'replace', path: '/fields/a~1b', value: 2 });
    expect(ops).toContainEqual({ op: 'remove', path: '/fields/gone' });
    expect(ops).toContainEqual({ op: 'add', path: '/fields/fresh', value: 4 });
  });

  it('inserts into the middle and at the tail of an array', () => {
    const a = doc(blk('a'), blk('c'));
    expect(roundTrip(a, doc(blk('a'), blk('b'), blk('c')))).toEqual([{ op: 'add', path: '/fields/body/1', value: blk('b') }]);
    expect(roundTrip(a, doc(blk('a'), blk('c'), blk('d')))).toEqual([{ op: 'add', path: '/fields/body/2', value: blk('d') }]);
  });

  it('removes a run from the end first, so indexes stay valid', () => {
    const a = doc(blk('a'), blk('b'), blk('c'), blk('d'));
    const ops = roundTrip(a, doc(blk('a'), blk('d')));
    expect(ops.map((o) => o.path)).toEqual(['/fields/body/2', '/fields/body/1']);
  });

  it('replaces a reordered element, not its contents', () => {
    const a = doc(blk('a'), blk('b'));
    const ops = roundTrip(a, doc(blk('b'), blk('a')));
    expect(ops.every((o) => o.op === 'replace')).toBe(true);
  });

  it('handles nested slots', () => {
    const card = (id: string, title = id) => ({ id, type: 'card', props: { title } });
    const a = doc({ id: 'c', type: 'cards', props: {}, slots: { items: [card('1'), card('2')] } });
    const b = doc({ id: 'c', type: 'cards', props: {}, slots: { items: [card('1'), card('3'), card('2', 'x')] } });
    roundTrip(a, b);
    roundTrip(b, a);
    roundTrip(doc(), a);
    roundTrip(a, doc());
  });

  it('replaces the whole array when insert and edit are mixed', () => {
    const a = doc(blk('a'), blk('b'), blk('c'));
    const b = doc(blk('a', 'new'), blk('x'));
    roundTrip(a, b);
  });

  it('falls back to a whole-document replace when that is smaller', () => {
    const a = { v: 'aaaaaaaaaaaaaaaaaaaaaaaa', w: 1, x: 1, y: 1, z: 1 };
    const b = { v: 'b', w: 2, x: 2, y: 2, z: 2 };
    const ops = patchOps(a, b);
    expect(apply(a, ops)).toEqual(b);
    expect(ops.length).toBeLessThanOrEqual(5);
  });

  it('round trips random edits', () => {
    let seed = 7;
    const rnd = (n: number) => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) % n);
    const mk = (): any => doc(...Array.from({ length: rnd(5) }, (_, i) => blk('b' + rnd(6), 'h' + rnd(3), rnd(2) ? { slots: { s: Array.from({ length: rnd(3) }, () => blk('n' + rnd(4))) } } : {})));
    for (let i = 0; i < 300; i++) {
      const a = mk();
      const b = mk();
      expect(apply(a, patchOps(a, b))).toEqual(b);
    }
  });

  it('plain drops undefined', () => {
    expect(plain({ a: undefined, b: [1] })).toEqual({ b: [1] });
  });
});

describe('unsafe keys', () => {
  it('replaces the object that holds one instead of addressing it', () => {
    const a = { fields: { title: 'x', props: JSON.parse('{"a":1}') } };
    const b = { fields: { title: 'x', props: JSON.parse('{"a":1,"__proto__":{"z":1},"constructor":2}') } };
    const ops = diff(a, b);
    expect(ops).toEqual([{ op: 'replace', path: '/fields/props', value: b.fields.props }]);
    expect(JSON.stringify(ops)).not.toContain('/__proto__');
    expect(diff(b, a)).toEqual([{ op: 'replace', path: '/fields/props', value: a.fields.props }]);
  });
});
