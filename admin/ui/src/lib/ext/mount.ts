import { api } from '../api/client';
import { session } from '../auth/session.svelte';
import { config } from '../config';
import { navigateTo } from '../extensions.svelte';
import { t } from '../i18n';
import { toast } from '../ui/toast.svelte';

export interface Ctx {
  api: Record<'get' | 'post' | 'patch' | 'del', (...a: any[]) => Promise<any>>;
  t: typeof t;
  navigate(path: string): void;
  toast(kind: 'info' | 'success' | 'error', text: string): void;
  principal: { kind: string; id?: string; name: string };
  can(policy: string, opts?: { type?: string }): boolean;
  locale: string;
  prefix: string;
}

const loose = api as unknown as Record<'get' | 'post' | 'patch' | 'del', (path: string, o?: object) => Promise<unknown>>;
const sig = (init?: RequestInit) => ({ signal: init?.signal ?? undefined });

function buildCtx(): Ctx {
  const p = session.principal;
  return {
    api: {
      get: (path: string, init?: RequestInit) => loose.get(path, sig(init)),
      post: (path: string, body?: unknown, init?: RequestInit) => loose.post(path, { body, ...sig(init) }),
      patch: (path: string, body?: unknown, init?: RequestInit) => loose.patch(path, { body, ...sig(init) }),
      del: (path: string, init?: RequestInit) => loose.del(path, sig(init)),
    },
    t,
    navigate: navigateTo,
    toast: (kind, text) => void toast.push(kind, text),
    principal: p ? { kind: p.kind, id: p.id, name: p.name } : { kind: 'anonymous', name: '' },
    can: (policy, opts) => session.can(policy, opts),
    locale: 'en',
    prefix: config().prefix,
  };
}

let cached: Ctx | undefined;
let key: unknown[] = [];

/** Built once per session; a new object after the principal or grants change. */
export function getCtx(): Ctx {
  const k = [session.principal, session.grants, config().prefix];
  if (!cached || k.some((x, i) => x !== key[i])) {
    cached = buildCtx();
    key = k;
  }
  return cached;
}

export interface Mounted {
  el: HTMLElement;
  update(props: Record<string, unknown>): void;
  setCtx(ctx: Ctx): void;
  destroy(): void;
}

const same = (a: unknown, b: unknown) => {
  if (Object.is(a, b)) return true;
  if (a && b && typeof a === 'object' && typeof b === 'object') {
    try {
      return JSON.stringify(a) === JSON.stringify(b);
    } catch {
      return false;
    }
  }
  return false;
};

/** Events whose payload the admin reads: the detail must be an object. */
const NEEDS_DETAIL = new Set(['change', 'navigate']);

/**
 * Creates `<tag>`, assigns `ctx` and the props as properties (never attributes) before it connects, and
 * appends it to `container`. `on` maps event names to listeners on the element; only CustomEvents count (a native
 * `change` bubbling from an inner input is ignored), and `change`/`navigate` need an object `detail`.
 */
export function mountElement(
  container: Element,
  tag: string,
  props: Record<string, unknown>,
  opts: { ctx: Ctx; on?: Record<string, (e: CustomEvent) => void> },
): Mounted {
  const el = document.createElement(tag) as HTMLElement & Record<string, unknown>;
  el.ctx = opts.ctx;
  const cur: Record<string, unknown> = {};
  const update = (next: Record<string, unknown>) => {
    for (const [k, v] of Object.entries(next)) {
      if (k in cur && same(cur[k], v)) continue;
      cur[k] = v;
      el[k] = v;
    }
  };
  update(props);
  const offs = Object.entries(opts.on ?? {}).map(([name, fn]) => {
    const h = (e: Event) => {
      if (!(e instanceof CustomEvent)) return;
      if (NEEDS_DETAIL.has(name) && (typeof e.detail !== 'object' || e.detail === null)) return;
      fn(e);
    };
    el.addEventListener(name, h);
    return () => el.removeEventListener(name, h);
  });
  container.append(el);
  return {
    el,
    update,
    setCtx(c) {
      el.ctx = c;
    },
    destroy() {
      for (const o of offs) o();
      el.remove();
    },
  };
}
