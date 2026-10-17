import { ApiError, api, csrfToken, handleFailure } from '../api/client';
import { config } from '../config';
import { fileOf, type FileValue } from './logic';

// The media routes are newer than the generated paths: the untyped surface of the same wrapper.
type Loose = Record<'get' | 'post' | 'patch' | 'del', (path: string, o?: object) => Promise<any>>;
const loose = api as unknown as Loose;

export interface Uploaded {
  file: FileValue;
  /** The server already held these bytes. */
  existing: boolean;
}

const aborted = () => new DOMException('aborted', 'AbortError');

/** A request that took longer than this is given up on (ten minutes: slow links, big files). */
export const UPLOAD_TIMEOUT = 600_000;
const PROGRESS_EVERY = 100;

/**
 * POST /uploads (multipart) with progress. XHR rather than fetch: fetch cannot report upload progress.
 * `type` makes the server check `accept`/`max_size` before it stores anything. A 401 or 5xx gets the same global
 * handling as the API client's calls (sign-in, server-error report).
 */
export function uploadFile(file: File, o: { type?: string; signal?: AbortSignal; timeout?: number; onProgress?: (fraction: number) => void } = {}): Promise<Uploaded> {
  return new Promise((resolve, reject) => {
    if (o.signal?.aborted) return reject(aborted());
    const xhr = new XMLHttpRequest();
    xhr.open('POST', config().apiPrefix + '/uploads');
    xhr.setRequestHeader('Accept', 'application/json');
    const csrf = csrfToken();
    if (csrf) xhr.setRequestHeader('X-CSRF-Token', csrf);
    xhr.withCredentials = true;
    xhr.timeout = o.timeout ?? UPLOAD_TIMEOUT;
    const onAbort = () => xhr.abort();
    o.signal?.addEventListener('abort', onAbort, { once: true });
    const settle = <T>(fn: (v: T) => void, v: T) => {
      o.signal?.removeEventListener('abort', onAbort);
      fn(v);
    };
    let last = 0;
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable || e.total <= 0) return;
      const now = Date.now();
      if (now - last < PROGRESS_EVERY && e.loaded < e.total) return;
      last = now;
      o.onProgress?.(e.loaded / e.total);
    };
    xhr.onerror = () => settle(reject, new ApiError(0, 'network', 'network error'));
    xhr.ontimeout = () => settle(reject, new ApiError(0, 'timeout', 'upload timed out'));
    xhr.onabort = () => settle(reject, aborted());
    xhr.onload = () => {
      let body: any;
      try {
        body = xhr.responseText ? JSON.parse(xhr.responseText) : undefined;
      } catch {
        body = undefined;
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        const f = fileOf(body?.file ?? body);
        if (!f) return settle(reject, new ApiError(xhr.status, 'bad_response', 'unexpected upload response'));
        o.onProgress?.(1);
        // 201: new bytes; 200: the server already held them (an item may already use them).
        settle(resolve, { file: f, existing: xhr.status === 200 });
      } else {
        const env = body?.error;
        const err = new ApiError(xhr.status, env?.code ?? 'error', env?.message ?? xhr.statusText, { fields: env?.fields, body: env });
        handleFailure(err);
        settle(reject, err);
      }
    };
    const form = new FormData();
    if (o.type) form.append('type', o.type);
    form.append('file', file, file.name);
    xhr.send(form);
  });
}

export interface AssetRow {
  id: string;
  name: string;
  type: string;
  status: string;
  updatedAt: string;
  file?: FileValue;
  folder: string;
  tags: string[];
}

const rowOf = (i: any): AssetRow => ({
  id: i.id,
  name: i.name,
  type: i.type,
  status: i.status,
  updatedAt: i.updatedAt,
  file: fileOf(i.fields?.file),
  folder: typeof i.fields?.folder === 'string' && i.fields.folder ? i.fields.folder : '/',
  tags: Array.isArray(i.fields?.tags) ? i.fields.tags.filter((x: unknown) => typeof x === 'string') : [],
});

export interface AssetQuery {
  q?: string;
  folder?: string;
  tag?: string;
  limit?: number;
  offset?: number;
}

export interface Facet {
  value: string;
  count: number;
}
export interface Facets {
  folders: Facet[];
  tags: Facet[];
}

const facetList = (v: unknown): Facet[] =>
  Array.isArray(v)
    ? v.filter((x): x is Facet => !!x && typeof x.value === 'string' && x.value !== '').map((x) => ({ value: x.value, count: Number(x.count) || 0 }))
    : [];

export const mediaApi = {
  async list(type: string, q: AssetQuery, signal?: AbortSignal): Promise<{ items: AssetRow[]; total: number }> {
    const query: Record<string, string | number | undefined> = {
      fields: 'file,folder,tags',
      sort: '-createdAt',
      limit: q.limit ?? 48,
      offset: q.offset ?? 0,
      q: q.q?.trim() || undefined,
    };
    // `under`: the folder and its descendants, so a folder shows its subfolders' assets
    if (q.folder && q.folder !== '/') query['where[folder][under]'] = q.folder;
    if (q.tag) query['where[tags][contains]'] = q.tag;
    const r = await loose.get('/collections/{type}/items', { params: { type }, query, signal });
    return { items: (r.items as any[]).map(rowOf), total: r.total ?? r.items.length };
  },

  /** The distinct folders and tags in use by the assets of a type, with counts (one call; the server aggregates). */
  async facets(type: string, signal?: AbortSignal): Promise<Facets> {
    const r = await loose.get('/collections/{type}/facets', { params: { type }, query: { fields: 'folder,tags' }, signal, quiet: true });
    return { folders: facetList(r?.folder), tags: facetList(r?.tags) };
  },

  /** The first item of the upload types that holds the file with this hash (the server filters by file id). */
  async bySha(types: string[], sha: string, signal?: AbortSignal): Promise<{ id: string; name: string } | undefined> {
    for (const type of types) {
      const r = await loose.get('/collections/{type}/items', { params: { type }, query: { 'where[file][eq]': sha, limit: 1 }, signal });
      const it = (r.items as any[])[0];
      if (it) return { id: it.id, name: it.name ?? '' };
    }
    return undefined;
  },

  async create(type: string, file: FileValue, extra: { folder?: string; tags?: string[] } = {}): Promise<{ id: string }> {
    const fields: Record<string, unknown> = { file };
    if (extra.folder && extra.folder !== '/') fields.folder = extra.folder;
    if (extra.tags?.length) fields.tags = extra.tags;
    return loose.post('/collections/{type}/items', { params: { type }, body: { fields } });
  },

  references(id: string, signal?: AbortSignal): Promise<{ items: { id: string; name: string; type: string; field: string; onDelete: string }[]; total: number; hidden?: number }> {
    return loose.get('/items/{id}/references', { params: { id }, signal });
  },
};
