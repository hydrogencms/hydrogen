import { flushSync, mount } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import Select from './Select.svelte';
import { mounted } from '../../test-setup';

const options = [
  { value: 'a', label: 'Apple' },
  { value: 'b', label: 'Banana', disabled: true },
  { value: 'c', label: 'Cherry' },
  { value: 'd', label: 'Cranberry' },
];

function setup(props: Record<string, unknown> = {}) {
  const target = document.body.appendChild(document.createElement('div'));
  mounted.push(mount(Select, { target, props: { options, ...props } }));
  flushSync();
  const trigger = target.querySelector<HTMLButtonElement>('button')!;
  const key = (k: string) => {
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    flushSync();
  };
  const opts = () => [...document.querySelectorAll<HTMLElement>('[role=option]')];
  const active = () => document.getElementById(trigger.getAttribute('aria-activedescendant') ?? '')?.textContent?.trim();
  return { target, trigger, key, opts, active };
}

describe('Select', () => {
  it('is a select-only combobox that shows the label or the muted placeholder', () => {
    const s = setup({ value: 'c' });
    expect(s.trigger.getAttribute('role')).toBe('combobox');
    expect(s.trigger.getAttribute('aria-haspopup')).toBe('listbox');
    expect(s.trigger.getAttribute('aria-expanded')).toBe('false');
    expect(s.trigger.textContent).toContain('Cherry');
    const p = setup({ value: '', placeholder: 'Pick one' });
    expect(p.trigger.textContent).toContain('Pick one');
    expect(p.trigger.classList.contains('h-select--empty')).toBe(true);
  });

  it('opens with the keyboard, moves over enabled options only and selects with Enter', () => {
    const onchange = vi.fn();
    const s = setup({ value: 'a', onchange });
    s.key('ArrowDown');
    expect(s.trigger.getAttribute('aria-expanded')).toBe('true');
    expect(s.opts().length).toBe(4);
    expect(s.opts()[0].getAttribute('aria-selected')).toBe('true');
    expect(s.opts()[1].getAttribute('aria-disabled')).toBe('true');
    expect(s.active()).toBe('Apple');
    s.key('ArrowDown');
    expect(s.active()).toBe('Cherry');
    s.key('End');
    expect(s.active()).toBe('Cranberry');
    s.key('Home');
    expect(s.active()).toBe('Apple');
    s.key('ArrowUp');
    expect(s.active()).toBe('Apple');
    s.key('End');
    s.key('Enter');
    expect(onchange).toHaveBeenCalledWith('d');
    expect(s.trigger.textContent).toContain('Cranberry');
    expect(s.opts().length).toBe(0);
    expect(document.activeElement).toBe(s.trigger);
  });

  it('Space selects, Escape and Tab close without changing', () => {
    const onchange = vi.fn();
    const s = setup({ value: 'a', onchange });
    s.key(' ');
    s.key('ArrowDown');
    s.key('Escape');
    expect(s.opts().length).toBe(0);
    expect(s.trigger.textContent).toContain('Apple');
    s.key('Enter');
    s.key('ArrowDown');
    s.key('Tab');
    expect(s.opts().length).toBe(0);
    expect(onchange).not.toHaveBeenCalled();
    s.key('Enter');
    s.key('ArrowDown');
    s.key(' ');
    expect(onchange).toHaveBeenCalledWith('c');
  });

  it('type-ahead jumps to the next label starting with the letters', () => {
    const s = setup({ value: 'a' });
    s.key('ArrowDown');
    s.key('c');
    expect(s.active()).toBe('Cherry');
    s.key('r');
    expect(s.active()).toBe('Cranberry');
  });

  it('the placeholder row selects the empty value', () => {
    const onchange = vi.fn();
    const s = setup({ value: 'c', placeholder: 'None', onchange });
    s.trigger.click();
    flushSync();
    expect(s.opts().length).toBe(5);
    s.opts()[0].click();
    flushSync();
    expect(onchange).toHaveBeenCalledWith('');
    expect(s.trigger.textContent).toContain('None');
  });

  it('click selects, disabled options ignore clicks, outside pointerdown closes', () => {
    const onchange = vi.fn();
    const s = setup({ value: 'a', onchange });
    s.trigger.click();
    flushSync();
    s.opts()[1].click();
    flushSync();
    expect(onchange).not.toHaveBeenCalled();
    expect(s.opts().length).toBe(4);
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    flushSync();
    expect(s.opts().length).toBe(0);
    s.trigger.click();
    flushSync();
    s.opts()[2].click();
    flushSync();
    expect(onchange).toHaveBeenCalledWith('c');
  });

  it('a disabled select does not open; name renders a hidden input', () => {
    const s = setup({ disabled: true, name: 'fruit', value: 'a' });
    s.key('Enter');
    expect(s.trigger.getAttribute('aria-expanded')).toBe('false');
    expect(s.target.querySelector<HTMLInputElement>('input[type=hidden][name=fruit]')?.value).toBe('a');
  });
});
