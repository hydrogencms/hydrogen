// The browser side of an item's collaboration session: an EventSource for what the server relays, plain POSTs
// for what this client sends (through the API client, so CSRF and error handling are the usual ones). It knows
// nothing about Yjs: updates are opaque bytes, base64 on the wire.
import { api } from '../api/client';
import { config } from '../config';

export interface Participant {
  client: string;
  principal: string;
  name: string;
  color: string;
  locale: string;
  focus?: string;
  idle?: boolean;
  edit: boolean;
}

export interface RemoteUpdate {
  seq: number;
  data: Uint8Array;
  client: string;
  init?: string[];
}

export interface JoinResult {
  client: string;
  session: string;
  /** The log's head when joining. */
  seq: number;
  updates: RemoteUpdate[];
  /** Parts this client must seed (and post with `init`). */
  init: string[];
  /** Parts someone else is seeding. */
  pending: string[];
  presence: Participant[];
  etag: string;
}

export interface SavedEvent {
  etag: string;
  client: string;
  name: string;
  /** The `X-Hydrogen-Collab-Seq` the saver sent: the highest update it had applied. */
  seq: number;
}

export interface CollabTransport {
  readonly client: string;
  join(locale: string, edit: boolean): Promise<JoinResult>;
  send(update: Uint8Array, init?: string[], keepalive?: boolean): Promise<number>;
  compact(state: Uint8Array, upto: number): Promise<void>;
  presence(p: { locale: string; focus?: string; idle?: boolean }): void;
  onUpdate(f: (u: RemoteUpdate) => void): () => void;
  onPresence(f: (clients: Participant[]) => void): () => void;
  onSaved(f: (s: SavedEvent) => void): () => void;
  onReset(f: (reason: string) => void): () => void;
  onCompact(f: (upto: number) => void): () => void;
  /** Fires after every re-join the transport did (a gap, a dropped stream, a lost client), with its answer. */
  onRejoin(f: (r: JoinResult) => void): () => void;
  /**
   * Joins again with the same client id (the one place that re-joins). The answer arrives through `onUpdate`
   * (the replayed log, idempotent) and `onRejoin`. Resolves when done; rejects if the join failed.
   */
  rejoin(): Promise<void>;
  /** Drops the whole session on the server (a client that cannot apply the log); every stream then gets `reset`. */
  reset(): Promise<void>;
  /** True once this client can only watch (it joined as a viewer, or the server refused its updates). */
  readonly readOnly: boolean;
  onReadOnly(f: () => void): () => void;
  /** Runs when the connection is about to leave (also on `pagehide`, with `pagehide` true): flush what is unsent. */
  onLeave(f: (pagehide: boolean) => void): () => void;
  /**
   * `keep` leaves the client registered on the server, for the next connection to re-join with the same id.
   * The leave call waits for updates in flight (except on `pagehide`, where everything is sent with keepalive).
   */
  leave(opts?: { keep?: boolean; pagehide?: boolean }): void;
}

/** `connecting` covers the first join and a silent re-join; `unavailable` is what the banner reports. */
export type CollabState = 'idle' | 'connecting' | 'live' | 'unavailable';

export interface CollabConnection extends CollabTransport {
  readonly state: CollabState;
  onState(f: (s: CollabState) => void): () => void;
}

export function toBase64(b: Uint8Array): string {
  let s = '';
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return btoa(s);
}

export function fromBase64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export interface EventSourceLike {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  onopen: ((e: any) => void) | null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  onerror: ((e: any) => void) | null;
  addEventListener(type: string, f: (e: { data: string }) => void): void;
  close(): void;
}

export interface Deps {
  post(op: string, body: object, keepalive?: boolean): Promise<any>;
  /** Reads a page of the log (`updates`); only needed when a join says `more`. */
  get?(op: string, query: Record<string, string | number>): Promise<any>;
  /** `since` is the seq of the join answer: the stream replays what came after it. */
  eventSource(client: string, since: number): EventSourceLike;
  setTimeout: (f: () => void, ms: number) => unknown;
  clearTimeout: (h: unknown) => void;
  setInterval: (f: () => void, ms: number) => unknown;
  clearInterval: (h: unknown) => void;
  /** pagehide registration; returns the removal. */
  onPageHide(f: () => void): () => void;
}

