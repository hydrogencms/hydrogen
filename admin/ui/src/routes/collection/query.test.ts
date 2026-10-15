import { describe, expect, it } from 'vitest';
import type { CompiledField } from '../../lib/types.svelte';
import { filterableFields, nextSort, opsFor, parseQuery, parseViews, sortState, toApiQuery, toUrlPatch, upsertView } from './query';

const url = '?q=hello+world&status=draft&locale=de&sort=-title&page=3&where[sku][prefix]=AB&where[price][gte]=10&where[active]=true';

describe('list query', () => {
  it('reads the address bar', () => {
    const q = parseQuery(new URLSearchParams(url));
    expect(q).toMatchObject({ q: 'hello world', status: 'draft', locale: 'de', sort: '-title', page: 3, pageSize: 25 });
    expect(q.filters).toEqual([
      { field: 'sku', op: 'prefix', value: 'AB' },
      { field: 'price', op: 'gte', value: '10' },
      { field: 'active', op: 'eq', value: 'true' },
    ]);
  });
  it('maps to API parameters', () => {
    expect(toApiQuery(parseQuery(new URLSearchParams(url)))).toEqual({
      limit: 25, offset: 50, q: 'hello world', status: 'draft', locale: 'de', sort: '-title',
      'where[sku][prefix]': 'AB', 'where[price][gte]': '10', 'where[active][eq]': 'true',
    });
  });
  it('defaults', () => {
    expect(toApiQuery(parseQuery(new URLSearchParams('')), 'en')).toEqual({ limit: 25, offset: 0, locale: 'en', sort: '-updatedAt' });
  });
  it('ignores junk', () => {
    const q = parseQuery(new URLSearchParams('status=bogus&page=-2&where[a][zz]=1&where[]=2&size=9999'));
    expect(q).toMatchObject({ status: '', page: 1, pageSize: 25, filters: [] });
  });
  it('round trips URL -> state -> URL patch', () => {
    const cur = new URLSearchParams(url);
    const q = parseQuery(cur);
    const patch = toUrlPatch(q, cur);
    const next = new URLSearchParams();
    for (const [k, v] of Object.entries(patch)) if (v !== undefined) next.set(k, String(v));
    expect(parseQuery(next)).toEqual(q);
  });
  it('clears stale filters in the patch', () => {
    const cur = new URLSearchParams('where[a][eq]=1&where[b][ne]=2');
    const q = parseQuery(cur);
    q.filters = q.filters.slice(1);
    const patch = toUrlPatch(q, cur);
    expect(patch['where[b][ne]']).toBe('2');
    expect(patch['where[a][eq]']).toBeUndefined();
    expect('where[a][eq]' in patch).toBe(true);
  });
  it('sends exists with its boolean and skips empty values', () => {
    const q = parseQuery(new URLSearchParams(''));
    q.filters = [{ field: 'a', op: 'exists', value: 'true' }, { field: 'b', op: 'eq', value: '' }];
    expect(toApiQuery(q)).toMatchObject({ 'where[a][exists]': 'true' });
    expect(toApiQuery(q)).not.toHaveProperty('where[b][eq]');
  });
});

describe('sorting', () => {
  it('toggles direction', () => {
    const q = parseQuery(new URLSearchParams('sort=title'));
    expect(sortState(q)).toEqual({ key: 'title', dir: 'asc' });
    expect(nextSort(q, 'title')).toBe('-title');
    expect(nextSort(parseQuery(new URLSearchParams('sort=-title')), 'title')).toBe('title');
    expect(nextSort(q, 'name')).toBe('name');
    expect(nextSort(q, 'updatedAt', true)).toBe('-updatedAt');
    expect(sortState(parseQuery(new URLSearchParams(''))).key).toBe('updatedAt');
  });
});

describe('operators', () => {
  const f = (base: string, extra: Partial<CompiledField> = {}) => ({ name: 'x', label: 'x', type: base, index: true, shape: { base }, ...extra }) as CompiledField;
  it('depends on the base type', () => {
    expect(opsFor(f('string'))).toEqual(['eq', 'ne', 'in', 'prefix', 'exists']);
    expect(opsFor(f('number'))).toContain('gte');
    expect(opsFor(f('number'))).not.toContain('prefix');
    expect(opsFor(f('string', { shape: { base: 'string', format: 'date' } }))).toEqual(['eq', 'ne', 'lt', 'lte', 'gt', 'gte', 'exists']);
    expect(opsFor(f('boolean'))).toEqual(['eq', 'ne', 'in', 'exists']);
    expect(opsFor(f('list', { of: f('string') }))).toContain('contains');
  });
  it('lists only indexed filterable fields', () => {
    expect(filterableFields([f('string'), f('richtext'), { ...f('string'), index: false }]).length).toBe(1);
  });
});

describe('saved views', () => {
  it('parses safely and upserts by name', () => {
    expect(parseViews('nope')).toEqual([]);
    expect(parseViews('[{"name":"a","query":"q=1"},5]')).toEqual([{ name: 'a', query: 'q=1' }]);
    expect(upsertView([{ name: 'b', query: '1' }], { name: 'a', query: '2' }).map((v) => v.name)).toEqual(['a', 'b']);
    expect(upsertView([{ name: 'a', query: '1' }], { name: 'a', query: '2' })).toEqual([{ name: 'a', query: '2' }]);
  });
});
