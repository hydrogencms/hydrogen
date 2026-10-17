import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';
import FocalPicker from './FocalPicker.svelte';
import type { Focal } from './logic';

let app: ReturnType<typeof mount> | undefined;
afterEach(() => {
  if (app) unmount(app);
  app = undefined;
  document.body.innerHTML = '';
});

const file = { id: 'ab', name: 'a.png', mime: 'image/png', size: 1, width: 4, height: 3 };
const stage = () => document.querySelector('[aria-label="Focal point"]') as HTMLElement;
const key = (k: string, shiftKey = false) => {
  stage().dispatchEvent(new KeyboardEvent('keydown', { key: k, shiftKey, bubbles: true, cancelable: true }));
  flushSync();
};

describe('focal point picker', () => {
  it('is a focusable, named slider that moves with the arrow keys', () => {
    const got: (Focal | undefined)[] = [];
    app = mount(FocalPicker, { target: document.body, props: { file, onchange: (f: Focal | undefined) => got.push(f) } });
    flushSync();
    expect(stage().getAttribute('role')).toBe('slider');
    expect(stage().tabIndex).toBe(0);
    expect(stage().getAttribute('aria-valuetext')).toContain('Not set');
    key('ArrowRight');
    key('ArrowRight');
    key('ArrowDown');
    expect(got.at(-1)).toEqual({ x: 0.6, y: 0.55 });
    expect(stage().getAttribute('aria-valuetext')).toBe('Horizontal 60%, vertical 55%');
    key('ArrowLeft', true);
    expect(got.at(-1)).toEqual({ x: 0.59, y: 0.55 });
    expect([...document.querySelectorAll('button')].some((b) => b.textContent?.trim() === 'Reset')).toBe(true);
  });
  it('ignores keys when read-only', () => {
    const got: unknown[] = [];
    app = mount(FocalPicker, { target: document.body, props: { file, readonly: true, onchange: (f: unknown) => got.push(f) } });
    flushSync();
    key('ArrowRight');
    expect(got).toEqual([]);
    expect(stage().tabIndex).toBe(-1);
  });
});
