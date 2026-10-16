import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { connect as rawConnect, fromBase64, toBase64, type Deps, type EventSourceLike } from './transport';

class FakeES implements EventSourceLike {
  onopen: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  closed = false;
  #l = new Map<string, ((e: { data: string }) => void)[]>();
  constructor(
    public client: string,
    public since: number,
  ) {}
  addEventListener(t: string, f: (e: { data: string }) => void) {
    this.#l.set(t, [...(this.#l.get(t) ?? []), f]);
  }
  close() {
    this.closed = true;
  }
  emit(t: string, data: unknown) {
    for (const f of this.#l.get(t) ?? []) f({ data: JSON.stringify(data) });
  }
}

const b64 = (...n: number[]) => toBase64(new Uint8Array(n));

const open: { leave(): void }[] = [];
const connect = (id: string, d: Deps) => {
  const c = rawConnect(id, d);
  open.push(c);
  return c;
};
afterEach(() => {
  for (const c of open.splice(0)) c.leave();
  vi.useRealTimers();
});

let posts: { op: string; body: any; keepalive?: boolean }[];
let sources: FakeES[];
let joinN: number;
let joinSeq: number;
let failJoin: boolean;
let joinError: unknown;
let hidden: (() => void) | undefined;

function deps(): Deps {
  return {
    post: async (op, body, keepalive) => {
      posts.push({ op, body, keepalive });
      if (op === 'join') {
        if (joinError) {
          const e = joinError;
          joinError = undefined;
          throw e;
        }
        if (failJoin) throw new Error('boom');
        joinN++;
        return { client: (body as { client?: string }).client ?? 'c' + joinN, session: 's', seq: joinSeq, updates: [{ seq: joinSeq, data: b64(9), client: 'z' }], init: ['shared'], pending: [], presence: [], etag: 'e1' };
      }
      if (op === 'updates') return { seq: 100 };
      return {};
    },
    eventSource: (c, since) => {
      const s = new FakeES(c, since);
      sources.push(s);
      return s;
    },
    setTimeout: (f, ms) => setTimeout(f, ms),
    clearTimeout: (h) => clearTimeout(h as never),
    setInterval: (f, ms) => setInterval(f, ms),
    clearInterval: (h) => clearInterval(h as never),
    onPageHide: (f) => {
      hidden = f;
      return () => (hidden = undefined);
    },
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  posts = [];
  sources = [];
  joinN = 0;
  joinSeq = 5;
  failJoin = false;
  joinError = undefined;
  hidden = undefined;
});

describe('base64', () => {
  it('round-trips bytes, including large arrays', () => {
    const big = new Uint8Array(100_000).map((_, i) => i % 256);
    expect(fromBase64(toBase64(big))).toEqual(big);
    expect(toBase64(new Uint8Array([1, 2, 3]))).toBe('AQID');
  });
});

describe('connect', () => {
  it('joins, decodes the log and opens the stream for the returned client', async () => {
    const tr = connect('item1', deps());
    const r = await tr.join('en', true);
    expect(posts[0]).toMatchObject({ op: 'join', body: { locale: 'en', edit: true } });
    expect(r.client).toBe('c1');
    expect(tr.client).toBe('c1');
    expect(r.updates[0]!.data).toEqual(new Uint8Array([9]));
    expect(r.init).toEqual(['shared']);
    expect(sources[0]!.client).toBe('c1');
    expect(sources[0]!.since).toBe(5); // the join's seq
    expect(tr.state).toBe('live');
  });

  it('reports unavailable when the join fails', async () => {
    failJoin = true;
    const tr = connect('item1', deps());
    vi.useFakeTimers();
    const j = tr.join('en', true);
    await vi.advanceTimersByTimeAsync(10);
    expect(tr.state).toBe('unavailable');
    // The first join keeps trying, with backoff, until it works.
    failJoin = false;
    await vi.advanceTimersByTimeAsync(1_100);
    await expect(j).resolves.toMatchObject({ client: 'c1' });
    expect(tr.state).toBe('live');
  });

  it('delivers other clients\' updates once, in any order, and not its own', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const got: number[] = [];
    tr.onUpdate((u) => got.push(u.seq));
    const s = sources[0]!;
    s.emit('update', { seq: 7, data: b64(1), client: 'x' });
    s.emit('update', { seq: 6, data: b64(2), client: 'x' });
    s.emit('update', { seq: 6, data: b64(2), client: 'x' });
    s.emit('update', { seq: 8, data: b64(3), client: 'c1' });
    expect(got).toEqual([7, 6]);
    vi.advanceTimersByTime(5000);
    expect(joinN).toBe(1); // no hole: nothing re-joined
  });

  it('send posts base64 with init and counts the returned seq as seen', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    expect(await tr.send(new Uint8Array([4, 5]), ['shared'])).toBe(100);
    expect(posts.at(-1)).toMatchObject({ op: 'updates', body: { client: 'c1', data: b64(4, 5), init: ['shared'] } });
  });

