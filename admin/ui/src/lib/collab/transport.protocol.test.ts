import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { connect, toBase64, type Deps, type EventSourceLike } from './transport';

class ES implements EventSourceLike {
  onopen: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  closed = false;
  #l = new Map<string, ((e: { data: string }) => void)[]>();
  constructor(public since: number) {}
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
const err = (status: number, code: string, extra: object = {}) => Object.assign(new Error(code), { status, code, ...extra });

let calls: { op: string; body: any; keepalive?: boolean }[];
let gets: { op: string; query: any }[];
let sources: ES[];
let handlers: Record<string, (body: any, n: number) => any>;
let counts: Record<string, number>;
let pageHide: (() => void) | undefined;

function deps(): Deps {
  return {
    post: async (op, body, keepalive) => {
      calls.push({ op, body, keepalive });
      counts[op] = (counts[op] ?? 0) + 1;
      const h = handlers[op];
      if (h) return h(body, counts[op]!);
      if (op === 'join') return { client: (body as { client?: string }).client ?? 'c1', session: 's', seq: 0, updates: [], init: [], pending: [], presence: [], etag: 'e' };
      if (op === 'updates') return { seq: counts[op] };
      return {};
    },
    get: async (op, query) => {
      gets.push({ op, query });
      return handlers['get']!(query, gets.length);
    },
    eventSource: (_c, since) => {
      const s = new ES(since);
      sources.push(s);
      return s;
    },
    setTimeout: (f, ms) => setTimeout(f, ms),
    clearTimeout: (h) => clearTimeout(h as never),
    setInterval: (f, ms) => setInterval(f, ms),
    clearInterval: (h) => clearInterval(h as never),
    onPageHide: (f) => {
      pageHide = f;
      return () => (pageHide = undefined);
    },
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  calls = [];
  gets = [];
  sources = [];
  handlers = {};
  counts = {};
  pageHide = undefined;
});
afterEach(() => vi.useRealTimers());

describe('paged join', () => {
  it('reads the rest of the log in order before opening the stream from its last seq', async () => {
    handlers.join = () => ({ client: 'c1', session: 's', seq: 2, more: true, updates: [{ seq: 1, data: b64(1), client: 'x' }, { seq: 2, data: b64(2), client: 'x' }], init: [], pending: [], presence: [], etag: 'e' });
    handlers.get = (q, n) =>
      n === 1
        ? { updates: [{ seq: 3, data: b64(3), client: 'x' }, { seq: 4, data: b64(4), client: 'x' }], seq: 4, more: true }
        : { updates: [{ seq: 5, data: b64(5), client: 'x' }], seq: 5, more: false };
    const tr = connect('i', deps());
    const r = await tr.join('en', true);
    expect(gets.map((g) => g.query)).toEqual([{ client: 'c1', since: 2 }, { client: 'c1', since: 4 }]);
    expect(r.updates.map((u) => u.seq)).toEqual([1, 2, 3, 4, 5]);
    expect(r.seq).toBe(5);
    expect(sources[0]!.since).toBe(5);
    tr.leave();
  });

  it('gives up on a page that does not advance', async () => {
    handlers.join = () => ({ client: 'c1', seq: 1, more: true, updates: [], init: [], pending: [], presence: [], etag: 'e' });
    handlers.get = () => ({ updates: [], seq: 1, more: true });
    const tr = connect('i', deps());
    const j = tr.join('en', true).catch((e) => e);
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.state).toBe('unavailable');
    tr.leave();
    await j;
  });
});

describe('sending', () => {
  it('honours Retry-After on rate_limited, then succeeds', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    handlers.updates = (_b, n) => {
      if (n === 1) throw err(429, 'rate_limited', { retryAfter: 2 });
      return { seq: 9 };
    };
    const p = tr.send(new Uint8Array([1]));
    await vi.advanceTimersByTimeAsync(1_900);
    expect(counts.updates).toBe(1);
    await vi.advanceTimersByTimeAsync(200);
    await expect(p).resolves.toBe(9);
    expect(counts.updates).toBe(2);
    tr.leave();
  });

  it('waits for the compaction on compact_required and retries', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    handlers.updates = (_b, n) => {
      if (n === 1) throw err(409, 'compact_required');
      return { seq: 3 };
    };
    const p = tr.send(new Uint8Array([1]));
    await vi.advanceTimersByTimeAsync(10);
    expect(counts.updates).toBe(1);
    sources[0]!.emit('compact', { upto: 5 });
    await vi.advanceTimersByTimeAsync(600);
    await expect(p).resolves.toBe(3);
    tr.leave();
  });

  it('read_only makes the connection read-only and fails the send', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const seen = vi.fn();
    tr.onReadOnly(seen);
    handlers.updates = () => {
      throw err(403, 'read_only');
    };
    await expect(tr.send(new Uint8Array([1]))).rejects.toMatchObject({ code: 'read_only' });
    expect(seen).toHaveBeenCalledTimes(1);
    expect(tr.readOnly).toBe(true);
    tr.leave();
  });

  it('unknown_client re-joins with the same id', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    handlers.updates = () => {
      throw err(404, 'unknown_client');
    };
    await expect(tr.send(new Uint8Array([1]))).rejects.toMatchObject({ code: 'unknown_client' });
    await vi.advanceTimersByTimeAsync(10);
    expect(calls.filter((c) => c.op === 'join').map((c) => c.body.client)).toEqual([undefined, 'c1']);
    tr.leave();
  });

  it('ignores compact_not_requested and posts reset', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    handlers.compact = () => {
      throw err(409, 'compact_not_requested');
    };
    await expect(tr.compact(new Uint8Array([1]), 3)).resolves.toBeUndefined();
    await tr.reset();
    expect(calls.at(-1)).toMatchObject({ op: 'reset', body: { client: 'c1' } });
    tr.leave();
  });
});

