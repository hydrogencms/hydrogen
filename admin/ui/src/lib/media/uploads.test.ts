import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import { setConfig } from '../config';
import type { FileValue } from './logic';

const h = vi.hoisted(() => ({
  uploadFile: vi.fn(),
  create: vi.fn(),
}));
vi.mock('./upload', () => ({ uploadFile: h.uploadFile, mediaApi: { create: h.create } }));

import { Uploads, type UploadTarget } from './uploads.svelte';

const fv = (id: string, name = 'a.png'): FileValue => ({ id, name, mime: 'image/png', size: 5 });
const file = (name: string, size = 5) => new File([new Uint8Array(size)], name, { type: 'image/png' });
const types = [{ name: 'image', upload: { accept: ['image/*'] } }];
const settle = () => new Promise((r) => setTimeout(r, 0));

const target = (over: Partial<UploadTarget> = {}): UploadTarget => ({
  types,
  known: () => undefined,
  ondone: vi.fn(),
  ...over,
});

/** An uploadFile whose promise the test resolves; it rejects with an AbortError when its signal aborts. */
function hang() {
  const calls: { file: File; signal: AbortSignal; progress: (n: number) => void; resolve: (v: { file: FileValue; existing: boolean }) => void }[] = [];
  h.uploadFile.mockImplementation(
    (f: File, o: { signal: AbortSignal; onProgress: (n: number) => void }) =>
      new Promise((resolve, reject) => {
        o.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
        calls.push({ file: f, signal: o.signal, progress: o.onProgress, resolve });
      }),
  );
  return calls;
}

beforeEach(() => {
  h.uploadFile.mockReset();
  h.create.mockReset();
  h.create.mockImplementation(async () => ({ id: 'item-' + h.create.mock.calls.length }));
  setConfig({ media: undefined });
});
afterEach(() => setConfig({ media: undefined }));

describe('clearFinished', () => {
  it('writes nothing when no entry finished (safe inside an effect)', async () => {
    const calls = hang();
    const u = new Uploads();
    const p = u.add([file('a.png')], target());
    await settle();
    const before = u.entries;
    u.clearFinished();
    expect(u.entries).toBe(before);
    calls[0]!.resolve({ file: fv('x'), existing: false });
    await p;
    expect(u.entries.map((e) => e.state)).toEqual(['done']);
    u.clearFinished();
    expect(u.entries).toEqual([]);
    const empty = u.entries;
    u.clearFinished();
    expect(u.entries).toBe(empty);
  });
});

describe('abort', () => {
  it('aborts the requests, never calls ondone, and the queue is usable again', async () => {
    const calls = hang();
    const t = target();
    const u = new Uploads();
    const p = u.add([file('a.png'), file('b.png')], t);
    await settle();
    u.abort();
    await p;
    expect(calls.every((c) => c.signal.aborted)).toBe(true);
    expect(u.entries.map((e) => e.state)).toEqual(['cancelled', 'cancelled']);
    expect(t.ondone).not.toHaveBeenCalled();
    expect(u.busy).toBe(false);

    const again = u.add([file('c.png')], t);
    await settle();
    calls.at(-1)!.resolve({ file: fv('c'), existing: false });
    await again;
    expect(t.ondone).toHaveBeenCalledTimes(1);
  });

  it('a completion that arrives after the abort is dropped', async () => {
    const calls = hang();
    h.uploadFile.mockImplementation(
      (f: File, o: { signal: AbortSignal }) => new Promise((resolve) => calls.push({ file: f, signal: o.signal, progress: () => {}, resolve })),
    );
    const t = target();
    const u = new Uploads();
    const p = u.add([file('a.png')], t);
    await settle();
    u.destroy();
    calls[0]!.resolve({ file: fv('x'), existing: false });
    await p;
    expect(t.ondone).not.toHaveBeenCalled();
    expect(u.entries[0]!.state).toBe('cancelled');
  });

  it('cancels one file, a queued one included', async () => {
    const calls = hang();
    const t = target();
    const u = new Uploads();
    const p = u.add(['a', 'b', 'c', 'd', 'e'].map((n) => file(n + '.png')), t);
    await settle();
    expect(calls).toHaveLength(3); // three at a time
    u.cancel(u.entries[4]!.id); // waiting
    u.cancel(u.entries[0]!.id); // running
    await settle();
    expect(calls[0]!.signal.aborted).toBe(true);
    expect(u.entries.map((e) => e.state)).toEqual(['cancelled', 'uploading', 'uploading', 'uploading', 'cancelled']);
    calls[3]?.resolve({ file: fv('d'), existing: false }); // the freed slot goes to the fourth
    for (const c of calls.slice(1)) c.resolve({ file: fv(c.file.name), existing: false });
    await p;
    expect(h.uploadFile).toHaveBeenCalledTimes(4);
  });
});

