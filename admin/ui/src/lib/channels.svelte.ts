import { api, type Schemas } from './api/client';
import { session } from './auth/session.svelte';

export type Channel = Schemas['Channel'];

const key = () => `h.channel.${session.principalKey}`;

class Channels {
  items = $state.raw<Channel[]>([]);
  loaded = $state(false);
  /** Selected channel name; '' means all channels. */
  current = $state('');

  #inflight: Promise<void> | undefined;

  constructor() {
    session.setChannelResolver((name) => this.items.find((c) => c.name === name)?.collections);
    session.onClear(() => this.reset());
  }

  load(force = false): Promise<void> {
    if (this.loaded && !force) return Promise.resolve();
    this.#inflight ??= (async () => {
      try {
        this.items = (await api.get('/channels')).items;
        const saved = localStorage.getItem(key()) ?? '';
        this.current = this.items.some((c) => c.name === saved) ? saved : '';
        this.loaded = true;
      } finally {
        this.#inflight = undefined;
      }
    })();
    return this.#inflight;
  }

  reset() {
    this.items = [];
    this.loaded = false;
    this.current = '';
  }

  select(name: string) {
    this.current = name;
    if (name) localStorage.setItem(key(), name);
    else localStorage.removeItem(key());
  }

  get active(): Channel | undefined {
    return this.items.find((c) => c.name === this.current);
  }

  /** Collections the active channel delivers; null means no filtering. */
  get collections(): string[] | null {
    return this.active?.collections ?? null;
  }

  /** Languages of the active channel (ordered fallback); null when all channels are selected. */
  get languages(): string[] | null {
    return this.active?.languages ?? null;
  }

  /** The pages-module site of the active channel, if it has one. */
  get site(): string | null {
    const s = (this.active?.options as { site?: unknown } | undefined)?.site;
    return typeof s === 'string' ? s : null;
  }

  /** True when `type` is visible under the active channel. */
  showsType(type: string): boolean {
    const c = this.collections;
    return !this.active || c == null || c.includes(type);
  }
}

export const channels = new Channels();
