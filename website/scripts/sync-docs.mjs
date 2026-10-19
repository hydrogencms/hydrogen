// Copies docs/*.md into src/content/docs for Starlight. See sidebar.mjs for the page order.
import { readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync, copyFileSync, existsSync, statSync } from 'node:fs';
import path from 'node:path';
import { root, outDir } from './lib.mjs';
import { groups } from '../sidebar.mjs';

const docsDir = path.resolve(root, '../docs');
const repoUrl = 'https://github.com/hydrogencms/hydrogen/blob/main/';
const base = (process.env.DOCS_BASE ?? '/hydrogen').replace(/\/$/, '');
const strict = process.env.DOCS_STRICT === '1' || process.env.CI === 'true';

const errors = [];
const warnings = [];
const slugOf = (file) => (file === 'README.md' ? 'index' : file.replace(/\.md$/, ''));

rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });

const files = readdirSync(docsDir).filter((f) => f.endsWith('.md'));
const slugs = new Set(files.map(slugOf));

function description(lines) {
  const para = [];
  for (const l of lines) {
    if (!l.trim()) { if (para.length) break; continue; }
    if (/^(#|```|\||[-*] |\d+\. |>|<)/.test(l)) { if (para.length) break; else continue; }
    para.push(l.trim());
  }
  let t = para.join(' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[`*_]/g, '')
    .trim();
  if (t.length > 200) t = t.slice(0, 197).replace(/\s+\S*$/, '') + '...';
  return t;
}

function rewriteLinks(text, file) {
  let inFence = false;
  return text.split('\n').map((line) => {
    if (/^\s*(```|~~~)/.test(line)) { inFence = !inFence; return line; }
    if (inFence) return line;
    // split out inline code so links inside it stay untouched
    return line.split(/(`[^`]*`)/).map((part, i) => i % 2 ? part : part.replace(/(\]\()([^)\s]+)((?:\s+"[^"]*")?\))/g, (m, a, href, c) => {
      if (/^([a-z][a-z0-9+.-]*:|#|\/\/)/i.test(href)) return m;
      const [p, hash = ''] = href.split(/#(.*)/s, 2).concat('').slice(0, 2);
      const h = hash ? '#' + hash : '';
      const target = path.posix.normalize(path.posix.join('/docs', p));
      if (p.endsWith('.md') && !p.includes('/')) {
        const s = slugOf(p);
        if (!slugs.has(s)) {
          const msg = `${file}: link to missing docs page ${p}`;
          (strict ? errors : warnings).push(msg);
        }
        return `${a}${base}${s === 'index' ? '/' : `/${s}/`}${h}${c}`;
      }
      if (p.startsWith('../')) {
        const repoPath = path.posix.normalize(path.posix.join('docs', p));
        if (repoPath.startsWith('..')) { errors.push(`${file}: link escapes the repository: ${href}`); return m; }
        if (repoPath.startsWith('planning/')) { errors.push(`${file}: link into planning/: ${href}`); return m; }
        const abs = path.resolve(docsDir, p);
        const isDir = existsSync(abs) && statSync(abs).isDirectory();
        return `${a}${repoUrl.replace('/blob/', isDir ? '/tree/' : '/blob/')}${repoPath}${h}${c}`;
      }
      return m;
    })).join('');
  }).join('\n');
}

for (const file of files) {
  const src = readFileSync(path.join(docsDir, file), 'utf8').replace(/\r\n/g, '\n');
  const lines = src.split('\n');
  const hi = lines.findIndex((l) => /^# /.test(l));
  if (hi < 0) { errors.push(`${file}: no "# " heading`); continue; }
  const title = lines[hi].replace(/^# /, '').replace(/[`*]/g, '').trim();
  const rest = [...lines.slice(0, hi), ...lines.slice(hi + 1)];
  const desc = description(rest);
  const body = rewriteLinks(rest.join('\n').replace(/^\n+/, ''), file);
  const fm = ['---', `title: ${JSON.stringify(title)}`, ...(desc ? [`description: ${JSON.stringify(desc)}`] : []),
    `editUrl: ${JSON.stringify(`https://github.com/hydrogencms/hydrogen/edit/main/docs/${file}`)}`, '---', ''].join('\n');
  writeFileSync(path.join(outDir, slugOf(file) + '.md'), fm + body);
}

// images and other assets next to the pages (docs/images/…, docs/*.png)
function copyAssets(dir, rel) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const s = path.join(dir, e.name);
    if (e.isDirectory()) { copyAssets(s, path.join(rel, e.name)); continue; }
    if (!/\.(png|jpe?g|gif|svg|webp|avif)$/i.test(e.name)) continue;
    mkdirSync(path.join(outDir, rel), { recursive: true });
    copyFileSync(s, path.join(outDir, rel, e.name));
  }
}
copyAssets(docsDir, '');

const listed = new Set();
for (const g of groups) for (const s of g.pages) {
  listed.add(s);
  if (!slugs.has(s)) (strict ? errors : warnings).push(`sidebar lists "${s}" but docs has no such page`);
}
for (const s of [...slugs].sort()) if (!listed.has(s)) warnings.push(`docs/${s}.md is not in sidebar.mjs; it goes to "Other"`);

for (const w of warnings) console.warn('sync-docs: warning:', w);
for (const e of errors) console.error('sync-docs: error:', e);
if (errors.length) process.exit(1);
console.log(`sync-docs: ${files.length} pages`);
