import { api, type ApiError } from '../../lib/api/client';
import '../../lib/messages/collections';
import { t } from '../../lib/i18n';

/** An item as the management API returns it (the generated union is per type; the editor is generic). */
export interface ItemData {
  id: string;
  type: string;
  key?: string;
  locale: string;
  mainLocale: string;
  alwaysAvailable: boolean;
  status: 'draft' | 'published' | 'changed' | 'unpublished';
  version: number;
  draft: boolean;
  name: string;
  fields: Record<string, unknown>;
  locales: string[];
  draftParts: string[];
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
  actions?: { edit: boolean; publish: boolean; delete: boolean };
}

export interface Referrer {
  id: string;
  type: string;
  name: string;
  field: string;
  kind: string;
  onDelete: 'block' | 'nullify' | 'warn';
}

/** Where an item lives: a collection item by id, or a singleton by type (its item exists after the first save). */
export type Target = { kind: 'item'; id: string } | { kind: 'singleton'; type: string };

// The generated paths only list the singletons of the server the types were generated from, so these calls go
// through the untyped surface of the same wrapper.
type Loose = Record<'get' | 'post' | 'patch' | 'del', (path: string, o?: object) => Promise<any>>;
const loose = api as unknown as Loose;

const base = (t: Target) => (t.kind === 'item' ? { path: '/items/{id}', params: { id: t.id } } : { path: '/singletons/{type}', params: { type: t.type } });

/** Optimistic concurrency: `ifMatch` is the ETag read earlier; `onHeaders` receives the new one after a success. */
export interface Concurrency {
  ifMatch?: string;
  onHeaders?: (h: Headers) => void;
  /** Extra request headers (the collaboration client id). */
  headers?: Record<string, string>;
}

export const itemApi = {
  read(t: Target, locale: string, signal?: AbortSignal, c: Concurrency = {}): Promise<ItemData> {
    const b = base(t);
    return loose.get(b.path, { params: b.params, query: { draft: true, locale: locale || undefined }, signal, onHeaders: c.onHeaders });
  },
  update(t: Target, body: object, c: Concurrency = {}): Promise<ItemData> {
    const b = base(t);
    return loose.patch(b.path, { params: b.params, body, ...c });
  },
  publish(t: Target, body: object, c: Concurrency = {}): Promise<ItemData> {
    return t.kind === 'item'
      ? loose.post('/items/{id}/publish', { params: { id: t.id }, body, ...c })
      : loose.post('/singletons/{type}/publish', { params: { type: t.type }, body, ...c });
  },
  create(type: string, body: object, c: Concurrency = {}): Promise<ItemData> {
    return loose.post('/collections/{type}/items', { params: { type }, body, onHeaders: c.onHeaders });
  },
  discard(id: string, parts?: string[], c: Concurrency = {}): Promise<void> {
    return loose.del('/items/{id}/draft', { params: { id }, query: { parts: parts?.join(',') }, ...c });
  },
  /** Moves the item to the trash. */
  remove(id: string, c: Concurrency = {}): Promise<void> {
    return loose.del('/items/{id}', { params: { id }, ...c });
  },
  /** Collection items only: singletons cannot be unpublished. */
  unpublish(t: Target, c: Concurrency = {}): Promise<ItemData> {
    if (t.kind !== 'item') return Promise.reject(new Error('singletons cannot be unpublished'));
    return loose.post('/items/{id}/unpublish', { params: { id: t.id }, ...c });
  },
};

/** A 412 from a write: the item changed since it was read. */
export const isConflict = (e: unknown): boolean => (e as ApiError)?.status === 412;

export const isNotFound = (e: unknown) => (e as ApiError)?.status === 404;

/** True when it is fine to replace the form with a fresh load: nothing unsaved, or the user agreed to discard it. */
export function confirmDiscard(dirty: boolean, ask: (message: string) => boolean): boolean {
  return !dirty || ask(t('collections.reloadDiscard'));
}
