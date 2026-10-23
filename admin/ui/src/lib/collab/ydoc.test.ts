import * as Y from 'yjs';
import { describe, expect, it } from 'vitest';
import { hydrate } from '../fields/logic';
import { draft, doc as rich, env, fields, sectionField } from './fixture';
import {
  canonRich,
  dedupeBlocks,
  diffText,
  jsonEq,
  lcsGaps,
  mergeLocal,
  partFields,
  plainBlock,
  projectBlocks,
  projectMap,
  projectPart,
  richSchema,
  seedPart,
  spliceText,
  syncPart,
  toPlain,
  type Doc,
} from './ydoc';

const hyd = () => hydrate(fields, structuredClone(draft.fields)) as Doc;
const defs = partFields(fields, 'en', 'l:en');
const shared = partFields(fields, 'en', 'shared');

function seeded(values: Doc = hyd()) {
  const d = new Y.Doc();
  d.transact(() => {
    seedPart(d.getMap('shared'), shared, values, env);
    seedPart(d.getMap('l:en'), defs, values, env);
  });
  return d;
}
const read = (d: Y.Doc) => ({ ...(toPlain(d.getMap('shared')) as Doc), ...(toPlain(d.getMap('l:en')) as Doc) });
const link = (a: Y.Doc, b: Y.Doc) => {
  Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
  Y.applyUpdate(a, Y.encodeStateAsUpdate(b));
};

describe('text splices', () => {
  it('finds the minimal middle', () => {
    expect(diffText('hello world', 'hello brave world')).toEqual([6, 0, 'brave ']);
    expect(diffText('abc', 'abc')).toEqual([3, 0, '']);
    expect(diffText('abcdef', 'abXef')).toEqual([2, 2, 'X']);
  });
  it('never splits a surrogate pair', () => {
    const [at, del, ins] = diffText('a😀b', 'a😁b');
    expect([...('a😀b'.slice(0, at) + ins + 'a😀b'.slice(at + del))].join('')).toBe('a😁b');
    expect(at).toBe(1);
    expect(del).toBe(2);
  });
  it('edits a Y.Text with one delete and insert', () => {
    const d = new Y.Doc();
    const t = d.getText('t');
    t.insert(0, 'hello world');
    const sizes: number[] = [];
    d.on('update', (u: Uint8Array) => sizes.push(u.length));
    spliceText(t, 'hello brave world');
    expect(t.toString()).toBe('hello brave world');
    expect(sizes).toHaveLength(1);
  });
});

describe('lcsGaps', () => {
  it('reports only what differs', () => {
    const a = ['a', 'b', 'c', 'd'];
    const b = ['a', 'x', 'c', 'd', 'e'];
    expect(lcsGaps(a.length, b.length, (i, j) => a[i] === b[j])).toEqual([
      { i0: 1, i1: 2, j0: 1, j1: 2 },
      { i0: 4, i1: 4, j0: 4, j1: 5 },
    ]);
  });
});

describe('mapping', () => {
  it('round trips a hydrated draft', () => {
    const v = hyd();
    expect(jsonEq(read(seeded(v)), v)).toBe(true);
  });

  it('uses the Y types of the contract', () => {
    const d = seeded();
    const l = d.getMap('l:en');
    const s = d.getMap('shared');
    expect(l.get('title')).toBeInstanceOf(Y.Text);
    expect(l.get('body')).toBeInstanceOf(Y.XmlFragment);
    expect(l.get('blocks')).toBeInstanceOf(Y.Array);
    expect((l.get('blocks') as Y.Array<unknown>).get(0)).toBeInstanceOf(Y.Map);
    expect(s.get('meta')).toBeInstanceOf(Y.Map);
    expect(s.get('sections')).toBeInstanceOf(Y.Array);
    expect((s.get('sections') as Y.Array<unknown>).get(0)).toBeInstanceOf(Y.Map);
    expect(s.get('count')).toBe(1);
    expect(s.get('kind')).toBeUndefined();
    expect(s.get('tags')).toEqual(['x']);
    expect(s.get('slug')).toBeInstanceOf(Y.Text);
  });

  it('keeps empty rich text fields editable (a fragment exists even without a value)', () => {
    const v = hyd();
    delete v.body;
    const d = seeded(v);
    expect(d.getMap('l:en').get('body')).toBeInstanceOf(Y.XmlFragment);
    expect(read(d).body).toBeUndefined();
  });

  it('keeps rich text the editor cannot load as it is, instead of an empty fragment', () => {
    const odd = { type: 'doc', content: [{ type: 'callout', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'kept' }] }] }] };
    const v = hyd();
    v.body = odd;
    const d = seeded(v);
    expect(d.getMap('l:en').get('body')).not.toBeInstanceOf(Y.XmlFragment);
    expect(read(d).body).toEqual(odd);
    const updates: number[] = [];
    d.on('update', (u: Uint8Array) => updates.push(u.length));
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(updates).toHaveLength(0);
    v.body = rich('fixed');
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(d.getMap('l:en').get('body')).toBeInstanceOf(Y.XmlFragment);
    expect(read(d).body).toEqual(rich('fixed'));
  });

  it('canonicalises rich text like the editor emits it', () => {
    const s = richSchema();
    const json = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'a', marks: [{ type: 'italic' }, { type: 'bold' }] }, { type: 'text', text: 'b' }] }] };
    const d = new Y.Doc();
    d.getMap('m').set('f', new Y.XmlFragment());
    const v = hyd();
    v.body = json;
    const y = seeded(v);
    expect(jsonEq(read(y).body, canonRich(s, json))).toBe(true);
  });
});

