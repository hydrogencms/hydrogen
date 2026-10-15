import { core } from './messages/core';

const table: Record<string, string> = { ...core };

// Area catalogs (collections, pages, settings) register themselves when their route chunk loads.
// Add keys from a module or project (H5c overrides). Later registrations win.
export function register(extra: Record<string, string>) {
  Object.assign(table, extra);
}

export function t(key: string, vars?: Record<string, string | number>): string {
  const s = table[key];
  if (s === undefined) return key;
  return vars ? s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m)) : s;
}
