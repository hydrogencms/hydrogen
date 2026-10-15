import { api } from '../api/client';
import { types } from '../types.svelte';
import { blocksFieldOf, type Block } from './tree';

export interface SharedInfo {
  id: string;
  name: string;
  type: string;
  status: string;
  /** The blocks of the shared item's draft in the locale (empty when it has none yet). */
  blocks: Block[];
}

/** Reads a blockset item (its draft, in `locale`) with its blocks: what a shared block shows. */
export async function readShared(id: string, locale: string, signal?: AbortSignal): Promise<SharedInfo> {
  await types.load();
  const v = await api.get('/items/{id}', { params: { id }, query: { draft: true, locale: locale || undefined } as never, signal });
  const field = blocksFieldOf(types.get(v.type)?.fields ?? []);
  const raw = field ? (v.fields as Record<string, unknown>)[field.name] : undefined;
  return { id, name: v.name || id, type: v.type, status: v.status, blocks: Array.isArray(raw) ? (raw as Block[]) : [] };
}
