import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { session } from '../auth/session.svelte';
import { setConfig } from '../config';
import { facetCache } from './library.svelte';
import { reactive } from './reactive.svelte';

const h = vi.hoisted(() => ({ uploadFile: vi.fn(), create: vi.fn() }));
vi.mock('./upload', () => ({
  uploadFile: h.uploadFile,
  mediaApi: {
    create: h.create,
    list: async () => ({ items: [], total: 0 }),
    facets: async () => ({ folders: [], tags: [] }),
    bySha: async () => undefined,
  },
}));

import AssetPicker from './AssetPicker.svelte';

let app: ReturnType<typeof mount> | undefined;
const settle = () => new Promise((r) => setTimeout(r, 10));

beforeEach(() => {
  facetCache.invalidate();
  session.grants = [{ policy: '*', limits: [] }] as never;
  setConfig({ media: undefined });
});
afterEach(() => {
  if (app) unmount(app);
  app = undefined;
  document.body.innerHTML = '';
  session.grants = [];
});

describe('asset picker', () => {
  it('closes after a finished upload without looping its effects', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {});
    h.uploadFile.mockResolvedValue({ file: { id: 'ab', name: 'a.png', mime: 'image/png', size: 1 }, existing: false });
    h.create.mockResolvedValue({ id: 'item-1' });
    const chosen: unknown[] = [];
    const props = reactive({ open: true, names: ['image'], onselect: (a: unknown) => chosen.push(a) });
    app = mount(AssetPicker, { target: document.body, props });
    flushSync();
    await settle();

    const tab = [...document.querySelectorAll<HTMLElement>('[role=tab]')].find((b) => b.textContent?.trim() === 'Upload')!;
    tab.click();
    flushSync();
    const input = document.querySelector<HTMLInputElement>('input[type=file]')!;
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'a.png', { type: 'image/png' })], configurable: true });
    input.dispatchEvent(new Event('change', { bubbles: true }));
    await settle();
    flushSync();

    expect(chosen).toEqual([{ id: 'item-1', name: 'a.png', file: expect.objectContaining({ id: 'ab' }), status: 'draft' }]);
    expect(props.open).toBe(false);
    await settle();
    expect(errors.mock.calls.flat().join(' ')).not.toMatch(/effect_update_depth_exceeded/);
    errors.mockRestore();
  });

  it('closing aborts a running upload, which then chooses nothing', async () => {
    let signal!: AbortSignal;
    let finish!: (v: unknown) => void;
    h.uploadFile.mockImplementation((_f: File, o: { signal: AbortSignal }) => {
      signal = o.signal;
      return new Promise((resolve) => (finish = resolve));
    });
    const chosen: unknown[] = [];
    const props = reactive({ open: true, names: ['image'], onselect: (a: unknown) => chosen.push(a) });
    app = mount(AssetPicker, { target: document.body, props });
    flushSync();
    [...document.querySelectorAll<HTMLElement>('[role=tab]')].find((b) => b.textContent?.trim() === 'Upload')!.click();
    flushSync();
    const input = document.querySelector<HTMLInputElement>('input[type=file]')!;
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'a.png', { type: 'image/png' })], configurable: true });
    input.dispatchEvent(new Event('change', { bubbles: true }));
    await settle();
    expect(signal.aborted).toBe(false);

    props.open = false;
    flushSync();
    await settle();
    expect(signal.aborted).toBe(true);
    finish({ file: { id: 'ab', name: 'a.png', mime: 'image/png', size: 1 }, existing: false });
    await settle();
    expect(chosen).toEqual([]);
  });
});