describe('checks before sending', () => {
  it('refuses a file over the configured upload limit without a request', async () => {
    setConfig({ media: { maxUpload: 10 } });
    const t = target();
    const u = new Uploads();
    await u.add([file('big.png', 20)], t);
    expect(h.uploadFile).not.toHaveBeenCalled();
    expect(u.entries[0]).toMatchObject({ state: 'error' });
    expect(u.entries[0]!.message).toContain('big.png');
    expect(u.entries[0]!.message).toMatch(/upload limit/);
  });

  it('applies the smaller of the limit and the type max_size', async () => {
    setConfig({ media: { maxUpload: 100 } });
    const u = new Uploads();
    await u.add([file('a.png', 20)], target({ types: [{ name: 'image', upload: { accept: ['image/*'], max_size: 10 } }] }));
    expect(h.uploadFile).not.toHaveBeenCalled();
    expect(u.entries[0]!.state).toBe('error');
  });

  it('turns a network failure into one clear message', async () => {
    h.uploadFile.mockRejectedValue(new ApiError(0, 'network', 'network error'));
    const u = new Uploads();
    await u.add([file('a.png')], target());
    expect(u.entries[0]).toMatchObject({ state: 'error' });
    expect(u.entries[0]!.message).toMatch(/too large, or the connection was lost/);
  });
});

describe('the same bytes twice in one batch', () => {
  it('creates one item; the other entry points at it', async () => {
    h.uploadFile.mockImplementation(async () => ({ file: fv('same'), existing: false }));
    const t = target();
    const u = new Uploads();
    await u.add([file('a.png'), file('copy-of-a.png')], t);
    expect(h.create).toHaveBeenCalledTimes(1);
    expect(u.entries.map((e) => e.state).sort()).toEqual(['done', 'exists']);
    expect(new Set(u.entries.map((e) => e.item)).size).toBe(1);
    expect(t.ondone).toHaveBeenCalledTimes(2);
  });

  it('different bytes each create their own', async () => {
    h.uploadFile.mockImplementation(async (f: File) => ({ file: fv(f.name), existing: false }));
    const u = new Uploads();
    await u.add([file('a.png'), file('b.png')], target());
    expect(h.create).toHaveBeenCalledTimes(2);
  });

  it('forgets the batch when the queue empties (a trashed item is created again later)', async () => {
    h.uploadFile.mockImplementation(async () => ({ file: fv('same'), existing: false }));
    const u = new Uploads();
    await u.add([file('a.png')], target());
    await u.add([file('a.png')], target());
    expect(h.create).toHaveBeenCalledTimes(2);
  });
});

describe('progress and summary', () => {
  it('updates the one entry in place', async () => {
    const calls = hang();
    const u = new Uploads();
    const p = u.add([file('a.png')], target());
    await settle();
    const entry = u.entries[0]!;
    calls[0]!.progress(0.5);
    expect(u.entries[0]).toBe(entry);
    expect(entry.progress).toBe(0.5);
    calls[0]!.resolve({ file: fv('x'), existing: false });
    await p;
  });

  it('says how many finished and failed, and nothing while empty', async () => {
    h.uploadFile.mockImplementation(async (f: File) => {
      if (f.name === 'bad.png') throw new ApiError(500, 'boom', 'boom');
      return { file: fv(f.name), existing: false };
    });
    const u = new Uploads();
    expect(u.summary).toBe('');
    await u.add([file('a.png'), file('b.png'), file('bad.png')], target());
    expect(u.summary).toBe('2 of 3 uploaded, 1 failed');
  });
});
