import { api, ApiError } from '../api/client';
import { session } from '../auth/session.svelte';
import type { RichCustom } from './config';

const none: RichCustom = { nodes: [], marks: [] };

/**
 * The project's custom rich text nodes and marks (`GET /richtext`), loaded once per session. A server without
 * the endpoint (404) has none.
 */
class RichCatalog {
  value = $state.raw<RichCustom>(none);
  loaded = $state(false);
  #inflight: Promise<void> | undefined;

  constructor() {
    session.onClear(() => this.reset());
  }

  /** Never rejects: a failed load leaves the catalog empty and unloaded, so the next editor tries again. */
  load(): Promise<void> {
    if (this.loaded) return Promise.resolve();
    this.#inflight ??= (async () => {
      try {
        const r = (await api.get('/richtext' as never)) as unknown as Partial<RichCustom>;
        this.value = {
          nodes: (r.nodes ?? []).map((n) => ({ ...n, parts: n.parts ?? [], attrs: n.attrs ?? [] })),
          marks: (r.marks ?? []).map((m) => ({ ...m, attrs: m.attrs ?? [] })),
        };
        this.loaded = true;
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) this.loaded = true;
      } finally {
        this.#inflight = undefined;
      }
    })();
    return this.#inflight;
  }

  reset() {
    this.value = none;
    this.loaded = false;
  }
}

export const richCatalog = new RichCatalog();
