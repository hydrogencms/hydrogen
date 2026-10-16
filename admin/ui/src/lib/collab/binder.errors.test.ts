import * as Y from 'yjs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { hydrate } from '../fields/logic';
import { Binder } from './binder';
import { draft, env, type } from './fixture';
import type { JoinResult, RemoteUpdate } from './transport';
import { type Doc } from './ydoc';

const err = (status: number, code: string) => Object.assign(new Error(code), { status, code });

function setup(send: (data: Uint8Array, init?: string[], keepalive?: boolean) => Promise<number>, join: Partial<JoinResult> = {}) {
  const listeners = new Set<(u: RemoteUpdate) => void>();
  const leave = new Set<(pagehide: boolean) => void>();
  const ro = new Set<() => void>();
  const tr = {
    client: 'me',
    send: vi.fn(send),
    compact: vi.fn(async () => undefined),
    reset: vi.fn(async () => undefined),
    onUpdate: (f: (u: RemoteUpdate) => void) => (listeners.add(f), () => void listeners.delete(f)),
    onCompact: () => () => undefined,
    onLeave: (f: (p: boolean) => void) => (leave.add(f), () => void leave.delete(f)),
    onReadOnly: (f: () => void) => (ro.add(f), () => void ro.delete(f)),
  };
  const j: JoinResult = {
    client: 'me',
    session: 's',
    seq: 0,
    updates: [],
    init: ['shared', 'l:en'],
    pending: [],
    presence: [{ client: 'me', principal: 'p', name: 'n', color: '#000', locale: 'en', edit: true }],
    etag: 'e',
    ...join,
  };
  const v = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
  const ticks: (() => void)[] = [];
  const fatal = vi.fn();
  const b = new Binder({ transport: tr, join: j, type, locale: 'en', draft, fields: () => v.fields, snapshot: (x) => structuredClone(x), env, schedule: (f) => ticks.push(f), onFatal: fatal });
  return { b, tr, v, ticks, fatal, listeners, leave, ro };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('failed posts', () => {
  it('a retried seed keeps its init parts', async () => {
    let n = 0;
    const { b, tr } = setup(async () => {
      if (++n === 1) throw new Error('offline');
      return 5;
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.send.mock.calls[0]![1]).toEqual(['shared', 'l:en']);
    await vi.advanceTimersByTimeAsync(2_100);
    expect(tr.send).toHaveBeenCalledTimes(2);
    expect(tr.send.mock.calls[1]![1]).toEqual(['shared', 'l:en']);
    expect(b.unsynced()).toBe(false);
    b.destroy();
  });

  it('claim_lost is fatal and is not retried', async () => {
    const { b, tr, fatal } = setup(async () => {
      throw err(409, 'claim_lost');
    });
    await vi.advanceTimersByTimeAsync(10_000);
    expect(fatal).toHaveBeenCalledWith('claim_lost');
    expect(tr.send).toHaveBeenCalledTimes(1);
    b.destroy();
  });

  it('another 4xx stops retrying and shows as a failed, unsynced state', async () => {
    const { b, tr } = setup(async () => {
      throw err(413, 'too_large');
    });
    await vi.advanceTimersByTimeAsync(10_000);
    expect(tr.send).toHaveBeenCalledTimes(1);
    expect(b.failed()).toBe('too_large');
    expect(b.unsynced()).toBe(true);
    b.destroy();
  });

  it('backs off between retries of an outage', async () => {
    const { b, tr, v } = setup(async (_d, init) => {
      if (init) return 1;
      throw new Error('offline');
    });
    await vi.advanceTimersByTimeAsync(0);
    v.fields.count = 77;
    b.flush();
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.send).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(2_100);
    expect(tr.send).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(2_100);
    expect(tr.send).toHaveBeenCalledTimes(3); // the second wait is longer
    await vi.advanceTimersByTimeAsync(2_100);
    expect(tr.send).toHaveBeenCalledTimes(4);
    expect(b.unsynced()).toBe(true);
    b.destroy();
  });

  it('read_only turns the binder read-only and drops what is unsent', async () => {
    const { b, v } = setup(async (_d, init) => {
      if (init) return 1;
      throw err(403, 'read_only');
    });
    await vi.advanceTimersByTimeAsync(0);
    v.fields.count = 5;
    b.flush();
    await vi.advanceTimersByTimeAsync(0);
    expect(b.readOnly).toBe(true);
    expect(b.unsynced()).toBe(false);
    b.destroy();
  });
});

describe('claims and bad updates', () => {
  it("another client's seed for a part still being seeded here is a lost claim", async () => {
    const { b, fatal, listeners } = setup(() => new Promise(() => undefined));
    await vi.advanceTimersByTimeAsync(0);
    const other = new Y.Doc();
    other.getMap('shared').set('count', 3);
    for (const f of listeners) f({ seq: 1, data: Y.encodeStateAsUpdate(other), client: 'x', init: ['shared'] });
    expect(fatal).toHaveBeenCalledWith('claim_lost');
    expect(b.lostParts().sort()).toEqual(['l:en', 'shared']);
    b.destroy();
  });

  it('an update that cannot be applied asks the server to reset the session', async () => {
    const { b, tr, fatal, listeners } = setup(async () => 1);
    await vi.advanceTimersByTimeAsync(0);
    for (const f of listeners) f({ seq: 1, data: new Uint8Array([255, 255, 255, 255, 9]), client: 'x' });
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.reset).toHaveBeenCalledTimes(1);
    expect(fatal).toHaveBeenCalledWith('poison');
    b.destroy();
  });

  it('a failed reset falls back to a fatal restart', async () => {
    const { b, tr, fatal, listeners } = setup(async () => 1);
    tr.reset.mockRejectedValueOnce(err(403, 'read_only'));
    await vi.advanceTimersByTimeAsync(0);
    for (const f of listeners) f({ seq: 1, data: new Uint8Array([255, 255, 255, 255, 9]), client: 'x' });
    await vi.advanceTimersByTimeAsync(0);
    expect(fatal).toHaveBeenCalledWith('reset-failed');
    b.destroy();
  });
});

describe('sequence and leaving', () => {
  it('applied() counts own acknowledged updates', async () => {
    const { b, v } = setup(async (_d, init) => (init ? 1 : 2));
    await vi.advanceTimersByTimeAsync(0);
    expect(b.applied()).toBe(1);
    v.fields.count = 9;
    b.flush();
    await vi.advanceTimersByTimeAsync(0);
    expect(b.applied()).toBe(2);
    b.destroy();
  });

  it('flushes the form and sends with keepalive when the connection leaves on pagehide', async () => {
    const { b, tr, v, leave } = setup(async () => 1);
    await vi.advanceTimersByTimeAsync(0);
    tr.send.mockClear();
    v.fields.count = 31;
    for (const f of leave) f(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(tr.send).toHaveBeenCalledTimes(1);
    expect(tr.send.mock.calls[0]![2]).toBe(true);
    b.destroy();
  });
});