describe('sync and project', () => {
  it('edits text by splice and writes nothing for equal values', () => {
    const d = seeded();
    const v = hyd();
    const updates: number[] = [];
    d.on('update', (u: Uint8Array) => updates.push(u.length));
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(updates).toHaveLength(0);
    v.title = 'Hello there';
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(updates).toHaveLength(1);
    expect(read(d).title).toBe('Hello there');
  });

  it('moves a block by delete and insert, editing the others in place', () => {
    const v = hyd();
    v.blocks = ['a', 'b', 'c'].map((id) => ({ id, type: 'text', props: { heading: id } }));
    const d = seeded(v);
    const arr = d.getMap('l:en').get('blocks') as Y.Array<Y.Map<unknown>>;
    const [ma, mb, mc] = arr.toArray();
    const next = hyd();
    const vb = v.blocks as Doc[];
    next.blocks = [vb[2], vb[0], vb[1]].map((b) => structuredClone(b));
    (next.blocks as Doc[])[1]!.props = { heading: 'A!' };
    syncPart(d.getMap('l:en'), defs, next, env);
    const now = arr.toArray();
    expect(now.map((m) => m.get('id'))).toEqual(['c', 'a', 'b']);
    expect(now[1]).toBe(ma);
    expect(now[2]).toBe(mb);
    expect(now[0]).not.toBe(mc);
    expect(read(d).blocks).toEqual(next.blocks);
  });

  it('keeps both blocks inserted concurrently, and nested slots', () => {
    const a = seeded();
    const b = new Y.Doc();
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    const va = read(a);
    const vb = read(b);
    (va.blocks as Doc[]).push({ id: 'x', type: 'columns', props: {}, slots: { left: [{ id: 'x1', type: 'text', props: { heading: 'in slot' } }] } });
    (vb.blocks as Doc[]).push({ id: 'y', type: 'text', props: { heading: 'y' } });
    syncPart(a.getMap('l:en'), defs, va, env);
    syncPart(b.getMap('l:en'), defs, vb, env);
    link(a, b);
    const ids = (read(a).blocks as Doc[]).map((x) => x.id);
    expect(ids).toContain('x');
    expect(ids).toContain('y');
    expect(read(a)).toEqual(read(b));
  });

  it('list of groups: concurrent appends both survive, edits happen in place', () => {
    const a = seeded();
    const b = new Y.Doc();
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    const items = (d: Y.Doc) => d.getMap('shared').get('sections') as Y.Array<Y.Map<unknown>>;
    const first = items(a).get(0);
    const va = read(a);
    const vb = read(b);
    (va.sections as Doc[]).push({ heading: 'from a', n: 5 });
    (vb.sections as Doc[]).push({ heading: 'from b', n: 6 });
    (va.sections as Doc[])[0]!.heading = 'one!';
    syncPart(a.getMap('shared'), shared, va, env);
    syncPart(b.getMap('shared'), shared, vb, env);
    link(a, b);
    const heads = (read(a).sections as Doc[]).map((s) => s.heading);
    expect(heads).toContain('from a');
    expect(heads).toContain('from b');
    expect(heads[0]).toBe('one!');
    expect(items(a).get(0)).toBe(first);
    expect(read(a)).toEqual(read(b));
    expect(sectionField.of?.fields).toBeDefined();
  });

  it('list of groups absent from the draft: the first items added concurrently both survive', () => {
    const start = hyd();
    delete start.sections;
    const a = seeded(start);
    const b = new Y.Doc();
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    const va = read(a);
    const vb = read(b);
    va.sections = [{ heading: 'from a' }];
    vb.sections = [{ heading: 'from b' }];
    syncPart(a.getMap('shared'), shared, va, env);
    syncPart(b.getMap('shared'), shared, vb, env);
    link(a, b);
    expect((read(a).sections as Doc[]).map((x) => x.heading).sort()).toEqual(['from a', 'from b']);
    expect(read(a)).toEqual(read(b));
  });

  it('an absent list of groups stays absent in the form', () => {
    const start = hyd();
    delete start.sections;
    const d = seeded(start);
    const v = hyd();
    delete v.sections;
    projectPart(d.getMap('shared'), shared, v, env, {});
    expect(v.sections).toBeUndefined();
    syncPart(d.getMap('shared'), shared, v, env);
    expect((d.getMap('shared').get('sections') as Y.Array<unknown>).length).toBe(0);
  });

  it('projects into a form object touching only what changed', () => {
    const d = seeded();
    const v = hyd();
    const kept = (v.blocks as Doc[])[0]!;
    const sect = (v.sections as Doc[])[0]!;
    const remote = read(d);
    (remote.meta as Doc).note = 'changed';
    (remote.blocks as Doc[]).unshift({ id: 'new', type: 'text', props: {} });
    syncPart(d.getMap('shared'), shared, remote, env);
    syncPart(d.getMap('l:en'), defs, remote, env);
    projectPart(d.getMap('shared'), shared, v, env, {});
    projectPart(d.getMap('l:en'), defs, v, env, {});
    expect((v.blocks as Doc[])[1]).toBe(kept);
    expect((v.blocks as Doc[])[0]!.id).toBe('new');
    expect((v.sections as Doc[])[0]).toBe(sect);
    expect((v.meta as Doc).note).toBe('changed');
    expect(jsonEq(v, remote)).toBe(true);
  });

  it('reports text changes for caret handling', () => {
    const d = seeded();
    const v = hyd();
    const next = hyd();
    next.title = 'Hello world';
    syncPart(d.getMap('l:en'), defs, next, env);
    const seen: unknown[][] = [];
    projectPart(d.getMap('l:en'), defs, v, env, { text: (...a) => seen.push(a) });
    expect(seen).toEqual([['title', 'Hello', 'Hello world']]);
  });

  it('removes duplicate block ids, keeping the first copy', () => {
    const v = hyd();
    v.blocks = [{ id: 'a', type: 'columns', props: {}, slots: { left: [{ id: 'k', type: 'text', props: {} }] } }, { id: 'b', type: 'text', props: {} }];
    const d = seeded(v);
    const arr = d.getMap('l:en').get('blocks') as Y.Array<Y.Map<unknown>>;
    d.transact(() => {
      arr.push([arr.get(1).clone() as Y.Map<unknown>]);
      const left = (arr.get(0).get('slots') as Y.Map<unknown>).get('left') as Y.Array<Y.Map<unknown>>;
      left.push([arr.get(1).clone() as Y.Map<unknown>]);
    });
    expect(dedupeBlocks(arr)).toBe(true);
    const ids: string[] = [];
    const walk = (l: Y.Array<Y.Map<unknown>>) => l.forEach((m) => (ids.push(m.get('id') as string), ((m.get('slots') as Y.Map<unknown> | undefined)?.get('left') as Y.Array<Y.Map<unknown>> | undefined) && walk((m.get('slots') as Y.Map<unknown>).get('left') as Y.Array<Y.Map<unknown>>)));
    walk(arr);
    expect(ids.sort()).toEqual(['a', 'b', 'k']);
  });

  it('plain rich text values written to a fragment come back canonical', () => {
    const d = seeded();
    const v = hyd();
    v.body = rich('one', 'two');
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(read(d).body).toEqual(rich('one', 'two'));
    v.body = undefined;
    syncPart(d.getMap('l:en'), defs, v, env);
    expect(read(d).body).toBeUndefined();
  });
});

