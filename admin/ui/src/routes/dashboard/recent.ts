export interface AuditLike {
  action: string;
  actorKind: string;
  actorId?: string | null;
  subject?: string | null;
  at: string;
}

export interface Folded<E extends AuditLike> {
  e: E;
  actions: string[];
}

export const VERBS = ['item.create', 'item.update', 'item.publish', 'item.unpublish', 'item.trash', 'item.restore', 'item.purge', 'item.delete', 'item.discard'];
export const GONE = ['item.purge', 'item.delete'];
export const TEN_MIN = 600_000;

/** Entries newest first; those of the same actor on the same item within ten minutes of an earlier kept one join it. */
export function foldEvents<E extends AuditLike>(entries: readonly E[]): Folded<E>[] {
  const out: Folded<E>[] = [];
  const near = (a: E, b: E) => Math.abs(Date.parse(a.at) - Date.parse(b.at)) <= TEN_MIN;
  const same = (a: E, b: E) => a.actorKind === b.actorKind && a.actorId === b.actorId && a.subject === b.subject;
  for (const e of entries) {
    const hit = out.find((r) => same(r.e, e) && near(r.e, e));
    if (hit) hit.actions.push(e.action);
    else out.push({ e, actions: [e.action] });
  }
  return out;
}

/** The newest action on each item, from entries newest first. */
export function lastActions(entries: readonly AuditLike[]): Map<string, string> {
  const last = new Map<string, string>();
  for (const e of entries) if (e.subject && !last.has(e.subject)) last.set(e.subject, e.action);
  return last;
}
