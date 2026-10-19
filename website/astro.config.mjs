import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import { existsSync } from 'node:fs';
import { groups, otherLabel } from './sidebar.mjs';
import { allPages } from './scripts/lib.mjs';

const site = process.env.DOCS_SITE || 'https://hydrogencms.github.io';
const base = process.env.DOCS_BASE ?? '/hydrogen';

const present = new Set(allPages());
const sidebar = [...groups, { label: otherLabel, pages: [...present].filter((s) => !groups.some((g) => g.pages.includes(s))).sort() }]
  .map((g) => ({ label: g.label, items: g.pages.filter((s) => present.has(s)).map((slug) => ({ slug })) }))
  .filter((g) => g.items.length);

export default defineConfig({
  site,
  base: base || undefined,
  integrations: [
    starlight({
      title: 'Hydrogen',
      description: 'A headless, composable CMS in Go.',
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/hydrogencms/hydrogen' }],
      editLink: { baseUrl: 'https://github.com/hydrogencms/hydrogen/edit/main/docs/' },
      customCss: ['./src/styles/theme.css'],
      sidebar,
      plugins: [starlightLinksValidator({ errorOnInvalidHashes: true, errorOnRelativeLinks: false, errorOnLocalLinks: false })],
    }),
  ],
});
