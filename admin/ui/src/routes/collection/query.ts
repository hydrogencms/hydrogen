// The list screen's state: the URL query string, and how it maps to the management API's list parameters
// (`where[field][op]=value`, `sort=-field`, `q`, `status`, `locale`, `limit`, `offset`).
import type { CompiledField } from '../../lib/types.svelte';

export type Op = 'eq' | 'ne' | 'in' | 'prefix' | 'lt' | 'lte' | 'gt' | 'gte' | 'exists' | 'contains';

export interface Filter {
  field: string;
  op: Op;
  value: string;
}

export interface ListQuery {
  q: string;
  status: '' | 'published' | 'draft';
  locale: string;
  /** API grammar: `-updatedAt`, `title,-createdAt`. */
  sort: string;
  page: number;
  pageSize: number;
  filters: Filter[];
}

export const PAGE_SIZE = 25;
export const DEFAULT_SORT = '-updatedAt';
export const OPS: Op[] = ['eq', 'ne', 'in', 'prefix', 'lt', 'lte', 'gt', 'gte', 'exists', 'contains'];

const WHERE = /^where\[([^\]]+)\](?:\[([a-z]+)\])?$/;

export function emptyQuery(): ListQuery {
  return { q: '', status: '', locale: '', sort: '', page: 1, pageSize: PAGE_SIZE, filters: [] };
}

/** Reads the list state from the address bar. Unknown or malformed pieces are ignored. */
export function parseQuery(p: URLSearchParams): ListQuery {
  const q = emptyQuery();
  q.q = p.get('q') ?? '';
  const st = p.get('status');
  q.status = st === 'published' || st === 'draft' ? st : '';
  q.locale = p.get('locale') ?? '';
  q.sort = p.get('sort') ?? '';
  const page = Number(p.get('page'));
  q.page = Number.isInteger(page) && page >= 1 ? page : 1;
  const size = Number(p.get('size'));
  if (Number.isInteger(size) && size >= 1 && size <= 200) q.pageSize = size;
  for (const [k, v] of p.entries()) {
    const m = WHERE.exec(k);
    if (!m) continue;
    const op = (m[2] ?? 'eq') as Op;
    if (OPS.includes(op)) q.filters.push({ field: m[1]!, op, value: v });
  }
  return q;
}

/** Address bar patch for `q`: every key owned by the list (stale `where[...]` keys become undefined). */
export function toUrlPatch(q: ListQuery, current: URLSearchParams): Record<string, string | number | undefined> {
  const patch: Record<string, string | number | undefined> = {
    q: q.q || undefined,
    status: q.status || undefined,
    locale: q.locale || undefined,
    sort: q.sort || undefined,
    page: q.page > 1 ? q.page : undefined,
    size: q.pageSize !== PAGE_SIZE ? q.pageSize : undefined,
  };
  for (const k of current.keys()) if (WHERE.test(k)) patch[k] = undefined;
  for (const f of q.filters) patch[`where[${f.field}][${f.op}]`] = f.value;
  return patch;
}

/** The API's query parameters; `defaultLocale` is used when the URL names none (e.g. the channel's first language). */
export function toApiQuery(q: ListQuery, defaultLocale = ''): Record<string, string | number> {
  const out: Record<string, string | number> = { limit: q.pageSize, offset: (q.page - 1) * q.pageSize };
  if (q.q.trim()) out.q = q.q.trim();
  if (q.status) out.status = q.status;
  const locale = q.locale || defaultLocale;
  if (locale) out.locale = locale;
  out.sort = q.sort || DEFAULT_SORT;
  for (const f of q.filters) {
    if (f.field && (f.value !== '' || f.op === 'exists')) out[`where[${f.field}][${f.op}]`] = f.value;
  }
  return out;
}

/** Sort state of a table header: key and direction of the first sort key. */
export function sortState(q: ListQuery): { key: string; dir: 'asc' | 'desc' } {
  const first = (q.sort || DEFAULT_SORT).split(',')[0]!;
  return first.startsWith('-') ? { key: first.slice(1), dir: 'desc' } : { key: first, dir: 'asc' };
}

/** Header click: same key flips direction, a new key starts ascending (dates start descending). */
export function nextSort(q: ListQuery, key: string, descFirst = false): string {
  const cur = sortState(q);
  if (cur.key === key) return cur.dir === 'asc' ? `-${key}` : key;
  return descFirst ? `-${key}` : key;
}

/** Filter operators the API accepts for a field, by base type. */
export function opsFor(f: CompiledField): Op[] {
  const list = f.shape.base === 'list';
  const shape = list && f.of ? f.of.shape : f.shape;
  const isTime = shape.format === 'date' || shape.format === 'datetime';
  let ops: Op[];
  switch (shape.base) {
    case 'number':
      ops = ['eq', 'ne', 'lt', 'lte', 'gt', 'gte', 'in'];
      break;
    case 'boolean':
    case 'ref':
      ops = ['eq', 'ne', 'in'];
      break;
    default:
      ops = isTime ? ['eq', 'ne', 'lt', 'lte', 'gt', 'gte'] : ['eq', 'ne', 'in', 'prefix'];
  }
  ops.push('exists');
  if (list) ops.push('contains');
  return ops;
}

/** Fields the list can filter or sort on: top-level, indexed, of a filterable base. */
export function filterableFields(fields: CompiledField[]): CompiledField[] {
  return fields.filter((f) => {
    if (!f.index) return false;
    const base = (f.shape.base === 'list' ? f.of?.shape.base : f.shape.base) ?? '';
    return ['string', 'number', 'boolean', 'ref'].includes(base);
  });
}

// ---- saved views (per user, in localStorage)

export interface SavedView {
  name: string;
  query: string;
}

export function parseViews(raw: string | null): SavedView[] {
  try {
    const v = JSON.parse(raw ?? '[]');
    return Array.isArray(v)
      ? v.filter((x): x is SavedView => typeof x?.name === 'string' && typeof x?.query === 'string')
      : [];
  } catch {
    return [];
  }
}

/** Adds or replaces the view called `name`. */
export function upsertView(views: SavedView[], view: SavedView): SavedView[] {
  return [...views.filter((v) => v.name !== view.name), view].sort((a, b) => a.name.localeCompare(b.name));
}

/** A stable text for "is this the same view?": ignores the page and the order of the filters. */
export function canonicalQuery(qs: string | URLSearchParams): string {
  const q = parseQuery(typeof qs === 'string' ? new URLSearchParams(qs) : qs);
  const filters = q.filters.map((f) => `${f.field}\u0000${f.op}\u0000${f.value}`).sort();
  return JSON.stringify([q.q, q.status, q.locale, q.sort || DEFAULT_SORT, q.pageSize, filters]);
}

/** Renames the view `from`; a clash with another view's name replaces that one. */
export function renameView(views: SavedView[], from: string, to: string): SavedView[] {
  const v = views.find((x) => x.name === from);
  if (!v) return views;
  return upsertView(views.filter((x) => x.name !== from), { ...v, name: to });
}
