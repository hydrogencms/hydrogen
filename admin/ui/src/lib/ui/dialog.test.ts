import { flushSync, mount, createRawSnippet } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import Dialog from './Dialog.svelte';
import { mounted } from '../../test-setup';

const body = createRawSnippet(() => ({
  render: () => '<div><button id="b1">One</button><button id="b2">Two</button></div>',
}));

describe('Dialog', () => {
  it('moves focus in, traps Tab, closes on Escape and returns focus', async () => {
    const opener = document.body.appendChild(document.createElement('button'));
    opener.textContent = 'open';
    opener.focus();
    const target = document.body.appendChild(document.createElement('div'));
    const onclose = vi.fn();
    const props = ({ open: true, title: 'Confirm', onclose, children: body });
    mounted.push(mount(Dialog, { target, props }));
    flushSync();

    const box = document.querySelector<HTMLElement>('[role=dialog]')!;
    expect(box.getAttribute('aria-modal')).toBe('true');
    expect(box.getAttribute('aria-labelledby')).toBeTruthy();
    expect(document.activeElement?.id).toBe('b1');

    const tab = (shift = false) => {
      const e = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: shift, bubbles: true, cancelable: true });
      document.activeElement!.dispatchEvent(e);
      return e.defaultPrevented;
    };
    document.getElementById('b2')!.focus();
    expect(tab()).toBe(true);
    expect(document.activeElement?.classList.contains('h-dialog__x') || document.activeElement?.id === 'b1').toBe(true);
    (document.querySelector('.h-dialog__x') as HTMLElement).focus();
    expect(tab(true)).toBe(true);
    expect(document.activeElement?.id).toBe('b2');

    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    flushSync();
    expect(onclose).toHaveBeenCalled();
    expect(document.querySelector('[role=dialog]')).toBeNull();
    expect(document.activeElement).toBe(opener);
  });
});
