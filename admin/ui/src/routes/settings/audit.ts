export interface AuditFilters {
  actor: string;
  action: string;
  subject: string;
  /** Value of a datetime-local input (local time) or an RFC 3339 time. */
  since: string;
}

export const AUDIT_PAGE = 50;
export const emptyFilters = (): AuditFilters => ({ actor: '', action: '', subject: '', since: '' });

/** Relative `since` presets kept in the URL, resolved when the log loads. */
export const SINCE_PRESETS: Record<string, number> = { '1h': 3_600_000, '24h': 86_400_000, '7d': 7 * 86_400_000, '30d': 30 * 86_400_000 };

/** A preset (`24h`), local `YYYY-MM-DDTHH:mm` or any date text to the RFC 3339 instant the API wants; '' when unparseable. */
export function sinceToRFC3339(local: string, now: number = Date.now()): string {
  if (!local) return '';
  const span = SINCE_PRESETS[local];
  if (span !== undefined) return new Date(now - span).toISOString();
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

export function filtersFromQuery(q: URLSearchParams): AuditFilters {
  return {
    actor: q.get('actor') ?? '',
    action: q.get('action') ?? '',
    subject: q.get('subject') ?? '',
    since: q.get('since') ?? '',
  };
}

/** The GET /audit query: blanks dropped, since converted, offset for the n-th loaded page (0-based). */
export function auditQuery(f: AuditFilters, page = 0) {
  return {
    actor: f.actor.trim() || undefined,
    action: f.action.trim() || undefined,
    subject: f.subject.trim() || undefined,
    since: sinceToRFC3339(f.since) || undefined,
    limit: AUDIT_PAGE,
    offset: page * AUDIT_PAGE || undefined,
  };
}

/** Audit action prefixes offered as filters: the label key and the pattern the API takes. */
export const ACTION_GROUPS: { id: string; pattern: string }[] = [
  { id: 'item', pattern: 'item.*' },
  { id: 'page', pattern: 'page.*' },
  { id: 'release', pattern: 'release.*' },
  { id: 'review', pattern: 'review.*' },
  { id: 'preview', pattern: 'preview.*' },
  { id: 'user', pattern: 'user.*' },
  { id: 'group', pattern: 'group.*' },
  { id: 'role', pattern: 'role.*' },
  { id: 'token', pattern: 'token.*' },
  { id: 'assignment', pattern: 'assignment.*' },
  { id: 'auth', pattern: 'auth.*' },
];

/** What an action's subject is: the part of the code before the first dot. */
export const subjectKind = (action: string): string => action.split('.')[0] ?? '';

/** Last-resort wording for an action without a translation: "page.move" becomes "page move". */
export const humanizeAction = (action: string): string => action.replace(/[._]+/g, ' ');

/** A shortened id for a subject that cannot be resolved to a name. */
export const shortId = (id: string): string => (id.length > 12 ? `${id.slice(0, 8)}…` : id);

interface Entry {
  action: string;
  subject?: string;
  data?: unknown;
}

const dataOf = (e: Entry): Record<string, unknown> => (e.data && typeof e.data === 'object' ? (e.data as Record<string, unknown>) : {});

/** Actions whose subject is an item id: content, review and preview-link entries. */
const ITEM_KINDS = new Set(['item', 'review', 'preview']);

/** The item an entry is about, whose name is shown: the subject for content, review and preview entries; `data.item` for page placements. */
export function itemIdOf(e: Entry): string {
  const k = subjectKind(e.action);
  if (ITEM_KINDS.has(k)) return e.subject ?? '';
  if (k === 'page') {
    const item = dataOf(e).item;
    return typeof item === 'string' ? item : '';
  }
  return '';
}

/** A release entry's release (its subject) and the name recorded with it. */
export function releaseOf(e: Entry): { id: string; name: string } | undefined {
  if (subjectKind(e.action) !== 'release' || !e.subject) return undefined;
  const name = dataOf(e).name;
  return { id: e.subject, name: typeof name === 'string' ? name : '' };
}

/** The role an assignment entry names: recorded as `role` on create and as `roleId` on delete (resolved by the caller). */
export function assignmentRole(e: Entry): { name?: string; id?: string } {
  const d = dataOf(e);
  return { name: typeof d.role === 'string' ? d.role : undefined, id: typeof d.roleId === 'string' ? d.roleId : undefined };
}

export const reviewBypassed = (e: Entry): boolean => e.action === 'item.publish' && dataOf(e).reviewBypassed === true;

export const restoredVersion = (e: Entry): number | undefined => {
  const v = dataOf(e).version;
  return e.action === 'item.restore_version' && typeof v === 'number' ? v : undefined;
};
