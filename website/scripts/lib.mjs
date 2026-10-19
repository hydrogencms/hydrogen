import { readdirSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

export const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const outDir = path.join(root, 'src/content/docs');

// slugs of the generated pages
export function allPages() {
  if (!existsSync(outDir)) return [];
  const out = [];
  const walk = (d, pre) => {
    for (const e of readdirSync(d, { withFileTypes: true })) {
      if (e.isDirectory()) walk(path.join(d, e.name), pre + e.name + '/');
      else if (e.name.endsWith('.md')) out.push(pre + e.name.slice(0, -3));
    }
  };
  walk(outDir, '');
  return out;
}
