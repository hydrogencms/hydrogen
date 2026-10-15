import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { collections } from './collections';

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? files(p) : /\.(svelte|ts)$/.test(n) && !n.endsWith('.test.ts') ? [p] : [];
  });
}

describe('collections messages', () => {
  it('defines every collections.* key the screens use', () => {
    const src = join(__dirname, '..', '..');
    const used = new Set<string>();
    for (const f of [...files(join(src, 'routes/collection')), ...files(join(src, 'routes/item')), ...files(join(src, 'routes/singleton')), ...files(join(src, 'lib/fields'))]) {
      for (const m of readFileSync(f, 'utf8').matchAll(/['`](collections\.[A-Za-z0-9_.]+(?:\$\{[^}]+\}[A-Za-z0-9_.]*)?)['`]/g)) used.add(m[1]!);
    }
    const missing = [...used].filter((k) => (k.includes('${') ? false : !(k in collections)));
    expect(missing).toEqual([]);
    for (const k of Object.keys(collections)) expect(k.startsWith('collections.')).toBe(true);
  });
});
