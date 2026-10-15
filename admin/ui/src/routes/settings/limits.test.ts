import { describe, expect, it } from 'vitest';
import {
  addable, addPolicy, applicableTo, applicableToRole, cleanPolicies, formatValues, groupPolicies, parseValues,
  removePolicy, setLimit, setPolicyLimit, toggleValue, type LimitDef, type PolicyDef,
} from './limits';

const limits: LimitDef[] = [
  { name: 'channel', choices: [{ value: 'careers', label: 'Careers' }, { value: 'corp', label: 'Corporate' }], policies: ['content.read'] },
  { name: 'owner', choices: null, policies: ['content.update'] },
];
const policies: PolicyDef[] = [
  { name: 'content.update', description: '', limitations: ['owner'] },
  { name: 'content.read', description: '', limitations: ['channel'] },
  { name: 'user.manage', description: '', limitations: [] },
];

describe('limitation values', () => {
  it('parses free text split on commas and newlines, trimmed and de-duplicated', () => {
    expect(parseValues(' a, b ,,a\nc ')).toEqual(['a', 'b', 'c']);
    expect(parseValues('')).toEqual([]);
    expect(formatValues(['a', 'b'])).toBe('a, b');
    expect(formatValues(undefined)).toBe('');
  });
  it('toggles choices without disturbing order or duplicating', () => {
    let v = toggleValue(undefined, 'careers', true);
    v = toggleValue(v, 'corp', true);
    expect(toggleValue(v, 'careers', true)).toEqual(['careers', 'corp']);
    expect(toggleValue(v, 'careers', false)).toEqual(['corp']);
  });
  it('removes a limitation when it has no values and never mutates', () => {
    const a = { channel: ['x'] };
    expect(setLimit(a, 'channel', [])).toEqual({});
    expect(setLimit(a, 'owner', ['me'])).toEqual({ channel: ['x'], owner: ['me'] });
    expect(a).toEqual({ channel: ['x'] });
  });
});

describe('applicable limitations', () => {
  it('uses the policy list, and every limitation for *', () => {
    expect(applicableTo('content.read', policies, limits)).toEqual(['channel']);
    expect(applicableTo('*', policies, limits)).toEqual(['channel', 'owner']);
    expect(applicableTo('nope.x', policies, limits)).toEqual([]);
  });
  it('unions a role', () => {
    expect(applicableToRole([{ policy: 'content.read' }, { policy: 'user.manage' }], policies, limits)).toEqual(['channel']);
    expect(applicableToRole([{ policy: '*' }], policies, limits)).toEqual(['channel', 'owner']);
  });
});

describe('role policy table', () => {
  it('groups the catalog by module prefix', () => {
    const g = groupPolicies(policies);
    expect(g.map((x) => x.module)).toEqual(['content', 'user']);
    expect(g[0].policies.map((p) => p.name)).toEqual(['content.read', 'content.update']);
  });
  it('adds, limits and removes policies', () => {
    let list = addPolicy([], 'content.read');
    list = addPolicy(list, '*');
    list = setPolicyLimit(list, 0, 'channel', ['careers']);
    expect(list[0].limitations).toEqual({ channel: ['careers'] });
    expect(list[1].limitations).toEqual({});
    expect(addable(policies, list).map((p) => p.name)).toEqual(['content.update', 'user.manage']);
    expect(removePolicy(list, 0).map((p) => p.policy)).toEqual(['*']);
  });
  it('cleans empty limitations out of the body', () => {
    const body = cleanPolicies([
      { policy: 'content.read', limitations: { channel: ['careers'], owner: [] } },
      { policy: '*', limitations: {} },
      { policy: 'user.manage' },
    ]);
    expect(body).toEqual([{ policy: 'content.read', limitations: { channel: ['careers'] } }, { policy: '*' }, { policy: 'user.manage' }]);
  });
});