describe('emptying a string field', () => {
  it('empties the Y.Text instead of deleting the key, so a concurrent edit still merges', () => {
    const a = seeded();
    const b = new Y.Doc();
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    const va = hyd();
    va.slug = undefined;
    syncPart(a.getMap('shared'), shared, va, env);
    expect(a.getMap('shared').get('slug')).toBeInstanceOf(Y.Text);
    expect((a.getMap('shared').get('slug') as Y.Text).toString()).toBe('');
    (b.getMap('shared').get('slug') as Y.Text).insert(5, '-x');
    link(a, b);
    expect(read(a).slug).toBe(read(b).slug);
    expect(read(a).slug).toBe('-x');
  });

  it('an empty text reads back as an absent value, not as a change', () => {
    const d = seeded();
    const v = hyd();
    v.slug = undefined;
    syncPart(d.getMap('shared'), shared, v, env);
    projectPart(d.getMap('shared'), shared, v, env, {});
    expect(v.slug).toBeUndefined();
  });
});

describe('merging edits made before joining', () => {
  const base = () => hyd();
  const withRemote = (edit: (m: Y.Map<unknown>) => void) => {
    const d = seeded(base());
    edit(d.getMap('shared'));
    return d;
  };

  it('text: applies the local splice around what others wrote, never reverting it', () => {
    const d = withRemote((m) => (m.get('meta') as Y.Map<unknown>) && ((m.get('meta') as Y.Map<unknown>).get('note') as Y.Text).insert(1, ' remote'));
    const cur = base();
    (cur.meta as Doc).note = 'LOCAL n';
    mergeLocal(d.getMap('shared'), shared, cur, base(), env);
    expect((((d.getMap('shared').get('meta') as Y.Map<unknown>).get('note')) as Y.Text).toString()).toBe('LOCAL n remote');
  });

  it('text: a deletion applies only to the characters that are still there', () => {
    const b = base();
    (b.meta as Doc).note = 'abcdef';
    const d = seeded(b);
    const note = (d.getMap('shared').get('meta') as Y.Map<unknown>).get('note') as Y.Text;
    note.delete(2, 2); // others removed "cd"
    const cur = structuredClone(b);
    (cur.meta as Doc).note = 'aef'; // local removed "bcd"
    mergeLocal(d.getMap('shared'), shared, cur, b, env);
    expect(note.toString()).toBe('aef');
  });

  it('a scalar others changed keeps their value; one nobody changed takes the local edit', () => {
    const d = withRemote((m) => m.set('count', 50));
    const cur = base();
    cur.count = 7;
    cur.kind = 'b';
    mergeLocal(d.getMap('shared'), shared, cur, base(), env);
    expect(d.getMap('shared').get('count')).toBe(50);
    expect(d.getMap('shared').get('kind')).toBe('b');
  });

  it('blocks: a block added locally is kept next to what others changed', () => {
    const d = seeded(base());
    const arr = d.getMap('l:en').get('blocks') as Y.Array<Y.Map<unknown>>;
    arr.delete(0); // others removed the only block
    const cur = base();
    (cur.blocks as Doc[]).push({ id: 'mine', type: 'text', props: { heading: 'x' } });
    mergeLocal(d.getMap('l:en'), defs, cur, base(), env);
    expect(arr.toArray().map((m) => m.get('id'))).toEqual(['mine']);
  });
});

describe('rich text inside new blocks uses the field schema', () => {
  it('a block inserted from Y reads exactly as one projected in place', () => {
    const d = seeded();
    const arr = d.getMap('l:en').get('blocks') as Y.Array<Y.Map<unknown>>;
    const quote = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'q', marks: [{ type: 'bold' }] }] }] };
    d.transact(() => {
      const v = hyd();
      (v.blocks as Doc[]).push({ id: 'n', type: 'text', props: { quote } });
      syncPart(d.getMap('l:en'), defs, v, env);
    });
    const fresh: Doc[] = [];
    projectBlocks(arr, fresh, env, {}, 'blocks');
    const inPlace = structuredClone(fresh);
    projectBlocks(arr, inPlace, env, {}, 'blocks');
    expect(inPlace).toEqual(fresh);
    expect((plainBlock(arr.get(1), env).props as Doc).quote).toEqual(quote);
    const o: Doc = {};
    projectMap(d.getMap('l:en'), o, defs, env, {}, '');
    expect(((o.blocks as Doc[])[1]!.props as Doc).quote).toEqual(quote);
  });
});
