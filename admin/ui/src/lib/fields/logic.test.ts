import { describe, expect, it } from 'vitest';
import type { CompiledField } from '../types.svelte';
import type { Doc } from './logic';
import {
  autoSlug, isHomePage, hydrate, cleanDoc, copyTranslatable, errorFieldNames, evalWhen, fieldAtPath, fieldsPatch, fromLocalInput, isSlug,
  joinPath, listAdd, listMove, listRemove, mergePatch, parsePath, slugify, slugSource, toLocalInput,
  runPool, unroutedErrors, viewValues, overlayState, resolvedDoc, overrideValue,
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

describe('overlays', () => {
  const fs = [
    f('title', 'string', { translatable: true }),
    f('body', 'richtext', { translatable: true }),
    f('seo', 'group', { translatable: true, fields: [f('desc', 'string'), f('noindex', 'boolean')] }),
    f('tags', 'list', { translatable: true, of: f('t', 'string') }),
    f('count', 'number'),
  ];
  const view = { fields: { title: 'Hallo', body: { type: 'doc' }, count: 3 }, inherits: 'de', overrides: ['title'] };

  it('keeps only the shared fields and the overrides in the form', () => {
    expect(viewValues(fs, view)).toEqual({ title: 'Hallo', count: 3 });
    expect(viewValues(fs, { fields: view.fields })).toEqual({ ...view.fields, seo: {} });
  });
  it('reports the field state', () => {
    const doc = viewValues(fs, view);
    expect(overlayState(doc, 'title')).toBe('overridden');
    expect(overlayState(doc, 'body')).toBe('inherited');
    delete doc.title;
    expect(overlayState(doc, 'title')).toBe('inherited');
  });
  it('sends only the override diff', () => {
    const saved = viewValues(fs, view);
    expect(fieldsPatch(fs, saved, { ...saved }, true)).toEqual({});
    expect(fieldsPatch(fs, saved, { ...saved, title: 'Servus' }, true)).toEqual({ title: 'Servus' });
    expect(fieldsPatch(fs, saved, { ...saved, body: { type: 'doc', content: [] } }, true)).toEqual({ body: { type: 'doc', content: [] } });
    expect(fieldsPatch(fs, saved, { ...saved, count: 4 }, true)).toEqual({ count: 4 });
  });
  it('sends null for a reset override and nothing for an untouched inherited field', () => {
    const saved = viewValues(fs, view);
    const cur = { ...saved };
    delete cur.title;
    expect(fieldsPatch(fs, saved, cur, true)).toEqual({ title: null });
    expect(fieldsPatch(fs, {}, {}, true)).toEqual({});
  });
  it('treats an emptied override as an override, not a reset', () => {
    const saved = viewValues(fs, view);
    expect(fieldsPatch(fs, saved, { ...saved, title: '' }, true)).toEqual({ title: '' });
    expect(fieldsPatch(fs, {}, { tags: [] }, true)).toEqual({ tags: [] });
    expect(fieldsPatch(fs, { tags: [] }, { tags: [] }, true)).toEqual({});
  });
  it('merges inside an overridden group', () => {
    expect(fieldsPatch(fs, { seo: { desc: 'a', noindex: true } }, { seo: { desc: 'b', noindex: true } }, true)).toEqual({ seo: { desc: 'b' } });
    expect(fieldsPatch(fs, {}, { seo: { desc: 'a' } }, true)).toEqual({ seo: { desc: 'a' } });
  });
  it('override then reset round trips to no change; the non-overlay patch is unchanged', () => {
    const saved = viewValues(fs, view);
    const cur: Doc = { ...saved, body: { type: 'doc' } };
    delete cur.body;
    expect(fieldsPatch(fs, saved, cur, true)).toEqual({});
    expect(fieldsPatch(fs, { title: 'A' }, { title: '' })).toEqual({ title: null });
  });
  it('resolves inherited fields from the parent for conditions', () => {
    const doc = { title: 'x', count: 1 };
    expect(resolvedDoc(fs, doc, { title: 'p', body: 'b', count: 9 })).toEqual({ title: 'x', body: 'b', count: 1 });
    expect(resolvedDoc(fs, doc, undefined)).toEqual(doc);
  });
  it('starts an override from a copy of the inherited value', () => {
    const inh = { type: 'doc', content: [1] };
    const v = overrideValue(fs[1]!, inh) as typeof inh;
    expect(v).toEqual(inh);
    expect(v).not.toBe(inh);
    expect(overrideValue(fs[0]!, undefined)).toBe('');
    expect(overrideValue(fs[3]!, undefined)).toEqual([]);
    expect(overrideValue(f('n', 'number', { translatable: true }), undefined)).toBeNull();
  });
});

describe('isHomePage', () => {
  it('is true for a current route at the root only', () => {
    expect(isHomePage([{ path: '/', state: 'current' }])).toBe(true);
    expect(isHomePage([{ path: '/about', state: 'current' }, { path: '/', state: 'alias' }])).toBe(false);
    expect(isHomePage([])).toBe(false);
  });
});
