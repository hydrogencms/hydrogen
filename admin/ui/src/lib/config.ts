export interface AdminConfig {
  apiPrefix: string;
  prefix: string;
  pages: boolean;
  deliveryPrefix: string;
  /** `content.default_locale`: the language screens start in when nothing else says. */
  defaultLocale?: string;
  /** `delivery.share_links`: false hides the Share button (absent = on). */
  shareLinks?: boolean;
  /** Raw `extensions` manifest; parsed by lib/extensions.svelte.ts. */
  extensions?: unknown;
  /** Present when the project has upload types. */
  media?: {
    /** The largest accepted upload, in bytes. */
    maxUpload?: number;
    /** Where assets are served from: a path or an absolute URL; files live at `<base>/<sha>/<name>`. */
    assetBase?: string;
    /** Where image thumbnails are served from (`<base>/<sha>/<name>`); absent without the images module. */
    thumbBase?: string;
  };
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
// at any admin.prefix and for any deep link. (A variable keeps Vite from treating the URL as an asset.) The Vite
// dev server serves modules unbundled from <prefix>/src/…, where that relative path misses, so dev uses its base.
const CONFIG = '../config.json';
export function configURL(dev = import.meta.env.DEV, base = import.meta.env.BASE_URL, from = import.meta.url): URL {
  return dev ? new URL(base + 'config.json', from) : new URL(CONFIG, from);
}

export async function loadConfig(fetcher: typeof fetch = fetch): Promise<AdminConfig> {
  const res = await fetcher(configURL(), { credentials: 'same-origin' });
  if (!res.ok) throw new Error(`config.json: ${res.status}`);
  setConfig(await res.json());
  return current;
}
