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

/** Where to look an item entry up again: the item, the languages it touched and the version it published or unpublished. */
export interface ItemRef {
  id: string;
  locales: string[];
  version?: number;
}

export function itemRefOf(e: Entry): ItemRef | undefined {
  const id = itemIdOf(e);
  if (!id) return undefined;
  const d = dataOf(e);
  const locales = Array.isArray(d.locales) ? d.locales.filter((l): l is string => typeof l === 'string' && l !== '') : [];
  if (typeof d.locale === 'string' && d.locale) locales.push(d.locale);
  const v = d.version;
  const versioned = e.action === 'item.publish' || e.action === 'item.unpublish';
  return { id, locales, version: versioned && typeof v === 'number' && v > 0 ? v : undefined };
}

// Readable descriptions -------------------------------------------------------------------------------------------

/** Name of the thing `id` of `kind` (user, group, role, token, item), when it has been looked up. */
export type Lookup = (kind: string, id: string) => string | undefined;
export type Translate = (key: string, vars?: Record<string, string | number>) => string;
export interface Change {
  label: string;
  value: string;
}
export interface Description {
  /** One plain-language sentence about what happened (the actor is shown separately). */
  text: string;
  /** What changed, with human labels and names for ids. */
  changes: Change[];
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const ISO_TIME = /^\d{4}-\d\d-\d\dT\d\d:\d\d/;

/** Data keys that hold the id of a directory object or item. */
const KEY_KIND: Record<string, string> = { user: 'user', item: 'item', add: 'item', remove: 'item', removedRef: 'item', roleId: 'role' };
const DIRECTORY_KINDS = new Set(['user', 'group', 'role', 'token']);
/** Data keys never listed as changes: the agent block (shown with the actor) and technical ids. */
const HIDDEN = new Set(['share', 'principalKind', 'principalId']);

/** Every (kind, id) an entry mentions, so the page can look their names up in one batch. */
export function referencedIds(e: Entry): [string, string][] {
  const out: [string, string][] = [];
  const k = subjectKind(e.action);
  if (e.subject && UUID.test(e.subject)) {
    if (DIRECTORY_KINDS.has(k)) out.push([k, e.subject]);
    else if (k === 'auth') out.push(['user', e.subject]);
    else if (ITEM_KINDS.has(k)) out.push(['item', e.subject]);
  }
  const d = dataOf(e);
  for (const [key, kind] of Object.entries(KEY_KIND)) {
    const v = d[key];
    if (typeof v === 'string' && UUID.test(v)) out.push([kind, v]);
  }
  const pk = d.principalKind;
  if (typeof pk === 'string' && DIRECTORY_KINDS.has(pk) && typeof d.principalId === 'string') out.push([pk, d.principalId]);
  if (e.action === 'release.publish' && Array.isArray(d.items)) {
    for (const i of d.items) if (typeof i === 'string' && UUID.test(i)) out.push(['item', i]);
  }
  return out;
}

const humanKey = (key: string): string => {
  const s = key.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/[._]+/g, ' ').toLowerCase();
  return s.charAt(0).toUpperCase() + s.slice(1);
};