describe('joining', () => {
  it('retries the first join with backoff while the server is down', async () => {
    let n = 0;
    handlers.join = (b) => {
      if (++n < 3) throw err(503, 'unavailable');
      return { client: 'c1', seq: 0, updates: [], init: [], pending: [], presence: [], etag: 'e', session: 's' };
    };
    const tr = connect('i', deps());
    const j = tr.join('en', true);
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.state).toBe('unavailable');
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.advanceTimersByTimeAsync(2_000);
    await expect(j).resolves.toMatchObject({ client: 'c1' });
    expect(n).toBe(3);
    tr.leave();
  });

  it('does not retry an answer (404) and reports unavailable', async () => {
    handlers.join = () => {
      throw err(404, 'not_found');
    };
    const tr = connect('i', deps());
    await expect(tr.join('en', true)).rejects.toMatchObject({ status: 404 });
    expect(tr.state).toBe('unavailable');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(counts.join).toBe(1);
    tr.leave();
  });

  it('joins as a viewer when edit access is refused', async () => {
    handlers.join = (b) => {
      if (b.edit) throw err(403, 'forbidden');
      return { client: 'c1', seq: 0, updates: [], init: [], pending: [], presence: [], etag: 'e', session: 's' };
    };
    const tr = connect('i', deps());
    await tr.join('en', true);
    expect(calls.filter((c) => c.op === 'join').map((c) => c.body.edit)).toEqual([true, false]);
    expect(tr.readOnly).toBe(true);
    expect(tr.state).toBe('live');
    tr.leave();
  });

  it('leaves the client a join created after leave() ran meanwhile', async () => {
    let release!: (v: unknown) => void;
    handlers.join = () => new Promise((r) => (release = r));
    const tr = connect('i', deps());
    const j = tr.join('en', true).catch((e) => e);
    await vi.advanceTimersByTimeAsync(0);
    tr.leave();
    release({ client: 'late', seq: 0, updates: [], init: [], pending: [], presence: [], etag: 'e', session: 's' });
    await j;
    expect(calls.at(-1)).toMatchObject({ op: 'leave', body: { client: 'late' } });
    expect(sources).toHaveLength(0);
  });

  it('backs off while streams keep failing before they open', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    for (let i = 0; i < 4; i++) {
      sources.at(-1)!.onerror?.({});
      await vi.advanceTimersByTimeAsync(0);
    }
    // First failure re-joins at once; the next ones wait 1 s, 2 s, ...
    const joins = () => calls.filter((c) => c.op === 'join').length;
    expect(joins()).toBe(2);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(joins()).toBe(3);
    sources.at(-1)!.onerror?.({});
    await vi.advanceTimersByTimeAsync(1_900);
    expect(joins()).toBe(3);
    await vi.advanceTimersByTimeAsync(200);
    expect(joins()).toBe(4);
    sources.at(-1)!.onopen?.({});
    sources.at(-1)!.onerror?.({});
    await vi.advanceTimersByTimeAsync(0);
    expect(joins()).toBe(5); // an opened stream resets the backoff
    tr.leave();
  });
});

describe('presence and updates before the binding', () => {
  it('sends presence only once the session is live', async () => {
    let release!: (v: unknown) => void;
    handlers.join = () => new Promise((r) => (release = r));
    const tr = connect('i', deps(), 'old');
    tr.presence({ locale: 'en', focus: 'title' });
    const j = tr.join('en', true);
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.some((c) => c.op === 'presence')).toBe(false);
    release({ client: 'old', seq: 0, updates: [], init: [], pending: [], presence: [], etag: 'e', session: 's' });
    await j;
    expect(calls.filter((c) => c.op === 'presence')).toHaveLength(1);
    tr.leave();
  });

  it('holds stream updates until the first subscriber', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    sources[0]!.emit('update', { seq: 1, data: b64(1), client: 'x' });
    sources[0]!.emit('update', { seq: 2, data: b64(2), client: 'x' });
    const got: number[] = [];
    tr.onUpdate((u) => got.push(u.seq));
    expect(got).toEqual([1, 2]);
    tr.leave();
  });

  it('delivers saved with its seq', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const s = vi.fn();
    tr.onSaved(s);
    sources[0]!.emit('saved', { etag: 'e9', client: 'x', name: 'Al', seq: 12 });
    expect(s).toHaveBeenCalledWith({ etag: 'e9', client: 'x', name: 'Al', seq: 12 });
    tr.leave();
  });
});

describe('leaving', () => {
  it('lets updates in flight finish before the leave call', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    let done!: (v: unknown) => void;
    handlers.updates = () => new Promise((r) => (done = r));
    const p = tr.send(new Uint8Array([1]));
    await vi.advanceTimersByTimeAsync(0);
    tr.leave();
    await vi.advanceTimersByTimeAsync(10);
    expect(calls.some((c) => c.op === 'leave')).toBe(false);
    done({ seq: 4 });
    await p;
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.at(-1)!.op).toBe('leave');
  });

  it('flushes through onLeave and leaves at once on pagehide, with keepalive', async () => {
    const tr = connect('i', deps());
    await tr.join('en', true);
    const flushed: boolean[] = [];
    tr.onLeave((ph) => flushed.push(ph));
    handlers.updates = () => new Promise(() => undefined);
    void tr.send(new Uint8Array([1]));
    pageHide!();
    expect(flushed).toEqual([true]);
    expect(calls.at(-1)).toMatchObject({ op: 'leave', keepalive: true });
  });
});
