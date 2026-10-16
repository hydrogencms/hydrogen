// The admin (parent) side of the preview bridge protocol v1: see docs/bridge-protocol.md.
import type { LayoutNode } from '../canvas/drop';
import type { TextRef } from '../canvas/texts';
import { equal, patchOps, plain } from './patch';

export type Phase = 'connecting' | 'disconnected' | 'unverified' | 'live' | 'reload' | 'error';
export interface HostStatus {
  phase: Phase;
  message?: string;
}

export interface HostOptions {
  /** `scheme://host[:port]` of the preview URL: the only origin messages are sent to and accepted from (unlocked frames). */
  origin: string;
  /**
   * The frame is sandboxed without `allow-same-origin` (the preview shares the admin's origin): its origin reads
   * "null", so messages are accepted by source window alone and sent with target `*`.
   */
  locked?: boolean;
  /** The preview token the pane minted; a `ready` is answered only when it carries this token's SHA-256 hex. */
  token: string;
  frame(): Window | null;
  item: string;
  itemType: string;
  locale: string;
  /** Reload mode: make the server hold the working document (the relay); resolves true when it does. */
  save?(): Promise<boolean>;
  onStatus?(s: HostStatus): void;
  onRendered?(ms: number, rev: number): void;
  /** Canvas editing (bridge `edit: true`). Messages below are routed only from a verified frame. */
  onLayout?(nodes: LayoutNode[], view: { w: number; h: number }): void;
  onHover?(path: string | null): void;
  onSelect?(path: string | null, edit: boolean): void;
  /** `null` = cancelled or refused. */
  onInlineEdit?(path: string, value: string | null): void;
  /** Read after each verified `ready`. */
  overlay?(): { on: boolean; texts: TextRef[] };
  /** From each verified `ready`. */
  onCapabilities?(edit: boolean): void;
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
  /** The last verified `ready` had `edit: true`. */
  editCapable = false;
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
  #overlay: { on: boolean; texts: TextRef[] } | null = null;
  #overlaySent = '';
  #expect: Promise<string>;
  /** Locked reload mode: when the `reload` was sent, and whether the frame's load event has been seen since. */
  #reloading: { t0: number; loaded: boolean; ready: boolean } | null = null;

  constructor(o: HostOptions) {
    this.#o = o;
    this.#expect = sha256Hex(o.token).catch(() => '');
  }

