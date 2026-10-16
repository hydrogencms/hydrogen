// The item's Activity timeline: versions, audit entries about the item and review events merged newest first. Pure.
import type { ReviewEvent } from '../review/logic';

export interface VersionMeta {
  version: number;
  createdAt: string;
  createdBy: string;
  locales: string[];
}

export interface AuditRow {
  id: number;
  action: string;
  actorKind: string;
  actorId?: string;
  at: string;
  data?: unknown;
}

export interface Activity {
  key: string;
  at: string;
  kind: 'version' | 'audit' | 'review';
  /** i18n suffix: `history.act.<what>` (versions and audit) or `history.ev.review.<what>`. */
  what: string;
  /** `user:<id>`, `agent:<id>`, `token:<id>`, `system`, or '' when only `actorName` is known. */
  actor: string;
  actorName?: string;
  vars?: Record<string, string | number>;
  comment?: string;
  /** Extra line, e.g. the review was bypassed. */
  note?: 'bypassed';
}

/** Audit actions that are noise in a timeline (every autosave) or shown from a better source. */
const HIDDEN = new Set(['item.update']);

export const actorRef = (kind: string, id?: string): string => (kind === 'system' || !id ? kind : `${kind}:${id}`);

export function mergeActivity(src: { versions: VersionMeta[]; audit: AuditRow[]; review: ReviewEvent[] | null }): Activity[] {
  const out: Activity[] = [];
  for (const v of src.versions) {
    out.push({ key: `v${v.version}`, at: v.createdAt, kind: 'version', what: 'version', actor: v.createdBy, vars: { n: v.version } });
  }
  for (const e of src.audit) {
    if (HIDDEN.has(e.action)) continue;
    // Published versions are listed from the version list; keep only the review bypass as a note.
    const data = (e.data ?? {}) as { reviewBypassed?: boolean; version?: number };
    if (e.action === 'item.publish') {
      if (data.reviewBypassed) out.push({ key: `a${e.id}`, at: e.at, kind: 'audit', what: 'bypass', actor: actorRef(e.actorKind, e.actorId), note: 'bypassed' });
      continue;
    }
    // Review events come from the review thread itself, with comments and names.
    if (e.action.startsWith('review.') && src.review) continue;
    out.push({
      key: `a${e.id}`,
      at: e.at,
      kind: 'audit',
      what: e.action,
      actor: actorRef(e.actorKind, e.actorId),
      vars: typeof data.version === 'number' ? { n: data.version } : undefined,
    });
  }
  for (const r of src.review ?? []) {
    out.push({ key: `r${r.id}`, at: r.at, kind: 'review', what: r.kind, actor: r.actor, actorName: r.actorName, comment: r.comment || undefined });
  }
  return out.filter((a) => a.at).sort((a, b) => (a.at < b.at ? 1 : a.at > b.at ? -1 : a.key < b.key ? 1 : -1));
}

export interface AuditPage {
  items: AuditRow[];
  hasMore: boolean;
  /** The cursor for the next (older) page. */
  next?: number;
}

/** Autosave entries are hidden from the timeline, so the log is read page by page until enough others turned up. */
export const isShown = (e: AuditRow): boolean => !HIDDEN.has(e.action);

/** Reads audit pages (newest first) until `want` entries that show up are collected, the log ends or `pages` were read. */
export async function collectAudit(
  fetchPage: (before?: number) => Promise<AuditPage>,
  o: { want?: number; pages?: number; before?: number } = {},
): Promise<{ rows: AuditRow[]; hasMore: boolean; next?: number }> {
  const want = o.want ?? 30;
  let left = o.pages ?? 5;
  const rows: AuditRow[] = [];
  let shown = 0;
  let before = o.before;
  for (;;) {
    const p = await fetchPage(before);
    rows.push(...p.items);
    shown += p.items.filter(isShown).length;
    left--;
    before = p.next;
    const more = p.hasMore && before !== undefined;
    if (!more) return { rows, hasMore: false };
    if (shown >= want || left <= 0) return { rows, hasMore: true, next: before };
  }
}
