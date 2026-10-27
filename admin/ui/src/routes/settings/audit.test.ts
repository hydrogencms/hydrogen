import { describe, expect, it } from 'vitest';
import '../../lib/messages/settings';
import '../../lib/messages/history';
import { t } from '../../lib/i18n';
import { describeEntry, referencedIds, type Lookup, ACTION_GROUPS, AUDIT_PAGE, assignmentRole, auditQuery, itemIdOf, releaseOf, restoredVersion, reviewBypassed, emptyFilters, filtersFromQuery, humanizeAction, shortId, sinceToRFC3339, subjectKind } from './audit';

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

describe('audit subjects', () => {
  it('finds the item an entry is about', () => {
    expect(itemIdOf({ action: 'item.trash', subject: 'i1' })).toBe('i1');
    expect(itemIdOf({ action: 'review.approved', subject: 'i2' })).toBe('i2');
    expect(itemIdOf({ action: 'preview.share.create', subject: 'i3' })).toBe('i3');
    expect(itemIdOf({ action: 'page.place', subject: 'node', data: { item: 'i4' } })).toBe('i4');
    expect(itemIdOf({ action: 'page.move', subject: 'node' })).toBe('');
    expect(itemIdOf({ action: 'user.create', subject: 'u' })).toBe('');
  });
  it('names releases from the entry', () => {
    expect(releaseOf({ action: 'release.publish', subject: 'r1', data: { name: 'Spring' } })).toEqual({ id: 'r1', name: 'Spring' });
    expect(releaseOf({ action: 'item.publish', subject: 'r1' })).toBeUndefined();
  });
  it('reads the role of create and delete assignments', () => {
    expect(assignmentRole({ action: 'assignment.create', data: { role: 'editor' } })).toEqual({ name: 'editor', id: undefined });
    expect(assignmentRole({ action: 'assignment.delete', data: { roleId: 'r9' } })).toEqual({ name: undefined, id: 'r9' });
  });
  it('flags bypassed reviews and restored versions', () => {
    expect(reviewBypassed({ action: 'item.publish', data: { reviewBypassed: true } })).toBe(true);
    expect(reviewBypassed({ action: 'item.publish' })).toBe(false);
    expect(restoredVersion({ action: 'item.restore_version', data: { version: 4 } })).toBe(4);
  });
  it('offers filters for the editorial actions', () => {
    expect(ACTION_GROUPS.map((g) => g.pattern)).toEqual(expect.arrayContaining(['release.*', 'review.*', 'preview.*']));
  });
});

