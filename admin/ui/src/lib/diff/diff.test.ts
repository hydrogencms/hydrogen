import { describe, expect, it } from 'vitest';
import type { CompiledField } from '../types.svelte';
import type { BlockType } from '../blocks/tree';
import { canonKey, changedLocales, diffBlocks, diffDocuments, diffFields, diffValue, mergeLocale, refIds, sharedChanged, type Change } from './diff';
import { diffRich } from './rich';
import { diffSeq, diffWords, diffWordsEx } from './seq';

const f = (name: string, base: string, extra: Partial<CompiledField> = {}): CompiledField => ({
  name,
  label: name[0]!.toUpperCase() + name.slice(1),
  type: base,
  shape: { base },
  ...extra,
});
const para = (text: string) => ({ type: 'paragraph', content: [{ type: 'text', text }] });
const doc = (...n: unknown[]) => ({ type: 'doc', content: n });

describe('diffSeq / diffWords', () => {
  it('finds common prefix, suffix and middle', () => {
    const ops = diffSeq(['a', 'b', 'c', 'd'], ['a', 'x', 'c', 'd']);
    expect(ops.map((o) => o.k)).toEqual(['eq', 'del', 'ins', 'eq', 'eq']);
  });
  it('word diff keeps whitespace and joins runs', () => {
    expect(diffWords('the quick fox', 'the slow fox')).toEqual([
      { k: 'eq', text: 'the ' },
      { k: 'del', text: 'quick' },
      { k: 'ins', text: 'slow' },
      { k: 'eq', text: ' fox' },
    ]);
  });
  it('matches words, not spacing, and keeps a changed phrase in one piece', () => {
    expect(diffWords('a b c d e f', 'a b X Y e f')).toEqual([
      { k: 'eq', text: 'a b ' },
      { k: 'del', text: 'c d' },
      { k: 'ins', text: 'X Y' },
      { k: 'eq', text: ' e f' },
    ]);
  });
  it('shows mostly different texts as one deletion and one insertion', () => {
    expect(diffWords('alpha beta gamma delta', 'one two three four alpha')).toEqual([
      { k: 'del', text: 'alpha beta gamma delta' },
      { k: 'ins', text: 'one two three four alpha' },
    ]);
  });
  it('shows a spacing-only change instead of no change', () => {
    const segs = diffWords('a b', 'a  b');
    expect(segs.map((x) => x.k)).toEqual(['del', 'ins']);
  });
  it('reports the fallback for inputs over the limit', () => {
    const a = Array.from({ length: 30 }, (_, i) => `a${i}`).join(' ');
    const b = Array.from({ length: 30 }, (_, i) => `a${i % 2 ? i : 'x' + i}`).join(' ');
    expect(diffWordsEx(a, b, 10).approx).toBe(true);
    expect(diffWordsEx(a, b).approx).toBe(false);
  });
  it('canonical keys follow deepEqual', () => {
    expect(canonKey({ a: 1, b: '' })).toBe(canonKey({ a: 1 }));
    expect(canonKey({ b: 2, a: 1 })).toBe(canonKey({ a: 1, b: 2 }));
    expect(canonKey([1, 2])).not.toBe(canonKey([2, 1]));
  });
  it('missing sides are all inserted or all deleted', () => {
    expect(diffWords(undefined, 'new text')).toEqual([{ k: 'ins', text: 'new text' }]);
    expect(diffWords('old', undefined)).toEqual([{ k: 'del', text: 'old' }]);
  });
  it('degrades to a whole replacement for huge inputs', () => {
    const a = Array.from({ length: 3000 }, (_, i) => `a${i}`);
    const b = Array.from({ length: 3000 }, (_, i) => `b${i}`);
    const ops = diffSeq(a, b);
    expect(ops.filter((o) => o.k === 'del')).toHaveLength(3000);
    expect(ops.filter((o) => o.k === 'ins')).toHaveLength(3000);
  });
});

describe('rich text', () => {
  it('diffs per block, collapsing unchanged ones', () => {
    const a = doc(para('one'), para('two words'), para('three'));
    const b = doc(para('one'), para('two changed words'), para('three'), para('four'));
    const r = diffRich(a, b);
    expect(r.same).toBe(2);
    expect(r.rows.map((x) => x.status)).toEqual(['changed', 'added']);
    expect(r.rows[0]!.segs.some((s) => s.k === 'ins' && s.text.includes('changed'))).toBe(true);
  });
  it('reports removed blocks and heading levels', () => {
    const r = diffRich(doc({ type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'Title' }] }), doc());
    expect(r.rows).toEqual([{ status: 'removed', node: 'heading 2', segs: [{ k: 'del', text: 'Title' }] }]);
  });
  it('flags formatting-only changes', () => {
    const plain = doc(para('same'));
    const bold = doc({ type: 'paragraph', content: [{ type: 'text', text: 'same', marks: [{ type: 'bold' }] }] });
    const r = diffRich(plain, bold);
    expect(r.rows).toHaveLength(1);
    expect(r.rows[0]!.formatting).toBe(true);
  });
});

