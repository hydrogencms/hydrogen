// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyMarks, readable } from './marks';
import { focusPath, IdleWatcher } from './presence.svelte';
import type { Participant } from './transport';

const P = (o: Partial<Participant>): Participant => ({ client: 'x', principal: 'p', name: 'Alex', color: '#c33', locale: 'en', edit: true, ...o });

describe('IdleWatcher', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('goes idle after the timeout without input and back on the next touch', () => {
    const seen: boolean[] = [];
    const w = new IdleWatcher((i) => seen.push(i), 60_000);
    vi.advanceTimersByTime(59_000);
    w.touch();
    vi.advanceTimersByTime(59_000);
    expect(seen).toEqual([]);
    vi.advanceTimersByTime(2_000);
    expect(seen).toEqual([true]);
    w.touch();
    expect(seen).toEqual([true, false]);
    w.stop();
  });
});

describe('focusPath and marks', () => {
  const html = `<form><div data-field="title"><input id="t"></div>
    <div data-field="body"><div data-block-id="b1"><div data-field="body[0].props.heading"><input id="h"></div></div></div></form>`;

  it('uses the innermost data-field', () => {
    document.body.innerHTML = html;
    expect(focusPath(document.getElementById('h'))).toBe('body[0].props.heading');
    expect(focusPath(document.getElementById('t'))).toBe('title');
    expect(focusPath(document.body)).toBeUndefined();
  });

  it('marks fields and their block card, and clears them again', () => {
    document.body.innerHTML = html;
    const root = document.querySelector('form') as HTMLElement;
    const tpl = (n: string) => `${n} is editing`;
    const many = (n: number) => `${n} editing`;
    applyMarks(root, [P({ focus: 'body[0].props.heading' }), P({ client: 'y', name: 'Sam', focus: 'title' }), P({ client: 'z', name: 'Kim', focus: 'title' })], tpl, many);
    const title = root.querySelector('[data-field="title"]') as HTMLElement;
    const head = root.querySelector('[data-field="body[0].props.heading"]') as HTMLElement;
    expect(head.getAttribute('data-collab-name')).toBe('Alex is editing');
    expect(head.style.getPropertyValue('--h-collab')).toBe('#c33');
    expect(root.querySelector('[data-block-id]')!.classList.contains('h-collab-card')).toBe(true);
    expect(title.getAttribute('data-collab-name')).toBe('2 editing');
    applyMarks(root, [], tpl, many);
    expect(root.querySelectorAll('.h-collab-mark, .h-collab-card')).toHaveLength(0);
    expect(title.hasAttribute('data-collab-name')).toBe(false);
  });
});

describe('colours and descriptions', () => {
  const lum = (hex: string) => {
    if (hex.length === 4) hex = '#' + [...hex.slice(1)].map((c) => c + c).join('');
    const v = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
    return 0.2126 * v[0]! + 0.7152 * v[1]! + 0.0722 * v[2]!;
  };
  const ratio = (a: string, b: string) => (Math.max(lum(a), lum(b)) + 0.05) / (Math.min(lum(a), lum(b)) + 0.05);

  it('every palette colour carries its text at 4.5:1', () => {
    for (const c of ['#d9480f', '#2f9e44', '#1971c2', '#9c36b5', '#c2255c', '#0c8599', '#e67700', '#5f3dc4']) {
      const r = readable(c);
      expect(ratio(r.bg, r.fg), c).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('describes a marked control with real text and removes it again', () => {
    document.body.innerHTML = '<form><div data-field="title"><input id="t" aria-describedby="hint"></div></form>';
    const root = document.querySelector('form') as HTMLElement;
    const input = document.getElementById('t')!;
    applyMarks(root, [P({ focus: 'title' })], (n) => `${n} is editing`, (n) => `${n}`);
    const ids = input.getAttribute('aria-describedby')!.split(' ');
    expect(ids[0]).toBe('hint');
    expect(document.getElementById(ids[1]!)!.textContent).toBe('Alex is editing');
    applyMarks(root, [], () => '', () => '');
    expect(input.getAttribute('aria-describedby')).toBe('hint');
    expect(root.querySelector('[data-collab-desc]')).toBeNull();
  });
});