export const formatBytes = (n: number): string => (n < 1024 ? `${n} B` : n < 1024 * 1024 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`);

/**
 * A plain-language description of an audit entry: a sentence and the changed fields. Ids become names where
 * `name` knows them (else a name recorded with the entry, else a short id). Unknown actions get the humanized
 * action and every data key as a generic row; odd data never throws.
 */
export function describeEntry(e: Entry, name: Lookup, t: Translate): Description {
  const d = dataOf(e);
  const used = new Set<string>();
  const str = (key: string): string => {
    used.add(key);
    const v = d[key];
    return typeof v === 'string' ? v : '';
  };
  const nm = (kind: string, id: string, recorded = ''): string => (id && name(kind, id)) || recorded || (id ? shortId(id) : '');
  const k = subjectKind(e.action);
  const subject = e.subject ?? '';
  const tt = (suffix: string, vars: Record<string, string | number>) => t(`settings.audit.d.${e.action}${suffix}`, vars);

  let text = '';
  switch (e.action) {
    case 'item.create':
    case 'item.update':
    case 'item.publish':
    case 'item.unpublish':
    case 'item.trash':
    case 'item.restore':
    case 'item.purge':
    case 'item.discard':
    case 'item.delete':
    case 'item.migrate':
    case 'review.submitted':
    case 'review.approved':
    case 'review.changes_requested':
    case 'review.withdrawn':
    case 'preview.share.create':
    case 'preview.share.revoke':
      text = tt('', { item: nm('item', itemIdOf(e)) });
      used.add('item');
      break;
    case 'item.restore_version':
      text = tt('', { item: nm('item', itemIdOf(e)), n: typeof d.version === 'number' ? d.version : '?' });
      used.add('version');
      break;
    case 'release.create':
    case 'release.update':
    case 'release.delete':
    case 'release.publish':
    case 'release.fail':
    case 'release.cancel':
    case 'release.schedule': {
      const release = str('name') || t('settings.audit.d.release');
      if (e.action === 'release.update' && typeof d.add === 'string') text = tt('.add', { item: nm('item', str('add')), release });
      else if (e.action === 'release.update' && typeof d.remove === 'string') text = tt('.remove', { item: nm('item', str('remove')), release });
      else if (e.action === 'release.update' && d.unscheduled === true) text = tt('.unscheduled', { release }), used.add('unscheduled');
      else if (e.action === 'release.schedule') text = tt('', { release, at: typeof d.at === 'string' ? new Date(d.at).toLocaleString() : '?' }), used.add('at');
      else text = tt('', { release });
      break;
    }
    case 'page.place':
      text = tt('', { item: nm('item', str('item')) });
      break;
    case 'page.mount':
      text = tt('', { collection: str('collection') });
      break;
    case 'media.gc':
      text = tt('', { n: typeof d.count === 'number' ? d.count : 0 });
      used.add('count');
      break;
    case 'user.create':
    case 'user.delete':
    case 'auth.changed':
      text = tt('', { user: nm('user', subject, str('login')) });
      used.add('login');
      break;
    case 'user.update': {
      const user = nm('user', subject, str('login'));
      const onlyPassword = Array.isArray(d.fields) && d.fields.length === 1 && d.fields[0] === 'password';
      text = tt(onlyPassword ? '.password' : '', { user });
      used.add('login');
      if (onlyPassword) used.add('fields');
      break;
    }
    case 'group.create':
    case 'group.update':
    case 'group.delete':
      text = tt('', { group: nm('group', subject, str('name')) });
      used.add('name');
      break;
    case 'group.member_add':
    case 'group.member_remove':
      text = tt('', { user: nm('user', str('user'), str('login')), group: nm('group', subject, str('group')) });
      break;
    case 'role.create':
    case 'role.update':
    case 'role.delete':
      text = tt('', { role: nm('role', subject, str('name')) });
      used.add('name');
      break;
    case 'token.create':
    case 'token.revoke':
    case 'token.delete':
      text = tt('', { token: nm('token', subject, str('name')) });
      used.add('name');
      break;
    case 'assignment.create':
    case 'assignment.delete':
      text = tt('', { role: str('role') || nm('role', str('roleId')), who: nm(str('principalKind'), str('principalId')) || t('settings.audit.d.someone') });
      break;
    case 'auth.login':
    case 'auth.logout':
    case 'auth.login_failed':
    case 'role.seed':
    case 'media.upload':
    case 'page.link':
    case 'page.update':
    case 'page.move':
    case 'page.delete':
      text = tt('', {});
      break;
  }
  if (text.startsWith('settings.audit.d.')) text = '';
  if (!text) {
    const verb = t(`settings.audit.v.${e.action}`);
    text = verb.startsWith('settings.audit.v.') ? humanizeAction(e.action) : verb;
    text = text.charAt(0).toUpperCase() + text.slice(1);
    used.clear();
  }

  const label = (key: string): string => {
    const l = t(`settings.audit.f.${key}`);
    return l.startsWith('settings.audit.f.') ? humanKey(key) : l;
  };
  const value = (key: string, v: unknown): string => {
    if (v === null || v === undefined || v === '') return '—';
    if (typeof v === 'boolean') return t(v ? 'common.yes' : 'common.no');
    if (typeof v === 'number') return key === 'size' ? formatBytes(v) : String(v);
    if (typeof v === 'string') {
      if (UUID.test(v)) return nm(KEY_KIND[key] ?? (key === 'items' ? 'item' : ''), v);
      return ISO_TIME.test(v) ? new Date(v).toLocaleString() : v;
    }
    if (Array.isArray(v)) return v.length ? v.map((x) => (typeof x === 'string' && (key === 'fields' || key === 'changed') ? label(x).toLowerCase() : value(key, x))).join(', ') : '—';
    return JSON.stringify(v);
  };
  const changes: Change[] = [];
  for (const [key, v] of Object.entries(d)) {
    if (used.has(key) || HIDDEN.has(key)) continue;
    if (key === 'actor' && (k === 'review' || typeof v === 'object')) continue;
    // The ids the sentence names, and the item an entry is about, are not repeated as rows.
    if ((key === 'user' && e.action.startsWith('group.')) || (key === 'item' && (ITEM_KINDS.has(k) || k === 'page'))) continue;
    if (e.action === 'release.update' && (key === 'add' || key === 'remove')) continue;
    changes.push({ label: label(key), value: value(key, v) });
  }
  return { text, changes };
}
