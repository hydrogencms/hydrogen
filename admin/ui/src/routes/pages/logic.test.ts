import { describe, expect, it } from 'vitest';
import {
  addableTypes, canHold, emptyFilter, filterText, matchesFilter, mountQuery, parseFilter, placement, urlChanges,
  visibleIds, type PNode,
} from './logic';

const opts: Record<string, Record<string, unknown>> = {
  page: { children: ['page', 'landing'] },
  landing: { children: [] },
  free: {},
};
const optionsOf = (t: string) => opts[t];

const n = (id: string, name: string, extra: Partial<PNode> = {}): PNode => ({
  id, site: 's', kind: 'page', hidden: false, depth: 0, name, children: 0, type: 'page', status: 'published', ...extra,
});

describe('allowed children', () => {
  it('lists the declared types, or any page type when absent', () => {
    const types = ['page', 'landing', 'free'];
    expect(addableTypes({ kind: 'page', type: 'page' }, types, optionsOf)).toEqual(['page', 'landing']);
    expect(addableTypes({ kind: 'page', type: 'landing' }, types, optionsOf)).toEqual([]);
    expect(addableTypes({ kind: 'page', type: 'free' }, types, optionsOf)).toEqual(types);
  });
  it('refuses mounts, links and restricted parents', () => {
    expect(addableTypes({ kind: 'mount' }, ['page'], optionsOf)).toEqual([]);
    expect(canHold({ kind: 'page', type: 'page', restricted: true }, { kind: 'page', type: 'page' }, optionsOf)).toBe(false);
    expect(canHold({ kind: 'link' }, { kind: 'page', type: 'page' }, optionsOf)).toBe(false);
    expect(canHold(undefined, { kind: 'page', type: 'page' }, optionsOf)).toBe(false);
  });
  it('checks the moved page type against the parent', () => {
    expect(canHold({ kind: 'page', type: 'page' }, { kind: 'page', type: 'landing' }, optionsOf)).toBe(true);
    expect(canHold({ kind: 'page', type: 'landing' }, { kind: 'page', type: 'page' }, optionsOf)).toBe(false);
    expect(canHold({ kind: 'page', type: 'landing' }, { kind: 'link' }, optionsOf)).toBe(true);
    expect(canHold({ kind: 'page', type: 'free' }, { kind: 'page', type: 'landing' }, optionsOf)).toBe(true);
  });
});

describe('placement', () => {
  const kids: Record<string, string[]> = { p: ['a', 'b', 'c'], q: [] };
  const siblings = (id: string) => kids[id] ?? [];
  it('inside goes last, or first into an empty parent', () => {
    expect(placement('inside', { id: 'p' }, 'x', siblings)).toEqual({ parent: 'p', after: 'c' });
    expect(placement('inside', { id: 'q' }, 'x', siblings)).toEqual({ parent: 'q', after: null });
  });
  it('before and after are relative to the target, skipping the moving node', () => {
    expect(placement('after', { id: 'b', parent: 'p' }, 'x', siblings)).toEqual({ parent: 'p', after: 'b' });
    expect(placement('before', { id: 'b', parent: 'p' }, 'x', siblings)).toEqual({ parent: 'p', after: 'a' });
    expect(placement('before', { id: 'a', parent: 'p' }, 'x', siblings)).toEqual({ parent: 'p', after: null });
    expect(placement('before', { id: 'c', parent: 'p' }, 'b', siblings)).toEqual({ parent: 'p', after: 'a' });
  });
  it('has no place beside the root', () => {
    expect(placement('before', { id: 'r' }, 'x', siblings)).toBeNull();
  });
});

describe('urlChanges', () => {
  const sub = {
    en: [
      { id: 'm', name: 'Team', kind: 'page' as const, path: '/about/team', status: 'published' as const },
      { id: 'c1', name: 'Bio', kind: 'page' as const, path: '/about/team/bio', status: 'draft' as const },
      { id: 'c2', name: 'Teammates', kind: 'page' as const, path: '/about/teammates', status: 'published' as const },
    ],
    de: [{ id: 'm', name: 'Team', kind: 'page' as const, path: '/ueber/mitarbeiter', status: 'changed' as const }],
  };
  it('re-roots the subtree below the new parent per locale', () => {
    const ch = urlChanges('m', { en: '/company', de: '/' }, sub);
    expect(ch.map((c) => [c.locale, c.from, c.to, c.redirect])).toEqual([
      ['de', '/ueber/mitarbeiter', '/mitarbeiter', true],
      ['en', '/about/team', '/company/team', true],
      ['en', '/about/team/bio', '/company/team/bio', false],
    ]);
  });
  it('reports nothing when the parent stays and skips locales without a path', () => {
    expect(urlChanges('m', { en: '/about' }, sub)).toEqual([]);
    expect(urlChanges('m', { en: '/x' }, { en: sub.en, fr: [{ id: 'm', name: 'T', kind: 'page' }] }).length).toBe(2);
  });
});

describe('filter', () => {
  const nodes = [
    n('r', 'Home', { children: 3 }),
    n('a', 'About', { parent: 'r', children: 1 }),
    n('t', 'Team', { parent: 'a', status: 'draft', type: 'landing' }),
    n('c', 'Contact', { parent: 'r', hidden: true }),
    n('x', 'Secret', { parent: 'r', restricted: true }),
  ];
  const parentOf = (id: string) => nodes.find((x) => x.id === id)?.parent;
  const locs: Record<string, string[]> = { r: ['en', 'de'], a: ['en', 'de'], t: ['en'], c: ['en', 'de'] };
  const run = (f: Partial<ReturnType<typeof emptyFilter>>) =>
    visibleIds(nodes, (x) => matchesFilter(x, { ...emptyFilter(), ...f }, locs[x.id], ['en', 'de']), parentOf);

  it('shows matches with their ancestors', () => {
    const r = run({ q: 'team' });
    expect([...r.visible].sort()).toEqual(['a', 'r', 't']);
    expect([...r.matches]).toEqual(['t']);
  });
  it('filters by type, status, hidden and language gaps', () => {
    expect([...run({ type: 'landing' }).matches]).toEqual(['t']);
    expect([...run({ status: 'draft' }).matches]).toEqual(['t']);
    expect([...run({ status: 'hidden' }).matches]).toEqual(['c']);
    expect([...run({ gaps: true }).matches]).toEqual(['t']);
  });
  it('never matches restricted nodes', () => {
    expect(run({ q: 'secret' }).visible.size).toBe(0);
  });
});

describe('mount list query', () => {
  it('maps the filter to where[field][op] parameters', () => {
    const { type, query } = mountQuery(
      { collection: 'article', filter: { status: 'open', tag: ['a', 'b'], skip: null } },
      { limit: 20, offset: 40 },
      'de',
    );
    expect(type).toBe('article');
    expect(query).toEqual({ limit: 20, offset: 40, locale: 'de', 'where[status][eq]': 'open', 'where[tag][in]': 'a,b' });
  });
  it('round trips the filter text', () => {
    const f = parseFilter('category=news\ntags = a, b\nbad\n=x');
    expect(f).toEqual({ category: 'news', tags: ['a', 'b'] });
    expect(parseFilter(filterText(f))).toEqual(f);
    expect(parseFilter('')).toBeUndefined();
  });
});
