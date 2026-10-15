import { api, type Schemas } from '../../lib/api/client';
import type { TreeNode } from '../../lib/ui/tree';
import { mountQuery, type PNode } from './logic';

type Summary = Schemas['Summary'];

export type RowData =
  | { row: 'node'; n: PNode }
  | { row: 'item'; mount: string; item: Summary }
  | { row: 'more'; mount: string; left: number };

export interface MountPage {
  items: Summary[];
  total: number;
  limit: number;
}

export const MOUNT_PAGE = 20;
const KIND_ICON = { page: 'page', mount: 'layers', link: 'link' } as const;

/** The loaded part of one site's tree, in one locale. The Tree component only renders what `nodes` derives. */
export class SiteTree {
  site: string;
  locale = $state('');
  byId = $state.raw(new Map<string, PNode>());
  kids = $state.raw(new Map<string, string[]>());
  mounts = $state.raw(new Map<string, MountPage>());
  locales = $state.raw(new Map<string, string[]>());
  rootsLoaded = $state(false);
  /** True once the whole tree (to depth 10) was loaded for filtering. */
  full = $state(false);
  #gen = 0;

  constructor(site: string, locale: string) {
    this.site = site;
    this.locale = locale;
  }

  nodes = $derived(this.#build(''));

  /** Filtered view: only `visible` ids, every loaded level shown. */
  pruned(visible: Set<string>): TreeNode[] {
    return this.#build('', visible);
  }

  #build(parent: string, visible?: Set<string>): TreeNode[] {
    return (this.kids.get(parent) ?? []).filter((id) => !visible || visible.has(id)).map((id) => this.#node(id, visible));
  }

  #node(id: string, visible?: Set<string>): TreeNode {
    const n = this.byId.get(id)!;
    const t: TreeNode = {
      id,
      label: n.name || '…',
      icon: KIND_ICON[n.kind],
      hasChildren: n.kind === 'mount' ? true : n.children > 0,
      disabled: !!n.restricted,
      data: { row: 'node', n } satisfies RowData,
    };
    if (n.kind === 'mount') {
      const m = this.mounts.get(id);
      if (m && !visible) {
        const kids: TreeNode[] = m.items.map((it) => ({
          id: `item:${id}:${it.id}`,
          label: it.name,
          icon: 'file',
          data: { row: 'item', mount: id, item: it } satisfies RowData,
        }));
        if (m.total > m.items.length)
          kids.push({
            id: `more:${id}`,
            label: '',
            data: { row: 'more', mount: id, left: m.total - m.items.length } satisfies RowData,
          });
        t.children = kids;
      } else if (visible) t.children = [];
    } else if (this.kids.has(id)) t.children = this.#build(id, visible);
    return t;
  }

  /** Stores nodes below `parent` ('' = the site roots) and any nested levels. */
  ingest(parent: string, list: PNode[]) {
    const byId = new Map(this.byId);
    const kids = new Map(this.kids);
    const locales = new Map(this.locales);
    const walk = (p: string, items: PNode[]) => {
      kids.set(p, items.map((n) => n.id));
      for (const n of items) {
        const { nodes, ...rest } = n;
        byId.set(n.id, { ...rest, parent: n.parent ?? (p || undefined) });
        if (n.item && n.locales) locales.set(n.item, n.locales);
        if (nodes) walk(n.id, nodes);
      }
    };
    walk(parent, list);
    this.byId = byId;
    this.kids = kids;
    this.locales = locales;
    for (const n of list) if (n.kind === 'mount' && !n.restricted && !this.mounts.has(n.id)) void this.loadMount(n.id, MOUNT_PAGE);
  }

  async loadChildren(parent: string, depth = 1): Promise<void> {
    const gen = this.#gen;
    const res = await api.get('/pages/sites/{site}/tree', {
      params: { site: this.site },
      query: { parent: parent || undefined, depth, locale: this.locale || undefined },
    });
    if (gen !== this.#gen) return;
    const { items, root } = res as unknown as { items: PNode[]; root?: PNode };
    if (parent) this.ingest(parent, items);
    else if (root) {
      // The API lists the children of the site's root page; the root itself is the single top row.
      this.ingest(root.id, items);
      const byId = new Map(this.byId);
      byId.set(root.id, { ...root, children: items.length });
      this.byId = byId;
      if (root.item && root.locales) this.locales = new Map(this.locales).set(root.item, root.locales);
      this.kids = new Map(this.kids).set('', [root.id]);
    } else this.kids = new Map(this.kids).set('', []);
    if (!parent) this.rootsLoaded = true;
  }

  async loadMount(id: string, limit: number): Promise<void> {
    const n = this.byId.get(id);
    if (!n?.mount) return;
    const gen = this.#gen;
    const { type, query } = mountQuery(n.mount, { limit, offset: 0 }, this.locale);
    const res = await api.get('/collections/{type}/items', { params: { type }, query: query as never });
    if (gen !== this.#gen) return;
    const m = new Map(this.mounts);
    m.set(id, { items: res.items, total: res.total, limit });
    this.mounts = m;
  }

  /** Loads the open levels (breadth first) so a remembered expansion renders at once. */
  async restore(expanded: Set<string>): Promise<void> {
    await this.loadChildren('');
    let pending = [...expanded].filter((id) => this.byId.has(id));
    const done = new Set<string>();
    while (pending.length) {
      const level = pending.filter((id) => !done.has(id) && !this.kids.has(id) && this.byId.get(id)?.kind === 'page' && this.byId.get(id)!.children > 0);
      level.forEach((id) => done.add(id));
      await Promise.all(level.map((id) => this.loadChildren(id).catch(() => undefined)));
      pending = [...expanded].filter((id) => this.byId.has(id) && !done.has(id));
      if (!level.length) break;
    }
  }

  /** Reloads the children of a parent after a change, and its mounts' item lists. */
  async refresh(parent: string): Promise<void> {
    await this.loadChildren(parent);
    if (parent) {
      const p = await api.get('/pages/nodes/{id}', { params: { id: parent } });
      this.byId = new Map(this.byId).set(parent, { ...this.byId.get(parent)!, children: this.kids.get(parent)?.length ?? 0, hidden: p.hidden });
    }
  }

  /** The full tree (depth 10) for filtering; mounts are not expanded. */
  async loadAll(): Promise<void> {
    await this.loadChildren('', 10);
    this.full = true;
  }

  parentOf = (id: string): string | undefined => this.byId.get(id)?.parent;
  siblings = (parent: string): string[] => this.kids.get(parent) ?? [];

  invalidate() {
    this.#gen++;
  }
}
