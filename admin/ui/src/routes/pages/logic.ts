// Pure helpers of the pages screen: kept free of Svelte and the API so they can be unit tested.

export interface MountSpec {
  segment: string;
  collection: string;
  filter?: Record<string, unknown>;
  route: string;
  canonical?: boolean;
  label?: Record<string, string>;
}

export interface LinkSpec {
  label: Record<string, string>;
  item?: string;
  node?: string;
  url?: string;
}

/** A node of GET /pages/sites/{site}/tree (hand-typed: the schema leaves `nodes` open). */
export interface PNode {
  id: string;
  site: string;
  parent?: string;
  kind: 'page' | 'mount' | 'link';
  item?: string;
  hidden: boolean;
  depth: number;
  mount?: MountSpec;
  link?: LinkSpec;
  type?: string;
  name: string;
  path?: string;
  status?: 'draft' | 'published' | 'changed';
  children: number;
  /** Published locales of a page's item. */
  locales?: string[];
  /** Pending draft parts of a page's item ("" = shared). */
  draftParts?: string[];
  restricted?: boolean;
  target?: string;
  broken?: boolean;
  nodes?: PNode[];
}

export type Position = 'before' | 'after' | 'inside';

/** The page types a parent type allows as children; null = any (the `children` option is absent). */
export function allowedChildren(options: Record<string, unknown> | undefined): string[] | null {
  const c = options?.children;
  return Array.isArray(c) ? c.filter((x): x is string => typeof x === 'string') : null;
}

/** Page types that may be added below `parent`. */
export function addableTypes(
  parent: Pick<PNode, 'kind' | 'restricted' | 'type'>,
  pageTypes: string[],
  optionsOf: (type: string) => Record<string, unknown> | undefined,
): string[] {
  if (parent.kind !== 'page' || parent.restricted) return [];
  const allowed = allowedChildren(parent.type ? optionsOf(parent.type) : undefined);
  return allowed ? pageTypes.filter((p) => allowed.includes(p)) : pageTypes;
}

/** True when `child` may sit under `parent`: only restricted-free page nodes hold children, and page children
 *  must be of an allowed type. Mounts and links go anywhere a page does. */
export function canHold(
  parent: Pick<PNode, 'kind' | 'restricted' | 'type'> | undefined,
  child: Pick<PNode, 'kind' | 'type'>,
  optionsOf: (type: string) => Record<string, unknown> | undefined,
): boolean {
  if (!parent || parent.kind !== 'page' || parent.restricted) return false;
  if (child.kind !== 'page') return true;
  const allowed = allowedChildren(parent.type ? optionsOf(parent.type) : undefined);
  return !allowed || (!!child.type && allowed.includes(child.type));
}

export interface Placement {
  parent: string;
  /** Sibling to follow; null = first. */
  after: string | null;
}

/** Where a drop lands: the new parent and the sibling to follow. `siblings(parent)` lists the loaded children of
 *  a parent in order (the moving node may be among them and is skipped). */
export function placement(
  position: Position,
  target: Pick<PNode, 'id' | 'parent'>,
  movingId: string,
  siblings: (parent: string) => string[],
): Placement | null {
  if (position === 'inside') {
    const sibs = siblings(target.id).filter((id) => id !== movingId);
    return { parent: target.id, after: sibs.length ? sibs[sibs.length - 1] : null };
  }
  if (!target.parent) return null;
  const sibs = siblings(target.parent).filter((id) => id !== movingId);
  const i = sibs.indexOf(target.id);
  if (i < 0) return null;
  return { parent: target.parent, after: position === 'after' ? target.id : i > 0 ? sibs[i - 1] : null };
}

export interface SubNode {
  id: string;
  name: string;
  kind: PNode['kind'];
  path?: string;
  status?: PNode['status'];
}

export interface UrlChange {
  locale: string;
  id: string;
  name: string;
  from: string;
  to: string;
  /** A published page whose old URL will redirect to the new one. */
  redirect: boolean;
}

export function joinPath(parent: string, segment: string): string {
  return (parent.endsWith('/') ? parent : parent + '/') + segment;
}

const lastSegment = (p: string) => p.slice(p.lastIndexOf('/') + 1);

