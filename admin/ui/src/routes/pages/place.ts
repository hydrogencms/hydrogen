import { api } from '../../lib/api/client';
import type { UnplacedItem } from './logic';

/** Puts an existing page item into a site's tree (no parent: as the site's root page). Returns the node id. */
export async function placeItem(site: string, item: string, parent?: string): Promise<string> {
  const node = await api.post('/pages/sites/{site}/nodes', {
    params: { site },
    body: { kind: 'page', item, parent: parent || undefined } as never,
  });
  return (node as { id: string }).id;
}

export async function listUnplaced(
  query: { locale?: string; limit: number; offset: number; item?: string },
  signal?: AbortSignal,
): Promise<{ items: UnplacedItem[]; total: number }> {
  const res = await api.get('/pages/unplaced', { query: { ...query, locale: query.locale || undefined }, signal });
  return res as { items: UnplacedItem[]; total: number };
}
