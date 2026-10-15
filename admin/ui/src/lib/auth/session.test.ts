import { describe, expect, it } from 'vitest';
import { session } from './session.svelte';

function grant(policy: string, limits?: Array<{ name: string; values: string[] }>) {
  return { policy, role: 'r', limits };
}

describe('session.can', () => {
  it('is false without grants, true for the policy or *', () => {
    session.grants = [];
    expect(session.can('audit.read')).toBe(false);
    session.grants = [grant('audit.read')];
    expect(session.can('audit.read')).toBe(true);
    expect(session.can('user.manage')).toBe(false);
    session.grants = [grant('*')];
    expect(session.can('anything')).toBe(true);
  });

  it('checks collection, locale and channel limitations coarsely', () => {
    session.setChannelResolver((c) => (c === 'careers_en' ? ['page', 'landing'] : null));
    session.grants = [grant('content.edit', [{ name: 'collection', values: ['article'] }])];
    expect(session.can('content.edit')).toBe(true);
    expect(session.can('content.edit', { type: 'article' })).toBe(true);
    expect(session.can('content.edit', { type: 'product' })).toBe(false);

    session.grants = [grant('content.read', [{ name: 'channel', values: ['careers_en'] }])];
    expect(session.can('content.read', { type: 'page' })).toBe(true);
    expect(session.can('content.read', { type: 'product' })).toBe(false);
    expect(session.can('content.read', { channel: 'corporate_en' })).toBe(false);

    session.grants = [grant('content.edit', [{ name: 'locale', values: ['en'] }]), grant('content.edit', [{ name: 'locale', values: ['de'] }])];
    expect(session.can('content.edit', { locale: 'de' })).toBe(true);
    expect(session.can('content.edit', { locale: 'fr' })).toBe(false);
  });

  it('clear() empties the session and runs registered resets', () => {
    let called = 0;
    session.onClear(() => called++);
    session.principal = { kind: 'user', name: 'a' };
    session.clear();
    expect(session.principal).toBeNull();
    expect(called).toBe(1);
  });
});