  /**
   * Call from the iframe's load event. A bridge connects while the page loads, so a `ready` seen since the last
   * load belongs to this page; without one the page has no bridge (or navigated away from it) and the old state
   * would be stale.
   */
  frameLoaded() {
    if (this.#readySince) {
      this.#readySince = false;
      this.#reloadStep('loaded');
      return;
    }
    if (this.#reloading && !this.#reloading.ready) {
      // A locked frame reloads itself: its bridge may connect after the load event.
      this.#reloading.loaded = true;
      return;
    }
    this.#reloading = null;
    this.mode = null;
    this.#sent = null;
    this.#pending = false;
    clearTimeout(this.#timer);
    this.#unverify();
    this.#o.onStatus?.({ phase: 'disconnected' });
  }

  destroy() {
    this.#dead = true;
    clearTimeout(this.#timer);
  }

  #post(msg: Record<string, unknown>) {
    this.#o.frame()?.postMessage({ h: 1, ...msg }, this.#o.locked ? '*' : this.#o.origin);
  }

  handle(e: { origin: string; source: unknown; data: unknown }) {
    const w = this.#o.frame();
    if (this.#dead || !w || e.source !== w || (!this.#o.locked && e.origin !== this.#o.origin)) return;
    const m = e.data as Record<string, unknown> | null;
    if (!m || typeof m !== 'object' || m.h !== 1 || typeof m.type !== 'string') return;
    if (m.type === 'ready') {
      this.#readySince = true;
      void this.#ready(m);
      return;
    }
    // Only a page that proved it is the previewed one (a verified `ready`) is listened to further.
    if (!this.mode) return;
    switch (m.type) {
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
      case 'layout': {
        const nodes = parseNodes(m.nodes);
        if (nodes && isNum(m.vw) && isNum(m.vh)) this.#o.onLayout?.(nodes, { w: m.vw, h: m.vh });
        break;
      }
      case 'hover':
        if (m.path === null || typeof m.path === 'string') this.#o.onHover?.(m.path);
        break;
      case 'select':
        if (m.path === null || typeof m.path === 'string') this.#o.onSelect?.(m.path, m.edit === true);
        break;
      case 'inlineEdit':
        if (typeof m.path !== 'string') break;
        if (typeof m.value === 'string') this.#o.onInlineEdit?.(m.path, m.value);
        else if (m.cancel === true) this.#o.onInlineEdit?.(m.path, null);
        break;
    }
  }

  async #ready(m: Record<string, unknown>) {
    const want = await this.#expect;
    if (this.#dead) return;
    if (!want || typeof m.preview !== 'string' || m.preview.toLowerCase() !== want) {
      this.mode = null;
      this.#sent = null;
      this.#pending = false;
      this.#reloading = null;
      clearTimeout(this.#timer);
      this.#unverify();
      this.#o.onStatus?.({ phase: 'unverified' });
      return;
    }
    this.mode = m.mode === 'reload' ? 'reload' : 'live';
    this.editCapable = m.edit === true;
    this.#o.onCapabilities?.(this.editCapable);
    this.#o.onStatus?.({ phase: this.mode });
    if (this.mode === 'live') this.#init();
    else this.#sent ??= this.#doc;
    this.#reloadStep('ready');
    // A new page has no overlay: send it again (after `init`, so the first layout shows the document).
    this.#overlaySent = '';
    const ov = this.#o.overlay?.();
    if (ov) this.#overlay = { on: ov.on, texts: ov.texts };
    this.#sendOverlay();
  }

  /** Nothing the frame reports can be trusted or placed any more. */
  #unverify() {
    this.editCapable = false;
    this.#overlaySent = '';
    this.#o.onCapabilities?.(false);
    this.#o.onLayout?.([], { w: 0, h: 0 });
  }

  #sendOverlay() {
    if (!this.mode || !this.#overlay) return;
    const key = JSON.stringify(this.#overlay);
    if (key === this.#overlaySent) return;
    this.#overlaySent = key;
    this.#post({ type: 'overlay', on: this.#overlay.on, texts: this.#overlay.texts });
  }

  /** Canvas editing on/off and the texts the frame may map; posted when verified and changed. */
  setOverlay(on: boolean, texts: TextRef[]) {
    this.#overlay = { on, texts: texts.map((t) => ({ path: t.path, text: t.text, scope: t.scope })) };
    this.#sendOverlay();
  }

  scrollTo(target: string | { by: number }) {
    if (!this.mode) return;
    this.#post(typeof target === 'string' ? { type: 'scrollTo', path: target } : { type: 'scrollTo', by: target.by });
  }

  editText(path: string, multiline: boolean) {
    if (!this.mode) return;
    // The caret goes into the frame: give it the focus (a click may have moved it to the admin meanwhile).
    this.#o.frame()?.focus?.();
    this.#post({ type: 'editText', path, multiline });
  }

  #reloadStep(what: 'loaded' | 'ready') {
    const r = this.#reloading;
    if (!r) return;
    if (what === 'loaded') r.loaded = true;
    else r.ready = true;
    if (!(r.loaded && r.ready)) return;
    this.#reloading = null;
    this.#o.onRendered?.(Math.round(performance.now() - r.t0), this.rev);
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
    if (this.#o.locked) this.#reloading = { t0: performance.now(), loaded: false, ready: false };
    this.#post({ type: 'reload', rev: this.rev });
  }
}

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

function parseNodes(v: unknown): LayoutNode[] | null {
  if (!Array.isArray(v) || v.length > 5000) return null;
  const out: LayoutNode[] = [];
  for (const n of v as Record<string, unknown>[]) {
    if (!n || typeof n !== 'object' || typeof n.path !== 'string' || n.path.length > 512) return null;
    const r = n.rect;
    if (!Array.isArray(r) || r.length !== 4 || !r.every(isNum)) return null;
    if (n.block !== undefined && typeof n.block !== 'string') return null;
    const node: LayoutNode = { path: n.path, rect: [r[0], r[1], r[2], r[3]] };
    if (n.block !== undefined) node.block = n.block;
    if (n.text === true) node.text = true;
    out.push(node);
  }
  return out;
}

/** SHA-256 of `text` as lower-case hex. */
export async function sha256Hex(text: string): Promise<string> {
  const d = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
