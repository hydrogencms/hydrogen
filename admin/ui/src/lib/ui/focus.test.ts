import { describe, expect, it } from 'vitest';
import { portal, within } from './focus';

describe('within', () => {
  it('follows portaled layers back to where they were opened', () => {
    const pop = document.body.appendChild(document.createElement('div'));
    const inner = pop.appendChild(document.createElement('div'));
    const layer = inner.appendChild(document.createElement('div'));
    const opt = layer.appendChild(document.createElement('span'));
    portal(layer);
    expect(layer.parentNode).toBe(document.body);
    expect(pop.contains(opt)).toBe(false);
    expect(within(pop, opt)).toBe(true);
    expect(within(inner, opt)).toBe(true);
    const other = document.body.appendChild(document.createElement('div'));
    expect(within(other, opt)).toBe(false);
    expect(within(undefined, opt)).toBe(false);
  });
});
