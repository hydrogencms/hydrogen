import { describe, expect, it } from 'vitest';
import { match } from './router.svelte';

const mk = (path: string) => {
  const parts = path.split('/').filter(Boolean);
  const splat = parts[parts.length - 1] === '*';
  if (splat) parts.pop();
  return { path, parts, splat, load: async () => ({ default: null as never }) };
};

describe('route matching', () => {
  const routes = [mk('/'), mk('/collections/:type'), mk('/collections/:type/new'), mk('/items/:id'), mk('/settings/*')];
  it('matches static and param routes', () => {
    expect(match(routes, '/')?.route.path).toBe('/');
    expect(match(routes, '/collections/article')?.params).toEqual({ type: 'article' });
    expect(match(routes, '/collections/article/new')?.route.path).toBe('/collections/:type/new');
    expect(match(routes, '/items/a%20b')?.params).toEqual({ id: 'a b' });
  });
  it('captures the rest for splat routes', () => {
    expect(match(routes, '/settings/users/1')?.params).toEqual({ rest: 'users/1' });
    expect(match(routes, '/settings')?.params).toEqual({ rest: '' });
  });
  it('returns null when nothing matches', () => {
    expect(match(routes, '/nope')).toBeNull();
    expect(match(routes, '/collections')).toBeNull();
    expect(match(routes, '/items/%E0%A4%A')).toBeNull();
  });
});
