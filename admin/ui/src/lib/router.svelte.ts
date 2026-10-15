import type { Component } from 'svelte';
import { config } from './config';

type Loader = () => Promise<{ default: Component<any> }>;
type Guard = (r: Route | null, path: string) => string | void;

export interface Route {
  /** `/collections/:type`; a trailing `/*` captures the rest as params.rest. */
  path: string;
  load: Loader;
  /** Reachable without a session. */
  public?: boolean;
  /** i18n key for document.title. */
  title?: string;
}

interface Compiled extends Route {
  parts: string[];
  splat: boolean;
}

export function match(routes: Compiled[], path: string): { route: Compiled; params: Record<string, string> } | null {
  let segs: string[];
  try {
    segs = path.split('/').filter(Boolean).map(decodeURIComponent);
  } catch {
    return null; // malformed %-escape: not found
  }
  for (const r of routes) {
    const params: Record<string, string> = {};
    let ok = r.splat ? segs.length >= r.parts.length : segs.length === r.parts.length;
    for (let i = 0; ok && i < r.parts.length; i++) {
      const p = r.parts[i];
      if (p.startsWith(':')) params[p.slice(1)] = segs[i];
      else ok = p === segs[i];
    }
    if (ok) {
      if (r.splat) params.rest = segs.slice(r.parts.length).join('/');
      return { route: r, params };
    }
  }
  return null;
}

class Router {
  path = $state('/');
  query = $state(new URLSearchParams());
  params = $state<Record<string, string>>({});
  component = $state.raw<Component<any> | null>(null);
  route = $state.raw<Route | null>(null);
  loading = $state(false);
  loadError = $state<Error | null>(null);

  #routes: Compiled[] = [];
  #notFound: Loader | undefined;
  #guard: Guard | undefined;
  #blockers = new Set<() => boolean>();
  #dirty = new Set<() => boolean>();
  #seq = 0;

  get prefix() {
    return config().prefix;
  }

  /** Register the route table (first match wins; put `/*` routes last). */
  define(routes: Route[], opts: { notFound?: Loader; guard?: Guard } = {}) {
    this.#routes = routes.map((r) => {
      const parts = r.path.split('/').filter(Boolean);
      const splat = parts[parts.length - 1] === '*';
      if (splat) parts.pop();
      return { ...r, parts, splat };
    });
    this.#notFound = opts.notFound;
    this.#guard = opts.guard;
  }

  /** Called by forms with unsaved changes: return false to cancel a navigation. */
  block(fn: () => boolean): () => void {
    this.#blockers.add(fn);
    return () => this.#blockers.delete(fn);
  }

  /** Forms register a dirty check (no prompt): used to avoid leaving the page when a 401 arrives. */
  trackDirty(fn: () => boolean): () => void {
    this.#dirty.add(fn);
    return () => this.#dirty.delete(fn);
  }

  get anyDirty(): boolean {
    for (const d of this.#dirty) if (d()) return true;
    return false;
  }

  #allowed() {
    for (const b of this.#blockers) if (!b()) return false;
    return true;
  }

  start() {
    addEventListener('popstate', () => {
      if (!this.#allowed()) {
        history.pushState(null, '', this.href(this.path, this.query));
        return;
      }
      void this.#resolve();
    });
    return this.#resolve();
  }

  /** Path inside the admin, e.g. `/collections/article` (without prefix). */
  #current(): string {
    let p = location.pathname;
    if (this.prefix && p.startsWith(this.prefix)) p = p.slice(this.prefix.length);
    return p || '/';
  }

  href(path: string, query?: Record<string, string | number | undefined | null> | URLSearchParams): string {
    const qs = query instanceof URLSearchParams ? query : toParams(query);
    const s = qs.toString();
    return this.prefix + path + (s ? '?' + s : '');
  }

  async navigate(to: string, opts: { replace?: boolean; force?: boolean } = {}) {
    if (!opts.force && !this.#allowed()) return;
    const url = this.prefix + to;
    if (opts.replace) history.replaceState(null, '', url);
    else history.pushState(null, '', url);
    await this.#resolve();
  }

  /** Change query parameters in place (list state lives in the URL). undefined/''/null removes a key. */
  setQuery(patch: Record<string, string | number | boolean | undefined | null>, opts: { push?: boolean } = {}) {
    const qs = new URLSearchParams(this.query);
    for (const [k, v] of Object.entries(patch)) {
      if (v === undefined || v === null || v === '' || v === false) qs.delete(k);
      else qs.set(k, String(v));
    }
    this.query = qs;
    const url = this.href(this.path, qs);
    if (opts.push) history.pushState(null, '', url);
    else history.replaceState(null, '', url);
  }

  async #resolve(): Promise<void> {
    const seq = ++this.#seq;
    const path = this.#current();
    let m = match(this.#routes, path);
    const redirect = this.#guard?.(m?.route ?? null, path);
    if (redirect) {
      history.replaceState(null, '', this.prefix + redirect);
      return this.#resolve();
    }
    this.path = path;
    this.query = new URLSearchParams(location.search);
    this.params = m?.params ?? {};
    this.route = m?.route ?? null;
    this.loading = true;
    this.loadError = null;
    try {
      const mod = await (m ? m.route.load() : this.#notFound!());
      if (seq !== this.#seq) return;
      this.component = mod.default;
    } catch (e) {
      if (seq !== this.#seq) return;
      this.loadError = e as Error;
    } finally {
      if (seq === this.#seq) this.loading = false;
    }
    window.scrollTo?.(0, 0);
  }

  retry() {
    return this.#resolve();
  }
}

function toParams(q?: Record<string, string | number | undefined | null>): URLSearchParams {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(q ?? {})) if (v !== undefined && v !== null && v !== '') qs.set(k, String(v));
  return qs;
}

export const router = new Router();

/** `href('/items/1', {tab: 'info'})`: a URL including the admin prefix. */
export const href = (path: string, query?: Record<string, string | number | undefined | null>) =>
  router.href(path, query);

/**
 * `<a use:link href={href('/items/1')}>`: in-app navigation without a reload. Modified clicks, targets and
 * links outside the admin prefix keep the browser's behaviour.
 */
export function link(node: HTMLAnchorElement) {
  const click = (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if ((node.target && node.target !== '_self') || node.hasAttribute('download')) return;
    const u = new URL(node.href, location.href);
    if (u.origin !== location.origin) return;
    const p = router.prefix;
    if (p && u.pathname !== p && !u.pathname.startsWith(p + '/')) return;
    e.preventDefault();
    void router.navigate((u.pathname.slice(p.length) || '/') + u.search);
  };
  node.addEventListener('click', click);
  return { destroy: () => node.removeEventListener('click', click) };
}
