export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(d);
}

/** Accept only same-app absolute paths for ?next= (no scheme, no protocol-relative). */
export function safeNext(next: string | null | undefined): string {
  return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/\\') ? next : '/';
}

const MIN = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;

/**
 * "2 hours ago", "yesterday", "in 3 days"; older than a month falls back to a short date. Pair it with
 * `formatDateTime(iso)` in a `title` so the exact time is one hover away. `now` is injectable for tests.
 */
export function relativeTime(date: string | Date | undefined | null, now: number = Date.now(), locale?: string): string {
  if (!date) return '';
  const at = (date instanceof Date ? date : new Date(date)).getTime();
  if (Number.isNaN(at)) return typeof date === 'string' ? date : '';
  const diff = at - now;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'narrow' });
  if (abs < 45_000) return rtf.format(0, 'second');
  if (abs < HOUR) return rtf.format(Math.round(diff / MIN), 'minute');
  if (abs < DAY) return rtf.format(Math.round(diff / HOUR), 'hour');
  if (abs < 30 * DAY) return rtf.format(Math.round(diff / DAY), 'day');
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(at);
}

/** Time of day greeting key: `morning`, `afternoon` or `evening`. */
export function dayPart(d: Date = new Date()): 'morning' | 'afternoon' | 'evening' {
  const h = d.getHours();
  return h < 12 ? 'morning' : h < 18 ? 'afternoon' : 'evening';
}

/** A site key as people read it: `careers` -> `Careers`, `eu-shop` -> `Eu Shop`. */
export function siteName(key: string): string {
  return key.replace(/[-_]+/g, ' ').replace(/\b\p{L}/gu, (c) => c.toUpperCase());
}