describe('diffValue', () => {
  it('is null for equal and equally empty values', () => {
    expect(diffValue(f('t', 'string'), 'a', 'a')).toBeNull();
    expect(diffValue(f('t', 'string'), undefined, '')).toBeNull();
    expect(diffValue(f('l', 'list', { of: f('x', 'string') }), [], undefined)).toBeNull();
  });
  it('word-diffs strings, but shows enums as values', () => {
    expect(diffValue(f('t', 'string'), 'a b', 'a c')!.kind).toBe('text');
    const e = diffValue(f('s', 'string', { shape: { base: 'string', format: 'enum' } }), 'x', 'y');
    expect(e).toEqual({ kind: 'value', before: 'x', after: 'y' });
  });
  it('compares numbers and booleans as values', () => {
    expect(diffValue(f('n', 'number'), 1, 2)).toEqual({ kind: 'value', before: 1, after: 2 });
    expect(diffValue(f('b', 'boolean'), false, true)).toEqual({ kind: 'value', before: false, after: true });
  });
  it('resolves reference names', () => {
    const c = diffValue(f('r', 'ref'), 'i1', 'i2', { names: { i1: 'One', i2: 'Two' } });
    expect(c).toEqual({ kind: 'ref', before: { id: 'i1', name: 'One' }, after: { id: 'i2', name: 'Two' } });
  });
  it('diffs lists by item, pairing edits', () => {
    const l = f('tags', 'list', { of: f('tag', 'string') });
    const c = diffValue(l, ['a', 'b', 'c'], ['a', 'x', 'c', 'd']) as Extract<Change, { kind: 'list' }>;
    expect(c.same).toBe(2);
    expect(c.rows.map((r) => [r.status, r.index])).toEqual([
      ['changed', 2],
      ['added', 4],
    ]);
  });
  it('recurses into groups', () => {
    const g = f('seo', 'group', { fields: [f('title', 'string'), f('desc', 'string')] });
    const c = diffValue(g, { title: 'a', desc: 'same' }, { title: 'b', desc: 'same' }) as Extract<Change, { kind: 'group' }>;
    expect(c.fields.map((x) => x.name)).toEqual(['title']);
    expect(c.same).toEqual(['Desc']);
  });
});

describe('diffFields', () => {
  const fields = [f('title', 'string'), f('count', 'number'), f('note', 'string')];
  it('lists changed fields and collapses the rest', () => {
    const d = diffFields(fields, { title: 'a', count: 1 }, { title: 'b', count: 1, note: 'n' });
    expect(d.fields.map((x) => [x.name, x.status])).toEqual([
      ['title', 'changed'],
      ['note', 'added'],
    ]);
    expect(d.same).toEqual([{ name: 'count', label: 'Count' }]);
  });
  it('marks removed fields', () => {
    expect(diffFields(fields, { title: 'a' }, {}).fields[0]!.status).toBe('removed');
  });
});

describe('locales', () => {
  const fields = [f('title', 'string', { translatable: true }), f('code', 'string')];
  const a = { shared: { code: 'x' }, locales: { en: { title: 'Hello' }, de: { title: 'Hallo' } } };
  const b = { shared: { code: 'y' }, locales: { en: { title: 'Hello' }, de: { title: 'Hallo!' }, fr: { title: 'Salut' } } };
  it('merges shared and locale fields', () => {
    expect(mergeLocale(fields, a, 'de')).toEqual({ doc: { code: 'x', title: 'Hallo' }, present: true });
    expect(mergeLocale(fields, a, 'fr').present).toBe(false);
  });
  it('diffs one locale and reports a missing side', () => {
    const d = diffDocuments(fields, a, b, 'fr');
    expect(d.missing).toEqual({ a: true, b: false });
    expect(d.fields.map((x) => x.name).sort()).toEqual(['code', 'title']);
  });
  it('lists locales with translatable changes', () => {
    expect(changedLocales(fields, a, b)).toEqual(['de', 'fr']);
    expect(sharedChanged(fields, a, b)).toBe(true);
  });
});