  it('re-joins after a gap that does not close, replaying the log', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const got: number[] = [];
    tr.onUpdate((u) => got.push(u.seq));
    sources[0]!.emit('update', { seq: 9, data: b64(1), client: 'x' }); // 6..8 missing
    await vi.advanceTimersByTimeAsync(2100);
    expect(joinN).toBe(2);
    expect(tr.client).toBe('c1'); // same client id
    expect(sources[0]!.closed).toBe(true);
    expect(sources).toHaveLength(2);
    expect(got).toEqual([9, 5]); // the live one, then the replayed log
  });

  it('re-joins when the stream errors, and retries while unavailable', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const states: string[] = [];
    tr.onState((s) => states.push(s));
    failJoin = true;
    sources[0]!.onerror?.({});
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.state).toBe('unavailable');
    failJoin = false;
    await vi.advanceTimersByTimeAsync(1100);
    expect(tr.state).toBe('live');
    expect(states).toEqual(['connecting', 'unavailable', 'connecting', 'live']);
  });

  it('heartbeats every 15 s and posts focus changes at once (deduplicated)', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    tr.presence({ locale: 'en', focus: 'title' });
    tr.presence({ locale: 'en', focus: 'title' });
    await vi.advanceTimersByTimeAsync(0);
    const pres = () => posts.filter((p) => p.op === 'presence');
    expect(pres()).toHaveLength(1);
    expect(pres()[0]!.body).toEqual({ client: 'c1', locale: 'en', focus: 'title', idle: undefined });
    await vi.advanceTimersByTimeAsync(15_000);
    expect(pres()).toHaveLength(2);
  });

  it('dispatches saved, reset, compact and presence events', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const seen: unknown[] = [];
    tr.onSaved((s) => seen.push(['saved', s]));
    tr.onReset((r) => seen.push(['reset', r]));
    tr.onCompact((n) => seen.push(['compact', n]));
    tr.onPresence((c) => seen.push(['presence', c.length]));
    const s = sources[0]!;
    s.emit('saved', { etag: 'e2', client: 'x', name: 'Alex', seq: 0 });
    s.emit('reset', { reason: 'external' });
    s.emit('compact', { upto: 12 });
    s.emit('presence', { clients: [{}, {}] });
    expect(seen).toEqual([['saved', { etag: 'e2', client: 'x', name: 'Alex', seq: 0 }], ['reset', 'external'], ['compact', 12], ['presence', 2]]);
  });

  it('leave posts with keepalive once, stops the stream and also runs on pagehide', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    hidden!();
    tr.leave();
    const leaves = posts.filter((p) => p.op === 'leave');
    expect(leaves).toHaveLength(1);
    expect(leaves[0]).toMatchObject({ body: { client: 'c1' }, keepalive: true });
    expect(sources[0]!.closed).toBe(true);
    expect(tr.state).toBe('idle');
  });

  it('re-joins with the same client id after the stream fails (404 on reconnect)', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const rejoined: string[][] = [];
    tr.onRejoin((r) => rejoined.push(r.init));
    sources[0]!.onerror?.({});
    await vi.advanceTimersByTimeAsync(0);
    const joins = posts.filter((p) => p.op === 'join');
    expect(joins[0]!.body).toEqual({ locale: 'en', edit: true });
    expect(joins[1]!.body).toEqual({ locale: 'en', edit: true, client: 'c1' });
    expect(tr.client).toBe('c1');
    expect(sources).toHaveLength(2);
    expect(sources[1]!.client).toBe('c1');
    expect(rejoined).toEqual([['shared']]);
  });

  it('after a reset the stream is closed, nothing re-joins, and presence posts stop', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const resets: string[] = [];
    tr.onReset((r) => resets.push(r));
    tr.presence({ locale: 'en' });
    await vi.advanceTimersByTimeAsync(0);
    const n = posts.length;
    sources[0]!.emit('reset', { reason: 'published' });
    expect(resets).toEqual(['published']);
    expect(sources[0]!.closed).toBe(true);
    sources[0]!.onerror?.({}); // a stale error must not re-join
    await vi.advanceTimersByTimeAsync(30_000);
    expect(posts.length).toBe(n);
    expect(joinN).toBe(1);
  });

  it('a presence 404 (client dropped) re-joins with the same id', async () => {
    const d = deps();
    const base = d.post;
    let drop = true;
    d.post = async (op, body, keep) => {
      if (op === 'presence' && drop) {
        drop = false;
        posts.push({ op, body });
        throw Object.assign(new Error('unknown'), { status: 404, code: 'unknown_client' });
      }
      return base(op, body, keep);
    };
    const tr = connect('i', d);
    await tr.join('en', true);
    tr.presence({ locale: 'en', focus: 'title' });
    await vi.advanceTimersByTimeAsync(0);
    expect(joinN).toBe(2);
    expect(posts.filter((p) => p.op === 'join')[1]!.body.client).toBe('c1');
    // the presence is announced again after the re-join
    expect(posts.filter((p) => p.op === 'presence')).toHaveLength(2);
  });

  it('asks for a fresh id when the one it re-joins with is taken', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    joinError = Object.assign(new Error('taken'), { status: 409, code: 'client_taken' });
    await tr.rejoin();
    const joins = posts.filter((p) => p.op === 'join');
    expect(joins[1]!.body.client).toBe('c1');
    expect(joins[2]!.body).toEqual({ locale: 'en', edit: true });
    expect(tr.client).toBe('c2');
  });

  it('rejoin() is shared with an automatic one in flight and reports the answer through onRejoin', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const got: number[] = [];
    tr.onRejoin((r) => got.push(r.seq));
    await Promise.all([tr.rejoin(), tr.rejoin()]);
    expect(joinN).toBe(2);
    expect(got).toEqual([5]);
  });

  it('resume asks for the earlier client id on the first join; leave({keep}) does not post leave', async () => {
    const tr = rawConnect('i', deps(), 'old-client-1');
    await tr.join('en', true);
    expect(posts[0]!.body).toEqual({ locale: 'en', edit: true, client: 'old-client-1' });
    expect(tr.client).toBe('old-client-1');
    tr.leave({ keep: true });
    expect(posts.filter((p) => p.op === 'leave')).toHaveLength(0);
    expect(sources[0]!.closed).toBe(true);
  });
});
