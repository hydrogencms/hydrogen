// The admin (parent) side of the preview bridge protocol v1: see docs/bridge-protocol.md.
import { equal, patchOps, plain } from './patch';

export type Phase = 'connecting' | 'disconnected' | 'live' | 'reload' | 'error';
export interface HostStatus {
  phase: Phase;
  message?: string;
}

export interface HostOptions {
  /** `scheme://host[:port]` of the preview URL: the only origin messages are sent to and accepted from. */
  origin: string;
  frame(): Window | null;
  item: string;
  itemType: string;
  locale: string;
  /** Reload mode: save the draft; resolves true when the server has what the form shows. */
  save?(): Promise<boolean>;
  onStatus?(s: HostStatus): void;
  onRendered?(ms: number, rev: number): void;
  patchDelay?: number;
  saveDelay?: number;
}

type Mode = 'live' | 'reload';

/**
 * Keeps one preview frame in step with the working document: `init` on `ready`, a debounced JSON Patch on every
 * change (live mode), a draft save then `reload` (reload mode), `resync` answered with a fresh `init`.
 */
export class PreviewHost {
  rev = 0;
  mode: Mode | null = null;
  #o: HostOptions;
  #doc: unknown = null;
  #sent: unknown = null;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #busy = false;
  #again = false;
  #dead = false;
  #pending = false;
  #readySince = false;

  constructor(o: HostOptions) {
    this.#o = o;
  }

  /**
   * Call from the iframe's load event. A bridge connects while the page loads, so a `ready` seen since the last
   * load belongs to this page; without one the page has no bridge (or navigated away from it) and the old state
   * would be stale.
   */
  frameLoaded() {
    if (this.#readySince) {
      this.#readySince = false;
      return;
    }
    this.mode = null;
    this.#sent = null;
    this.#pending = false;
    clearTimeout(this.#timer);
    this.#o.onStatus?.({ phase: 'disconnected' });
  }

  destroy() {
    this.#dead = true;
    clearTimeout(this.#timer);
  }

  #post(msg: Record<string, unknown>) {
    this.#o.frame()?.postMessage({ h: 1, ...msg }, this.#o.origin);
  }

  handle(e: { origin: string; source: unknown; data: unknown }) {
    const w = this.#o.frame();
    if (this.#dead || !w || e.source !== w || e.origin !== this.#o.origin) return;
    const m = e.data as Record<string, unknown> | null;
    if (!m || typeof m !== 'object' || m.h !== 1 || typeof m.type !== 'string') return;
    switch (m.type) {
      case 'ready':
        this.#readySince = true;
        this.mode = m.mode === 'reload' ? 'reload' : 'live';
        this.#o.onStatus?.({ phase: this.mode });
        if (this.mode === 'live') this.#init();
        else this.#sent ??= this.#doc;
        break;
      case 'resync':
        if (this.mode !== 'reload') this.#init();
        break;
      case 'rendered':
        if (typeof m.ms === 'number') this.#o.onRendered?.(m.ms, Number(m.rev));
        if (this.mode) this.#o.onStatus?.({ phase: this.mode });
        break;
      case 'error':
        this.#o.onStatus?.({ phase: 'error', message: String(m.message ?? '') });
        break;
    }
  }

  #init() {
    if (this.#doc === null) {
      this.#pending = true;
      return;
    }
    this.#pending = false;
    this.rev++;
    this.#sent = this.#doc;
    this.#post({ type: 'init', item: this.#o.item, itemType: this.#o.itemType, locale: this.#o.locale, rev: this.rev, doc: this.#doc });
  }

  /** The working document changed (any JSON; compared after a JSON round trip). */
  setDoc(doc: unknown) {
    this.#doc = plain(doc);
    if (!this.mode) return;
    if (this.#pending && this.mode === 'live') return this.#init();
    if (this.#sent === null) {
      this.#sent = this.#doc;
      return;
    }
    clearTimeout(this.#timer);
    if (equal(this.#sent, this.#doc)) return;
    this.#timer = setTimeout(() => void this.flush(), this.mode === 'live' ? (this.#o.patchDelay ?? 50) : (this.#o.saveDelay ?? 600));
  }

  /** Sends what changed now (live) or saves and reloads (reload mode). */
  async flush() {
    clearTimeout(this.#timer);
    if (this.#dead || !this.mode) return;
    if (this.mode === 'live') {
      if (this.#sent === null) return this.#init();
      const doc = this.#doc;
      if (equal(this.#sent, doc)) return;
      const ops = patchOps(this.#sent, doc);
      const base = this.rev;
      this.rev++;
      this.#sent = doc;
      this.#post({ type: 'patch', rev: this.rev, base, ops });
      return;
    }
    if (this.#busy) {
      this.#again = true;
      return;
    }
    this.#busy = true;
    try {
      const doc = this.#doc;
      const ok = (await this.#o.save?.()) ?? true;
      if (ok && !this.#dead) {
        this.#sent = doc;
        this.reload();
      }
    } finally {
      this.#busy = false;
      if (this.#again && !this.#dead) {
        this.#again = false;
        if (!equal(this.#sent, this.#doc)) void this.flush();
      }
    }
  }

  /** Reload mode: ask the page to fetch itself again. */
  reload() {
    this.rev++;
    this.#post({ type: 'reload', rev: this.rev });
  }
}
