import { api } from '../api/client';

// The share-link and relay routes of the management API.

export interface Share {
  id: string;
  channel: string;
  locale: string;
  createdBy: string;
  /** The creator's display name (absent on older servers). */
  createdByName?: string;
  createdAt: string;
  expiresAt: string;
  revokedAt?: string;
  viewers: number;
}

export interface CreatedShare {
  id: string;
  /** Absent when the item has no preview URL in the channel. */
  url?: string;
  token: string;
  expiresAt: string;
  channel: string;
  locale: string;
}

export type ShareState = 'active' | 'expired' | 'revoked';

export function shareState(s: Pick<Share, 'expiresAt' | 'revokedAt'>, now: number = Date.now()): ShareState {
  if (s.revokedAt) return 'revoked';
  return new Date(s.expiresAt).getTime() <= now ? 'expired' : 'active';
}

/** Expiry choices, in seconds (the unit `ttl` has on `/preview-tokens`). The server caps them. */
export const EXPIRIES = [
  { value: '3600', key: 'preview.share.exp1h' },
  { value: '86400', key: 'preview.share.exp1d' },
  { value: '604800', key: 'preview.share.exp7d' },
] as const;
export const DEFAULT_EXPIRY = '86400';

export const shareApi = {
  async list(item: string, signal?: AbortSignal): Promise<Share[]> {
    const r = await api.get('/preview-shares', { query: { item }, signal });
    return r?.shares ?? [];
  },
  create(body: { item: string; channel: string; locale?: string; ttl: number }): Promise<CreatedShare> {
    return api.post('/preview-shares', { body });
  },
  revoke(id: string): Promise<void> {
    return api.del('/preview-shares/{id}', { params: { id } });
  },
};

export const relayApi = {
  put(item: string, locale: string, fields: Record<string, unknown>): Promise<unknown> {
    return api.put('/preview-relay/{item}', { params: { item }, query: { locale }, body: { fields } });
  },
  del(item: string, locale: string, keepalive = false): Promise<unknown> {
    return api.del('/preview-relay/{item}', { params: { item }, query: { locale }, keepalive });
  },
};
