import { flushSync, mount } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import Combobox from './Combobox.svelte';
import { mounted } from '../../test-setup';

describe('Combobox', () => {
  it('searches asynchronously, navigates with the keyboard and selects with Enter', async () => {
    const search = vi.fn(async (q: string) => [
      { value: '1', label: `${q} one` },
      { value: '2', label: `${q} two` },
    ]);
    const onselect = vi.fn();
    const target = document.body.appendChild(document.createElement('div'));
    mounted.push(mount(Combobox, { target, props: { search, onselect, debounce: 0 } }));
    const input = target.querySelector('input')!;
    expect(input.getAttribute('role')).toBe('combobox');
    expect(input.getAttribute('aria-expanded')).toBe('false');

    input.focus();
    input.value = 'ab';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    flushSync();
    await vi.waitFor(() => expect(target.querySelectorAll('[role=option]').length).toBe(2));
    expect(search).toHaveBeenLastCalledWith('ab', expect.any(AbortSignal));
    expect(input.getAttribute('aria-expanded')).toBe('true');
    expect(input.getAttribute('aria-activedescendant')).toBe(target.querySelectorAll('[role=option]')[0].id);

    const key = (k: string) => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
      flushSync();
    };
    key('ArrowDown');
    key('Enter');
    expect(onselect).toHaveBeenCalledWith({ value: '2', label: 'ab two' });
    expect(input.value).toBe('ab two');
    expect(target.querySelector('[role=listbox]')).toBeNull();
  });

  it('Escape closes the list; stale responses are dropped', async () => {
    let n = 0;
    const search = vi.fn(async (q: string, signal: AbortSignal) => {
      const id = ++n;
      await new Promise((r) => setTimeout(r, id === 1 ? 40 : 1));
      return [{ value: String(id), label: `${q}#${id}` }];
    });
    const target = document.body.appendChild(document.createElement('div'));
    mounted.push(mount(Combobox, { target, props: { search, debounce: 0 } }));
    const input = target.querySelector('input')!;
    for (const v of ['a', 'ab']) {
      input.value = v;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      flushSync();
      await new Promise((r) => setTimeout(r, 5));
    }
    await vi.waitFor(() => expect(target.querySelector('[role=option]')?.textContent).toContain('ab#'));
    await new Promise((r) => setTimeout(r, 60));
    expect(target.querySelector('[role=option]')?.textContent).toContain('ab#');
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    flushSync();
    expect(target.querySelector('[role=listbox]')).toBeNull();
  });
});
