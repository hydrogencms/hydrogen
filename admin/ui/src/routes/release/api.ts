import { api } from '../../lib/api/client';
import { normRelease, type Release } from './logic';

// The release routes are not in the generated paths yet: the untyped surface of the same wrapper.
type Loose = Record<'get' | 'post' | 'put' | 'patch' | 'del', (path: string, o?: object) => Promise<any>>;
const loose = api as unknown as Loose;

export interface ReleaseList {
  items: Release[];
  total?: number;
  hasMore?: boolean;
}

export const releaseApi = {
  async list(query: { status?: string; kind?: string; item?: string; limit?: number; offset?: number } = {}, signal?: AbortSignal): Promise<ReleaseList> {
    const r = await loose.get('/releases', { query: { limit: 200, ...query }, signal });
    return { items: ((r.items ?? []) as Record<string, unknown>[]).map(normRelease), total: r.total, hasMore: r.hasMore };
  },
  async get(id: string, signal?: AbortSignal): Promise<Release> {
    return normRelease(await loose.get('/releases/{id}', { params: { id }, signal }));
  },
  async create(body: { name: string; description?: string; kind?: string }): Promise<Release> {
    return normRelease(await loose.post('/releases', { body }));
  },
  async update(id: string, body: { name?: string; description?: string }): Promise<Release> {
    return normRelease(await loose.patch('/releases/{id}', { params: { id }, body }));
  },
  remove(id: string): Promise<void> {
    return loose.del('/releases/{id}', { params: { id } });
  },
  putItem(id: string, item: string, body: { action: 'publish' | 'unpublish'; locales?: string[] }): Promise<unknown> {
    return loose.put('/releases/{id}/items/{item}', { params: { id, item }, body });
  },
  removeItem(id: string, item: string): Promise<void> {
    return loose.del('/releases/{id}/items/{item}', { params: { id, item } });
  },
  schedule(id: string, at: string): Promise<unknown> {
    return loose.post('/releases/{id}/schedule', { params: { id }, body: { at } });
  },
  unschedule(id: string): Promise<unknown> {
    return loose.post('/releases/{id}/unschedule', { params: { id } });
  },
  cancel(id: string): Promise<unknown> {
    return loose.post('/releases/{id}/cancel', { params: { id } });
  },
  publish(id: string, force = false): Promise<unknown> {
    return loose.post('/releases/{id}/publish', { params: { id }, body: force ? { force: true } : {} });
  },
  /** Open and scheduled releases that contain the item. */
  async ofItem(item: string, signal?: AbortSignal): Promise<Release[]> {
    const r = await loose.get('/items/{id}/releases', { params: { id: item }, signal });
    const list = (Array.isArray(r) ? r : (r.items ?? [])) as Record<string, unknown>[];
    return list.map(normRelease);
  },
};

export interface TrashRow {
  id: string;
  type: string;
  name: string;
  deletedAt: string;
  deletedBy: string;
  deletedByName: string;
}

export const trashApi = {
  async list(query: { type?: string; limit?: number; offset?: number }, signal?: AbortSignal): Promise<{ items: TrashRow[]; total?: number; hasMore?: boolean }> {
    return loose.get('/trash', { query, signal });
  },
  read(id: string, locale?: string, signal?: AbortSignal): Promise<any> {
    return loose.get('/items/{id}', { params: { id }, query: { trashed: true, draft: true, locale: locale || undefined }, signal });
  },
  restore(id: string): Promise<unknown> {
    return loose.post('/items/{id}/restore', { params: { id } });
  },
  purge(id: string): Promise<void> {
    return loose.del('/items/{id}', { params: { id }, query: { permanent: true } });
  },
};
