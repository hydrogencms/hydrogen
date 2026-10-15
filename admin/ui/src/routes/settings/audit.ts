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
