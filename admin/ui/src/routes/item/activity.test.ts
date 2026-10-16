import { describe, expect, it } from 'vitest';
import { actorRef, collectAudit, mergeActivity, type AuditPage } from './activity';

const ev = (id: string, kind: string, at: string) => ({ id, kind, actor: 'user:u1', actorName: 'Ann', comment: kind === 'comment' ? 'hi' : '', at });

describe('mergeActivity', () => {
  it('merges the sources newest first', () => {
    const list = mergeActivity({
      versions: [{ version: 1, createdAt: '2026-01-01T10:00:00Z', createdBy: 'user:u1', locales: ['en'] }],
      audit: [{ id: 5, action: 'item.unpublish', actorKind: 'user', actorId: 'u2', at: '2026-01-03T10:00:00Z' }],
      review: [ev('e1', 'submitted', '2026-01-02T10:00:00Z')],
    });
    expect(list.map((a) => a.key)).toEqual(['a5', 'r' + 'e1', 'v1']);
    expect(list[0]).toMatchObject({ what: 'item.unpublish', actor: 'user:u2' });
  });
  it('hides autosaves and plain publishes but keeps a review bypass', () => {
    const list = mergeActivity({
      versions: [],
      audit: [
        { id: 1, action: 'item.update', actorKind: 'user', actorId: 'u', at: '2026-01-01T00:00:00Z' },
        { id: 2, action: 'item.publish', actorKind: 'user', actorId: 'u', at: '2026-01-01T00:01:00Z' },
        { id: 3, action: 'item.publish', actorKind: 'user', actorId: 'u', at: '2026-01-01T00:02:00Z', data: { reviewBypassed: true } },
      ],
      review: null,
    });
    expect(list).toHaveLength(1);
    expect(list[0]).toMatchObject({ key: 'a3', note: 'bypassed' });
  });
  it('takes review events from the thread when it is loaded, from the audit otherwise', () => {
    const audit = [{ id: 9, action: 'review.approved', actorKind: 'user', actorId: 'u', at: '2026-01-01T00:00:00Z' }];
    expect(mergeActivity({ versions: [], audit, review: [] })).toHaveLength(0);
    expect(mergeActivity({ versions: [], audit, review: null })).toHaveLength(1);
  });
  it('carries the restored version number', () => {
    const list = mergeActivity({ versions: [], audit: [{ id: 1, action: 'item.restore_version', actorKind: 'user', actorId: 'u', at: '2026-01-01T00:00:00Z', data: { version: 3 } }], review: null });
    expect(list[0]!.vars).toEqual({ n: 3 });
  });
  it('builds actor references', () => {
    expect(actorRef('user', 'a')).toBe('user:a');
    expect(actorRef('system')).toBe('system');
  });
});

describe('collectAudit', () => {
  const row = (id: number, action: string) => ({ id, action, actorKind: 'user', at: '2026-01-01T00:00:00Z' });
  // Pages of three autosaves and one real entry, newest first.
  const pages = (n: number) => async (before?: number): Promise<AuditPage> => {
    const p = before === undefined ? 0 : n - before;
    const base = (n - p) * 10;
    return { items: [row(base + 3, 'item.update'), row(base + 2, 'item.update'), row(base + 1, 'item.update'), row(base, 'item.unpublish')], hasMore: p < n - 1, next: n - p - 1 };
  };
  it('pages until enough shown entries were read', async () => {
    const r = await collectAudit(pages(10), { want: 3 });
    expect(r.rows.filter((x) => x.action !== 'item.update')).toHaveLength(3);
    expect(r.hasMore).toBe(true);
  });
  it('stops at the page limit and says more exist', async () => {
    const r = await collectAudit(pages(20), { want: 100, pages: 2 });
    expect(r.rows).toHaveLength(8);
    expect(r.hasMore).toBe(true);
    expect(r.next).toBeDefined();
  });
  it('reads the whole short log', async () => {
    const r = await collectAudit(pages(2), { want: 100 });
    expect(r.hasMore).toBe(false);
    expect(r.rows).toHaveLength(8);
  });
});
