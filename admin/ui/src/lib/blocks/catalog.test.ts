import { beforeEach, describe, expect, it, vi } from 'vitest';

const get = vi.fn();
vi.mock('../api/client', () => ({ api: { get: (...a: unknown[]) => get(...a) } }));

import { blocksCatalog } from './catalog.svelte';

const payload = {
  blocks: [
    { name: 'hero', label: 'Hero', icon: 'layout', props: [{ name: 'heading', shape: { base: 'string' } }] },
    { name: 'cards', label: 'Cards', props: [], slots: [{ name: 'items', label: 'Cards', allow: ['card'], min: 1, max: 12 }] },
    { name: 'shared', label: 'Shared block', props: [{ name: 'item', shape: { base: 'ref', to: ['sharedBlock'] } }] },
  ],
  tokens: [{ name: 'tone', values: ['light', 'dark'] }],
};

describe('blocksCatalog', () => {
  beforeEach(() => {
    blocksCatalog.reset();
    get.mockReset().mockResolvedValue(payload);
  });

  it('loads once and serves block types, tokens and labels', async () => {
    await Promise.all([blocksCatalog.load(), blocksCatalog.load()]);
    await blocksCatalog.load();
    expect(get).toHaveBeenCalledTimes(1);
    expect(blocksCatalog.label('hero')).toBe('Hero');
    expect(blocksCatalog.label('mystery')).toBe('mystery');
    expect(blocksCatalog.icon('hero')).toBe('layout');
    expect(blocksCatalog.icon('shared')).toBe('link');
    expect(blocksCatalog.icon('cards')).toBe('square');
    expect(blocksCatalog.token('tone')).toEqual(['light', 'dark']);
    expect(blocksCatalog.token('nope')).toEqual([]);
  });

  it('lists the allowed types in the rule order, skipping unknown names', async () => {
    await blocksCatalog.load();
    expect(blocksCatalog.allowed({ allow: ['cards', 'ghost', 'hero'] }).map((b) => b.name)).toEqual(['cards', 'hero']);
  });

  it('knows the blockset types a shared block can point at', async () => {
    await blocksCatalog.load();
    expect(blocksCatalog.sharedTo).toEqual(['sharedBlock']);
  });

  it('builds a schema with slots for the helpers', async () => {
    await blocksCatalog.load();
    const s = blocksCatalog.schema({ allow: ['cards'], max: 3 });
    expect(s.root.max).toBe(3);
    expect(s.block('cards')!.slots![0]).toMatchObject({ min: 1, max: 12 });
  });

  it('retries after a failure', async () => {
    get.mockRejectedValueOnce(new Error('down'));
    await expect(blocksCatalog.load()).rejects.toThrow('down');
    await blocksCatalog.load();
    expect(blocksCatalog.loaded).toBe(true);
  });
});
