import { describe, expect, it } from 'vitest';
import type { CompiledField } from '../types.svelte';
import {
  autoSlug, hydrate, cleanDoc, copyTranslatable, errorFieldNames, evalWhen, fieldAtPath, fieldsPatch, fromLocalInput, isSlug,
  joinPath, listAdd, listMove, listRemove, mergePatch, parsePath, richPreview, slugify, slugSource, toLocalInput,
  runPool, unroutedErrors,
} from './logic';

const f = (name: string, base: string, extra: Partial<CompiledField> = {}): CompiledField =>
  ({ name, label: name, type: base, shape: { base }, ...extra }) as CompiledField;

describe('slugs', () => {
  it('slugifies like the server', () => {
    expect(slugify('Hello, World!')).toBe('hello-world');
    expect(slugify('  Crème Brûlée & Co. ')).toBe('creme-brulee-co');
    expect(slugify('Straße Æther')).toBe('strasse-aether');
    expect(slugify('日本語 タイトル')).toBe('日本語-タイトル');
    expect(slugify('---')).toBe('');
    expect(slugify('x'.repeat(200)).length).toBe(120);
  });
  it('validates slugs', () => {
    expect(isSlug('a-b-1')).toBe(true);
    for (const bad of ['', 'A', 'a--b', '-a', 'a-', 'a b', 'a_b']) expect(isSlug(bad)).toBe(false);
  });
  it('follows the source until edited', () => {
    expect(autoSlug('', '', 'Hello')).toBe('hello');
    expect(autoSlug('hello', 'Hello', 'Hello World')).toBe('hello-world');
    expect(autoSlug('custom', 'Hello', 'Hello World')).toBe('custom');
  });
  it('finds the source field', () => {
    expect(slugSource(f('slug', 'string', { options: { from: 'title' } }))).toBe('title');
    expect(slugSource(f('slug', 'string'), '{name} - {x}')).toBe('name');
    expect(slugSource(f('slug', 'string'))).toBeUndefined();
  });
});

describe('when', () => {
  const w = (op: string, value?: unknown) => ({ field: 'kind', op, value });
  it('eq / ne', () => {
    expect(evalWhen(w('eq', 'a'), { kind: 'a' })).toBe(true);
    expect(evalWhen(w('eq', 'a'), { kind: 'b' })).toBe(false);
    expect(evalWhen(w('eq', 'a'), {})).toBe(false);
    expect(evalWhen(w('ne', 'a'), {})).toBe(true);
    expect(evalWhen(w('ne', 'a'), { kind: 'a' })).toBe(false);
  });
  it('in', () => {
    expect(evalWhen(w('in', ['a', 'b']), { kind: 'b' })).toBe(true);
    expect(evalWhen(w('in', ['a', 'b']), { kind: 'c' })).toBe(false);
    expect(evalWhen(w('in', ['a']), {})).toBe(false);
  });
  it('empty', () => {
    expect(evalWhen(w('empty', true), {})).toBe(true);
    expect(evalWhen(w('empty', true), { kind: '' })).toBe(true);
    expect(evalWhen(w('empty', true), { kind: [] })).toBe(true);
    expect(evalWhen(w('empty', true), { kind: 'x' })).toBe(false);
    expect(evalWhen(w('empty', false), { kind: 'x' })).toBe(true);
    expect(evalWhen(w('empty', false), { kind: 0 })).toBe(true);
  });
  it('no condition shows the field', () => expect(evalWhen(undefined, {})).toBe(true));
  it('compares booleans and lists deeply', () => {
    expect(evalWhen(w('eq', true), { kind: true })).toBe(true);
    expect(evalWhen(w('eq', ['a']), { kind: ['a'] })).toBe(true);
  });
});

describe('patches', () => {
  const fields = [
    f('title', 'string'),
    f('count', 'number'),
    f('tags', 'list', { of: f('item', 'string') }),
    f('seo', 'group', { fields: [f('desc', 'string'), f('noindex', 'boolean')] }),
  ];
  it('sends only changed top-level fields', () => {
    const saved = { title: 'A', count: 1, tags: ['x'] };
    expect(fieldsPatch(fields, saved, { ...saved })).toEqual({});
    expect(fieldsPatch(fields, saved, { ...saved, title: 'B' })).toEqual({ title: 'B' });
  });
  it('removes cleared values with null', () => {
    expect(fieldsPatch(fields, { title: 'A', count: 1 }, { title: '', count: 1 })).toEqual({ title: null });
    expect(fieldsPatch(fields, { tags: ['a'] }, { tags: [] })).toEqual({ tags: null });
  });
  it('treats empty and missing alike', () => {
    expect(fieldsPatch(fields, {}, { title: '', tags: [], seo: { desc: '' } })).toEqual({});
  });
  it('patches groups key by key', () => {
    expect(fieldsPatch(fields, { seo: { desc: 'a', noindex: true } }, { seo: { desc: 'b', noindex: true } })).toEqual({ seo: { desc: 'b' } });
    expect(fieldsPatch(fields, { seo: { desc: 'a', noindex: true } }, { seo: { desc: 'a' } })).toEqual({ seo: { noindex: null } });
  });
  it('replaces lists whole', () => {
    expect(fieldsPatch(fields, { tags: ['a', 'b'] }, { tags: ['b', 'a'] })).toEqual({ tags: ['b', 'a'] });
  });
  it('cleanDoc keeps booleans false and numbers 0', () => {
    expect(cleanDoc(fields, { count: 0, seo: { noindex: false } })).toEqual({ count: 0, seo: { noindex: false } });
  });
  it('mergePatch of unequal scalars', () => expect(mergePatch(1, 2)).toBe(2));
  it('copies untranslated translatable fields', () => {
    const fs = [f('title', 'string', { translatable: true }), f('sku', 'string'), f('intro', 'string', { translatable: true })];
    expect(copyTranslatable(fs, { title: '', sku: '', intro: 'mine' }, { title: 'T', sku: 'S', intro: 'I' })).toEqual({
      title: 'T', sku: '', intro: 'mine',
    });
  });
});