describe('audit descriptions', () => {
  const U = '01a17b79-8836-771b-8f16-31a1419425dd';
  const G = '01a17b79-8836-771b-8f16-aaaaaaaaaaaa';
  const I = '01a17b79-8836-771b-8f16-bbbbbbbbbbbb';
  const R = '01a17b79-8836-771b-8f16-cccccccccccc';
  const names: Record<string, string> = { [`user:${U}`]: 'jane@example.com', [`group:${G}`]: 'Editors', [`item:${I}`]: 'Spring sale', [`role:${R}`]: 'editor' };
  const lookup: Lookup = (k, id) => names[`${k}:${id}`];
  const d = (e: Parameters<typeof describeEntry>[0], n: Lookup = lookup) => describeEntry(e, n, t);
  const none: Lookup = () => undefined;

  it('names the user and group of a membership change', () => {
    expect(d({ action: 'group.member_add', subject: G, data: { user: U } }).text).toBe('Added jane@example.com to the group Editors');
    expect(d({ action: 'group.member_remove', subject: G, data: { user: U } }).changes).toEqual([]);
  });
  it('uses the names recorded with the entry once the targets are gone', () => {
    const r = d({ action: 'group.member_remove', subject: G, data: { user: U, login: 'jane', group: 'Editors' } }, none);
    expect(r.text).toBe('Removed jane from the group Editors');
    expect(d({ action: 'group.member_add', subject: G, data: { user: U } }, none).text).toBe('Added 01a17b79… to the group 01a17b79…');
  });
  it('describes content, review and preview entries', () => {
    expect(d({ action: 'item.publish', subject: I, data: { type: 'article', version: 3, locales: ['en', 'de'], reviewBypassed: true } })).toEqual({
      text: 'Published Spring sale',
      changes: [
        { label: 'Content type', value: 'article' },
        { label: 'Version', value: '3' },
        { label: 'Languages', value: 'en, de' },
        { label: 'Review bypassed', value: 'Yes' },
      ],
    });
    expect(d({ action: 'item.restore_version', subject: I, data: { version: 2 } }).text).toBe('Restored version 2 of Spring sale');
    expect(d({ action: 'item.trash', subject: I }).text).toBe('Moved Spring sale to the trash');
    const rv = d({ action: 'review.changes_requested', subject: I, data: { item: I, type: 'a', state: 'changes_requested', actor: 'user:x', reviewers: ['bob'] } });
    expect(rv.changes.map((c) => c.label)).toEqual(['Content type', 'State', 'Reviewers']);
    const sh = d({ action: 'preview.share.create', subject: I, data: { share: 's', channel: 'web', locale: 'en', expiresAt: '2026-01-02T03:04:05Z' } });
    expect(sh.changes.map((c) => c.label)).toEqual(['Channel', 'Language', 'Expires']);
  });
  it('describes release entries', () => {
    expect(d({ action: 'release.create', subject: 'r', data: { name: 'Spring', kind: 'release', items: 3 } }).text).toBe('Created Spring');
    expect(d({ action: 'release.update', subject: 'r', data: { add: I, action: 'publish' } })).toEqual({ text: 'Added Spring sale to the release', changes: [{ label: 'Action', value: 'publish' }] });
    expect(d({ action: 'release.update', subject: 'r', data: { remove: I } }).text).toBe('Removed Spring sale from the release');
    expect(d({ action: 'release.update', subject: 'r', data: { unscheduled: true } }).text).toBe('Unscheduled the release');
    expect(d({ action: 'release.schedule', subject: 'r', data: { at: '2026-01-02T03:04:05Z' } }).text).toMatch(/^Scheduled the release for /);
    const p = d({ action: 'release.publish', subject: 'r', data: { name: 'Spring', items: [I, 'gone'], force: false } });
    expect(p.text).toBe('Published Spring');
    expect(p.changes).toEqual([{ label: 'Items', value: 'Spring sale, gone' }, { label: 'Forced', value: 'No' }]);
  });
  it('describes page tree entries', () => {
    expect(d({ action: 'page.place', subject: 'n', data: { item: I, site: 'main' } })).toEqual({ text: 'Placed Spring sale in the page tree', changes: [{ label: 'Site', value: 'main' }] });
    expect(d({ action: 'page.update', subject: 'n', data: { changed: ['move', 'link'], site: 's' } }).changes[0]).toEqual({ label: 'What changed', value: 'position, link' });
    expect(d({ action: 'page.mount', subject: 'n', data: { collection: 'blog', segment: 'blog' } }).text).toBe('Mounted the collection blog in the page tree');
  });
  it('describes user, group, role and token entries', () => {
    expect(d({ action: 'user.create', subject: U, data: { login: 'jane', password: true } })).toEqual({ text: 'Created the user jane@example.com', changes: [{ label: 'Has a password', value: 'Yes' }] });
    expect(d({ action: 'user.delete', subject: 'x'.repeat(36), data: { login: 'old' } }).text).toBe('Deleted the user old');
    expect(d({ action: 'user.update', subject: U, data: { login: 'jane', fields: ['password'] } })).toEqual({ text: 'Changed the password of jane@example.com', changes: [] });
    expect(d({ action: 'user.update', subject: U, data: { login: 'jane', fields: ['name', 'email'] } }).changes).toEqual([{ label: 'Fields changed', value: 'name, email' }]);
    expect(d({ action: 'group.delete', subject: 'g', data: { name: 'Old group' } }, none).text).toBe('Deleted the group Old group');
    expect(d({ action: 'role.update', subject: R, data: { name: 'editor', fields: ['policies'] } }).text).toBe('Updated the role editor');
    const tok = d({ action: 'token.create', subject: 't', data: { name: 'CI', kind: 'agent', prefix: 'hyd_ab', roles: ['editor'], expiresAt: null } });
    expect(tok.changes.map((c) => c.value)).toEqual(['agent', 'hyd_ab', 'editor', '—']);
    expect(d({ action: 'token.revoke', subject: 't', data: { name: 'CI', prefix: 'p' } }).text).toBe('Revoked the API token CI');
  });
  it('describes assignments, with or without the recorded role name', () => {
    expect(d({ action: 'assignment.create', subject: 'a', data: { role: 'editor', principalKind: 'user', principalId: U, limitations: {} } })).toEqual({
      text: 'Gave the role editor to jane@example.com',
      changes: [{ label: 'Limitations', value: '{}' }],
    });
    const del = { action: 'assignment.delete', subject: 'a', data: { roleId: R, principalKind: 'group', principalId: G } };
    expect(d(del).text).toBe('Removed the role editor from Editors');
    expect(d(del, none).text).toBe('Removed the role 01a17b79… from 01a17b79…');
  });
  it('describes sign-ins and system entries', () => {
    expect(d({ action: 'auth.login', subject: U, data: { login: 'jane', ip: '1.2.3.4' } })).toEqual({
      text: 'Signed in',
      changes: [{ label: 'Login', value: 'jane' }, { label: 'IP address', value: '1.2.3.4' }],
    });
    expect(d({ action: 'auth.changed', subject: U }).text).toBe('Changed the password of jane@example.com');
    expect(d({ action: 'media.upload', subject: 'abc', data: { mime: 'image/png', size: 2048 } }).changes[1]).toEqual({ label: 'Size', value: '2.0 KB' });
    expect(d({ action: 'media.gc', data: { count: 4, files: ['a', 'b'] } }).text).toBe('Cleaned up 4 unused files');
  });
  it('falls back to the humanized action and generic rows for unknown actions', () => {
    expect(d({ action: 'widget.frobnicate', subject: I, data: { someKey: 1, nested: { a: 1 }, tags: [], flag: false, who: U } })).toEqual({
      text: 'Widget frobnicate',
      changes: [
        { label: 'Some key', value: '1' },
        { label: 'Nested', value: '{"a":1}' },
        { label: 'Tags', value: '—' },
        { label: 'Flag', value: 'No' },
        { label: 'Who', value: '01a17b79…' },
      ],
    });
  });
  it('never throws on missing or odd data', () => {
    for (const action of ['item.publish', 'group.member_add', 'assignment.create', 'release.update', 'user.update', 'zzz']) {
      for (const data of [undefined, null, 'x', 5, [], { fields: 7, items: 'no' }]) expect(() => d({ action, data })).not.toThrow();
    }
  });
  it('lists the ids an entry mentions', () => {
    expect(referencedIds({ action: 'group.member_add', subject: G, data: { user: U } })).toEqual([['group', G], ['user', U]]);
    expect(referencedIds({ action: 'assignment.delete', subject: 'a', data: { roleId: R, principalKind: 'token', principalId: 'tid' } })).toEqual([['role', R], ['token', 'tid']]);
    expect(referencedIds({ action: 'release.publish', subject: 'r', data: { items: [I, 'x'] } })).toEqual([['item', I]]);
    expect(referencedIds({ action: 'auth.login', subject: U })).toEqual([['user', U]]);
  });
});