const HEARTBEAT = 15_000;
const GAP_GRACE = 2_000;
const RETRY = [1_000, 2_000, 5_000, 10_000, 30_000];
const SEND_TRIES = 6;
const LEAVE_WAIT = 3_000;

const errStatus = (e: unknown) => (e as { status?: number })?.status;
const errCode = (e: unknown) => (e as { code?: string })?.code;
/** A failure that may go away by itself (network, server, rate limit); anything else is an answer. */
const transient = (e: unknown) => {
  const s = errStatus(e);
  return s === undefined || s === 0 || s === 429 || s >= 500;
};

function defaults(itemId: string): Deps {
  type Loose = { post(path: string, o?: object): Promise<any>; get(path: string, o?: object): Promise<any> };
  const loose = api as unknown as Loose;
  return {
    // Background calls are quiet (no error toasts); only a join may send the user to sign in.
    post: (op, body, keepalive) => loose.post(`/collab/items/{id}/${op}`, { params: { id: itemId }, body, keepalive, quiet: op !== 'join' }),
    get: (op, query) => loose.get(`/collab/items/{id}/${op}`, { params: { id: itemId }, query, quiet: true }),
    eventSource: (client, since) =>
      new EventSource(`${config().apiPrefix}/collab/items/${encodeURIComponent(itemId)}/events?client=${encodeURIComponent(client)}&since=${since}`),
    setTimeout: (f, ms) => setTimeout(f, ms),
    clearTimeout: (h) => clearTimeout(h as number),
    setInterval: (f, ms) => setInterval(f, ms),
    clearInterval: (h) => clearInterval(h as number),
    onPageHide: (f) => {
      addEventListener('pagehide', f);
      return () => removeEventListener('pagehide', f);
    },
  };
}

type Listeners<T> = Set<(v: T) => void>;
const on = <T>(set: Listeners<T>, f: (v: T) => void) => {
  set.add(f);
  return () => void set.delete(f);
};
const emit = <T>(set: Listeners<T>, v: T) => {
  for (const f of [...set]) f(v);
};

/**
 * Opens (lazily, at `join`) the session transport for an item. `resume` is the client id of an earlier connection
 * (after a reset): the first join asks for it again, so the client id (and its header) stays the same.
 */
