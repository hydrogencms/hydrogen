// Pure helpers for releases and scheduling: time zones, validation, tabs and item states.

export const MAX_AHEAD_DAYS = 365;

/** The browser's IANA zone (`Europe/Berlin`). */
export function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

const pad = (n: number) => String(n).padStart(2, '0');

interface Parts {
  y: number;
  mo: number;
  d: number;
  h: number;
  mi: number;
}

function parseLocal(s: string): Parts | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(s.trim());
  if (!m) return null;
  const p: Parts = { y: +m[1]!, mo: +m[2]!, d: +m[3]!, h: +m[4]!, mi: +m[5]! };
  const probe = new Date(Date.UTC(p.y, p.mo - 1, p.d, p.h, p.mi));
  const same = probe.getUTCFullYear() === p.y && probe.getUTCMonth() === p.mo - 1 && probe.getUTCDate() === p.d && probe.getUTCHours() === p.h;
  return same ? p : null;
}

/** Wall-clock parts of an instant in a zone. */
function partsIn(ms: number, zone: string): Parts {
  const f = new Intl.DateTimeFormat('en-CA', { timeZone: zone, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
  const o: Record<string, number> = {};
  for (const x of f.formatToParts(new Date(ms))) if (x.type !== 'literal') o[x.type] = +x.value;
  return { y: o.year!, mo: o.month!, d: o.day!, h: o.hour!, mi: o.minute! };
}

const asUTC = (p: Parts) => Date.UTC(p.y, p.mo - 1, p.d, p.h, p.mi);

/**
 * `2026-10-20T14:30` (a datetime-local value) read in `zone` -> an ISO instant. Null when the text is not a valid
 * date-time. A time skipped by a clock change resolves forward; an ambiguous one takes the earlier instant.
 */
export function localToISO(local: string, zone: string = localZone()): string | null {
  const p = parseLocal(local);
  if (!p) return null;
  const want = asUTC(p);
  const offAt = (ms: number) => asUTC(partsIn(ms, zone)) - ms;
  // The zone's offset a day before and after bounds any single clock change.
  const before = offAt(want - 86_400_000);
  const after = offAt(want + 86_400_000);
  const valid = [before, after]
    .map((o) => want - o)
    .filter((c) => asUTC(partsIn(c, zone)) === want)
    .sort((a, b) => a - b);
  // No valid candidate: the time was skipped, so shift it by the elapsed offset (first instant after the gap).
  return new Date(valid[0] ?? want - before).toISOString();
}

/** An ISO instant as a datetime-local value in `zone`. */
export function isoToLocal(iso: string, zone: string = localZone()): string {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return '';
  const p = partsIn(ms, zone);
  return `${p.y}-${pad(p.mo)}-${pad(p.d)}T${pad(p.h)}:${pad(p.mi)}`;
}

/** "20 Oct 2026, 14:30" in `zone` (default: the browser's). */
export function formatInZone(iso: string | undefined | null, zone: string = localZone(), locale?: string): string {
  if (!iso) return '';
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return iso;
  return new Intl.DateTimeFormat(locale, { timeZone: zone, dateStyle: 'medium', timeStyle: 'short' }).format(ms);
}

/** The zone as a short offset label: `GMT+2`. */
export function zoneOffsetLabel(zone: string = localZone(), at: number = Date.now(), locale?: string): string {
  const x = new Intl.DateTimeFormat(locale, { timeZone: zone, timeZoneName: 'short' }).formatToParts(at).find((p) => p.type === 'timeZoneName');
  return x?.value ?? '';
}

export const MIN_LEAD_MS = 5 * 60_000;

/** The next full hour at least five minutes after `now`, as a datetime-local value in `zone`. */
export function defaultSchedule(now: number = Date.now(), zone: string = localZone()): string {
  let next = Math.floor(now / 3_600_000) * 3_600_000 + 3_600_000;
  if (next - now < MIN_LEAD_MS) next += 3_600_000;
  return isoToLocal(new Date(next).toISOString(), zone);
}

export type ScheduleProblem = 'invalid' | 'past' | 'toofar';

/** Why a chosen instant cannot be scheduled (the server says the same: in the future, at most a year ahead). */
export function scheduleProblem(iso: string | null, now: number = Date.now()): ScheduleProblem | null {
  if (!iso) return 'invalid';
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return 'invalid';
  if (ms <= now) return 'past';
  if (ms > now + MAX_AHEAD_DAYS * 86_400_000) return 'toofar';
  return null;
}

/** `Article — publish 2026-10-20 14:30`: the name of the one-item release behind "Schedule…". */
export function scheduleName(itemName: string, action: 'publish' | 'unpublish', iso: string, zone: string = localZone()): string {
  return `${itemName} — ${action} ${isoToLocal(iso, zone).replace('T', ' ')}`;
}

// ---- releases

export type ReleaseStatus = 'open' | 'scheduled' | 'publishing' | 'published' | 'failed' | 'cancelled';

export interface ReleaseError {
  item?: string;
  code?: string;
  message?: string;
  path?: string;
}

export interface ReleaseItem {
  item: string;
  name: string;
  type: string;
  action: 'publish' | 'unpublish';
  locales: string[];
  /** draft changes / up to date / trashed / missing */
  state: ItemState;
}

export interface Release {
  id: string;
  name: string;
  description: string;
  kind: string;
  status: ReleaseStatus;
  scheduledAt?: string;
  publishedAt?: string;
  errors: ReleaseError[];
  createdByName: string;
  createdAt: string;
  updatedAt: string;
  items: ReleaseItem[];
  itemCount: number;
}

/** `hidden`: the item exists but the viewer may not read it. */
export type ItemState = 'changes' | 'current' | 'trashed' | 'missing' | 'hidden';

type Raw = Record<string, unknown>;
const str = (v: unknown) => (typeof v === 'string' ? v : '');
const strs = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

/** What a release item row says about its item. Servers may send an explicit `state`, or flags. */
export function itemState(r: Raw): ItemState {
  const s = str(r.state) || str(r.itemState);
  if (s === 'trashed' || s === 'missing' || s === 'changes' || s === 'current' || s === 'hidden') return s;
  if (s === 'draft_changes' || s === 'changed') return 'changes';
  if (s === 'up_to_date' || s === 'published') return 'current';
  if (r.missing === true || r.exists === false) return 'missing';
  if (r.trashed === true || r.deletedAt) return 'trashed';
  if (r.hasDraftChanges === true || r.draftChanges === true || r.changed === true) return 'changes';
  if (r.hasDraftChanges === false || r.draftChanges === false || r.changed === false) return 'current';
  const st = str(r.status);
  if (st === 'changed' || st === 'draft') return 'changes';
  if (st) return 'current';
  return 'current';
}

export function normItem(raw: Raw): ReleaseItem {
  return {
    item: str(raw.item) || str(raw.itemId) || str(raw.id),
    name: str(raw.name) || str(raw.itemName),
    type: str(raw.type) || str(raw.itemType),
    action: raw.action === 'unpublish' ? 'unpublish' : 'publish',
    locales: strs(raw.locales),
    state: itemState(raw),
  };
}

export function normRelease(raw: Raw): Release {
  const items = Array.isArray(raw.items) ? (raw.items as Raw[]).map(normItem) : [];
  const errs = Array.isArray(raw.errors) ? (raw.errors as Raw[]) : [];
  return {
    id: str(raw.id),
    name: str(raw.name),
    description: str(raw.description),
    kind: str(raw.kind) || 'release',
    status: (str(raw.status) || 'open') as ReleaseStatus,
    scheduledAt: str(raw.scheduledAt) || undefined,
    publishedAt: str(raw.publishedAt) || undefined,
    errors: errs.map((e) => ({ item: str(e.item) || str(e.itemId) || undefined, code: str(e.code) || undefined, message: str(e.message) || undefined, path: str(e.path) || undefined })),
    createdByName: str(raw.createdByName) || str(raw.createdBy),
    createdAt: str(raw.createdAt),
    updatedAt: str(raw.updatedAt),
    items,
    itemCount: typeof raw.itemCount === 'number' ? raw.itemCount : items.length,
  };
}

export type ReleaseTab = 'open' | 'scheduled' | 'published' | 'failed' | 'cancelled' | 'items';

export const TABS: ReleaseTab[] = ['open', 'scheduled', 'published', 'failed', 'cancelled', 'items'];

export const parseTab = (v: string | null | undefined): ReleaseTab => (TABS.includes(v as ReleaseTab) ? (v as ReleaseTab) : 'open');

/** The API filter behind a tab: one-item releases live in their own tab, whatever their status. */
export function tabQuery(tab: ReleaseTab): { status?: string; kind?: string } {
  return tab === 'items' ? { kind: 'item' } : { status: tab, kind: 'release' };
}

/** The list rows of a tab (a guard on top of the server's filter). */
export function inTab(r: Pick<Release, 'status' | 'kind'>, tab: ReleaseTab): boolean {
  if (tab === 'items') return r.kind === 'item';
  return r.kind !== 'item' && r.status === tab;
}

/** Edits are possible while a release is open, scheduled-then-unscheduled, or failed (any edit reopens it). */
export const editable = (s: ReleaseStatus) => s === 'open' || s === 'failed';
export const canSchedule = (s: ReleaseStatus) => s === 'open' || s === 'failed';
export const canPublish = (s: ReleaseStatus) => s === 'open' || s === 'scheduled' || s === 'failed';
export const canCancel = (s: ReleaseStatus) => s === 'open' || s === 'scheduled';
export const canDeleteRelease = (s: ReleaseStatus) => s === 'open' || s === 'cancelled';

export const statusTone = (s: string): 'neutral' | 'success' | 'warning' | 'danger' | 'info' =>
  s === 'published' ? 'success' : s === 'scheduled' ? 'info' : s === 'failed' ? 'danger' : s === 'publishing' ? 'warning' : 'neutral';

/** Message key and tone for an item row: what publishing the release does to that item. */
export function itemStatus(r: Pick<ReleaseItem, 'action' | 'state'>): { key: string; tone: 'warning' | 'success' | 'danger' } {
  if (r.state === 'trashed' || r.state === 'missing' || r.state === 'hidden') return { key: `releases.state.${r.state}`, tone: 'danger' };
  if (r.action === 'unpublish') return r.state === 'changes' ? { key: 'releases.state.willUnpublish', tone: 'warning' } : { key: 'releases.state.alreadyUnpublished', tone: 'warning' };
  return r.state === 'changes' ? { key: 'releases.state.changes', tone: 'warning' } : { key: 'releases.state.nothing', tone: 'warning' };
}

/** Counts for the "Publish now" confirmation. */
export function actionCounts(items: Pick<ReleaseItem, 'action'>[]): { publish: number; unpublish: number } {
  let publish = 0;
  for (const i of items) if (i.action === 'publish') publish++;
  return { publish, unpublish: items.length - publish };
}

/** The earliest scheduled release of a list, for the editor's "Scheduled · date" badge. */
export function nextScheduled<T extends { status: string; scheduledAt?: string }>(list: T[]): T | undefined {
  return list
    .filter((r) => r.status === 'scheduled' && r.scheduledAt)
    .sort((a, b) => Date.parse(a.scheduledAt!) - Date.parse(b.scheduledAt!))[0];
}
