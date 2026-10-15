import { describe, expect, it } from 'vitest';
import { dayPart, relativeTime } from './format';

const now = Date.parse('2026-10-15T12:00:00Z');
const ago = (ms: number) => new Date(now - ms).toISOString();

describe('relativeTime', () => {
  it('reads recent times as a sentence fragment', () => {
    expect(relativeTime(ago(10_000), now, 'en')).toBe('now');
    expect(relativeTime(ago(5 * 60_000), now, 'en')).toMatch(/^5\s?m(in)? ago$/);
    expect(relativeTime(ago(2 * 3_600_000), now, 'en')).toMatch(/^2\s?h(r)? ago$/);
    expect(relativeTime(ago(86_400_000), now, 'en')).toBe('yesterday');
    expect(relativeTime(ago(3 * 86_400_000), now, 'en')).toMatch(/^3\s?d(ays)? ago$/);
  });
  it('reads future times', () => {
    expect(relativeTime(new Date(now + 3 * 3_600_000), now, 'en')).toMatch(/in 3/);
  });
  it('falls back to a date after a month and tolerates bad input', () => {
    expect(relativeTime(ago(90 * 86_400_000), now, 'en')).toMatch(/2026/);
    expect(relativeTime('', now)).toBe('');
    expect(relativeTime(undefined, now)).toBe('');
    expect(relativeTime('not a date', now)).toBe('not a date');
  });
});

describe('dayPart', () => {
  it('splits the day', () => {
    expect(dayPart(new Date(2026, 0, 1, 8))).toBe('morning');
    expect(dayPart(new Date(2026, 0, 1, 14))).toBe('afternoon');
    expect(dayPart(new Date(2026, 0, 1, 20))).toBe('evening');
  });
});
