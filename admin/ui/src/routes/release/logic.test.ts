import { describe, expect, it } from 'vitest';
import { defaultSchedule, isoToLocal, itemStatus, localToISO, zoneOffsetLabel } from './logic';

describe('localToISO', () => {
  it('reads a plain time in the zone', () => {
    expect(localToISO('2026-10-20T14:30', 'Europe/Berlin')).toBe('2026-10-20T12:30:00.000Z');
    expect(localToISO('2026-10-20T14:30', 'UTC')).toBe('2026-10-20T14:30:00.000Z');
    expect(localToISO('nope', 'UTC')).toBeNull();
  });
  it('resolves a skipped time forward (Europe/Berlin)', () => {
    expect(localToISO('2026-03-29T02:30', 'Europe/Berlin')).toBe('2026-03-29T01:30:00.000Z'); // 03:30 CEST
  });
  it('resolves a skipped time forward (Australia/Lord_Howe, 30 minute gap)', () => {
    const iso = localToISO('2026-10-04T02:15', 'Australia/Lord_Howe')!;
    expect(iso).toBe('2026-10-03T15:45:00.000Z'); // 02:45 +11:00
    expect(isoToLocal(iso, 'Australia/Lord_Howe') >= '2026-10-04T02:30').toBe(true);
  });
  it('takes the earlier instant of an ambiguous time', () => {
    expect(localToISO('2026-10-25T02:30', 'Europe/Berlin')).toBe('2026-10-25T00:30:00.000Z'); // CEST
    expect(localToISO('2026-04-05T01:45', 'Australia/Lord_Howe')).toBe('2026-04-04T14:45:00.000Z'); // +11:00
  });
  it('round-trips ordinary times', () => {
    const iso = localToISO('2026-12-01T09:00', 'America/New_York')!;
    expect(isoToLocal(iso, 'America/New_York')).toBe('2026-12-01T09:00');
  });
});

describe('zoneOffsetLabel', () => {
  it('follows the chosen date, not now', () => {
    const summer = Date.parse('2026-07-01T12:00:00Z');
    const winter = Date.parse('2026-01-01T12:00:00Z');
    expect(zoneOffsetLabel('Europe/Berlin', summer, 'en-GB')).not.toBe(zoneOffsetLabel('Europe/Berlin', winter, 'en-GB'));
  });
});

describe('defaultSchedule', () => {
  it('is at least five minutes ahead', () => {
    const now = Date.parse('2026-10-20T10:58:00Z');
    expect(defaultSchedule(now, 'UTC')).toBe('2026-10-20T12:00');
    expect(defaultSchedule(Date.parse('2026-10-20T10:30:00Z'), 'UTC')).toBe('2026-10-20T11:00');
  });
});

describe('itemStatus', () => {
  it('labels by action', () => {
    expect(itemStatus({ action: 'publish', state: 'current' })).toEqual({ key: 'releases.state.nothing', tone: 'warning' });
    expect(itemStatus({ action: 'publish', state: 'changes' }).key).toBe('releases.state.changes');
    expect(itemStatus({ action: 'unpublish', state: 'changes' }).key).toBe('releases.state.willUnpublish');
    expect(itemStatus({ action: 'unpublish', state: 'current' }).key).toBe('releases.state.alreadyUnpublished');
    expect(itemStatus({ action: 'publish', state: 'trashed' }).tone).toBe('danger');
  });
});
