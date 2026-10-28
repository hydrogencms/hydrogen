import { describe, expect, it } from 'vitest';
import { foldEvents, lastActions, type AuditLike } from './recent';

const ev = (action: string, at: string, subject = 'a', actorId = 'u1', actorKind = 'user'): AuditLike => ({ action, actorKind, actorId, subject, at });

describe('recent activity', () => {
  it('folds same-actor events on one item within ten minutes', () => {
    const f = foldEvents([ev('item.publish', '2026-01-01T10:05:00Z'), ev('item.update', '2026-01-01T10:00:00Z'), ev('item.create', '2026-01-01T09:56:00Z')]);
    expect(f).toHaveLength(1);
    expect(f[0]!.actions).toEqual(['item.publish', 'item.update', 'item.create']);
  });
  it('keeps other actors, items and distant events apart', () => {
    const f = foldEvents([
      ev('item.update', '2026-01-01T10:00:00Z'),
      ev('item.update', '2026-01-01T09:59:00Z', 'a', 'u2'),
      ev('item.update', '2026-01-01T09:58:00Z', 'b'),
      ev('item.update', '2026-01-01T09:00:00Z'),
    ]);
    expect(f.map((x) => x.actions.length)).toEqual([1, 1, 1, 1]);
  });
  it('compares against the first entry of a group, not the last folded', () => {
    const f = foldEvents([ev('item.update', '2026-01-01T10:00:00Z'), ev('item.update', '2026-01-01T09:55:00Z'), ev('item.update', '2026-01-01T09:49:00Z')]);
    expect(f.map((x) => x.actions.length)).toEqual([2, 1]);
  });
  it('takes the newest action per item', () => {
    const last = lastActions([ev('item.trash', '2026-01-01T10:00:00Z'), ev('item.update', '2026-01-01T09:00:00Z'), { ...ev('item.update', '2026-01-01T08:00:00Z'), subject: null }]);
    expect([...last]).toEqual([['a', 'item.trash']]);
  });
});
