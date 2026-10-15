// Gzip budgets: initial JS (entry chunk plus its static imports) and all CSS. Reads the Vite manifest.
import { readFileSync, readdirSync } from 'node:fs';
import { gzipSync } from 'node:zlib';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'dist');
const JS_BUDGET = 120 * 1024;
const CSS_BUDGET = 30 * 1024;

const manifest = JSON.parse(readFileSync(join(dist, 'manifest.json'), 'utf8'));
const gz = (file) => gzipSync(readFileSync(join(dist, file)), { level: 9 }).length;

const seen = new Set();
const initial = [];
const walk = (key) => {
  if (seen.has(key)) return;
  seen.add(key);
  const c = manifest[key];
  initial.push(c.file);
  for (const i of c.imports ?? []) walk(i);
};
for (const [key, c] of Object.entries(manifest)) if (c.isEntry) walk(key);

const js = initial.reduce((n, f) => n + gz(f), 0);
const cssFiles = readdirSync(join(dist, 'assets')).filter((f) => f.endsWith('.css'));
const css = cssFiles.reduce((n, f) => n + gz(join('assets', f)), 0);
const lazy = Object.values(manifest)
  .filter((c) => c.file.endsWith('.js') && !initial.includes(c.file))
  .reduce((n, c) => n + gz(c.file), 0);

const kb = (n) => (n / 1024).toFixed(1) + ' KB';
console.log(`initial JS (gzip): ${kb(js)} / ${kb(JS_BUDGET)}  [${initial.join(', ')}]`);
console.log(`CSS (gzip):        ${kb(css)} / ${kb(CSS_BUDGET)}`);
console.log(`lazy JS (gzip):    ${kb(lazy)}`);
if (js > JS_BUDGET || css > CSS_BUDGET) {
  console.error('size budget exceeded');
  process.exit(1);
}
