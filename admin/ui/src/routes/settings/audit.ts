export interface AuditFilters {
  actor: string;
  action: string;
  subject: string;
  /** Value of a datetime-local input (local time) or an RFC 3339 time. */
  since: string;
}

export const AUDIT_PAGE = 50;
export const emptyFilters = (): AuditFilters => ({ actor: '', action: '', subject: '', since: '' });

/** Local `YYYY-MM-DDTHH:mm` (or any date text) to the RFC 3339 instant the API wants; '' when unparseable. */
export function sinceToRFC3339(local: string): string {
  if (!local) return '';
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
