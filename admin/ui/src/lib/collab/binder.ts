// The Yjs side of a collaborative form, free of Svelte so tests can drive it with plain objects and manual
// scheduling. `bind.svelte.ts` wraps it around a Form.
//
// Direction rules: a local change is detected by comparing the form with Y (`syncPart`, idempotent) when the
// scheduled tick runs; a remote update first flushes pending local changes, is applied with origin REMOTE and
// is then projected into the form (`projectPart`, only the fields that changed). Neither direction writes when
// the two already agree, so there are no echo loops.
import * as Y from 'yjs';
import { hydrate } from '../fields/logic';
import type { CompiledField, CompiledType } from '../types.svelte';
import type { CollabTransport, JoinResult, RemoteUpdate } from './transport';
import { dedupeBlocks, jsonEq, localePart, mergeLocal, partFields, partOf, plainOf, projectPart, seedPart, SHARED, syncPart, yAtPath, type Doc, type Env, type Hooks } from './ydoc';

export interface BinderOptions {
  /** `rejoin`, `onRejoin` and `onPresence` are optional so a minimal transport works; without them pending parts wait. */
  transport: Pick<CollabTransport, 'client' | 'send' | 'compact' | 'onUpdate' | 'onCompact'> &
    Partial<Pick<CollabTransport, 'rejoin' | 'onRejoin' | 'onPresence' | 'onReadOnly' | 'onLeave' | 'reset'>>;
  join: JoinResult;
  type: Pick<CompiledType, 'fields'>;
  locale: string;
  /** The loaded item (with `.fields`); parts are seeded from it. */
  draft: unknown;
  /** The live values object of the type's fields (`form.values.fields`); read again on every use. */
  fields(): Doc;
  /** Plain copy of a possibly reactive value. */
  snapshot?(v: unknown): unknown;
  env?: Env;
  hooks?: Hooks;
  /** Runs `fn` soon (an animation frame); batches local edits. */
  schedule?(fn: () => void): void;
  /**
   * Parts whose form values were edited against the draft and must be merged into the session (default: all). A
   * restart after a lost claim passes only the lost parts: in the others the form already shows what others wrote.
   */
  merge?: string[];
  /**
   * A restart after a lost claim: for `parts` the form's edits are merged against `base` (what the server held,
   * as `ackedValues()` of the previous binder reported) instead of the draft, so edits that were never sent
   * survive and what others wrote is not applied twice.
   */
  resume?: { parts: string[]; base: Doc };
  /** Awaited before the first update is applied (e.g. the block catalog). */
  before?: Promise<unknown>;
  /** Called when unsynced/pending/ready state changed. */
  onState?(): void;
  /** Called after remote changes were written into the form. */
  afterProject?(): void;
  /**
   * The session cannot go on as it is and needs a fresh start: `claim_lost` (a part this client seeded went to
   * someone else: join again from the draft), `poison` (an update could not be applied; the server was asked to
   * reset the session) or `reset-failed`.
   */
  onFatal?(reason: 'claim_lost' | 'poison' | 'reset-failed'): void;
  setTimeout?: (f: () => void, ms: number) => unknown;
  clearTimeout?: (h: unknown) => void;
}

const REMOTE = { origin: 'remote' };
const LOCAL = { origin: 'local' };
const SEED = { origin: 'seed' };
const DEDUPE = { origin: 'dedupe' };

export class Binder {
  readonly doc = new Y.Doc();
  readonly parts: string[];
  readonly started: Promise<void>;
  #o: BinderOptions;
  #env: Env;
  #ready = new Set<string>();
  #pending = new Set<string>();
  #dirty = new Map<string, Set<string>>();
  #queue: RemoteUpdate[] = [];
  #begun = false;
  #destroyed = false;
  #scheduled = false;
  #outbox: Uint8Array | null = null;
  #inflight = 0;
  #retry: unknown;
  #pendingTimer: unknown;
  #rejoining = false;
  #cooling = false;
  #applied: number;
  #seen = new Set<number>();
  #flushing = false;
  #offs: (() => void)[] = [];
  #ro: boolean;
  #failed = '';
  #retries = 0;
  #poisoned = false;
  // What the server holds as far as this client knows: everything received plus its own acknowledged updates.
  #server = new Y.Doc();
  // Parts seeded here whose seed the server has not acknowledged yet: someone else's seed for one means the claim is lost.
  #seeding = new Set<string>();