describe('blocks', () => {
  const hero: BlockType = { name: 'hero', label: 'Hero', props: [f('title', 'string'), f('level', 'number')], slots: [{ name: 'items', label: 'Items', allow: ['card'] }] };
  const card: BlockType = { name: 'card', label: 'Card', props: [f('text', 'string')] };
  const o = { block: (t: string) => ({ hero, card })[t as 'hero'] };
  type B = { id: string; type: string; props: Record<string, unknown>; slots?: Record<string, B[]> };
  const blk = (id: string, type: string, props: Record<string, unknown> = {}, slots?: Record<string, B[]>): B => ({ id, type, props, slots });

  it('has no rows when nothing changed', () => {
    const l = [blk('1', 'hero', { title: 'a' })];
    expect(diffBlocks(l, structuredClone(l), o)).toEqual({ rows: [], same: 1 });
  });
  it('detects added, removed and changed blocks with prop diffs', () => {
    const a = [blk('1', 'hero', { title: 'Hello world', level: 1 }), blk('2', 'card', { text: 'gone' })];
    const b = [blk('1', 'hero', { title: 'Hello there', level: 1 }), blk('3', 'card', { text: 'new' })];
    const r = diffBlocks(a, b, o);
    expect(r.rows.map((x) => [x.id, x.status])).toEqual([
      ['1', 'changed'],
      ['3', 'added'],
      ['2', 'removed'],
    ]);
    expect(r.rows[0]!.props.map((p) => p.name)).toEqual(['title']);
    expect(r.rows[0]!.sameProps).toEqual(['Level']);
    expect(r.rows[0]!.label).toBe('Hero');
  });
  it('compares blocks without ids by position', () => {
    const a = [{ type: 'card', props: { text: 'same' } }, { type: 'card', props: { text: 'old' } }];
    const b = [{ type: 'card', props: { text: 'same' } }, { type: 'card', props: { text: 'new' } }];
    const r = diffBlocks(a, b, o);
    expect(r.same).toBe(1);
    expect(r.rows.map((x) => x.status)).toEqual(['changed']);
  });
  it('does not count moved children in a removed block', () => {
    const a = [blk('1', 'hero', {}, { items: [blk('2', 'card'), blk('3', 'card')] })];
    const b = [blk('2', 'card')];
    const r = diffBlocks(a, b, o);
    const removed = r.rows.find((x) => x.status === 'removed')!;
    expect(removed.id).toBe('1');
    expect(removed.nested).toBe(1);
  });
  it('detects moves within a list, once', () => {
    const a = [blk('1', 'card', { text: 'a' }), blk('2', 'card', { text: 'b' }), blk('3', 'card', { text: 'c' })];
    const b = [blk('2', 'card', { text: 'b' }), blk('3', 'card', { text: 'c' }), blk('1', 'card', { text: 'a' })];
    const r = diffBlocks(a, b, o);
    expect(r.rows.map((x) => [x.id, x.status, x.moved])).toEqual([['1', 'moved', true]]);
    expect(r.same).toBe(2);
  });
  it('detects a move into another slot and nested changes', () => {
    const a = [blk('h', 'hero', { title: 't' }, { items: [blk('c1', 'card', { text: 'x' })] }), blk('c2', 'card', { text: 'y' })];
    const b = [blk('h', 'hero', { title: 't' }, { items: [blk('c1', 'card', { text: 'x2' }), blk('c2', 'card', { text: 'y' })] })];
    const r = diffBlocks(a, b, o);
    expect(r.rows).toHaveLength(1);
    const h = r.rows[0]!;
    expect(h.status).toBe('changed');
    expect(h.slots[0]!.rows.map((x) => [x.id, x.status, x.moved])).toEqual([
      ['c1', 'changed', false],
      ['c2', 'moved', true],
    ]);
  });
  it('counts blocks inside a removed block', () => {
    const a = [blk('h', 'hero', {}, { items: [blk('c', 'card'), blk('d', 'card')] })];
    const r = diffBlocks(a, [], o);
    expect(r.rows[0]).toMatchObject({ status: 'removed', nested: 2 });
  });
  it('compares shared blocks by reference', () => {
    const a = [blk('s', 'shared', { item: 'i1' })];
    const b = [blk('s', 'shared', { item: 'i2' })];
    const r = diffBlocks(a, b, { names: { i1: 'Footer', i2: 'Footer v2' } });
    const c = r.rows[0]!.props[0]!.change;
    expect(c).toEqual({ kind: 'ref', before: { id: 'i1', name: 'Footer' }, after: { id: 'i2', name: 'Footer v2' } });
  });
  it('compares unknown block types by their values', () => {
    const r = diffBlocks([blk('1', 'mystery', { a: 1 })], [blk('1', 'mystery', { a: 2 })]);
    expect(r.rows[0]!.props[0]).toMatchObject({ name: 'a', change: { kind: 'value', before: 1, after: 2 } });
  });
  it('works through a blocks field', () => {
    const field = f('body', 'blocks');
    const c = diffValue(field, [blk('1', 'card', { text: 'a' })], [blk('1', 'card', { text: 'b' })], o);
    expect(c!.kind).toBe('blocks');
  });
});

describe('refIds', () => {
  it('collects references everywhere', () => {
    const card: BlockType = { name: 'card', label: 'Card', props: [f('link', 'ref')] };
    const fields = [f('author', 'ref'), f('related', 'list', { of: f('r', 'ref') }), f('body', 'blocks'), f('seo', 'group', { fields: [f('img', 'ref')] })];
    const d = {
      author: 'a1',
      related: ['r1', 'r2'],
      body: [{ id: 'x', type: 'card', props: { link: 'l1' } }, { id: 'y', type: 'shared', props: { item: 's1' } }],
      seo: { img: 'i1' },
    };
    expect(refIds(fields, [d], { block: (t) => (t === 'card' ? card : undefined) }).sort()).toEqual(['a1', 'i1', 'l1', 'r1', 'r2', 's1']);
  });
});
