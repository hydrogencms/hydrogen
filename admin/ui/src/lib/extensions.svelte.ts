import { session } from './auth/session.svelte';
import { config } from './config';
import { register } from './i18n';
import { router } from './router.svelte';
import { toast } from './ui/toast.svelte';

export interface Entry {
  tag: string;
  title?: string;
  types?: string[];
  policy?: string;
}
export interface ViewDef extends Entry {
  path: string;
  menu?: string;
}
export interface SlotDef {
  tag: string;
  at: string;
  policy?: string;
}
export interface Extensions {
  elements: string[];
  theme: { name?: string; logo?: string; accent?: string; css?: string };
  strings: string[];
  widgets: Record<string, string>;
  cells: Record<string, string>;
  panels: Entry[];
  actions: Entry[];
  dashboard: Entry[];
  views: ViewDef[];
  slots: SlotDef[];
}

const BAD_KEYS = new Set(['__proto__', 'constructor', 'prototype']);
const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined);

/** Own string -> string entries only, in a prototype-less object. */
export function cleanMap(v: unknown): Record<string, string> {
  const out: Record<string, string> = Object.create(null);
  if (!isObj(v)) return out;
  for (const [k, x] of Object.entries(v)) if (!BAD_KEYS.has(k) && typeof x === 'string') out[k] = x;
  return out;
}

const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

function entries<T>(v: unknown, make: (o: Record<string, unknown>) => T | undefined): T[] {
  return (Array.isArray(v) ? v : []).flatMap((o) => (isObj(o) ? (make(o) ?? []) : []));
}
const entry = (o: Record<string, unknown>): Entry | undefined => {
  const tag = str(o.tag);
  if (!tag) return undefined;
  return { tag, title: str(o.title), types: Array.isArray(o.types) ? strings(o.types) : undefined, policy: str(o.policy) };
};

/** Reads the `extensions` part of config.json; anything missing or malformed is dropped. */
export function parseExtensions(raw: unknown): Extensions {
  const r = isObj(raw) ? raw : {};
  const th = isObj(r.theme) ? r.theme : {};
  return {
    elements: strings(r.elements),
    theme: { name: str(th.name), logo: str(th.logo), accent: str(th.accent), css: str(th.css) },
    strings: typeof r.strings === 'string' ? [r.strings] : strings(r.strings),
    widgets: cleanMap(r.widgets),
    cells: cleanMap(r.cells),
    panels: entries(r.panels, entry),
    actions: entries(r.actions, entry),
    dashboard: entries(r.dashboard, entry),
    views: entries(r.views, (o) => {
      const e = entry(o);
      const path = str(o.path);
      return e && path ? { ...e, path, menu: str(o.menu) } : undefined;
    }),
    slots: entries(r.slots, (o) => {
      const tag = str(o.tag);
      const at = str(o.at);
      return tag && at ? { tag, at, policy: str(o.policy) } : undefined;
    }),
  };
}

/** An admin-relative path with a leading `/`, or null for anything that could leave the admin. */
export function safePath(p: unknown): string | null {
  if (typeof p !== 'string') return null;
  const s = p.trim();
  if (!s || /^[a-z][a-z0-9+.-]*:/i.test(s) || /^[/\\]{2}/.test(s) || s.startsWith('/\\') || s.startsWith('\\')) return null;
  return s.startsWith('/') ? s : '/' + s;
}

/** Navigates inside the admin; absolute and protocol URLs are refused with a console warning. */
export function navigateTo(p: unknown) {
  const s = safePath(p);
  if (s === null) console.warn(`admin extension: refusing to navigate to ${String(p)}`);
  else void router.navigate(s);
}

class ExtensionsStore {
  m = $state.raw<Extensions>(parseExtensions(undefined));
  /** Replaced by tests. */
  importer: (url: string) => Promise<unknown> = (url) => import(/* @vite-ignore */ url);
  fetcher: typeof fetch = (...a) => fetch(...a);
  #loads = new Map<string, Promise<void>>();
  #failed = new Set<string>();
  #warned = new Set<string>();

