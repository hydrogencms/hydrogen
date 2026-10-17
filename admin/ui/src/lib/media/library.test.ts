import { beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({ facets: vi.fn() }));
vi.mock('./upload', () => ({ mediaApi: { facets: h.facets, list: vi.fn() } }));

import { FACETS_TTL, Library, facetCache } from './library.svelte';

const result = { folders: [{ value: '/a/b', count: 2 }], tags: [{ value: 'hero', count: 1 }] };
const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  facetCache.invalidate();
  h.facets.mockReset();
  h.facets.mockResolvedValue(result);
  vi.useRealTimers();
});

describe('facets', () => {
  it('reads only the selected type, in one call', async () => {
    const lib = new Library(['image', 'file']);
    lib.loadFacets();
    await settle();
    expect(h.facets).toHaveBeenCalledTimes(1);
    expect(h.facets.mock.calls[0]![0]).toBe('image');
    expect(lib.folderPaths).toEqual(['/a', '/a/b']);
    expect(lib.tags).toEqual(['hero']);
  });

  it('is cached across picker opens and across libraries', async () => {
    const a = new Library(['image']);
    a.loadFacets();
    await settle();
    a.loadFacets(); // the picker opens again
    const b = new Library(['image']); // another asset field
    b.loadFacets();
    await settle();
    expect(h.facets).toHaveBeenCalledTimes(1);
    expect(b.tags).toEqual(['hero']);
  });

  it('reads again only after the folders or tags changed, or the cache aged', async () => {
    const lib = new Library(['image']);
    lib.loadFacets();
    await settle();
    lib.reload(); // a plain list refresh
    lib.loadFacets();
    expect(h.facets).toHaveBeenCalledTimes(1);

    lib.invalidateFacets();
    lib.loadFacets();
    await settle();
    expect(h.facets).toHaveBeenCalledTimes(2);

    vi.useFakeTimers();
    vi.setSystemTime(Date.now() + FACETS_TTL + 1000);
    lib.loadFacets();
    expect(h.facets).toHaveBeenCalledTimes(3);
  });

  it('survives a server without the endpoint', async () => {
    h.facets.mockRejectedValue(new Error('404'));
    const lib = new Library(['image']);
    lib.loadFacets();
    await settle();
    expect(lib.tags).toEqual([]);
    expect(lib.folderPaths).toEqual([]);
  });
});