/**
 * URLs that change when `movedId` goes below a parent whose path is `newParent[locale]`. `subtree[locale]` holds
 * the moved node and its descendants with their current paths; the moved node keeps its last segment, and every
 * descendant swaps the old prefix for the new one.
 */
export function urlChanges(
  movedId: string,
  newParent: Record<string, string>,
  subtree: Record<string, SubNode[]>,
): UrlChange[] {
  const out: UrlChange[] = [];
  for (const locale of Object.keys(subtree).sort()) {
    const list = subtree[locale];
    const root = list.find((n) => n.id === movedId)?.path;
    const parent = newParent[locale];
    if (!root || parent === undefined) continue;
    const next = joinPath(parent, lastSegment(root));
    if (next === root) continue;
    for (const n of list) {
      if (!n.path) continue;
      let to: string;
      if (n.path === root) to = next;
      else if (n.path.startsWith(root + '/')) to = next + n.path.slice(root.length);
      else continue;
      out.push({ locale, id: n.id, name: n.name, from: n.path, to, redirect: n.kind === 'page' && n.status !== 'draft' });
    }
  }
  return out;
}

export interface Filter {
  q: string;
  type: string;
  /** draft, published, changed or hidden. */
  status: string;
  /** Only nodes missing one of the listed languages. */
  gaps: boolean;
}

export const emptyFilter = (): Filter => ({ q: '', type: '', status: '', gaps: false });
export const filterActive = (f: Filter) => !!(f.q.trim() || f.type || f.status || f.gaps);

export function matchesFilter(n: PNode, f: Filter, locales: string[] | undefined, languages: string[]): boolean {
  const q = f.q.trim().toLowerCase();
  if (q && !n.name.toLowerCase().includes(q)) return false;
  if (f.type && n.type !== f.type) return false;
  if (f.status) {
    if (f.status === 'hidden' ? !n.hidden : n.status !== f.status) return false;
  }
  if (f.gaps) {
    if (n.kind !== 'page' || !locales) return false;
    if (languages.every((l) => locales.includes(l))) return false;
  }
  return true;
}

/** Ids of matching nodes plus all their ancestors. `parentOf` maps a node id to its parent id. */
export function visibleIds(
  all: Iterable<PNode>,
  match: (n: PNode) => boolean,
  parentOf: (id: string) => string | undefined,
): { visible: Set<string>; matches: Set<string> } {
  const visible = new Set<string>();
  const matches = new Set<string>();
  for (const n of all) {
    if (n.restricted || !match(n)) continue;
    matches.add(n.id);
    for (let id: string | undefined = n.id; id !== undefined && !visible.has(id); id = parentOf(id)) visible.add(id);
  }
  return { visible, matches };
}

/** Query parameters (flat, ready for the API wrapper) listing the items a mount shows. */
export function mountQuery(
  mount: Pick<MountSpec, 'collection' | 'filter'>,
  page: { limit: number; offset: number },
  locale?: string,
): { type: string; query: Record<string, string | number> } {
  const query: Record<string, string | number> = { limit: page.limit, offset: page.offset };
  if (locale) query.locale = locale;
  for (const [field, v] of Object.entries(mount.filter ?? {}).sort(([a], [b]) => a.localeCompare(b))) {
    if (Array.isArray(v)) query[`where[${field}][in]`] = v.map(String).join(',');
    else if (v !== null && v !== undefined) query[`where[${field}][eq]`] = String(v);
  }
  return { type: mount.collection, query };
}

/** "field=value" lines to a mount filter; a comma separated value matches any of them. */
export function parseFilter(text: string): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {};
  for (const line of text.split('\n')) {
    const i = line.indexOf('=');
    if (i <= 0) continue;
    const k = line.slice(0, i).trim();
    const v = line.slice(i + 1).trim();
    if (!k || !v) continue;
    out[k] = v.includes(',') ? v.split(',').map((s) => s.trim()).filter(Boolean) : v;
  }
  return Object.keys(out).length ? out : undefined;
}

export function filterText(f: Record<string, unknown> | undefined): string {
  return Object.entries(f ?? {})
    .map(([k, v]) => `${k}=${Array.isArray(v) ? v.join(',') : String(v)}`)
    .join('\n');
}

/** Languages of the page nodes' completeness dots: those the item has content in. */
export const hasLocale = (locales: string[] | undefined, l: string) => !!locales?.includes(l);
