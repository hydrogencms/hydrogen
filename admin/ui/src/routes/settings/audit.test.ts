import { describe, expect, it } from 'vitest';
import { AUDIT_PAGE, auditQuery, emptyFilters, filtersFromQuery, humanizeAction, shortId, sinceToRFC3339, subjectKind } from './audit';

describe('audit filters', () => {
  it('reads filters from the URL', () => {
    const f = filtersFromQuery(new URLSearchParams('actor=agent&action=content.publish&since=2026-01-02T03:04'));
    expect(f).toEqual({ actor: 'agent', action: 'content.publish', subject: '', since: '2026-01-02T03:04' });
  });
  it('drops blanks and trims', () => {
    expect(auditQuery({ ...emptyFilters(), actor: ' user:1 ' })).toEqual({
      actor: 'user:1', action: undefined, subject: undefined, since: undefined, limit: AUDIT_PAGE, offset: undefined,
    });
  });
  it('converts the local since time to an RFC 3339 instant', () => {
    const q = auditQuery({ ...emptyFilters(), since: '2026-01-02T03:04' });
    expect(q.since).toBe(new Date('2026-01-02T03:04').toISOString());
    expect(q.since).toMatch(/Z$/);
    expect(sinceToRFC3339('garbage')).toBe('');
  });
  it('offsets by whole pages', () => {
    expect(auditQuery(emptyFilters(), 2).offset).toBe(2 * AUDIT_PAGE);
  });
  it('resolves relative presets against now', () => {
    expect(sinceToRFC3339('24h', Date.parse('2026-01-02T00:00:00Z'))).toBe('2026-01-01T00:00:00.000Z');
    expect(sinceToRFC3339('7d', Date.parse('2026-01-08T00:00:00Z'))).toBe('2026-01-01T00:00:00.000Z');
  });
  it('words actions and subjects for people', () => {
    expect(subjectKind('item.publish')).toBe('item');
    expect(humanizeAction('page.member_add')).toBe('page member add');
    expect(shortId('0192e3f4-aaaa-bbbb-cccc-ddddeeeeffff')).toBe('0192e3f4…');
    expect(shortId('config')).toBe('config');
  });
});