  get logo() {
    return this.m.theme.logo;
  }

  /** Parse the manifest, apply the theme and merge the strings. Call after the config is loaded. */
  async boot(raw: unknown = (config() as { extensions?: unknown }).extensions) {
    this.m = parseExtensions(raw);
    this.#loads.clear();
    this.#failed.clear();
    const { theme, strings, views } = this.m;
    if (theme.name) register({ 'app.name': theme.name });
    if (theme.accent) document.documentElement.style.setProperty('--h-accent', theme.accent);
    if (theme.css) {
      const l = document.createElement('link');
      l.rel = 'stylesheet';
      l.href = theme.css;
      document.head.append(l);
    }
    views.forEach((v, i) => v.title && register({ [this.viewTitleKey(i)]: v.title }));
    const got = await Promise.all(
      strings.map(async (u) => {
        try {
          const r = await this.fetcher(u, { credentials: 'same-origin' });
          if (!r.ok) throw new Error(String(r.status));
          return cleanMap(await r.json());
        } catch {
          console.warn(`admin strings not loaded: ${u}`);
          return {};
        }
      }),
    );
    for (const s of got) register(s);
  }

  viewTitleKey(i: number) {
    return `ext.view.${i}`;
  }

  /**
   * Imports every element module on the first slot use. A URL is imported once; one that failed is retried on the
   * next call (one error toast per URL).
   */
  ensureElements(): Promise<void> {
    return Promise.all(
      this.m.elements.map((u) => {
        let p = this.#loads.get(u);
        if (!p) {
          p = this.importer(u).then(
            () => undefined,
            (e) => {
              this.#loads.delete(u);
              console.error(e);
              if (!this.#failed.has(u)) {
                this.#failed.add(u);
                toast.error(`Admin extension failed to load: ${u}`);
              }
            },
          );
          this.#loads.set(u, p);
        }
        return p;
      }),
    ).then(() => undefined);
  }

  warnOnce(msg: string) {
    if (this.#warned.has(msg)) return;
    this.#warned.add(msg);
    console.warn(msg);
  }

  widget(name?: string): string | undefined {
    if (!name) return undefined;
    const tag = this.m.widgets[name];
    if (!tag) this.warnOnce(`unknown admin widget "${name}": using the built-in one`);
    return tag;
  }

  cell(name: string): string | undefined {
    const tag = this.m.cells[name];
    if (!tag) this.warnOnce(`unknown admin cell "${name}": using the built-in one`);
    return tag;
  }

  allowed(e: { policy?: string }, type?: string) {
    return !e.policy || session.can(e.policy, type ? { type } : undefined);
  }
  #fits(e: Entry, type?: string) {
    return !e.types?.length || !type || e.types.includes(type);
  }
  panels(type: string) {
    return this.m.panels.filter((e) => this.#fits(e, type) && this.allowed(e, type));
  }
  actions(type: string) {
    return this.m.actions.filter((e) => this.#fits(e, type) && this.allowed(e, type));
  }
  dashboard() {
    return this.m.dashboard.filter((e) => this.allowed(e));
  }
  slots(at: string) {
    return this.m.slots.filter((s) => s.at === at && this.allowed(s));
  }
  /** Menu entries the principal may open, grouped by `menu` in manifest order. */
  menu(): { menu: string; views: (ViewDef & { index: number })[] }[] {
    const out: { menu: string; views: (ViewDef & { index: number })[] }[] = [];
    this.m.views.forEach((v, index) => {
      if (!v.menu || !this.allowed(v)) return;
      let g = out.find((x) => x.menu === v.menu);
      if (!g) out.push((g = { menu: v.menu, views: [] }));
      g.views.push({ ...v, index });
    });
    return out;
  }
}

export const ext = new ExtensionsStore();
