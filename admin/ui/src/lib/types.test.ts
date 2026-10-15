import { expect, it } from 'vitest';
import { groupTypes, type CompiledType } from './types.svelte';

const ty = (name: string, extra: Partial<CompiledType> = {}): CompiledType => ({ name, label: name.toUpperCase(), fields: [], ...extra });

it('groups by admin.group, ungrouped first, sorted by label', () => {
  const g = groupTypes([ty('b', { admin: { group: 'Shop' } }), ty('z'), ty('a'), ty('c', { admin: { group: 'Shop' } })]);
  expect(g.map((x) => x.group)).toEqual(['', 'Shop']);
  expect(g[0].types.map((x) => x.name)).toEqual(['a', 'z']);
  expect(g[1].types.map((x) => x.name)).toEqual(['b', 'c']);
});