export function connect(itemId: string, deps: Deps = defaults(itemId), resume = ''): CollabConnection {
  const d = deps;
  let client = resume;
  let state: CollabState = 'idle';
  let locale = '';
  let edit = false;
  let viewer = false;
  let readOnly = false;
  let cur: { locale: string; focus?: string; idle?: boolean } | null = null;
  let es: EventSourceLike | null = null;
  let gen = 0;
  let stopped = false;
  let keepOnLeave = false;
  let last = 0;
  const seen = new Set<number>();
  let gapTimer: unknown;
  let retryTimer: unknown;
  let beat: unknown;
  let sending = false;
  let resend = false;
  let unhook: (() => void) | null = null;
  // The stream ended with a `reset`: nothing re-joins until the owner asks (the editor reloads the item).
  let ended = false;
  let joining: Promise<void> | null = null;
  // Consecutive stream failures without an `open`: the re-join backs off instead of spinning.
  let streamFails = 0;
  let presenceBlocked = '';
  let presenceRetried = '';
  const inflight = new Set<Promise<unknown>>();
  let wake: (() => void) | null = null;
  let compactSeen: (() => void) | null = null;

  const L = {
    update: new Set<(u: RemoteUpdate) => void>(),
    presence: new Set<(c: Participant[]) => void>(),
    saved: new Set<(s: SavedEvent) => void>(),
    reset: new Set<(r: string) => void>(),
    compact: new Set<(n: number) => void>(),
    rejoin: new Set<(r: JoinResult) => void>(),
    state: new Set<(s: CollabState) => void>(),
    readOnly: new Set<() => void>(),
    leave: new Set<(pagehide: boolean) => void>(),
  };
  // Updates that arrive before the first subscriber (the binding's chunk still loading) wait here.
  const backlog: RemoteUpdate[] = [];
  const deliver = (u: RemoteUpdate) => {
    if (L.update.size) emit(L.update, u);
    else if (backlog.length < 5_000) backlog.push(u);
  };
  const setState = (s: CollabState) => {
    if (s === state) return;
    state = s;
    emit(L.state, s);
  };
  const setReadOnly = () => {
    if (readOnly) return;
    readOnly = true;
    for (const f of [...L.readOnly]) f();
  };

  /** Waits `ms` (cut short by `leave`); resolves either way. */
  const sleep = (ms: number) =>
    new Promise<void>((res) => {
      const h = d.setTimeout(() => {
        wake = null;
        res();
      }, ms);
      wake = () => {
        d.clearTimeout(h);
        wake = null;
        res();
      };
    });

  function note(seq: number): boolean {
    if (seq <= last || seen.has(seq)) return false;
    seen.add(seq);
    while (seen.has(last + 1)) seen.delete(++last);
    if (seen.size === 0) {
      d.clearTimeout(gapTimer);
      gapTimer = undefined;
    } else if (gapTimer === undefined) {
      // A hole may be our own POST's seq still on its way back: give it a moment before re-joining.
      gapTimer = d.setTimeout(() => {
        gapTimer = undefined;
        if (seen.size > 0 && !stopped) void rejoin(0);
      }, GAP_GRACE);
    }
    return true;
  }

  function decode(u: { seq: number; data: string; client: string; init?: string[] }): RemoteUpdate {
    return { seq: u.seq, data: fromBase64(u.data), client: u.client, init: u.init?.length ? u.init : undefined };
  }

  function closeStream() {
    if (es) {
      es.onopen = es.onerror = null;
      es.close();
      es = null;
    }
  }

  function openStream(g: number, since: number) {
    const s = d.eventSource(client, since);
    es = s;
    const json = (e: { data: string }) => {
      try {
        return JSON.parse(e.data);
      } catch {
        return null;
      }
    };
    s.addEventListener('update', (e) => {
      if (g !== gen) return;
      const u = json(e);
      if (!u || typeof u.seq !== 'number') return;
      if (note(u.seq) && u.client !== client) deliver(decode(u));
    });
    s.addEventListener('presence', (e) => {
      if (g !== gen) return;
      const p = json(e);
      if (p) emit(L.presence, (p.clients ?? []) as Participant[]);
    });
    s.addEventListener('saved', (e) => {
      if (g !== gen) return;
      const v = json(e);
      if (v) emit(L.saved, { etag: v.etag, client: v.client, name: v.name ?? '', seq: typeof v.seq === 'number' ? v.seq : 0 });
    });
    s.addEventListener('reset', (e) => {
      if (g !== gen) return;
      // The server ends the stream after a reset: reconnecting would only answer 404.
      ended = true;
      closeStream();
      emit(L.reset, json(e)?.reason ?? 'external');
    });
    s.addEventListener('compact', (e) => {
      if (g !== gen) return;
      const v = json(e);
      if (v) emit(L.compact, v.upto);
      compactSeen?.();
    });
    s.onopen = () => {
      if (g !== gen) return;
      streamFails = 0;
      setState('live');
    };
    // The browser would retry by itself, but the server may have dropped this client meanwhile: join again, and
    // back off while streams keep failing before they open (a refused stream, e.g. too many streams).
    s.onerror = () => {
      if (g !== gen || stopped) return;
      closeStream();
      streamFails++;
      if (streamFails <= 1) void rejoin(0);
      else {
        d.clearTimeout(retryTimer);
        retryTimer = d.setTimeout(() => void rejoin(0), RETRY[Math.min(streamFails - 2, RETRY.length - 1)]!);
      }
    };
  }

  async function doJoin(): Promise<JoinResult> {
    const g = ++gen;
    closeStream();
    d.clearTimeout(gapTimer);
    gapTimer = undefined;
    const ask = (withClient: boolean) => d.post('join', withClient && client ? { locale, edit: edit && !viewer, client } : { locale, edit: edit && !viewer });
    let r;
    try {
      r = await ask(true);
    } catch (e) {
      if (client && errCode(e) === 'client_taken' && !stopped && g === gen) {
        // The id we ask for belongs to someone else: take a fresh one.
        client = '';
        r = await ask(false);
      } else if (errStatus(e) === 403 && edit && !viewer && !stopped && g === gen) {
        // No edit access: watch instead of giving up.
        viewer = true;
        setReadOnly();
        r = await ask(true);
      } else throw e;
    }
    if (stopped || g !== gen) {
      // leave() ran while the join was in flight: the client it created must not linger.
      if (stopped && !keepOnLeave && r?.client) d.post('leave', { client: r.client }, true).catch(() => undefined);
      throw new Error('collab: superseded');
    }
    client = r.client;
    let updates = (r.updates ?? []).map(decode);
    let seq: number = r.seq ?? 0;
    let more = !!r.more;
    while (more) {
      if (!d.get) throw new Error('collab: paged join without get');
      const p = await d.get('updates', { client, since: seq });
      if (stopped || g !== gen) throw new Error('collab: superseded');
      const page = (p.updates ?? []).map(decode);
      const next: number = p.seq ?? seq;
      if (!page.length && next <= seq) throw new Error('collab: empty page');
      updates = updates.concat(page);
      seq = Math.max(seq, next);
      more = !!p.more;
    }
    ended = false;
    last = seq;
    seen.clear();
    openStream(g, last);
    return {
      client: r.client,
      session: r.session ?? '',
      seq,
      updates,
      init: r.init ?? [],
      pending: r.pending ?? [],
      presence: r.presence ?? [],
      etag: r.etag ?? '',
    };
  }

  /** One re-join: replays the log through `onUpdate`, then reports the answer through `onRejoin`. */
  function rejoinOnce(): Promise<void> {
    joining ??= (async () => {
      setState('connecting');
      try {
        const r = await doJoin();
        for (const u of r.updates) deliver(u);
        emit(L.presence, r.presence);
        setState('live');
        emit(L.rejoin, r);
        if (cur) postPresence();
      } finally {
        joining = null;
      }
    })();
    return joining;
  }

  async function rejoin(attempt: number) {
    if (stopped) return;
    d.clearTimeout(retryTimer);
    retryTimer = undefined;
    try {
      await rejoinOnce();
    } catch (e) {
      if (stopped || (e as Error).message === 'collab: superseded') return;
      setState('unavailable');
      // An answer (no access, no such session) will not change by asking again.
      if (!transient(e) && errStatus(e) !== undefined) return;
      const ra = errStatus(e) === 429 ? ((e as { retryAfter?: number }).retryAfter ?? 0) * 1000 : 0;
      const wait = Math.max(ra, RETRY[Math.min(attempt, RETRY.length - 1)]!);
      retryTimer = d.setTimeout(() => void rejoin(attempt + 1), wait);
    }
  }

  function postPresence() {
    // Only a live session heartbeats: a resumed client that is still joining would be answered 404.
    if (!client || !cur || stopped || ended || state !== 'live') return;
    if (presenceBlocked === cur.locale) return;
    if (sending) {
      resend = true;
      return;
    }
    sending = true;
    const sent = cur;
    d.post('presence', { client, ...sent })
      .catch((e) => {
        if (stopped || ended) return;
        const st = errStatus(e);
        // The server dropped this client (idle too long, restarted): join again with the same id.
        if (st === 404) void rejoin(0);
        else if (st === 403) {
          // A locale this client may not use: join again for it once; if that is refused too, stay quiet.
          if (presenceRetried !== sent.locale) {
            presenceRetried = sent.locale;
            locale = sent.locale;
            void rejoin(0);
          } else presenceBlocked = sent.locale;
        }
      })
      .finally(() => {
        sending = false;
        if (resend) {
          resend = false;
          postPresence();
        }
      });
  }

  async function post(update: Uint8Array, init: string[] | undefined, keepalive: boolean | undefined): Promise<number> {
    const body: Record<string, unknown> = { client, data: toBase64(update) };
    if (init?.length) body.init = init;
    let last_: unknown;
    for (let attempt = 0; attempt < SEND_TRIES; attempt++) {
      try {
        const r = await d.post('updates', body, keepalive);
        note(r.seq);
        return r.seq as number;
      } catch (e) {
        last_ = e;
        const code = errCode(e);
        const st = errStatus(e);
        if (code === 'read_only') {
          setReadOnly();
          throw e;
        }
        if (st === 404 && code === 'unknown_client') {
          void rejoin(0);
          throw e;
        }
        if (stopped || keepalive) throw e;
        if (code === 'compact_required') {
          // A compact event asks an editor to post the compacted state: retry after it (or after a pause).
          await new Promise<void>((res) => {
            const h = d.setTimeout(() => ((compactSeen = null), res()), 3_000);
            compactSeen = () => {
              compactSeen = null;
              d.clearTimeout(h);
              d.setTimeout(res, 500);
            };
          });
        } else if (st === 429 && code !== 'too_many_streams' && code !== 'session_full') {
          await sleep(Math.max(1, (e as { retryAfter?: number }).retryAfter ?? 1) * 1000);
        } else throw e;
        if (stopped) throw e;
      }
    }
    throw last_;
  }

  const self: CollabConnection = {
    get client() {
      return client;
    },
    get state() {
      return state;
    },
    get readOnly() {
      return readOnly;
    },
    async join(l, e) {
      stopped = false;
      locale = l;
      edit = e;
      viewer = false;
      setState('connecting');
      for (let attempt = 0; ; attempt++) {
        try {
          const r = await doJoin();
          setState('live');
          beat ??= d.setInterval(postPresence, HEARTBEAT);
          unhook ??= d.onPageHide(() => self.leave({ pagehide: true }));
          if (cur) postPresence();
          return r;
        } catch (err) {
          if (stopped || (err as Error).message === 'collab: superseded') throw err;
          setState('unavailable');
          // Only trouble that may pass is retried (with backoff); a refusal is final.
          if (!transient(err)) throw err;
          const ra = errStatus(err) === 429 ? ((err as { retryAfter?: number }).retryAfter ?? 0) * 1000 : 0;
          await sleep(Math.max(ra, RETRY[Math.min(attempt, RETRY.length - 1)]!));
          if (stopped) throw new Error('collab: superseded');
        }
      }
    },
    send(update, init, keepalive) {
      const p = post(update, init, keepalive);
      inflight.add(p);
      const done = () => void inflight.delete(p);
      p.then(done, done);
      return p;
    },
    async compact(state0, upto) {
      try {
        await d.post('compact', { client, data: toBase64(state0), upto });
      } catch (e) {
        // Someone else was asked, or the request lapsed: nothing to do.
        if (errCode(e) !== 'compact_not_requested') throw e;
      }
    },
    async reset() {
      await d.post('reset', { client });
    },
    presence(p) {
      const next = { locale: p.locale, focus: p.focus, idle: p.idle };
      if (cur && cur.locale === next.locale && cur.focus === next.focus && cur.idle === next.idle) return;
      cur = next;
      postPresence();
    },
    onUpdate: (f) => {
      const off = on(L.update, f);
      for (const u of backlog.splice(0)) f(u);
      return off;
    },
    onPresence: (f) => on(L.presence, f),
    onSaved: (f) => on(L.saved, f),
    onReset: (f) => on(L.reset, f),
    onCompact: (f) => on(L.compact, f),
    onRejoin: (f) => on(L.rejoin, f),
    onReadOnly: (f) => on(L.readOnly as Listeners<void>, f as () => void),
    onLeave: (f) => on(L.leave, f),
    async rejoin() {
      if (stopped) return;
      d.clearTimeout(retryTimer);
      retryTimer = undefined;
      try {
        await rejoinOnce();
      } catch (e) {
        if (!stopped && (e as Error).message !== 'collab: superseded') {
          setState('unavailable');
          if (transient(e) || errStatus(e) === undefined) retryTimer = d.setTimeout(() => void rejoin(1), RETRY[0]!);
        }
        throw e;
      }
    },
    onState: (f) => on(L.state, f),
    leave(opts) {
      if (stopped) return;
      // Flush first: the owners post what they hold (with keepalive when the page is going away).
      for (const f of [...L.leave]) {
        try {
          f(!!opts?.pagehide);
        } catch {
          // Best effort.
        }
      }
      stopped = true;
      keepOnLeave = !!opts?.keep;
      gen++;
      closeStream();
      d.clearTimeout(gapTimer);
      d.clearTimeout(retryTimer);
      wake?.();
      if (beat !== undefined) d.clearInterval(beat);
      beat = undefined;
      unhook?.();
      unhook = null;
      if (client && !opts?.keep) {
        const c = client;
        const post = () => d.post('leave', { client: c }, true).catch(() => undefined);
        if (opts?.pagehide || inflight.size === 0) void post();
        else {
          // Updates in flight go first, so the leave does not outrun them (bounded: a stuck post must not hold it).
          let fired = false;
          const go = () => {
            if (fired) return;
            fired = true;
            void post();
          };
          const h = d.setTimeout(go, LEAVE_WAIT);
          void Promise.allSettled([...inflight]).then(() => (d.clearTimeout(h), go()));
        }
      }
      setState('idle');
    },
  };
  return self;
}