describe('list state', () => {
  it('adds, removes, moves', () => {
    expect(listAdd(['a'], 'b')).toEqual(['a', 'b']);
    expect(listAdd(undefined, 'b')).toEqual(['b']);
    expect(listAdd(['a', 'c'], 'b', 1)).toEqual(['a', 'b', 'c']);
    expect(listRemove(['a', 'b', 'c'], 1)).toEqual(['a', 'c']);
    expect(listMove(['a', 'b', 'c'], 2, -1)).toEqual(['a', 'c', 'b']);
    expect(listMove(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c']);
  });
  it('ignores moves off the ends and does not mutate', () => {
    const l = ['a', 'b'];
    expect(listMove(l, 0, -1)).toEqual(['a', 'b']);
    expect(listMove(l, 1, 1)).toEqual(['a', 'b']);
    expect(l).toEqual(['a', 'b']);
  });
});

describe('error paths', () => {
  const fields = [
    f('title', 'string'),
    f('kind', 'string'),
    f('extra', 'string', { when: { field: 'kind', op: 'eq', value: 'x' } }),
    f('blocks', 'list', { of: f('b', 'group', { fields: [f('text', 'string')] }) }),
    f('secret', 'string', { admin: { hidden: true } }),
  ];
  it('parses and joins paths', () => {
    expect(parsePath('blocks[2].text')).toEqual(['blocks', 2, 'text']);
    expect(joinPath(joinPath('blocks', 2), 'text')).toBe('blocks[2].text');
    expect(joinPath('', 'title')).toBe('title');
  });
  it('resolves the field at a path', () => {
    expect(fieldAtPath(fields, 'blocks[1].text')?.name).toBe('text');
    expect(fieldAtPath(fields, 'blocks[1]')?.name).toBe('b');
    expect(fieldAtPath(fields, 'nope')).toBeUndefined();
  });
  it('routes errors to visible widgets and reports the rest', () => {
    const errors = { title: 'e1', 'blocks[0].text': 'e2', extra: 'e3', secret: 'e4', ghost: 'e5', 'blocks[0].nope': 'e6' };
    expect(unroutedErrors(fields, errors, { kind: 'y', blocks: [{}] })).toEqual({ extra: 'e3', secret: 'e4', ghost: 'e5', 'blocks[0].nope': 'e6' });
    expect(unroutedErrors(fields, { extra: 'e3' }, { kind: 'x' })).toEqual({});
  });
  it('lists top-level names with errors', () => {
    expect([...errorFieldNames({ 'blocks[0].text': 'x', title: 'y' })].sort()).toEqual(['blocks', 'title']);
  });
});

describe('dates', () => {
  it('round trips a local datetime', () => {
    const iso = fromLocalInput('2026-10-15T09:30');
    expect(iso).toMatch(/Z$/);
    expect(toLocalInput(iso)).toBe('2026-10-15T09:30');
    expect(fromLocalInput('')).toBe('');
    expect(toLocalInput('nonsense')).toBe('');
  });
});

describe('rich text preview', () => {
  it('flattens blocks', () => {
    const doc = {
      type: 'doc',
      content: [
        { type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'Hi' }] },
        { type: 'paragraph', content: [{ type: 'text', text: 'a' }, { type: 'hardBreak' }, { type: 'text', text: 'b' }] },
        { type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'one' }] }] }] },
      ],
    };
    expect(richPreview(doc)).toEqual([
      { kind: 'heading', level: 2, text: 'Hi' },
      { kind: 'paragraph', text: 'a\nb' },
      { kind: 'item', text: '• one' },
    ]);
    expect(richPreview(null)).toEqual([]);
  });
});

describe('runPool', () => {
  it('limits concurrency, keeps order and reports failures', async () => {
    let live = 0;
    let peak = 0;
    const r = await runPool([1, 2, 3, 4, 5], 2, async (n) => {
      live++;
      peak = Math.max(peak, live);
      await new Promise((res) => setTimeout(res, 5));
      live--;
      if (n === 3) throw new Error('boom');
      return n * 2;
    });
    expect(peak).toBe(2);
    expect(r.map((x) => (x.ok ? x.value : 'err'))).toEqual([2, 4, 'err', 8, 10]);
  });
});

describe('hydrate', () => {
  it('fills unset groups so widgets can write into them', () => {
    const fs = [f('seo', 'group', { fields: [f('desc', 'string'), f('og', 'group', { fields: [f('t', 'string')] })] }), f('title', 'string')];
    const h = hydrate(fs, { title: 'x' });
    expect(h).toEqual({ title: 'x', seo: { og: {} } });
    expect(fieldsPatch(fs, { title: 'x' }, h)).toEqual({});
  });
  it('reaches groups inside lists', () => {
    const fs = [f('rows', 'list', { of: f('r', 'group', { fields: [f('x', 'group', { fields: [f('y', 'string')] })] }) })];
    expect(hydrate(fs, { rows: [{}] })).toEqual({ rows: [{ x: {} }] });
  });
});