  constructor(o: BinderOptions) {
    this.#o = o;
    this.#env = o.env ?? {};
    this.parts = [SHARED, localePart(o.locale)];
    const me = o.join.presence?.find((p) => p.client === o.join.client);
    this.#ro = !!me && !me.edit;
    this.#applied = o.join.seq ?? 0;
    for (const p of this.parts) {
      const m = this.doc.getMap(p);
      m.observeDeep((events, tr) => {
        if (tr.origin !== REMOTE && tr.origin !== DEDUPE) return;
        const set = this.#dirty.get(p) ?? new Set<string>();
        for (const e of events) {
          let t: Y.AbstractType<unknown> = e.target as Y.AbstractType<unknown>;
          if (t === m) {
            for (const k of (e as Y.YMapEvent<unknown>).keysChanged) set.add(k);
            continue;
          }
          // Climb to the child of the part map (event.path is slow and unsafe for deleted types).
          while (t._item && t._item.parent !== m) t = t._item.parent as Y.AbstractType<unknown>;
          if (t._item?.parentSub) set.add(t._item.parentSub);
        }
        this.#dirty.set(p, set);
      });
    }
    this.doc.on('update', (u: Uint8Array, origin: unknown) => {
      if (origin === REMOTE || origin === SEED || this.#ro) return;
      const wasFlushing = this.#flushing;
      this.#outbox = this.#outbox ? Y.mergeUpdates([this.#outbox, u]) : u;
      if (!wasFlushing) this.#tick();
    });
    this.#offs.push(o.transport.onUpdate((u) => (this.#begun ? this.receive(u) : this.#queue.push(u))));
    this.#offs.push(o.transport.onCompact((upto) => this.#compact(upto)));
    if (o.transport.onReadOnly) this.#offs.push(o.transport.onReadOnly(() => this.#becameReadOnly()));
    // Leaving (or the page going away): the last edits go out first.
    if (o.transport.onLeave) this.#offs.push(o.transport.onLeave((pagehide) => this.#final(pagehide)));
    // The transport owns re-joining; a re-join's answer may hand this client parts it must now seed.
    if (o.transport.onRejoin) this.#offs.push(o.transport.onRejoin((r) => this.#rejoined(r)));
    // Nothing pushes "claim released": any presence change while parts are pending is a reason to look again.
    if (o.transport.onPresence) this.#offs.push(o.transport.onPresence(() => this.#askRejoin()));
    this.started = this.#start();
  }

  // ---- state

  get readOnly() {
    return this.#ro;
  }
  /** A non-empty reason once local changes can no longer be sent (the banner shows it); such edits are unsynced. */
  failed(): string {
    return this.#failed;
  }
  /** The field values of the ready parts as the server holds them (see `resume`). */
  ackedValues(): { parts: string[]; base: Doc } {
    const base: Doc = {};
    const parts = this.parts.filter((p) => this.#ready.has(p) && !this.#seeding.has(p));
    for (const p of parts) {
      const m = this.#server.getMap(p);
      for (const d of this.#defs(p)) {
        const v = m.get(d.name);
        if (v !== undefined) base[d.name] = plainOf(v, d, this.#env);
      }
    }
    return { parts, base };
  }
  /** Parts this client was seeding when its claim was lost. */
  lostParts(): string[] {
    return [...this.#seeding];
  }
  /** The highest seq applied by this client, its own acknowledged updates included (the `X-Hydrogen-Collab-Seq`). */
  applied(): number {
    return this.#applied;
  }
  unsynced(): boolean {
    return this.#scheduled || this.#outbox !== null || this.#inflight > 0 || (!!this.#failed && !this.#ro);
  }

  #becameReadOnly() {
    if (this.#ro) return;
    this.#ro = true;
    this.#outbox = null;
    this.#failed = '';
    this.#changed();
  }

  #final(pagehide: boolean) {
    if (this.#destroyed || this.#ro) return;
    try {
      this.#flushForm();
    } catch {
      // Best effort.
    }
    const data = this.#outbox;
    this.#outbox = null;
    if (data && !this.#failed) this.#post(data, undefined, pagehide);
  }
  pending(): string[] {
    return [...this.#pending];
  }
  isReady(part: string) {
    return this.#ready.has(part);
  }

  fragment(path: string): Y.XmlFragment | undefined {
    const name = /^[^.[]+/.exec(path)?.[0];
    const def = this.#o.type.fields.find((f) => f.name === name);
    if (!def) return undefined;
    const part = partOf(def, this.#o.locale);
    if (!this.#ready.has(part)) return undefined;
    const y = yAtPath(this.doc.getMap(part), path);
    return y instanceof Y.XmlFragment ? y : undefined;
  }

  #defs(part: string): CompiledField[] {
    return partFields(this.#o.type.fields, this.#o.locale, part);
  }
  #snap(): Doc {
    const f = this.#o.fields();
    return (this.#o.snapshot ? this.#o.snapshot(f) : f) as Doc;
  }
  #changed() {
    this.#o.onState?.();
  }

  // ---- local changes

  /** The form changed: sync it into Y on the next tick. */
  touch() {
    if (!this.#destroyed) this.#tick();
  }

  #tick() {
    if (this.#scheduled || this.#destroyed) return;
    this.#scheduled = true;
    const run = () => {
      this.#scheduled = false;
      this.flush();
    };
    (this.#o.schedule ?? ((f) => queueMicrotask(f)))(run);
    this.#changed();
  }

  /** Syncs the form into Y and sends what is outstanding. */
  flush() {
    if (this.#destroyed) return;
    this.#scheduled = false;
    this.#flushing = true;
    try {
      this.#flushForm();
    } finally {
      this.#flushing = false;
    }
    this.#send();
    this.#changed();
  }

  #flushForm() {
    if (this.#ro || this.#poisoned || !this.#begun) return;
    const values = this.#snap();
    this.doc.transact(() => {
      for (const p of this.parts) if (this.#ready.has(p)) syncPart(this.doc.getMap(p), this.#defs(p), values, this.#env);
    }, LOCAL);
  }

  #send() {
    const data = this.#outbox;
    if (!data || this.#ro || this.#failed) return;
    this.#outbox = null;
    this.#post(data);
  }

  /** Sends `data` (a seed carries its `init` parts, which a retry keeps). */
  #post(data: Uint8Array, init?: string[], keepalive?: boolean) {
    this.#inflight++;
    this.#o.transport.send(data, init, keepalive).then(
      (seq) => {
        this.#inflight--;
        this.#retries = 0;
        for (const p of init ?? []) this.#seeding.delete(p);
        try {
          Y.applyUpdate(this.#server, data);
        } catch {
          // The mirror is only for restarts.
        }
        if (typeof seq === 'number') this.#advance(seq);
        this.#changed();
      },
      (e) => {
        this.#inflight--;
        this.#failedPost(e, data, init, keepalive);
        this.#changed();
      },
    );
  }

  #failedPost(e: unknown, data: Uint8Array, init: string[] | undefined, keepalive: boolean | undefined) {
    if (this.#destroyed) return;
    const code = (e as { code?: string })?.code;
    const st = (e as { status?: number })?.status;
    if (code === 'read_only') return void this.#becameReadOnly();
    if (code === 'claim_lost') {
      this.#poisoned = true;
      return void this.#o.onFatal?.('claim_lost');
    }
    const transient = st === undefined || st === 0 || st >= 500 || st === 429 || (st === 404 && code === 'unknown_client') || code === 'compact_required';
    if (!transient) {
      // A refusal retrying cannot change (too large, malformed): stop, say so and keep the edits unsynced.
      this.#failed = code || 'rejected';
      return;
    }
    if (keepalive) return;
    if (init?.length) this.#seedRetry(data, init);
    else this.#outbox = this.#outbox ? Y.mergeUpdates([data, this.#outbox]) : data;
    const st0 = this.#o.setTimeout ?? ((f, ms) => setTimeout(f, ms));
    const wait = Math.min(2000 * 2 ** this.#retries++, 15_000);
    this.#retry ??= st0(() => {
      this.#retry = undefined;
      for (const [parts, d] of this.#seedQueue.splice(0)) this.#post(d, parts);
      this.#send();
      this.#changed();
    }, wait);
  }
  #seedQueue: [string[], Uint8Array][] = [];
  #seedRetry(data: Uint8Array, init: string[]) {
    this.#seedQueue.push([init, data]);
  }

  #compact(upto: number) {
    if (this.#ro || !this.#begun || this.#applied < upto) return;
    void this.#o.transport.compact(Y.encodeStateAsUpdate(this.doc), upto).catch(() => undefined);
  }

  /** `applied` is the highest seq such that everything up to it was received or sent by this client. */
  #advance(seq: number) {
    if (seq <= this.#applied) return;
    this.#seen.add(seq);
    while (this.#seen.delete(this.#applied + 1)) this.#applied++;
  }

  // ---- remote changes

  receive(u: RemoteUpdate) {
    if (this.#destroyed) return;
    if (u.client === this.#o.transport.client && this.#o.transport.client !== '') {
      this.#advance(u.seq);
      return;
    }
    if (this.#poisoned) return;
    if (u.init?.some((p) => this.#seeding.has(p))) {
      // Another client seeded a part this client was still trying to seed: its claim is gone.
      this.#poisoned = true;
      this.#o.onFatal?.('claim_lost');
      return;
    }
    this.#flushForm();
    if (!this.#apply(u.data)) return;
    this.#advance(u.seq);
    for (const p of u.init ?? []) {
      if (!this.#pending.delete(p)) continue;
      this.#adopt(p);
    }
    this.#finish();
    this.#send();
    this.#changed();
  }

  /** Applies an update; one that cannot be applied poisons the session: ask the server to drop it and start over. */
  #apply(data: Uint8Array): boolean {
    try {
      Y.applyUpdate(this.doc, data, REMOTE);
      Y.applyUpdate(this.#server, data);
      return true;
    } catch {
      this.#poisoned = true;
      const fatal = () => this.#o.onFatal?.('reset-failed');
      const tr = this.#o.transport;
      // The server answers with `reset` on every stream (ours too), which reloads the editor.
      if (tr.reset) tr.reset().then(() => this.#o.onFatal?.('poison'), fatal);
      else fatal();
      this.#changed();
      return false;
    }
  }

  /**
   * A pending part's first state arrived: the part becomes ready. Edits the user made to its fields meanwhile
   * (against the loaded draft) are applied on top of the seed instead of being overwritten by it.
   */
  #adopt(p: string) {
    this.#ready.add(p);
    const defs = this.#defs(p);
    const base = this.#base();
    const cur = this.#snap();
    const edited = this.#ro ? new Set<string>() : new Set(defs.filter((d) => !jsonEq(cur[d.name], base[d.name])).map((d) => d.name));
    if (edited.size) {
      const map = this.doc.getMap(p);
      this.doc.transact(() => mergeLocal(map, defs, cur, base, this.#env, edited), LOCAL);
    }
    // The merged state (seed, then the local edits on top) is what the form shows from here on.
    this.#dirty.set(p, new Set(defs.map((d) => d.name)));
  }

  #base(): Doc {
    const fields = (this.#o.draft as { fields?: Doc } | null)?.fields;
    const out = hydrate(this.#o.type.fields, fields) as Doc;
    // An inheriting locale: the groups hydrate adds are not overrides.
    if (this.#env.sparse) for (const f of this.#o.type.fields) if (f.translatable && !(fields && f.name in fields)) delete out[f.name];
    return out;
  }

  #finish() {
    let touched = false;
    for (const p of this.parts) {
      if (!this.#ready.has(p)) continue;
      const names = this.#dirty.get(p);
      if (!names?.size) continue;
      const defs = this.#defs(p).filter((d) => names.has(d.name));
      const map = this.doc.getMap(p);
      this.#dedupe(map, defs);
      const all = this.#dirty.get(p)!;
      this.#dirty.delete(p);
      projectPart(map, this.#defs(p), this.#o.fields(), this.#env, this.#o.hooks ?? {}, all);
      touched = true;
    }
    if (touched) this.#o.afterProject?.();
  }

  #dedupe(map: Y.Map<unknown>, defs: CompiledField[]) {
    this.doc.transact(() => {
      for (const d of defs) {
        const y = map.get(d.name);
        if (d.shape.base === 'blocks' && y instanceof Y.Array) dedupeBlocks(y as Y.Array<Y.Map<unknown>>);
      }
    }, DEDUPE);
  }

  // ---- start

  async #start() {
    await this.#o.before?.catch(() => undefined);
    if (this.#destroyed) return;
    this.#env = this.#o.env ?? this.#env;
    const j = this.#o.join;
    for (const u of [...j.updates].sort((a, b) => a.seq - b.seq)) {
      if (!this.#apply(u.data)) return;
      this.#advance(u.seq);
    }
    const init = j.init.filter((p) => this.parts.includes(p));
    const base = this.#base();
    if (init.length && !this.#ro) this.#seed(init, base);
    for (const p of j.pending ?? []) if (this.parts.includes(p) && !init.includes(p)) this.#pending.add(p);
    for (const p of this.parts) if (!this.#pending.has(p)) this.#ready.add(p);
    this.#dirty.clear();

    // Fields edited before the session was joined stay (as a delta against the draft they were loaded from, never
    // over what others changed meanwhile); the others take the merged state.
    const cur = this.#snap();
    this.#begun = true;
    const env = this.#env;
    for (const p of this.parts) {
      if (!this.#ready.has(p)) continue;
      const defs = this.#defs(p);
      const mergeHere = !this.#o.merge || this.#o.merge.includes(p) || !!this.#o.resume?.parts.includes(p);
      const from = this.#o.resume?.parts.includes(p) ? this.#o.resume.base : base;
      const edited = new Set(defs.filter((d) => mergeHere && !this.#ro && !jsonEq(cur[d.name], from[d.name])).map((d) => d.name));
      const map = this.doc.getMap(p);
      this.#dedupe(map, defs);
      if (edited.size && !this.#ro) this.doc.transact(() => mergeLocal(map, defs, cur, from, env, edited), LOCAL);
      projectPart(map, defs, this.#o.fields(), env, {});
    }
    this.#dirty.clear();
    this.#o.afterProject?.();
    const queued = this.#queue.splice(0);
    for (const u of queued) this.receive(u);
    this.#send();
    this.#watchPending();
    this.#changed();
  }

  #seed(parts: string[], base: Doc) {
    let data: Uint8Array | undefined;
    const h = (u: Uint8Array, origin: unknown) => {
      if (origin === SEED) data = u;
    };
    this.doc.on('update', h);
    this.doc.transact(() => {
      for (const p of parts) seedPart(this.doc.getMap(p), this.#defs(p), base, this.#env);
    }, SEED);
    this.doc.off('update', h);
    for (const p of parts) this.#seeding.add(p);
    this.#post(data ?? Y.encodeStateAsUpdate(this.doc), parts);
  }

  #watchPending() {
    if (!this.#pending.size || this.#destroyed || this.#pendingTimer !== undefined) return;
    const st = this.#o.setTimeout ?? ((f, ms) => setTimeout(f, ms));
    this.#pendingTimer = st(() => {
      this.#pendingTimer = undefined;
      // The seeder never delivered: re-join (same client id), which claims what is still unseeded.
      this.#askRejoin(true);
    }, 16_000);
  }

  /** Asks the transport to re-join while parts are pending (rate limited: a re-join itself causes presence events). */
  #askRejoin(force = false) {
    const tr = this.#o.transport;
    if (!this.#pending.size || this.#destroyed || !tr.rejoin) return;
    if (this.#rejoining || (!force && this.#cooling)) return;
    this.#rejoining = true;
    this.#cooling = true;
    const st = this.#o.setTimeout ?? ((f, ms) => setTimeout(f, ms));
    st(() => (this.#cooling = false), 3_000);
    tr.rejoin().then(
      () => {
        this.#rejoining = false;
        this.#watchPending();
      },
      () => {
        this.#rejoining = false;
        this.#watchPending();
      },
    );
  }

  /** A re-join's answer: the replayed log arrived through `receive`; here are the parts it hands to us. */
  #rejoined(r: JoinResult) {
    if (this.#destroyed || !this.#begun) return;
    const mine = r.init.filter((p) => this.#pending.has(p));
    if (!mine.length) return;
    if (this.#ro) return;
    const base = this.#base();
    const cur = this.#snap();
    this.#seed(mine, base);
    for (const p of mine) {
      this.#pending.delete(p);
      this.#ready.add(p);
      const defs = this.#defs(p);
      const map = this.doc.getMap(p);
      this.#dedupe(map, defs);
      // Edits that slipped in while the part was pending go on top of the seed.
      const edited = new Set(defs.filter((d) => !jsonEq(cur[d.name], base[d.name])).map((d) => d.name));
      if (edited.size) this.doc.transact(() => mergeLocal(map, defs, cur, base, this.#env, edited), LOCAL);
      projectPart(map, defs, this.#o.fields(), this.#env, {});
    }
    this.#send();
    this.#changed();
  }

  /** `discard`: drop what is unsent (the session is being restarted from the draft; the outbox no longer fits it). */
  destroy(opts: { discard?: boolean } = {}) {
    if (this.#destroyed) return;
    try {
      if (!opts.discard) {
        this.#flushForm();
        this.#send();
      }
    } catch {
      // Best effort: the page is going away.
    }
    this.#destroyed = true;
    for (const f of this.#offs.splice(0)) f();
    const ct = this.#o.clearTimeout ?? ((h) => clearTimeout(h as number));
    ct(this.#retry);
    ct(this.#pendingTimer);
    this.#changed();
  }
}
