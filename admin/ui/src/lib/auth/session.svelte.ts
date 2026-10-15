import { api, ApiError, type Schemas } from '../api/client';

type Grant = Schemas['Grant'];
type Principal = Schemas['Session']['principal'];

export interface CanOptions {
  type?: string;
  channel?: string;
  locale?: string;
}

class Session {
  principal = $state<Principal | null>(null);
  grants = $state<Grant[]>([]);
  csrf = $state('');
  ready = $state(false);

  #clear: Array<() => void> = [];
  #channelCollections: ((channel: string) => string[] | null | undefined) | undefined;

  /** Stores holding per-user data register here to be reset on sign-out. */
  onClear(fn: () => void) {
    this.#clear.push(fn);
  }

  /** The channels store tells how a `channel` limitation maps to collections. */
  setChannelResolver(fn: (channel: string) => string[] | null | undefined) {
    this.#channelCollections = fn;
  }

  async load(): Promise<void> {
    try {
      const s = await api.get('/auth/session', { noRedirect: true });
      this.#apply(s);
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 401) {
        this.ready = true;
        throw e;
      }
      this.clear();
    }
    this.ready = true;
  }

  #apply(s: Schemas['Session']) {
    this.principal = s.principal;
    this.grants = s.grants;
    this.csrf = s.csrf ?? this.csrf;
  }

  async login(login: string, password: string): Promise<void> {
    const r = await api.post('/auth/login', { body: { login, password }, noRedirect: true });
    this.csrf = r.csrf;
    await this.load();
  }

  async logout(): Promise<void> {
    try {
      await api.post('/auth/logout', { noRedirect: true });
    } finally {
      this.clear();
    }
  }

  /** Forget the signed-in user (sign-out or an expired session). */
  clear() {
    this.principal = null;
    this.grants = [];
    this.csrf = '';
    for (const f of this.#clear) f();
  }

  /** A stable key for per-user localStorage entries. */
  get principalKey(): string {
    const p = this.principal;
    return p ? (p.id ?? `${p.kind}:${p.name}`) : '';
  }

  /**
   * Coarse client-side check: is a grant for `policy` (or `*`) held, and do its collection, channel and locale
   * limitations allow the given options? Other limitations (owner, pages.*) are not evaluated. The API decides;
   * use this only to show or hide navigation and buttons.
   */
  can(policy: string, opts: CanOptions = {}): boolean {
    for (const g of this.grants) {
      if (g.policy !== '*' && g.policy !== policy) continue;
      if (this.#limitsAllow(g, opts)) return true;
    }
    return false;
  }

  #limitsAllow(g: Grant, o: CanOptions): boolean {
    for (const l of g.limits ?? []) {
      if (l.name === 'collection' && o.type && !l.values.includes(o.type)) return false;
      if (l.name === 'locale' && o.locale && !l.values.includes(o.locale)) return false;
      if (l.name === 'channel') {
        if (o.channel && !l.values.includes(o.channel)) return false;
        if (o.type && this.#channelCollections) {
          const ok = l.values.some((c) => {
            const cols = this.#channelCollections!(c);
            return cols == null || cols.includes(o.type!);
          });
          if (!ok) return false;
        }
      }
    }
    return true;
  }
}

export const session = new Session();
