import { folderTree, normFolder, type FolderNode } from './logic';
import { mediaApi, type AssetRow, type Facets } from './upload';

/** How long the folder and tag lists of a type are trusted without a change we know of (other users may add some). */
export const FACETS_TTL = 120_000;

/** The facets read so far, by upload type: shared by every picker and the Assets screen, so opening a picker is free. */
class FacetCache {
  #byType = new Map<string, { at: number; facets: Facets }>();
  #now: () => number;
  constructor(now: () => number = () => Date.now()) {
    this.#now = now;
  }
  get(type: string, ttl = FACETS_TTL): Facets | undefined {
    const e = this.#byType.get(type);
    return e && this.#now() - e.at < ttl ? e.facets : undefined;
  }
  set(type: string, facets: Facets) {
    this.#byType.set(type, { at: this.#now(), facets });
  }
  /** Folders or tags changed (an upload into a new folder, an edit, a trash): the next load reads them again. */
  invalidate(type?: string) {
    if (type) this.#byType.delete(type);
    else this.#byType.clear();
  }
}
export const facetCache = new FacetCache();

/** The state of one browsable view of the media library (the Assets screen and the picker each own one). */
export class Library {
  type = $state('');
  folder = $state('');
  tag = $state('');
  q = $state('');
  page = $state(1);
  readonly size: number;

  rows = $state.raw<AssetRow[] | null>(null);
  total = $state(0);
  failed = $state(false);
  /** The folders and tags in use by the selected type's assets. */
  facets = $state.raw<Facets>({ folders: [], tags: [] });
  extraFolders = $state.raw<string[]>([]);
  tick = $state(0);
  facetsTick = $state(0);

  readonly types: string[];
  #ctl: AbortController | undefined;
  #fctl: AbortController | undefined;

  constructor(types: string[], size = 48) {
    this.types = types;
    this.size = size;
    this.type = types[0] ?? '';
  }

  folders: FolderNode[] = $derived(
    folderTree([...this.facets.folders.map((f) => f.value), ...this.extraFolders, this.folder ? this.folder : undefined]),
  );
  tags: string[] = $derived([...new Set(this.facets.tags.map((x) => x.value))].sort((a, b) => a.localeCompare(b)));
  folderPaths: string[] = $derived(flat(this.folders));

  addFolder(path: string): string {
    const p = normFolder(path);
    if (p !== '/') this.extraFolders = [...new Set([...this.extraFolders, p])];
    return p;
  }

  /** The item that already holds these bytes, asked of the server (it filters by file hash). */
  async known(sha: string): Promise<{ id: string; name: string } | undefined> {
    return mediaApi.bySha(this.types, sha);
  }

  /** Re-reads the list. */
  reload() {
    this.tick++;
  }

  /** The folders or tags changed: they are read again, and the list too. */
  invalidateFacets() {
    facetCache.invalidate();
    this.facetsTick++;
    this.tick++;
  }

  /** Reads the list for the current state. Call it inside an `$effect`: it reads the state before awaiting. */
  load(): () => void {
    const type = this.type;
    const q = { q: this.q, folder: this.folder || undefined, tag: this.tag || undefined, limit: this.size, offset: (this.page - 1) * this.size };
    void this.tick;
    this.#ctl?.abort();
    const ctl = (this.#ctl = new AbortController());
    this.failed = false;
    if (!type) {
      this.rows = [];
      return () => ctl.abort();
    }
    mediaApi
      .list(type, q, ctl.signal)
      .then((r) => {
        this.rows = r.items;
        this.total = r.total;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        this.failed = true;
        this.rows = this.rows ?? [];
        void e;
      });
    return () => ctl.abort();
  }

  /**
   * Reads the folders and tags of the selected type (one call), or takes them from the cache. Call it inside an
   * `$effect`: it reruns on a type change or `invalidateFacets`, never on a plain `reload`.
   */
  loadFacets(): () => void {
    const type = this.type;
    void this.facetsTick;
    this.#fctl?.abort();
    const ctl = (this.#fctl = new AbortController());
    const cached = type ? facetCache.get(type) : undefined;
    if (!type || cached) {
      this.facets = cached ?? { folders: [], tags: [] };
      return () => ctl.abort();
    }
    mediaApi
      .facets(type, ctl.signal)
      .then((f) => {
        if (ctl.signal.aborted) return;
        facetCache.set(type, f);
        this.facets = f;
      })
      .catch(() => {
        // an old server has no facets endpoint: the tree shows the folders at hand
      });
    return () => ctl.abort();
  }
}

function flat(nodes: FolderNode[]): string[] {
  return nodes.flatMap((n) => [n.id, ...flat(n.children)]);
}
