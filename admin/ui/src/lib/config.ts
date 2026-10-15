export interface AdminConfig {
  apiPrefix: string;
  prefix: string;
  pages: boolean;
  deliveryPrefix: string;
  /** Raw `extensions` manifest; parsed by lib/extensions.svelte.ts. */
  extensions?: unknown;
}

let current: AdminConfig = {
  apiPrefix: '/api/manage/v1',
  prefix: '',
  pages: false,
  deliveryPrefix: '/api/deliver/v1',
};

export const config = (): AdminConfig => current;

export function setConfig(c: Partial<AdminConfig>) {
  current = { ...current, ...c, prefix: (c.prefix ?? current.prefix).replace(/\/+$/, '') };
}

// The bundle is served from <prefix>/assets/, so config.json is one level up from the entry script; this works
// at any admin.prefix and for any deep link. (A variable keeps Vite from treating the URL as an asset.)
const CONFIG = '../config.json';
export async function loadConfig(fetcher: typeof fetch = fetch): Promise<AdminConfig> {
  const res = await fetcher(new URL(CONFIG, import.meta.url), { credentials: 'same-origin' });
  if (!res.ok) throw new Error(`config.json: ${res.status}`);
  setConfig(await res.json());
  return current;
}
