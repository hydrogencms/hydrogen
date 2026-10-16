import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Relay, type RelayState } from './relay';
import { shareState } from './share';

class Err extends Error {
  constructor(public status: number) {
    super('e' + status);
  }
}

function setup(put?: (f: Record<string, unknown>) => Promise<unknown>) {
  const sent: Record<string, unknown>[] = [];
  const dels: (boolean | undefined)[] = [];
  const states: RelayState[] = [];
  const relay = new Relay({
    put: put ? (f) => (sent.push(f), put(f)) : async (f) => void sent.push(f),
    del: async (k) => void dels.push(k),
    onState: (s) => states.push(s),
  });
  return { relay, sent, dels, states };
}
const gate = () => {
  let release!: (err?: Error) => void;
  const p = new Promise<void>((res, rej) => (release = (e) => (e ? rej(e) : res())));
  return { p, release };
};

describe('Relay', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('a PUT after a clear waits for the DELETE in flight', async () => {
    const order: string[] = [];
    let release!: () => void;
    const slow = new Promise<void>((r) => (release = r));
    const relay = new Relay({
      put: async (f) => void order.push('put' + JSON.stringify(f)),
      del: async () => {
        order.push('del-start');
        await slow;
        order.push('del-end');
      },
    });
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    const cleared = relay.clear();
    await vi.advanceTimersByTimeAsync(0);
    relay.push({ v: 2 });
    await vi.advanceTimersByTimeAsync(500);
    expect(order).toEqual(['put{"v":1}', 'del-start']);
    release();
    await cleared;
    await vi.advanceTimersByTimeAsync(10);
    expect(order).toEqual(['put{"v":1}', 'del-start', 'del-end', 'put{"v":2}']);
  });

  it('push(fields, true) sends without the throttle delay', async () => {
    const { relay, sent } = setup();
    relay.push({ v: 1 }, true);
    await vi.advanceTimersByTimeAsync(0);
    expect(sent).toEqual([{ v: 1 }]);
  });

  it('throttles: one trailing request carrying the latest value', async () => {
    const { relay, sent } = setup();
    relay.push({ t: 'a' });
    relay.push({ t: 'ab' });
    await vi.advanceTimersByTimeAsync(249);
    expect(sent).toHaveLength(0);
    relay.push({ t: 'abc' });
    await vi.advanceTimersByTimeAsync(1);
    expect(sent).toEqual([{ t: 'abc' }]);
  });

  it('keeps one request in flight and sends the latest value after it', async () => {
    const g = gate();
    let n = 0;
    const { relay, sent } = setup(() => (n++ === 0 ? g.p : Promise.resolve()));
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    relay.push({ v: 2 });
    relay.push({ v: 3 });
    await vi.advanceTimersByTimeAsync(1000);
    expect(sent).toEqual([{ v: 1 }]);
    g.release();
    await vi.advanceTimersByTimeAsync(250);
    expect(sent).toEqual([{ v: 1 }, { v: 3 }]);
  });

  it('does not resend the value the server already holds', async () => {
    const { relay, sent } = setup();
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(500);
    expect(sent).toHaveLength(1);
  });

  it('flush resolves true once the latest value is sent, false on 422', async () => {
    const a = setup();
    a.relay.push({ v: 1 });
    const done = a.relay.flush();
    await vi.advanceTimersByTimeAsync(250);
    await expect(done).resolves.toBe(true);
    await expect(a.relay.flush()).resolves.toBe(true);

    const b = setup(async () => {
      throw new Err(422);
    });
    b.relay.push({ v: 1 });
    const bad = b.relay.flush();
    await vi.advanceTimersByTimeAsync(250);
    await expect(bad).resolves.toBe(false);
    expect(b.states.at(-1)).toBe('invalid');
    // The same invalid value is not sent again; a new one is.
    b.relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(500);
    expect(b.sent).toHaveLength(1);
    b.relay.push({ v: 2 });
    await vi.advanceTimersByTimeAsync(250);
    expect(b.sent).toHaveLength(2);
  });

  it('stops on 403 and sends nothing more, not even a delete', async () => {
    const { relay, sent, dels, states } = setup(async () => {
      throw new Err(403);
    });
    relay.push({ v: 1 });
    const f = relay.flush();
    await vi.advanceTimersByTimeAsync(250);
    await expect(f).resolves.toBe(false);
    expect(states.at(-1)).toBe('stopped');
    relay.push({ v: 2 });
    await vi.advanceTimersByTimeAsync(1000);
    expect(sent).toHaveLength(1);
    await relay.clear();
    expect(dels).toHaveLength(0);
    await expect(relay.flush()).resolves.toBe(false);
  });

  it('retries the same value after a network error', async () => {
    let n = 0;
    const { relay, sent, states } = setup(async () => {
      if (n++ === 0) throw new Err(0);
    });
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    expect(states.at(-1)).toBe('error');
    relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    expect(sent).toHaveLength(2);
    expect(states.at(-1)).toBe('sent');
  });

  it('clear cancels what is pending and deletes only when something was sent', async () => {
    const a = setup();
    a.relay.push({ v: 1 });
    await a.relay.clear();
    await vi.advanceTimersByTimeAsync(1000);
    expect(a.sent).toHaveLength(0);
    expect(a.dels).toHaveLength(0);

    const b = setup();
    b.relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    await b.relay.clear();
    expect(b.dels).toEqual([false]);
    await b.relay.clear();
    expect(b.dels).toHaveLength(1);
    // After a clear the same value is sent again.
    b.relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    expect(b.sent).toHaveLength(2);
  });

  it('clear waits for the request in flight, then deletes; keepalive does not wait', async () => {
    const g = gate();
    const a = setup(() => g.p);
    a.relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    const c = a.relay.clear();
    await vi.advanceTimersByTimeAsync(0);
    expect(a.dels).toHaveLength(0);
    g.release();
    await c;
    expect(a.dels).toEqual([false]);

    const g2 = gate();
    const b = setup(() => g2.p);
    b.relay.push({ v: 1 });
    await vi.advanceTimersByTimeAsync(250);
    await b.relay.clear(true);
    expect(b.dels).toEqual([true]);
  });
});

describe('shareState', () => {
  const now = Date.parse('2026-10-17T12:00:00Z');
  it('tells active, expired and revoked apart', () => {
    expect(shareState({ expiresAt: '2026-10-17T13:00:00Z' }, now)).toBe('active');
    expect(shareState({ expiresAt: '2026-10-17T11:00:00Z' }, now)).toBe('expired');
    expect(shareState({ expiresAt: '2026-10-17T13:00:00Z', revokedAt: '2026-10-17T11:30:00Z' }, now)).toBe('revoked');
  });
});
