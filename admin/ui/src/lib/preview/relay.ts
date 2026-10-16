// Relays the editor's unsaved working fields to the server (PUT /preview-relay/{item}) so frames and share links
// can show them without a draft save. Throttled and trailing, one request in flight, the latest value wins.

export type RelayState = 'idle' | 'sending' | 'sent' | 'invalid' | 'stopped' | 'error';

export interface RelayOptions {
  put(fields: Record<string, unknown>): Promise<unknown>;
  del(keepalive?: boolean): Promise<unknown>;
  /** Trailing throttle in ms (250). */
  delay?: number;
  onState?(s: RelayState): void;
}

const status = (e: unknown) => (e && typeof e === 'object' ? (e as { status?: number }).status : undefined);

export class Relay {
  state: RelayState = 'idle';
  #o: RelayOptions;
  #want: { json: string; fields: Record<string, unknown> } | null = null;
  /** The last value a request was made for (a successful or a rejected-as-invalid one is not sent again). */
  #attempted: string | null = null;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #inflight: Promise<void> | null = null;
  /** A DELETE under way: a later PUT waits for it, or the delete could land last and drop the newer value. */
  #deleting: Promise<void> | null = null;
  #ok = true;
  #stopped = false;
  #sentOnce = false;
  #waiters: ((ok: boolean) => void)[] = [];

  constructor(o: RelayOptions) {
    this.#o = o;
  }

  get stopped() {
    return this.#stopped;
  }

  #set(s: RelayState) {
    if (this.state === s) return;
    this.state = s;
    this.#o.onState?.(s);
  }

  #settle(ok: boolean) {
    const w = this.#waiters;
    this.#waiters = [];
    for (const f of w) f(ok);
  }

  /** The working fields changed: send them after the throttle delay (the latest value wins). */
  push(fields: Record<string, unknown>, immediate = false) {
    if (this.#stopped) return;
    const json = JSON.stringify(fields);
    if (json === this.#attempted && this.#ok) {
      // Back to what the server already holds.
      this.#want = null;
      if (!this.#inflight) {
        clearTimeout(this.#timer);
        this.#timer = undefined;
        this.#settle(true);
      }
      return;
    }
    if (json === this.#attempted && !this.#want) return;
    this.#want = { json, fields };
    if (this.#inflight) return;
    if (immediate) {
      clearTimeout(this.#timer);
      this.#run();
    } else if (!this.#timer) this.#timer = setTimeout(() => this.#run(), this.#o.delay ?? 250);
  }

  #run() {
    this.#timer = undefined;
    const w = this.#want;
    if (!w || this.#stopped) return this.#settle(this.#ok);
    this.#want = null;
    this.#attempted = w.json;
    this.#set('sending');
    this.#inflight = (async () => {
      try {
        await this.#deleting;
        await this.#o.put(w.fields);
        this.#sentOnce = true;
        this.#ok = true;
        this.#set('sent');
      } catch (e) {
        this.#ok = false;
        const st = status(e);
        if (st === 403) {
          this.#stopped = true;
          this.#set('stopped');
        } else if (st === 422) {
          this.#set('invalid');
        } else {
          // Network or server trouble: the same value may be sent again.
          this.#attempted = null;
          this.#set('error');
        }
      }
    })().finally(() => {
      this.#inflight = null;
      if (this.#stopped) {
        this.#want = null;
        this.#settle(false);
      } else if (this.#want) this.#timer = setTimeout(() => this.#run(), this.#o.delay ?? 250);
      else this.#settle(this.#ok);
    });
  }

  /** Resolves true when the server holds the latest pushed value (immediately when nothing is pending). */
  flush(): Promise<boolean> {
    if (this.#stopped) return Promise.resolve(false);
    if (!this.#want && !this.#inflight && !this.#timer) return Promise.resolve(this.#ok);
    return new Promise((r) => this.#waiters.push(r));
  }

  /**
   * Drops the relayed draft: cancels what is pending and, when something was sent, DELETEs it (viewers fall back
   * to the saved draft). `keepalive` is for `pagehide`: nothing is awaited.
   */
  async clear(keepalive = false): Promise<void> {
    clearTimeout(this.#timer);
    this.#timer = undefined;
    this.#want = null;
    if (!keepalive) await this.#inflight;
    this.#settle(this.#ok);
    const sent = this.#sentOnce || (keepalive && !!this.#inflight);
    this.#attempted = null;
    this.#ok = true;
    if (this.#stopped || !sent) return;
    this.#sentOnce = false;
    this.#set('idle');
    const d: Promise<void> = this.#o.del(keepalive).then(
      () => undefined,
      () => undefined, // nothing to do: the relayed draft expires on its own
    );
    this.#deleting = d;
    await d;
    if (this.#deleting === d) this.#deleting = null;
  }
}
